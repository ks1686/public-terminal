# Public Terminal

A btop/htop-style trading TUI for [Public.com](https://public.com), with direct index investing and automated daily portfolio rebalancing.

---

## Features

- **Live portfolio** — holdings, values, quantities, open orders
- **Manual orders** — market buy and sell for equities, ETFs, and crypto
- **Portfolio chart** — scrollable price history across all your holdings
- **Direct index investing** — top N stocks from S&P 500, NASDAQ-100, or DJIA, market-cap weighted, rebalanced daily
- **Margin support** — optionally deploy a configurable percentage of your margin capacity as additional buying power
- **Configurable exclusions** — skip specific tickers from rebalancing entirely
- **PDT protection** — day-trade ledger prevents selling positions opened the same day
- **Systemd timer** — fires Mon–Fri at 12:00 ET; fully manageable from inside the TUI

---

## Installation

### Latest release (single command)

Use either command below. Both install the newest GitHub release:

```bash
uv tool install --force https://github.com/ks1686/public-terminal/releases/latest/download/public_terminal-latest.tar.gz
```

```bash
pipx install --force https://github.com/ks1686/public-terminal/releases/latest/download/public_terminal-latest.tar.gz
```

### Specific release (single command)

Pin to an exact version by replacing `vX.Y.Z`:

```bash
uv tool install --force https://github.com/ks1686/public-terminal/releases/download/vX.Y.Z/public_terminal-X.Y.Z-py3-none-any.whl
```

```bash
pipx install --force https://github.com/ks1686/public-terminal/releases/download/vX.Y.Z/public_terminal-X.Y.Z-py3-none-any.whl
```

### Run

```bash
public-terminal
public-terminal-rebalance
```

Installed tool runtime files are stored in:

```text
$XDG_CONFIG_HOME/public-terminal/
# default: ~/.config/public-terminal/
```

### Source setup (dev only)

Prerequisites:

- Python 3.12+
- [uv](https://docs.astral.sh/uv/) — `curl -LsSf https://astral.sh/uv/install.sh | sh`
- A [Public.com](https://public.com) brokerage account with API access

Install dependencies:

```bash
uv sync
```

Configure credentials by creating `.env` in the project root:

```env
PUBLIC_ACCESS_TOKEN=<your API secret key from public.com>
PUBLIC_ACCOUNT_NUMBER=<your brokerage account number, e.g. 5OP95222>
```

Launch:

```bash
uv run main.py
```

---

## Interface

### Layout

```text
Header (clock)
Balance Bar       — total equity, buying power (cash / options / crypto)
Rebalancer Bar    — timer status, active config, key hint strip
Portfolio Chart   — scrollable price history
┌─ PORTFOLIO ──────┐  ┌─ OPEN ORDERS ──────┐
│ holdings table   │  │ pending orders      │
└──────────────────┘  └────────────────────┘
Footer (key bindings)
```

### Key bindings

| Key | Action |
|-----|--------|
| `r` | Refresh portfolio, orders, and rebalancer status |
| `b` | Place a market **buy** order |
| `s` | Place a market **sell** order |
| `c` | Cancel the selected open order |
| `h` | View order history |
| `[` | Scroll portfolio chart left (earlier) |
| `]` | Scroll portfolio chart right (later) |
| `t` | **Start / stop** the rebalancer systemd timer (this session) |
| `e` | **Enable / disable** the rebalancer timer (survives reboots) |
| `x` | **Skip the next** scheduled rebalance run |
| `R` | **Run the rebalancer now** (confirmation required) |
| `S` | Open **rebalance settings** modal |
| `q` | Quit |

### Placing orders (`b` / `s`)

A modal prompts for:

- **Symbol** — e.g. `AAPL`, `BTC`, `GLDM`
- **Instrument type** — Equity or Crypto
- **Quantity** — shares or coin units (fractional supported)

All orders are market orders, day-only.

### Cancelling orders (`c`)

Select a row in the Open Orders table, then press `c`. A confirmation modal shows the order details before cancellation.

### Portfolio chart (`[` / `]`)

Shows a price history chart for the positions in your portfolio, loaded in a single batched fetch. Use `[` and `]` to scroll the time window.

---

## Rebalancer

### Target allocation

| Asset | Allocation | Notes |
|-------|-----------|-------|
| Stocks | 65% | Top N from configured index, market-cap weighted |
| BTC | 15% | Bitcoin |
| ETH | 5% | Ethereum |
| GLDM | 10% | Gold ETF |
| Cash | 5% | Uninvested buying power (no orders placed) |

### How it works

Each run:

1. Fetches the current constituent list for the configured index from official ETF holdings, with Wikipedia as a fallback
2. Fetches market caps via yfinance (20 parallel workers; results cached up to 20 hours)
3. Selects the top N by market cap, filters out any excluded tickers, and computes within-slice weights
4. Fetches the current portfolio from Public.com
5. Computes dollar deltas for all buckets against the **investment base** (see Margin below)
6. Drift threshold: `max(0.5% of target, $1)` — positions within tolerance are left alone
7. Places SELL orders first, waits for them to clear, then places BUY orders
8. BUY orders are capped to the effective buying power budget
9. Full liquidations (stocks that dropped out of the index) use share-quantity orders to avoid broker rejection
10. Logs everything to `cache/rebalance.log`

### Margin investing

When margin investing is enabled on your Public.com account, you can configure what percentage of your available margin capacity the rebalancer uses as **additional** buying power on top of cash:

```text
investment_base    = total_equity + (margin_usage_pct × margin_capacity)
effective_bp       = cash_buying_power + (margin_usage_pct × margin_capacity)
```

All asset targets are computed against `investment_base`, so margin funds proportionally larger positions across every bucket — not just stocks. Set `margin_usage_pct = 0.0` to use cash only.

### PDT (Pattern Day Trading) protection

A daily buy ledger (`cache/today_buys.json`) records every equity symbol purchased in any run today. Subsequent runs skip selling those symbols to avoid same-day buy+sell round-trips. The ledger resets automatically at midnight.

If the broker signals a PDT restriction mid-run, the rebalancer aborts remaining orders cleanly and logs the error.

### Rebalance settings (`S`)

The settings modal lets you configure without editing any files:

| Field | Description |
|-------|-------------|
| **Index to track** | Determines which constituent list to track (see table below) |
| **Top N stocks** | How many of the top constituents to hold (by market cap). Default: 500 (full index) |
| **Margin usage** | `0.0` = cash only · `0.5` = 50% of margin capacity · `1.0` = full margin |
| **Excluded tickers** | Comma-separated symbols to skip entirely (no buys, no sells) |

Supported indexes:

| Config value | Index |
|--------------|-------|
| `SP500` | S&P 500 |
| `NASDAQ100` | NASDAQ-100 |
| `DJIA` | Dow Jones Industrial Average |

Legacy ETF values such as `SPY`, `QQQ`, and `DIA` are migrated automatically. Arbitrary ETFs are not supported. Settings are saved to `rebalance_config.json` and take effect on the next run.

### Skipping a run (`x`)

Pressing `x` creates a `cache/skip_next_rebalance` sentinel file. The next scheduled (or manual) run detects it, removes it, and exits immediately. Useful before travel or when you want to pause one cycle.

---

## Systemd Timer (Automated Daily Rebalance)

The rebalancer runs as a user-level systemd service — no root access required.

### Install

```bash
public-terminal --install-service
systemctl --user enable --now public-terminal-rebalance.timer
```

Source-run equivalent:

```bash
uv run main.py --install-service
```

The installer writes a service file for the current runtime. Source installs use the active Python interpreter and `main.py`; binary releases use the packaged executable.

The timer fires Mon–Fri at **12:00 ET** (DST-aware). `Persistent=true` means it catches up immediately after a missed run (e.g. system was off at noon).

### Manage from the TUI

| Key | Effect |
|-----|--------|
| `t` | Start or stop the timer for this session |
| `e` | Enable or disable the timer permanently (survives reboots) |
| `x` | Skip the next run |
| `R` | Trigger an immediate run |

### Manage from the shell

```bash
# Status
systemctl --user status public-terminal-rebalance.timer
systemctl --user list-timers public-terminal-rebalance.timer

# Live logs
journalctl --user -u public-terminal-rebalance.service -f
tail -f cache/rebalance.log

# Stop for this session
systemctl --user stop public-terminal-rebalance.timer

# Disable permanently
systemctl --user disable --now public-terminal-rebalance.timer
```

### Run manually

```bash
public-terminal-rebalance

# source-run equivalent
uv run rebalance.py
```

---

## Configuration files

For CLI installs (`uv tool install`, `pipx install`), these are stored under
`~/.config/public-terminal/` (or `$XDG_CONFIG_HOME/public-terminal/`).
For source runs, they are stored in the project directory.

| File | Purpose |
|------|---------|
| `.env` | API credentials (access token, account number) |
| `rebalance_config.json` | Index, top N, margin %, excluded tickers, target allocations |
| `cache/rebalance.log` | Full rebalancer run history |
| `cache/market_caps.json` | Same-day market cap cache (auto-refreshes) |
| `cache/today_buys.json` | PDT protection ledger (resets daily) |
| `cache/skip_next_rebalance` | Sentinel file — presence skips one run |
