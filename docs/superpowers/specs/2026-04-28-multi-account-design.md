# Multi-Account Support Design

**Date:** 2026-04-28  
**Status:** Approved

---

## Overview

Add support for multiple Public.com brokerage accounts within a single app session. All accounts share one API token but have distinct account numbers and fully independent settings. Users switch between accounts via a persistent tab bar at the top of the TUI.

---

## 1. Storage Layout

```
~/.config/public-terminal/
  .env                          # PUBLIC_ACCESS_TOKEN only
  accounts.json                 # ordered list: ["ACCT0001", "ACCT0002"]
  schema_version.json           # {"version": 1}
  accounts/
    ACCT0001/
      rebalance_config.json
      cache/
        portfolio_cache.json
        rebalance.log
    ACCT0002/
      rebalance_config.json
      cache/
        portfolio_cache.json
        rebalance.log
```

- `accounts.json` is the source of truth for which accounts exist and their display order.
- Order is preserved across restarts; the first entry is the default active account on launch.
- Each account directory is fully self-contained — adding future per-account data fits naturally.

---

## 2. Schema Versioning & Migration (`config.py`)

### Schema version file

`schema_version.json` at the config root tracks the current layout version:

```json
{"version": 1}
```

### `migrate_if_needed()`

Called once at startup before any other config is read. Dispatches migrations in sequence based on the current version:

1. If `schema_version.json` is absent → assume v0 (legacy single-account layout)
2. Read version, run all outstanding migrations in order
3. Write the latest version number back to `schema_version.json`

If any migration step fails (e.g., file permissions), log to stderr and continue — old files are never deleted until the migration succeeds, so no data is lost.

### Migration: v0 → v1

```
Old layout (v0):
  .env                        # PUBLIC_ACCESS_TOKEN + PUBLIC_ACCOUNT_NUMBER
  rebalance_config.json       # flat, at config root
  cache/                      # flat, at config root

New layout (v1):
  .env                        # PUBLIC_ACCESS_TOKEN only
  accounts.json               # ["<account_number>"]
  schema_version.json         # {"version": 1}
  accounts/<id>/
    rebalance_config.json
    cache/
```

**Why:** Introduced per-account subdirectories to support multiple accounts with independent settings and caches.

**Steps:**
1. Extract `PUBLIC_ACCOUNT_NUMBER` from `.env`
2. Create `accounts/<id>/` directory
3. Move `rebalance_config.json` → `accounts/<id>/rebalance_config.json` (if exists)
4. Move `cache/` → `accounts/<id>/cache/` (if exists)
5. Rewrite `.env` keeping only `PUBLIC_ACCESS_TOKEN`
6. Write `accounts.json` with the single account number
7. Write `schema_version.json` with `{"version": 1}`

### Adding future migrations

Each migration is a discrete, documented function:

```python
# v1 → v2: <description of what changed and why>
# Old layout: ...
# New layout: ...
def migrate_v1_to_v2(): ...
```

Register it in the `MIGRATIONS` list in order. Each entry is a `(from_version: int, fn: Callable)` tuple. `migrate_if_needed()` iterates this list and runs any entry where `from_version >= current_version`.

**Edge case:** if `schema_version.json` is absent but `accounts/` directory already exists (corrupted state), treat as v1 and skip migration rather than overwriting existing account data.

---

## 3. Config Layer (`config.py`)

All path-returning functions gain an `account_id: str` parameter:

| Function | Returns |
|---|---|
| `get_rebalance_config_path(account_id)` | `accounts/<id>/rebalance_config.json` |
| `get_cache_dir(account_id)` | `accounts/<id>/cache/` |
| `get_portfolio_cache_path(account_id)` | `accounts/<id>/cache/portfolio_cache.json` |
| `get_rebalance_log_path(account_id)` | `accounts/<id>/cache/rebalance.log` |

New functions:

| Function | Description |
|---|---|
| `get_accounts() -> list[str]` | Reads `accounts.json`, returns ordered list |
| `add_account(account_id: str)` | Appends to `accounts.json`, creates subdirectory |
| `remove_account(account_id: str)` | Removes from `accounts.json`, deletes subdirectory |
| `migrate_if_needed()` | Runs schema migrations at startup |

---

## 4. API Client (`client.py`)

`get_client()` becomes `get_client(account_id: str)`.

- `PUBLIC_ACCESS_TOKEN` is still read from `.env` (shared across all accounts)
- `account_id` is passed explicitly by callers rather than read from env
- All callers in `app.py` and `rebalance.py` pass the currently active account ID

---

## 5. UI — Account Tab Bar (`app.py`)

A persistent tab bar is rendered at the top of the TUI, above existing content. Each account appears as a tab; the active account is highlighted.

**Keybindings (no conflicts with existing bindings):**

| Key | Action |
|---|---|
| `ctrl+left` | Switch to previous account |
| `ctrl+right` | Switch to next account |
| `ctrl+a` | Open account management modal |
| Mouse click on tab | Switch directly to that account |

**Existing bindings (for reference, no changes):**
`q`, `r`, `b`, `s`, `c`, `h`, `l`, `t`, `e`, `x`, `R`, `S`, `[`, `]`

**On account switch:** load portfolio data from the new account's cache immediately (for instant display), then trigger a background refresh. Rebalance config is loaded from the new account's `rebalance_config.json`. Cache from the previous account is preserved.

---

## 6. Setup & Account Management (`modals.py`)

### `SetupModal` (initial setup, modified)

1. Prompt for `PUBLIC_ACCESS_TOKEN`
2. Prompt for first account number; show a running list of added accounts
3. "Add another account" button — allows entering additional account numbers before finishing
4. "Done" — validates all entries, writes `.env` (token only), `accounts.json`, and creates all `accounts/<id>/` directories at once

### Account management modal (new, opened via `ctrl+a`)

- **Add account** — text input for a new account number; validates non-empty and not a duplicate; calls `add_account()` and switches focus to the new account
- **Remove account** — lists existing accounts with a remove button next to each; the remove button is disabled when only one account remains (must always have at least one)

---

## 7. Data Flow Summary

```
startup
  └─ migrate_if_needed()          # silent schema migration if needed
  └─ get_accounts()               # load account list
  └─ set active_account = accounts[0]

account switch
  └─ active_account = selected
  └─ get_client(active_account)   # API client for this account
  └─ load portfolio cache         # get_portfolio_cache_path(active_account)
  └─ load rebalance config        # get_rebalance_config_path(active_account)

rebalance settings save
  └─ save to get_rebalance_config_path(active_account)
```
