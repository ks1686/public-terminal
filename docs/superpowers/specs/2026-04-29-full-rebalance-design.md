# Full Rebalance Redesign

**Date:** 2026-04-29  
**Status:** Approved

## Problem

The daily rebalancer fails to reach target allocations in a single run due to two compounding bugs:

1. **`estimate_margin_state` misclassifies unsettled cash as a margin loan.** When `cash_balance` is negative (from yesterday's unsettled T+1/T+2 buys), the code sets `current_margin_loan = -cash_balance`. This causes `effective_buying_power` to be underreported, leaving real buying power on the table.

2. **`cap_buy_orders_to_buying_power` uses a greedy skip.** Buys are sorted largest-delta-first. A high-priority $1,040 GOOG buy appears first, doesn't fit in $144, gets skipped entirely. $56 BTC then fits and gets placed instead. The most important position is never funded.

Together these cause partial rebalances every run, with non-index allocations (GLDM, crypto) and newly re-included stocks chronically underfunded.

## Goals

- Every rebalance run produces a portfolio as close to target as available capital allows.
- Non-stock allocations (BTC, ETH, GLDM) are always settled before stock index positions.
- Within the stock tier, highest market-cap positions are funded first.
- Leftover buying power after fully funding a tier is used for partial positions of the next tier down (minimum $5 API limit).
- If post-sell buying power is insufficient to fully fund the priority tiers, supplemental sells are generated from the lowest-priority stock positions before any buys are placed.
- Margin calculation is fully API-driven — no inference from `cash_balance`.

## Non-Goals

- Changing allocation percentages, index selection, or exclusion logic.
- Multi-day settlement tracking or prediction.
- Intraday re-triggering if supplemental sells are large.

---

## Design

### 1. Fix `estimate_margin_state`

**Remove** the `if cash_balance < 0: current_margin_loan = -cash_balance` branch entirely. Replace with API-native values:

```
margin_capacity     = max(0, buying_power - cash_only_buying_power)
allowed_margin_loan = margin_usage_pct × margin_capacity
effective_bp        = cash_only_buying_power + allowed_margin_loan
portfolio_nav       = total_equity + cash_balance        # unchanged
investment_base     = portfolio_nav + allowed_margin_loan
```

`cash_balance` only influences `portfolio_nav` (so target sizes stay correct relative to actual account value). It plays no role in buying power or margin calculations.

For the log line, a display-only margin estimate is retained:
```
displayed_margin_loan = max(0, -cash_balance)  if buying_power > cash_only_buying_power else 0
```
This is never used in any calculation.

**Why this is correct:**  
`cash_only_buying_power` is what the broker reports as spendable settled cash. `buying_power - cash_only_buying_power` is what the broker will lend. Everything about settlement, unsettled debits, and account state is already baked into these two broker-reported values. We don't need to re-derive them.

---

### 2. Buy priority ordering

Buys are placed in this fixed tier order:

| Priority | Asset | Notes |
|----------|-------|-------|
| 1 | BTC | crypto |
| 2 | ETH | crypto |
| 3 | GLDM | gold ETF |
| 4 | Stock index positions | sorted by **target value descending** within this tier |

Cash allocation is not a buy order — it is already excluded from `investment_base` by the existing allocation math.

Sells are generated before buys and are unaffected by this ordering (sell logic is unchanged — all over-weight positions are trimmed regardless of tier).

---

### 3. Replace `cap_buy_orders_to_buying_power` with `fill_buy_orders`

New function signature:
```python
def fill_buy_orders(
    buys: list[tuple[str, InstrumentType, OrderSide, Decimal]],  # pre-sorted by priority
    available_bp: Decimal,
) -> list[tuple[str, InstrumentType, OrderSide, Decimal]]:
```

Algorithm:
```
remaining = available_bp
result = []
for order in buys:                          # already in priority order
    if remaining >= order.amount:
        result.append(order)                # full fill
        remaining -= order.amount
    elif remaining >= MIN_ORDER_DOLLARS:    # $5
        result.append(order with amount=remaining)   # partial fill
        break                               # remaining is now < $5, stop
    else:
        break                               # < $5 left, nothing more can be placed
return result
```

Key differences from current `cap_buy_orders_to_buying_power`:
- Orders must be pre-sorted in priority tier order before this function is called.
- Partial fills are allowed for the first order that doesn't fully fit.
- Iteration stops after the partial fill (no greedy scan for smaller orders that might fit).

The caller is responsible for sorting:
1. BTC buy (if any)
2. ETH buy (if any)
3. GLDM buy (if any)
4. Stock buys, sorted by target value descending

---

### 4. Supplemental sells

After wave-1 sells execute and buying power is re-fetched, check whether the priority tiers can be fully funded.

```
priority_buy_need = sum of all buy amounts (full priority order)
shortfall = max(0, priority_buy_need - post_sell_effective_bp)
```

If `shortfall > 0`:

1. Build candidate list: current equity positions that are **not** already in the sell list, **not** in the buy list, and **not** BTC/ETH/GLDM — ordered by target value **ascending** (sacrifice least-important held stock positions first).
2. Walk the candidate list: for each candidate, the sell amount is `min(current_position_value, remaining_shortfall)`. Accumulate until `shortfall` is covered or candidates are exhausted. This means positions are partially sold (not necessarily liquidated) — only enough is taken to cover the gap.
3. Execute the supplemental sells and wait for them to clear (same `wait_for_orders_to_clear` logic).
4. Re-fetch buying power, then proceed to the buy phase.

**Limits:**  
- Never generate a supplemental sell that would take a position below $0 (i.e. don't sell more than the current position value).  
- If the candidate list is exhausted before covering the shortfall, proceed anyway — `fill_buy_orders` will handle partial fills gracefully.
- Supplemental sells never touch BTC, ETH, or GLDM positions.

---

## Execution flow (updated)

```
1. Load config, compute targets and investment_base (unchanged)
2. Snapshot portfolio (unchanged)
3. estimate_margin_state — NEW formula
4. Compute all deltas → wave-1 sells + all buy deltas (unchanged logic)
5. Execute wave-1 sells, wait to clear (unchanged)
6. Re-fetch buying power
7. Compute shortfall; if > 0, execute supplemental sells, wait to clear, re-fetch BP
8. Assemble buy list in priority tier order
9. fill_buy_orders(buys, effective_bp) — replaces cap_buy_orders_to_buying_power
10. Execute buys (unchanged)
```

Steps 1–4 and 10 are unchanged. Steps 3, 7, 8, 9 are new or modified.

---

## Affected files

| File | Change |
|------|--------|
| `rebalance.py` | `estimate_margin_state` — replace margin inference logic |
| `rebalance.py` | `cap_buy_orders_to_buying_power` → `fill_buy_orders` (rename + new algorithm) |
| `rebalance.py` | `rebalance()` — insert supplemental sell wave between sell-wait and buy phases; re-sort buys into priority tier order before calling `fill_buy_orders` |
| `test_rebalance.py` | Update / add tests for new functions |

No schema changes, no config changes, no new dependencies.

---

## Test cases

- `fill_buy_orders`: full fill when budget covers all; partial fill on the first order that doesn't fit; stop when < $5 remains.
- `estimate_margin_state`: zero margin when `margin_usage_pct=0`; correct scaling for partial margin; negative `cash_balance` does not affect `effective_bp`.
- Supplemental sells: shortfall triggers correct candidate selection (lowest target value first, excludes BTC/ETH/GLDM and already-sold positions); no supplemental sells when post-sell BP already covers all buys.
- Priority ordering: BTC/ETH/GLDM buys always appear before stock buys in the assembled list.
