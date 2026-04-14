#!/usr/bin/env python3
"""
Portfolio daily rebalancer.

Target allocation
  65%  Top-250 S&P 500 stocks, market-cap weighted within that slice
  20%  Bitcoin (BTC)
  10%  Gold via GLDM ETF
   5%  Cash (left uninvested as buying power)

Run manually:    uv run rebalance.py
Run via systemd: public-terminal-rebalance.timer fires Mon–Fri at 12:00 ET
"""

from __future__ import annotations

import io
import json
import logging
import sys
import time
import urllib.request
import uuid
from concurrent.futures import ThreadPoolExecutor, as_completed
from datetime import datetime, timedelta
from decimal import Decimal
from pathlib import Path

import pandas as pd
import yfinance as yf

from client import get_client
from public_api_sdk import (
    InstrumentType,
    OrderExpirationRequest,
    OrderInstrument,
    OrderRequest,
    OrderSide,
    OrderStatus,
    OrderType,
    TimeInForce,
)

# ---------------------------------------------------------------------------
# Allocation config
# ---------------------------------------------------------------------------

ALLOC_STOCKS = Decimal("0.65")   # top-250 S&P 500, market-cap weighted
ALLOC_BTC    = Decimal("0.15")   # Bitcoin
ALLOC_ETH    = Decimal("0.05")   # Ethereum
ALLOC_GOLD   = Decimal("0.10")   # GLDM ETF (gold)
ALLOC_SGOV   = Decimal("0.05")   # SGOV ETF (0–3 month T-bills, short-term yield)

assert ALLOC_STOCKS + ALLOC_BTC + ALLOC_ETH + ALLOC_GOLD + ALLOC_SGOV == Decimal("1.00")

SP500_TOP_N     = 250               # take the top N constituents by market cap
GOLD_SYMBOL     = "GLDM"
SGOV_SYMBOL     = "SGOV"
BTC_SYMBOL      = "BTC"
ETH_SYMBOL      = "ETH"
NON_STOCK_ETFS  = {GOLD_SYMBOL, SGOV_SYMBOL}  # equity symbols excluded from the S&P 500 slice
BROKER_TO_YF_SYMBOLS = {
    "BF.B": "BF-B",
    "BRK.B": "BRK-B",
}
YF_TO_BROKER_SYMBOLS = {yf_symbol: broker_symbol for broker_symbol, yf_symbol in BROKER_TO_YF_SYMBOLS.items()}

# ---------------------------------------------------------------------------
# Operational config
# ---------------------------------------------------------------------------

PROJECT_ROOT = Path(__file__).parent
CACHE_DIR    = PROJECT_ROOT / "cache"
MARKET_CAP_CACHE_FILE  = CACHE_DIR / "market_caps.json"
LOG_FILE               = CACHE_DIR / "rebalance.log"
SKIP_FILE              = CACHE_DIR / "skip_next_rebalance"

MARKET_CAP_CACHE_MAX_AGE_HOURS = 20   # same-day cache; refresh each noon run
MARKET_CAP_FETCH_WORKERS       = 20   # parallel threads for fast_info calls

MIN_ORDER_DOLLARS         = Decimal("1.00")   # ignore equity orders smaller than $1
MIN_CRYPTO_ORDER_DOLLARS  = Decimal("1.00")   # minimum notional for any crypto order
REBALANCE_THRESHOLD_PCT   = Decimal("0.005")  # only act if drift > 0.5% of target
BUYING_POWER_BUFFER       = Decimal("1.00")   # leave a small cushion to avoid broker-side shortfall errors
SELL_WAIT_TIMEOUT_SECONDS = 300               # wait up to 5 minutes for sell orders to clear
ORDER_STATUS_POLL_SECONDS = 2.0

# ---------------------------------------------------------------------------
# Logging
# ---------------------------------------------------------------------------

CACHE_DIR.mkdir(exist_ok=True)

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s  %(levelname)-8s  %(message)s",
    datefmt="%Y-%m-%d %H:%M:%S",
    handlers=[
        logging.StreamHandler(sys.stdout),
        logging.FileHandler(LOG_FILE),
    ],
)
log = logging.getLogger(__name__)


# ---------------------------------------------------------------------------
# S&P 500 constituents
# ---------------------------------------------------------------------------

def fetch_sp500_tickers() -> list[str]:
    """Scrape the current S&P 500 constituent list from Wikipedia."""
    log.info("Fetching S&P 500 constituent list from Wikipedia…")
    req = urllib.request.Request(
        "https://en.wikipedia.org/wiki/List_of_S%26P_500_companies",
        headers={"User-Agent": "Mozilla/5.0 (compatible; public-terminal/1.0)"},
    )
    with urllib.request.urlopen(req) as resp:
        html = resp.read().decode("utf-8")
    tables = pd.read_html(io.StringIO(html), attrs={"id": "constituents"})
    tickers = tables[0]["Symbol"].tolist()
    log.info("  → %d constituents", len(tickers))
    return tickers


# ---------------------------------------------------------------------------
# Market caps (daily cache, parallel fetch)
# ---------------------------------------------------------------------------

def _load_market_cap_cache() -> dict[str, float] | None:
    if not MARKET_CAP_CACHE_FILE.exists():
        return None
    try:
        data = json.loads(MARKET_CAP_CACHE_FILE.read_text())
        age = datetime.now() - datetime.fromisoformat(data["updated_at"])
        if age > timedelta(hours=MARKET_CAP_CACHE_MAX_AGE_HOURS):
            log.info("Market cap cache is %.1f hours old — will refresh.", age.total_seconds() / 3600)
            return None
        log.info("Using cached market caps (%d entries, %.0f min old).",
                 len(data["caps"]), age.total_seconds() / 60)
        # Older caches stored yfinance-style share-class tickers (e.g. BRK-B).
        return {
            YF_TO_BROKER_SYMBOLS.get(symbol, symbol): cap
            for symbol, cap in data["caps"].items()
        }
    except Exception as exc:
        log.warning("Could not read market cap cache: %s", exc)
        return None


def _save_market_cap_cache(caps: dict[str, float]) -> None:
    MARKET_CAP_CACHE_FILE.write_text(
        json.dumps({"updated_at": datetime.now().isoformat(), "caps": caps}, indent=2)
    )
    log.info("Market cap cache saved (%d entries).", len(caps))


def _fetch_one_market_cap(ticker: str) -> tuple[str, float | None]:
    try:
        yf_ticker = BROKER_TO_YF_SYMBOLS.get(ticker, ticker)
        mc = yf.Ticker(yf_ticker).fast_info.market_cap
        if mc and float(mc) > 0:
            return ticker, float(mc)
    except Exception:
        pass
    return ticker, None


def fetch_market_caps(tickers: list[str]) -> dict[str, float]:
    """
    Return {ticker: market_cap} for the given tickers.
    Same-day results are served from a local cache; otherwise fetched in parallel
    using fast_info (~30–60 s for 500 stocks with 20 workers).
    """
    cached = _load_market_cap_cache()
    if cached is not None:
        return cached

    log.info("Fetching market caps for %d tickers (%d workers)…",
             len(tickers), MARKET_CAP_FETCH_WORKERS)
    caps: dict[str, float] = {}
    done = 0
    with ThreadPoolExecutor(max_workers=MARKET_CAP_FETCH_WORKERS) as pool:
        futures = {pool.submit(_fetch_one_market_cap, t): t for t in tickers}
        for future in as_completed(futures):
            ticker, mc = future.result()
            if mc:
                caps[ticker] = mc
            done += 1
            if done % 100 == 0:
                log.info("  … %d / %d done", done, len(tickers))

    log.info("  → market caps for %d / %d tickers", len(caps), len(tickers))
    _save_market_cap_cache(caps)
    return caps


# ---------------------------------------------------------------------------
# Top-N selection and weight computation
# ---------------------------------------------------------------------------

def top_n_by_market_cap(
    tickers: list[str],
    market_caps: dict[str, float],
    n: int,
) -> list[str]:
    """Return the top-N S&P 500 tickers ranked by market cap."""
    ranked = sorted(
        [t for t in tickers if t in market_caps],
        key=lambda t: market_caps[t],
        reverse=True,
    )
    top = ranked[:n]
    log.info("Top %d stocks: largest=%s ($%.2fT), smallest=%s ($%.2fB)",
             n,
             top[0], market_caps[top[0]] / 1e12,
             top[-1], market_caps[top[-1]] / 1e9)
    return top


def compute_stock_weights(
    tickers: list[str],
    market_caps: dict[str, float],
) -> dict[str, Decimal]:
    """
    Return within-slice market-cap weights that sum to 1.0.
    These are multiplied by ALLOC_STOCKS to get each stock's share of the total portfolio.
    """
    caps = {t: market_caps[t] for t in tickers if market_caps.get(t, 0) > 0}
    total = sum(caps.values())
    if total == 0:
        raise RuntimeError("All market caps are zero — data problem")
    weights = {t: Decimal(str(cap / total)) for t, cap in caps.items()}
    log.info("Stock weights computed for %d positions.", len(weights))
    return weights


# ---------------------------------------------------------------------------
# Portfolio snapshot
# ---------------------------------------------------------------------------

def get_portfolio_snapshot(client) -> tuple[Decimal, Decimal, dict[str, Decimal], dict[str, Decimal]]:
    """
    Fetch current portfolio from Public.com.

    Returns:
        total_equity   — sum of all asset values (cash + investments)
        buying_power   — currently available buying power
        equity_pos     — {symbol: current_value} for EQUITY positions
        crypto_pos     — {symbol: current_value} for CRYPTO positions
    """
    portfolio = client.get_portfolio()
    total_equity = sum(e.value for e in portfolio.equity)
    buying_power = getattr(portfolio.buying_power, "buying_power", Decimal("0"))

    equity_pos: dict[str, Decimal] = {}
    crypto_pos: dict[str, Decimal] = {}
    for pos in portfolio.positions:
        if pos.current_value is None:
            continue
        if pos.instrument.type == InstrumentType.EQUITY:
            equity_pos[pos.instrument.symbol] = pos.current_value
        elif pos.instrument.type == InstrumentType.CRYPTO:
            crypto_pos[pos.instrument.symbol] = pos.current_value

    return total_equity, buying_power, equity_pos, crypto_pos


# ---------------------------------------------------------------------------
# Order helpers
# ---------------------------------------------------------------------------

def _make_order(
    symbol: str,
    instrument_type: InstrumentType,
    side: OrderSide,
    dollar_amount: Decimal,
    crypto_price: Decimal | None = None,
) -> OrderRequest:
    """
    Build a market order request.

    EQUITY orders use `amount` (dollar notional).
    CRYPTO orders require `quantity` (coin units) — the API does not support
    dollar-notional amounts for crypto. `crypto_price` must be provided for CRYPTO.
    """
    base = dict(
        order_id=str(uuid.uuid4()),
        instrument=OrderInstrument(symbol=symbol, type=instrument_type),
        order_side=side,
        order_type=OrderType.MARKET,
        expiration=OrderExpirationRequest(time_in_force=TimeInForce.DAY),
    )
    if instrument_type == InstrumentType.CRYPTO:
        if not crypto_price or crypto_price <= 0:
            raise ValueError(f"crypto_price required for CRYPTO order ({symbol})")
        if dollar_amount < MIN_CRYPTO_ORDER_DOLLARS:
            raise ValueError(
                f"CRYPTO order below minimum (${dollar_amount} < ${MIN_CRYPTO_ORDER_DOLLARS})"
            )
        quantity = (dollar_amount / crypto_price).quantize(Decimal("0.00001"))
        if quantity <= 0:
            raise ValueError(
                f"CRYPTO order quantity rounds to zero ({symbol}: ${dollar_amount} at ${crypto_price}/coin)"
            )
        return OrderRequest(**base, quantity=quantity)
    amount = dollar_amount.quantize(Decimal("0.01"))
    if amount <= 0:
        raise ValueError(f"Equity order amount rounds to zero ({symbol}: ${dollar_amount})")
    return OrderRequest(**base, amount=amount)


def fetch_crypto_price(client, symbol: str, yf_ticker: str) -> Decimal:
    """
    Fetch the current mid-price for a crypto symbol via the Public.com quotes endpoint.
    Falls back to yfinance if the API call fails.
    """
    try:
        quotes = client.get_quotes(
            [OrderInstrument(symbol=symbol, type=InstrumentType.CRYPTO)]
        )
        if quotes and quotes[0].last:
            return Decimal(str(quotes[0].last))
        if quotes and quotes[0].bid and quotes[0].ask:
            return (Decimal(str(quotes[0].bid)) + Decimal(str(quotes[0].ask))) / 2
    except Exception as exc:
        log.warning("Could not fetch %s price from Public API: %s — falling back to yfinance", symbol, exc)

    data = yf.download(yf_ticker, period="1d", auto_adjust=True, progress=False)
    if not data.empty:
        return Decimal(str(float(data["Close"].iloc[-1])))

    raise RuntimeError(f"Unable to fetch {symbol} price from any source")


_ACTIVE_ORDER_STATUSES = {
    OrderStatus.NEW,
    OrderStatus.PARTIALLY_FILLED,
    OrderStatus.PENDING_REPLACE,
    OrderStatus.PENDING_CANCEL,
}


def cancel_open_orders(client, orders: list) -> None:
    """Cancel any open/pending orders from a pre-fetched orders list."""
    open_orders = [o for o in orders if o.status in _ACTIVE_ORDER_STATUSES]
    if not open_orders:
        log.info("No open orders to cancel.")
        return
    log.info("Cancelling %d open order(s) before rebalancing…", len(open_orders))
    for order in open_orders:
        try:
            client.cancel_order(order.order_id)
            log.info("  ✓ Cancelled %s %s (ID: %s)", order.side.value, order.instrument.symbol, order.order_id[:8])
        except Exception as exc:
            log.warning("  ✗ Could not cancel %s (ID: %s): %s", order.instrument.symbol, order.order_id[:8], exc)
        time.sleep(0.05)


def place_orders(
    client,
    orders: list[tuple[str, InstrumentType, OrderSide, Decimal]],
    crypto_prices: dict[str, Decimal] | None = None,
) -> list[str]:
    """Place a list of (symbol, instrument_type, side, dollar_amount) market orders."""
    success = fail = 0
    submitted_order_ids: list[str] = []
    for symbol, inst_type, side, dollar_amount in orders:
        try:
            price = (crypto_prices or {}).get(symbol)
            req = _make_order(symbol, inst_type, side, dollar_amount, price)
            if inst_type == InstrumentType.CRYPTO:
                log.info("  ✓ %-6s %-6s $%9.2f  (%s coins @ $%.2f)",
                         side.value, symbol, dollar_amount, req.quantity, price)
            else:
                log.info("  ✓ %-6s %-6s $%9.2f", side.value, symbol, dollar_amount)
            new_order = client.place_order(req)
            log.info("      order id: %s", new_order.order_id[:8])
            submitted_order_ids.append(new_order.order_id)
            success += 1
        except Exception as exc:
            log.error("  ✗ %-6s %-6s $%9.2f  → %s", side.value, symbol, dollar_amount, exc)
            fail += 1
        time.sleep(0.1)
    log.info("Orders placed: %d  |  failed: %d", success, fail)
    return submitted_order_ids


def wait_for_orders_to_clear(
    client,
    order_ids: list[str],
    *,
    label: str,
    timeout_seconds: int = SELL_WAIT_TIMEOUT_SECONDS,
) -> bool:
    """
    Poll until the submitted orders are no longer in an active state.
    """
    pending = set(order_ids)
    if not pending:
        return True

    deadline = time.monotonic() + timeout_seconds
    while pending and time.monotonic() < deadline:
        portfolio = client.get_portfolio()
        active_order_ids = {
            order.order_id
            for order in portfolio.orders
            if order.status in _ACTIVE_ORDER_STATUSES
        }
        pending &= active_order_ids
        if not pending:
            log.info("All %s orders are no longer active.", label)
            return True
        log.info("Waiting for %d %s order(s) to clear before continuing…", len(pending), label)
        time.sleep(ORDER_STATUS_POLL_SECONDS)

    if pending:
        log.warning("Timed out waiting for %d %s order(s) to clear.", len(pending), label)
        return False
    return True


def cap_buy_orders_to_buying_power(
    orders: list[tuple[str, InstrumentType, OrderSide, Decimal]],
    buying_power: Decimal,
) -> list[tuple[str, InstrumentType, OrderSide, Decimal]]:
    """
    Keep only the prefix of buy orders that fits within currently available buying power.
    """
    available_budget = max(Decimal("0"), buying_power - BUYING_POWER_BUFFER)
    if available_budget <= 0:
        log.warning("No buy orders will be placed: buying power is only $%.2f.", buying_power)
        return []

    selected: list[tuple[str, InstrumentType, OrderSide, Decimal]] = []
    skipped: list[tuple[str, InstrumentType, OrderSide, Decimal]] = []
    remaining = available_budget
    for order in orders:
        amount = order[3]
        if amount <= remaining:
            selected.append(order)
            remaining -= amount
        else:
            skipped.append(order)

    total_selected = sum(order[3] for order in selected)
    total_skipped = sum(order[3] for order in skipped)
    log.info("Buy budget: $%.2f available after buffer, $%.2f selected, $%.2f skipped.",
             available_budget, total_selected, total_skipped)
    if skipped:
        skipped_symbols = ", ".join(symbol for symbol, *_ in skipped[:10])
        suffix = "…" if len(skipped) > 10 else ""
        log.warning("Skipped %d buy order(s) that exceed buying power: %s%s",
                    len(skipped), skipped_symbols, suffix)
    return selected


# ---------------------------------------------------------------------------
# Delta computation helpers
# ---------------------------------------------------------------------------

def compute_delta(
    symbol: str,
    instrument_type: InstrumentType,
    target_value: Decimal,
    current_value: Decimal,
    threshold: Decimal,
) -> tuple[str, InstrumentType, OrderSide, Decimal] | None:
    """
    Return a (symbol, type, side, amount) order tuple if the drift exceeds the threshold,
    or None if within tolerance.
    """
    delta = target_value - current_value
    drift_threshold = max(
        target_value * REBALANCE_THRESHOLD_PCT,
        MIN_ORDER_DOLLARS,
        threshold,
    )
    if delta > drift_threshold:
        return (symbol, instrument_type, OrderSide.BUY, delta.quantize(Decimal("0.01")))
    if delta < -drift_threshold and abs(delta) >= MIN_ORDER_DOLLARS:
        return (symbol, instrument_type, OrderSide.SELL, abs(delta).quantize(Decimal("0.01")))
    return None


def compute_unallocated_buy_delta(
    target_value: Decimal,
    current_value: Decimal,
    threshold: Decimal,
) -> Decimal:
    """
    Return the positive buy delta that is too small to place directly.
    """
    delta = target_value - current_value
    drift_threshold = max(
        target_value * REBALANCE_THRESHOLD_PCT,
        MIN_ORDER_DOLLARS,
        threshold,
    )
    if Decimal("0") < delta <= drift_threshold:
        return delta.quantize(Decimal("0.01"))
    return Decimal("0")


# ---------------------------------------------------------------------------
# Main rebalancing logic
# ---------------------------------------------------------------------------

def rebalance() -> None:
    log.info("=" * 64)
    log.info("PORTFOLIO REBALANCE  —  %s", datetime.now().strftime("%Y-%m-%d %H:%M:%S"))
    log.info("  Allocation: %.0f%% stocks  |  %.0f%% BTC  |  %.0f%% ETH  |  %.0f%% gold  |  %.0f%% SGOV",
             ALLOC_STOCKS * 100, ALLOC_BTC * 100, ALLOC_ETH * 100, ALLOC_GOLD * 100, ALLOC_SGOV * 100)
    log.info("  S&P 500 slice: top %d by market cap", SP500_TOP_N)
    log.info("=" * 64)

    if SKIP_FILE.exists():
        SKIP_FILE.unlink()
        log.info("SKIPPED — skip sentinel was set. Removed sentinel; next run will proceed normally.")
        return

    sp500_tickers = fetch_sp500_tickers()
    market_caps = fetch_market_caps(sp500_tickers)
    top250 = top_n_by_market_cap(sp500_tickers, market_caps, SP500_TOP_N)
    stock_weights = compute_stock_weights(top250, market_caps)

    log.info("Fetching portfolio from Public.com…")
    client = get_client()
    try:
        initial_portfolio = client.get_portfolio()
        cancel_open_orders(client, initial_portfolio.orders or [])

        # Re-fetch after cancellations so snapshot reflects the clean state
        total_equity, buying_power, equity_pos, crypto_pos = get_portfolio_snapshot(client)
        log.info("  Total equity: $%.2f  |  buying power: $%.2f  |  equity positions: %d  |  crypto positions: %d",
                 total_equity, buying_power, len(equity_pos), len(crypto_pos))

        sells: list[tuple[str, InstrumentType, OrderSide, Decimal]] = []
        buys:  list[tuple[str, InstrumentType, OrderSide, Decimal]] = []
        stock_residual_for_sgov = Decimal("0")

        def queue(order):
            if order is None:
                return
            if order[2] == OrderSide.SELL:
                sells.append(order)
            else:
                buys.append(order)

        log.info("--- Computing stock deltas (top-%d S&P 500) ---", SP500_TOP_N)
        all_stock_symbols = set(top250) | {
            s for s in equity_pos if s not in NON_STOCK_ETFS and s not in top250
        }
        for symbol in all_stock_symbols:
            if symbol in NON_STOCK_ETFS:
                continue
            weight = stock_weights.get(symbol, Decimal("0"))
            target = (weight * ALLOC_STOCKS * total_equity).quantize(Decimal("0.01"))
            current = equity_pos.get(symbol, Decimal("0"))
            stock_residual_for_sgov += compute_unallocated_buy_delta(target, current, Decimal("1.00"))
            queue(compute_delta(symbol, InstrumentType.EQUITY, target, current, Decimal("1.00")))

        def queue_etf_delta(symbol: str, alloc: Decimal, extra_target: Decimal = Decimal("0")) -> None:
            target  = ((alloc * total_equity) + extra_target).quantize(Decimal("0.01"))
            current = equity_pos.get(symbol, Decimal("0"))
            log.info("  %s  target=$%.2f  current=$%.2f  delta=$%.2f",
                     symbol, target, current, target - current)
            queue(compute_delta(symbol, InstrumentType.EQUITY, target, current, Decimal("1.00")))

        # Fetch BTC and ETH prices in parallel
        with ThreadPoolExecutor(max_workers=2) as pool:
            btc_future = pool.submit(fetch_crypto_price, client, BTC_SYMBOL, "BTC-USD")
            eth_future = pool.submit(fetch_crypto_price, client, ETH_SYMBOL, "ETH-USD")
            btc_price = btc_future.result()
            eth_price = eth_future.result()

        log.info("--- Computing BTC delta ---")
        btc_target  = (ALLOC_BTC * total_equity).quantize(Decimal("0.01"))
        btc_current = crypto_pos.get(BTC_SYMBOL, Decimal("0"))
        log.info("  BTC  price=$%.2f  target=$%.2f  current=$%.2f  delta=$%.2f",
                 btc_price, btc_target, btc_current, btc_target - btc_current)
        queue(compute_delta(BTC_SYMBOL, InstrumentType.CRYPTO, btc_target, btc_current, Decimal("1.00")))

        log.info("--- Computing ETH delta ---")
        eth_target  = (ALLOC_ETH * total_equity).quantize(Decimal("0.01"))
        eth_current = crypto_pos.get(ETH_SYMBOL, Decimal("0"))
        log.info("  ETH  price=$%.2f  target=$%.2f  current=$%.2f  delta=$%.2f",
                 eth_price, eth_target, eth_current, eth_target - eth_current)
        queue(compute_delta(ETH_SYMBOL, InstrumentType.CRYPTO, eth_target, eth_current, Decimal("1.00")))

        log.info("--- Computing GLDM / SGOV deltas ---")
        queue_etf_delta(GOLD_SYMBOL, ALLOC_GOLD)
        log.info("  Residual stock allocation too small to trade directly: $%.2f", stock_residual_for_sgov)
        queue_etf_delta(SGOV_SYMBOL, ALLOC_SGOV, extra_target=stock_residual_for_sgov)

        sells.sort(key=lambda order: (order[3], order[0]), reverse=True)
        buys.sort(key=lambda order: (order[3], order[0]), reverse=True)

        log.info("Rebalance plan: %d sells  |  %d buys", len(sells), len(buys))
        if not sells and not buys:
            log.info("Portfolio is within threshold on all buckets — nothing to do.")
            return

        crypto_prices = {BTC_SYMBOL: btc_price, ETH_SYMBOL: eth_price}

        if sells:
            log.info("--- Placing SELL orders (%d) ---", len(sells))
            sell_order_ids = place_orders(client, sells, crypto_prices=crypto_prices)
            wait_for_orders_to_clear(client, sell_order_ids, label="sell")

        if buys:
            post_sell_buying_power = buying_power
            try:
                _, post_sell_buying_power, _, _ = get_portfolio_snapshot(client)
            except Exception as exc:
                log.warning("Could not refresh buying power after sells: %s", exc)
            buys = cap_buy_orders_to_buying_power(buys, post_sell_buying_power)
        if buys:
            log.info("--- Placing BUY orders (%d) ---", len(buys))
            place_orders(client, buys, crypto_prices=crypto_prices)

        log.info("Rebalance complete.")
    finally:
        client.close()


if __name__ == "__main__":
    rebalance()
