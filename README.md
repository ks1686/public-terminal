# Public Terminal

A btop/htop-style TUI trading terminal for Public.com, with S&P 500 direct indexing and automated daily portfolio rebalancing.

## Features

- Live portfolio: holdings, buying power, open orders
- Market buy/sell orders for equities, ETFs, crypto, bonds
- S&P 500 direct indexing — top 250 by market cap, rebalanced daily
- Target allocation: 65% stocks · 15% BTC · 5% ETH · 10% GLDM · 5% SGOV
- Systemd timer fires Mon–Fri at 12:00 ET; manage it from inside the TUI

## Setup

### 1. Prerequisites

- Python 3.12+
- [uv](https://docs.astral.sh/uv/) (`curl -LsSf https://astral.sh/uv/install.sh | sh`)
- systemd user instance enabled (`loginctl enable-linger $USER`)

### 2. Install dependencies

```bash
uv sync
```

### 3. Configure credentials

Create `.env` in the project root:

```env
PUBLIC_ACCESS_TOKEN=<your API secret key from public.com>
PUBLIC_ACCOUNT_NUMBER=<your brokerage account number, e.g. 5OP95222>
```

Both values come from your Public.com account settings. The access token is an API secret key — the SDK exchanges it for short-lived bearer tokens automatically.

### 4. Run the TUI

```bash
uv run main.py
```

#### Key bindings

| Key | Action |
| --- | ------ |
| `r` | Refresh portfolio |
| `b` | Market buy |
| `s` | Market sell |
| `c` | Cancel selected order |
| `t` | Start / stop rebalancer timer |
| `e` | Enable / disable rebalancer (persists across reboots) |
| `q` | Quit |

## Systemd Setup (Daily Rebalancer)

The rebalancer runs as a user-level systemd service. No root access required.

### 1. Install the unit files

```bash
mkdir -p ~/.config/systemd/user
cp systemd/public-terminal-rebalance.service ~/.config/systemd/user/
cp systemd/public-terminal-rebalance.timer    ~/.config/systemd/user/
systemctl --user daemon-reload
```

### 2. Enable and start the timer

```bash
systemctl --user enable --now public-terminal-rebalance.timer
```

The timer fires Mon–Fri at **12:00 ET** (handles DST automatically). If the system was offline at noon, `Persistent=true` causes it to run immediately on next boot.

### 3. Check status

```bash
# Is the timer running?
systemctl --user status public-terminal-rebalance.timer

# When does it fire next?
systemctl --user list-timers public-terminal-rebalance.timer

# View rebalancer logs
journalctl --user -u public-terminal-rebalance.service -f

# Or read the log file directly
tail -f cache/rebalance.log
```

### 4. Stop / disable

```bash
# Stop for this session only
systemctl --user stop public-terminal-rebalance.timer

# Stop permanently (survives reboots)
systemctl --user disable --now public-terminal-rebalance.timer
```

You can also use **`t`** and **`e`** inside the TUI to start/stop and enable/disable without leaving the terminal.

### 5. Run a manual rebalance

```bash
uv run rebalance.py
```

Logs are written to `cache/rebalance.log`. Market cap data is cached for up to 20 hours in `cache/market_caps.json` so repeated same-day runs are fast.

## Rebalancing Logic

Each noon run:

1. Scrapes the current S&P 500 constituent list from Wikipedia.
2. Fetches market caps via yfinance (parallel, 20 workers). Results cached same-day.
3. Selects the top 250 by market cap and computes within-slice weights.
4. Fetches current portfolio from Public.com.
5. Computes dollar deltas for all four buckets. Drift threshold: max(0.5% of target, $5).
6. Places SELL orders first (to free cash), then BUY orders. BTC is converted from dollar delta to coin quantity using the live Public.com quote.
