# Public Terminal

A btop/htop-style trading TUI for [Public.com](https://public.com), with direct index investing and automated daily portfolio rebalancing. Built with Go (Bubble Tea + Lipgloss).

---

## Features

- **Multi-account** — persistent tab bar to switch between accounts; add/remove accounts at runtime
- **Live portfolio** — holdings, values, quantities, open orders, **options positions**
- **Four-pane layout** — Stocks, Crypto, Options, and Open Orders in a 2×2 grid
- **Manual orders** — market, limit, stop, and stop-limit orders for stocks, ETFs, and crypto
- **Options tracking** — view open options positions with contract details, expiration dates, and P&L
- **Live portfolio stream** — balance, holdings, options, and open orders refresh every 30 seconds
- **Direct index investing** — top N stocks from S&P 500, NASDAQ-100, DJIA, FTSE Global All Cap, or SPUS (Shariah), market-cap weighted, rebalanced daily
- **Margin support** — optionally deploy a configurable percentage of your margin capacity as additional buying power
- **Configurable exclusions** — skip specific tickers from rebalancing entirely
- **PDT protection** — day-trade ledger prevents selling positions opened the same day
- **Systemd timer** — fires Mon–Fri at 12:00 ET; fully manageable from inside the TUI

---

## Requirements

- **[Go](https://go.dev/) 1.26+**
- A [Public.com](https://public.com) brokerage account with API access
- **[`publicdotcom-cli`](https://github.com/PublicDotCom/publicdotcom-cli)** — the official Public.com CLI tool

### Install the Public CLI dependency

```bash
pipx install publicdotcom-cli
# or
uv tool install publicdotcom-cli
```

### Authenticate

This project does **not** handle credentials directly. Authentication is fully delegated to the Public CLI:

```bash
public auth login
```

This opens a browser for OAuth authorization and stores credentials in your OS keychain. Run `public auth logout` to remove them.

---

## Installation & Run

### Quick install (recommended)

```bash
go install github.com/ks1686/public-terminal/cmd/public-terminal@latest
```

This installs to `~/go/bin/public-terminal`. Make sure `~/go/bin` is on your `PATH`.

### Download binary

Pre-built binaries for Linux and macOS (amd64/arm64) are attached to each [GitHub release](https://github.com/ks1686/public-terminal/releases).

```bash
# Linux amd64
curl -L -o public-terminal https://github.com/ks1686/public-terminal/releases/download/v0.4.0/public-terminal-linux-amd64
chmod +x public-terminal

# macOS arm64 (Apple Silicon)
curl -L -o public-terminal https://github.com/ks1686/public-terminal/releases/download/v0.4.0/public-terminal-darwin-arm64
chmod +x public-terminal
```

### Build from source

```bash
git clone https://github.com/ks1686/public-terminal.git
cd public-terminal
go build -o public-terminal ./cmd/public-terminal
```

### Run

```bash
./public-terminal
```

On first run you will be prompted to enter your account ID(s). All config is stored under `$XDG_CONFIG_HOME/public-terminal/` (default: `~/.config/public-terminal/`).

### CLI flags

| Flag | Description |
|------|-------------|
| `--rebalance` | Run rebalancer once and exit |
| `--dry-run` | Run rebalancer in dry-run mode (no real orders) |
| `--validate` | Validate the rebalance plan without executing |
| `--verbose` | Enable verbose logging |

---

## Interface

### Layout

```text
Account Tabs      — one tab per account; switch with Ctrl+← / Ctrl+→; click to switch
Balance Bar       — total equity, buying power, options BP, crypto BP, cash or margin balance
Rebalancer Bar    — timer status, active config, key hint strip
┌─ STOCKS ────────────┐  ┌─ CRYPTO ───────────┐
│ stocks, ETFs         │  │ BTC, ETH, SOL, ... │
└──────────────────────┘  └────────────────────┘
┌─ OPTIONS ───────────┐  ┌─ OPEN ORDERS ──────┐
│ calls, puts          │  │ pending orders      │
└──────────────────────┘  └────────────────────┘
Key hints / Status bar
```

### Key bindings

| Key | Action |
|-----|--------|
| `r` | Refresh portfolio, orders, and rebalancer status |
| `Tab` / `Shift+Tab` | Cycle focused pane (Stocks → Crypto → Options → Orders) |
| `Alt+Arrow Keys` | Move pane focus in the 2×2 grid |
| `↑` / `↓` / `j` / `k` | Navigate rows in the focused pane |
| `b` | Place a market buy order |
| `s` | Place a market sell order |
| `v` | View / modify the selected open order |
| `c` | Cancel the selected open order |
| `h` | View order history |
| `t` | Pause / resume the installed rebalancer schedule |
| `e` | Install / remove the rebalancer schedule |
| `x` | Skip the next scheduled rebalance run |
| `S` | Open rebalance settings modal |
| `Ctrl+A` | Open account management modal (add / remove accounts) |
| `Ctrl+←` / `Ctrl+→` | Switch account tab |
| `q` | Quit |

### Placing orders (`b` / `s`)

A modal prompts for:
- **Symbol** — e.g. `AAPL`, `BTC`, `GLDM`
- **Instrument type** — Equity or Crypto
- **Order type** — Market, Limit, Stop, or Stop Limit
- **Quantity** — shares or coin units (fractional supported)

Limit and stop prices are shown conditionally based on the selected order type.

### Holdings & Options Display

- **Stocks table** — equities sorted by position value (largest to smallest)
- **Crypto table** — crypto positions (largest to smallest)
- **Options table** — options contracts with underlying, type (CALL/PUT), strike, expiration, quantity, value, and P&L
  - Options expiring within 7 days are highlighted for quick reference
- **Orders table** — open/pending orders with symbol, side, type, status, quantity, and amount

### Live portfolio streaming

The TUI polls Public.com every 30 seconds and refreshes balances, holdings, options, and open orders in-place. The status bar shows the active account with a `STREAMING` indicator.

---

## Rebalancer

### Target allocation

| Asset | Allocation |
|-------|-----------|
| Stocks | 65% |
| BTC | 15% |
| ETH | 5% |
| GLDM | 10% |
| Cash | 5% |

### How it works

1. Fetches the current constituent list for the configured index from official ETF holdings
2. Fetches market caps via Yahoo Finance (20 parallel workers; results cached up to 20 hours)
3. Selects top N by market cap, filters excluded tickers, computes within-slice weights
4. Fetches the current portfolio from Public.com
5. Computes dollar deltas for all buckets against the investment base
6. Drift threshold: `max(0.5% of target, $1)` — positions within tolerance are left alone
7. Places SELL orders first, waits for them to clear, then places BUY orders
8. BUY orders are capped to the effective buying power budget
9. Logs everything to `accounts/<id>/cache/rebalance.log`

### Margin investing

When enabled, the rebalancer uses a configurable percentage of available margin capacity as additional buying power:

```text
margin_capacity  = current_margin_loan + current_margin_buying_power
allowed_margin   = margin_usage_pct × margin_capacity
investment_base  = portfolio_nav + allowed_margin
effective_bp     = cash_buying_power + max(0, allowed_margin - current_margin_loan)
```

### PDT protection

A daily buy ledger records every equity symbol purchased in any run today. Subsequent runs skip selling those symbols.

### Supported indexes

| Value | Index |
|-------|-------|
| `SP500` | S&P 500 |
| `NASDAQ100` | NASDAQ-100 |
| `DJIA` | Dow Jones Industrial Average |
| `FTSE_GLOBAL_ALL_CAP` | Global equities via iShares ACWI |
| `SPUS` | S&P 500 Shariah Industry Exclusions |

---

## Systemd Timer (Automated Daily Rebalance)

### Install / Remove

Use `e` in the TUI to install or remove the automated rebalancer schedule. The timer fires Mon–Fri at 12:00 ET.

### Shell management

```bash
systemctl --user status public-terminal-rebalance.timer
journalctl --user -u public-terminal-rebalance.service -f
systemctl --user stop public-terminal-rebalance.timer
systemctl --user disable --now public-terminal-rebalance.timer
```

---

## Configuration files

```
~/.config/public-terminal/
  accounts.json                     — ordered list of registered account IDs
  schema_version.json               — internal migration marker
  accounts/<id>/
    rebalance_config.json           — per-account index, top N, margin %, exclusions
    cache/rebalance.log             — rebalancer run history
    cache/market_caps.json          — market cap cache (auto-refreshes)
    cache/portfolio_cache.json      — cached portfolio for instant startup
    cache/today_buys.json           — PDT protection ledger (resets daily)
    cache/skip_next_rebalance       — sentinel file — presence skips one run
```

Credentials are handled exclusively by the `public` CLI via OS keychain — this project never reads, stores, or transmits API tokens.

---

## Development

```bash
go build ./cmd/public-terminal
go test ./...
go vet ./...
```
