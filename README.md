# Public Terminal

A btop/htop-style trading TUI for [Public.com](https://public.com), with direct index investing and automated daily portfolio rebalancing.

---

## Features

- **Multi-account** — persistent tab bar to switch between accounts; add/remove accounts at runtime
- **Live portfolio** — holdings, values, quantities, open orders
- **Manual orders** — market buy and sell for equities, ETFs, and crypto
- **Portfolio chart** — scrollable price history across all your holdings
- **Direct index investing** — top N stocks from S&P 500, NASDAQ-100, DJIA, ACWI, or SPUS (Shariah), market-cap weighted, rebalanced daily
- **Margin support** — optionally deploy a configurable percentage of your margin capacity as additional buying power
- **Configurable exclusions** — skip specific tickers from rebalancing entirely
- **PDT protection** — day-trade ledger prevents selling positions opened the same day
- **Systemd timer** — fires Mon–Fri at 12:00 ET; fully manageable from inside the TUI

---

## Installation

### Install / upgrade (single command)

```bash
uv tool install --force https://github.com/ks1686/public-terminal/releases/latest/download/public_terminal-latest.tar.gz
```

```bash
pipx install --force https://github.com/ks1686/public-terminal/releases/latest/download/public_terminal-latest.tar.gz
```

Both commands always install the latest release. Re-run at any time to upgrade.

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
```

Then launch the app — on first run you will be prompted to enter your account number(s).

Launch:

```bash
uv run main.py
```

---

## Interface

### Layout

```text
Header (clock)
Account Tabs      — one tab per account; switch with Ctrl+Left / Ctrl+Right
Balance Bar       — total equity, buying power, options BP, crypto BP, cash or margin balance
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
| `l` | Toggle **live portfolio balance stream** on the chart |
| `b` | Place a market **buy** order |
| `s` | Place a market **sell** order |
| `c` | Cancel the selected open order |
| `h` | View order history |
| `[` | Scroll portfolio chart left (earlier) |
| `]` | Scroll portfolio chart right (later) |
| `t` | **Pause / resume** the installed rebalancer schedule |
| `e` | **Install / remove** the rebalancer schedule |
| `x` | **Skip the next** scheduled rebalance run |
| `S` | Open **rebalance settings** modal |
| `Ctrl+A` | Open **account management** modal (add / remove accounts) |
| `Ctrl+Left` | Switch to previous account tab |
| `Ctrl+Right` | Switch to next account tab |
| `q` | Quit |

### Placing orders (`b` / `s`)

A modal prompts for:

- **Symbol** — e.g. `AAPL`, `BTC`, `GLDM`
- **Instrument type** — Equity or Crypto
- **Quantity** — shares or coin units (fractional supported)

All orders are market orders, day-only.

### Cancelling orders (`c`)

Select a row in the Open Orders table, then press `c`. A confirmation modal shows the order details before cancellation.

### Portfolio chart (`[` / `]` / `l`)

Shows a price history chart for the positions in your portfolio, loaded in a single batched fetch. The chart title shows dollar and percentage movement for the selected time frame. The 24H view includes equity extended-hours data when available and crypto's 24/7 movement. Use `[` and `]` to scroll the time window.

Press `l` to toggle live balance streaming. Live mode polls Public.com every 30 seconds, refreshes balances/holdings/orders, and keeps a rolling 24-hour total-equity stream with dollar and percentage change so you can watch portfolio value changes as they come in after hours too. Press `l` again to return to the historical chart.

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
10. Logs everything to `accounts/<id>/cache/rebalance.log`

### Margin investing

When margin investing is enabled on your Public.com account, you can configure what percentage of your available margin capacity the rebalancer uses as **additional** buying power on top of cash:

```text
margin_capacity    = current_margin_loan + current_margin_buying_power
allowed_margin     = margin_usage_pct × margin_capacity
investment_base    = portfolio_nav + allowed_margin
effective_bp       = cash_buying_power + max(0, allowed_margin - current_margin_loan)
```

All asset targets are computed against `investment_base`, so margin funds proportionally larger positions across every bucket — not just stocks. Existing margin debt, including margin withdrawal loans, consumes the allowed margin buying-power budget before any new buy orders are placed. Set `margin_usage_pct = 0.0` to use cash only.

The TUI checks Public.com's reported buying power before allowing margin configuration. If total buying power does not exceed cash-only buying power, the margin input is disabled and saved as `0.0`.

### PDT (Pattern Day Trading) protection

A daily buy ledger (`cache/today_buys.json`) records every equity symbol purchased in any run today. Subsequent runs skip selling those symbols to avoid same-day buy+sell round-trips. The ledger resets automatically at midnight.

If the broker signals a PDT restriction mid-run, the rebalancer aborts remaining orders cleanly and logs the error.

### Rebalance settings (`S`)

The settings modal lets you configure without editing any files:

| Field | Description |
|-------|-------------|
| **Index to track** | Determines which constituent list to track (see table below) |
| **Top N stocks** | How many of the top constituents to hold (by market cap). Default: 500 (full index) |
| **Margin usage** | Enabled only when the account has margin buying power. `0.0` = cash only · `0.5` = 50% of margin capacity · `1.0` = full margin |
| **Excluded tickers** | Comma-separated symbols to skip entirely (no buys, no sells) |

Supported indexes:

| Config value | Index |
|--------------|-------|
| `SP500` | S&P 500 |
| `NASDAQ100` | NASDAQ-100 |
| `DJIA` | Dow Jones Industrial Average |
| `FTSE_GLOBAL_ALL_CAP` | Global equities via an iShares ACWI holdings proxy |
| `SPUS` | SP Funds S&P 500 Shariah Industry Exclusions ETF |

Legacy ETF values such as `SPY`, `QQQ`, `DIA`, and `VT` are migrated automatically. Arbitrary ETFs are not supported. Settings are saved to `rebalance_config.json` and take effect on the next run.

### Skipping a run (`x`)

Pressing `x` creates a `cache/skip_next_rebalance` sentinel file. The next scheduled (or manual) run detects it, removes it, and exits immediately. Useful before travel or when you want to pause one cycle.

---

## Systemd Timer (Automated Daily Rebalance)

The rebalancer runs as a user-level systemd service — no root access required.

### Install / Remove

Use `e` in the TUI to install or remove the automated rebalancer schedule. Installing writes the user-level service and timer files for the current runtime, then activates the timer for the scheduled runs. Removing stops and disables the timer, then deletes those service files.

The timer fires Mon–Fri at **12:00 ET** (DST-aware). `Persistent=true` means it catches up immediately after a missed run (e.g. system was off at noon).

### Manage from the TUI

| Key | Effect |
|-----|--------|
| `t` | Pause or resume the installed timer without removing service files |
| `e` | Install/activate or disable/remove the scheduled timer |
| `x` | Skip the next run |

### Manage from the shell

```bash
# Status
systemctl --user status public-terminal-rebalance.timer
systemctl --user list-timers public-terminal-rebalance.timer

# Live logs
journalctl --user -u public-terminal-rebalance.service -f
tail -f accounts/<id>/cache/rebalance.log

# Stop for this session
systemctl --user stop public-terminal-rebalance.timer

# Disable permanently
systemctl --user disable --now public-terminal-rebalance.timer
rm -f ~/.config/systemd/user/public-terminal-rebalance.service
rm -f ~/.config/systemd/user/public-terminal-rebalance.timer
systemctl --user daemon-reload
```

### Run manually

```bash
public-terminal-rebalance

# source-run equivalent
uv run rebalance.py

# dry-run: compute and validate the plan, but do not cancel or place orders
public-terminal-rebalance --dry-run
uv run rebalance.py --dry-run
```

---

## Configuration files

For CLI installs (`uv tool install`, `pipx install`), these are stored under
`~/.config/public-terminal/` (or `$XDG_CONFIG_HOME/public-terminal/`).
For source runs, they are stored in the project directory.

```text
.env                              — API access token (shared across all accounts)
accounts.json                     — ordered list of registered account IDs
schema_version.json               — internal migration marker
accounts/<id>/
  rebalance_config.json           — per-account index, top N, margin %, exclusions
  cache/rebalance.log             — full rebalancer run history
  cache/market_caps.json          — same-day market cap cache (auto-refreshes)
  cache/today_buys.json           — PDT protection ledger (resets daily)
  cache/skip_next_rebalance       — sentinel file — presence skips one run
```

### Migrating from v0.1.x

Existing single-account users are migrated automatically on first launch. The app reads `PUBLIC_ACCOUNT_NUMBER` from `.env`, moves `rebalance_config.json` and `cache/` into `accounts/<id>/`, rewrites `.env` with only the token, and writes `accounts.json`. No manual steps required.
