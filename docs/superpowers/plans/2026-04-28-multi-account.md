# Multi-Account Support Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add multi-account support so users can switch between Public.com brokerage accounts in a persistent tab bar, with fully independent per-account settings and automatic migration for existing single-account users.

**Architecture:** Per-account subdirectories under `~/.config/public-terminal/accounts/<id>/` hold isolated config and cache. A schema versioning system (`schema_version.json`) drives silent automatic migration on startup. A persistent Textual `Tabs` bar at the top of the TUI allows switching; all API calls pass the active account ID explicitly.

**Tech Stack:** Python 3.12, Textual ≥ 0.80.0 (TUI), python-dotenv, publicdotcom-py SDK, unittest (stdlib)

**Release:** v0.2.0

**Spec:** `docs/superpowers/specs/2026-04-28-multi-account-design.md`

**Agent assignments:**
- Claude — Tasks 1, 9, 10 (migration logic, AccountManagementModal, app.py tab bar)
- Codex — Tasks 2, 3, 6, 7 (mechanical path functions, CRUD, signature changes, rebalance wiring)
- Copilot — Tasks 4, 11 (small targeted edits, version bump)
- Gemini — Task 5 (test generation)
- Kiro — Task 8 (SetupModal rebuild from spec)

---

## File Map

| File | Action | Responsibility |
|---|---|---|
| `config.py` | Modify | Schema versioning, migration, account-scoped paths, CRUD |
| `client.py` | Modify | Accept explicit `account_id` parameter |
| `rebalance.py` | Modify | Pass account_id to `get_client()` |
| `main.py` | Modify | `--account` CLI flag for rebalancer |
| `modals.py` | Modify | `SetupModal` multi-account flow + new `AccountManagementModal` |
| `app.py` | Modify | Account tab bar, `ctrl+left`/`ctrl+right`/`ctrl+a` bindings, switch logic |
| `test_config.py` | Create | Unit tests for migration and account CRUD |
| `pyproject.toml` | Modify | Bump version to 0.2.0 |
| `.github/workflows/ci.yml` | Modify | Add `test_config.py` to compile + discover commands |

---

## Task 1: Schema versioning + migration v0→v1

**Assigned to: Claude**

**Files:**
- Modify: `config.py`

This task introduces the schema versioning system and the first migration. The migration detects the old single-account layout and moves files into the new per-account structure silently.

- [ ] **Step 1: Add schema versioning constants and helper to `config.py`**

Add after the existing path constants (after line 42, before `_HAS_SYSTEMCTL`):

```python
ACCOUNTS_FILE = _APP_DIR / "accounts.json"
SCHEMA_VERSION_FILE = _APP_DIR / "schema_version.json"
ACCOUNTS_DIR = _APP_DIR / "accounts"
CURRENT_SCHEMA_VERSION = 1
```

- [ ] **Step 2: Add `_read_schema_version()` helper**

```python
def _read_schema_version() -> int:
    """Return the current on-disk schema version, or 0 if absent."""
    try:
        return int(json.loads(SCHEMA_VERSION_FILE.read_text()).get("version", 0))
    except (FileNotFoundError, json.JSONDecodeError, OSError, TypeError, ValueError):
        return 0


def _write_schema_version(version: int) -> None:
    SCHEMA_VERSION_FILE.write_text(json.dumps({"version": version}))
```

- [ ] **Step 3: Add `_migrate_v0_to_v1()` with full docstring**

```python
def _migrate_v0_to_v1() -> None:
    """Migrate from schema v0 to v1.

    v0 (legacy single-account layout):
      .env                      — PUBLIC_ACCESS_TOKEN + PUBLIC_ACCOUNT_NUMBER
      rebalance_config.json     — flat, at config root
      cache/                    — flat, at config root

    v1 (multi-account layout):
      .env                      — PUBLIC_ACCESS_TOKEN only
      accounts.json             — ["<account_number>"]
      schema_version.json       — {"version": 1}
      accounts/<id>/
        rebalance_config.json
        cache/

    Why: Introduced per-account subdirectories to support multiple accounts
    with independent settings and caches.
    """
    from dotenv import load_dotenv

    load_dotenv(ENV_FILE)
    account_id = os.environ.get("PUBLIC_ACCOUNT_NUMBER", "").strip().upper()
    if not account_id:
        # .env had no account number — nothing to migrate, write v1 marker only
        _write_schema_version(1)
        return

    account_dir = ACCOUNTS_DIR / account_id
    account_dir.mkdir(parents=True, exist_ok=True)

    old_config = _APP_DIR / "rebalance_config.json"
    new_config = account_dir / "rebalance_config.json"
    if old_config.exists() and not new_config.exists():
        try:
            shutil.move(str(old_config), str(new_config))
        except OSError as exc:
            print(f"[migration v0→v1] could not move rebalance_config.json: {exc}", file=sys.stderr)

    old_cache = _APP_DIR / "cache"
    new_cache = account_dir / "cache"
    if old_cache.exists() and not new_cache.exists():
        try:
            shutil.move(str(old_cache), str(new_cache))
        except OSError as exc:
            print(f"[migration v0→v1] could not move cache/: {exc}", file=sys.stderr)

    # Rewrite .env — keep only PUBLIC_ACCESS_TOKEN
    token = os.environ.get("PUBLIC_ACCESS_TOKEN") or os.environ.get("PUBLIC_API_SECRET_KEY", "")
    if ENV_FILE.exists():
        lines = [
            line for line in ENV_FILE.read_text().splitlines()
            if line.split("=", 1)[0].strip() not in (
                "PUBLIC_ACCOUNT_NUMBER", "PUBLIC_ACCESS_TOKEN", "PUBLIC_API_SECRET_KEY"
            )
        ]
        if token:
            lines.append(f"PUBLIC_ACCESS_TOKEN={token}")
        try:
            ENV_FILE.write_text("\n".join(lines) + "\n")
        except OSError as exc:
            print(f"[migration v0→v1] could not rewrite .env: {exc}", file=sys.stderr)

    if not ACCOUNTS_FILE.exists():
        try:
            ACCOUNTS_FILE.write_text(json.dumps([account_id]))
        except OSError as exc:
            print(f"[migration v0→v1] could not write accounts.json: {exc}", file=sys.stderr)

    _write_schema_version(1)
```

- [ ] **Step 4: Add `MIGRATIONS` registry and `migrate_if_needed()`**

```python
# Each entry: (from_version: int, migration_fn: Callable[[], None])
# Add new migrations in ascending order. Never remove or reorder existing entries.
MIGRATIONS: list[tuple[int, object]] = [
    (0, _migrate_v0_to_v1),
]


def migrate_if_needed() -> None:
    """Run any outstanding schema migrations at startup.

    Call this once before any other config function. Safe to call multiple
    times — already-applied migrations are skipped.

    Edge case: if schema_version.json is absent but accounts/ exists,
    treat as v1 to avoid overwriting existing account data.
    """
    if not SCHEMA_VERSION_FILE.exists() and ACCOUNTS_DIR.exists():
        _write_schema_version(CURRENT_SCHEMA_VERSION)
        return

    current = _read_schema_version()
    if current >= CURRENT_SCHEMA_VERSION:
        return

    for from_version, fn in MIGRATIONS:
        if from_version >= current:
            try:
                fn()
                current = from_version + 1
            except Exception as exc:
                print(
                    f"[migration v{from_version}→v{from_version + 1}] failed: {exc}",
                    file=sys.stderr,
                )
                return
```

- [ ] **Step 5: Run compile check**

```bash
uv run python -m compileall config.py
```
Expected: `Compiling 'config.py'...` with no errors.

- [ ] **Step 6: Commit**

```bash
git add config.py
git commit -m "feat(config): add schema versioning and v0→v1 migration"
```

---

## Task 2: Account-scoped path functions

**Assigned to: Codex**

**Files:**
- Modify: `config.py`

Add per-account path helpers that replace the old flat global path constants.

- [ ] **Step 1: Add account-scoped path functions after `ACCOUNTS_DIR` constant**

```python
def get_account_dir(account_id: str) -> Path:
    path = ACCOUNTS_DIR / account_id.upper().strip()
    path.mkdir(parents=True, exist_ok=True)
    return path


def get_rebalance_config_path(account_id: str) -> Path:
    return get_account_dir(account_id) / "rebalance_config.json"


def get_cache_dir(account_id: str) -> Path:
    cache = get_account_dir(account_id) / "cache"
    cache.mkdir(exist_ok=True)
    return cache


def get_portfolio_cache_path(account_id: str) -> Path:
    return get_cache_dir(account_id) / "portfolio_cache.json"


def get_rebalance_log_path(account_id: str) -> Path:
    return get_cache_dir(account_id) / "rebalance.log"


def get_today_buys_path(account_id: str) -> Path:
    return get_cache_dir(account_id) / "today_buys.json"


def get_skip_file_path(account_id: str) -> Path:
    return get_cache_dir(account_id) / "skip_next_rebalance"


def get_market_cap_cache_path(account_id: str) -> Path:
    return get_cache_dir(account_id) / "market_caps.json"
```

- [ ] **Step 2: Run compile check**

```bash
uv run python -m compileall config.py
```
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add config.py
git commit -m "feat(config): add account-scoped path functions"
```

---

## Task 3: Account CRUD functions

**Assigned to: Codex**

**Files:**
- Modify: `config.py`

- [ ] **Step 1: Add `get_accounts()`, `add_account()`, `remove_account()`**

```python
def get_accounts() -> list[str]:
    """Return ordered list of account IDs from accounts.json. Empty list if missing."""
    try:
        data = json.loads(ACCOUNTS_FILE.read_text())
        return [str(a).upper().strip() for a in data if str(a).strip()]
    except (FileNotFoundError, json.JSONDecodeError, OSError):
        return []


def add_account(account_id: str) -> None:
    """Append account_id to accounts.json and create its directory. No-op if duplicate."""
    normalized = account_id.upper().strip()
    accounts = get_accounts()
    if normalized in accounts:
        return
    accounts.append(normalized)
    ACCOUNTS_FILE.write_text(json.dumps(accounts))
    get_account_dir(normalized)  # creates directory


def remove_account(account_id: str) -> None:
    """Remove account_id from accounts.json and delete its directory.

    Raises ValueError if it is the last account (must keep at least one).
    """
    normalized = account_id.upper().strip()
    accounts = get_accounts()
    if normalized not in accounts:
        return
    if len(accounts) == 1:
        raise ValueError("Cannot remove the last account.")
    accounts.remove(normalized)
    ACCOUNTS_FILE.write_text(json.dumps(accounts))
    account_dir = ACCOUNTS_DIR / normalized
    if account_dir.exists():
        shutil.rmtree(account_dir)
```

- [ ] **Step 2: Run compile check**

```bash
uv run python -m compileall config.py
```
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add config.py
git commit -m "feat(config): add get_accounts, add_account, remove_account"
```

---

## Task 4: Update `_credentials_present()` and `_write_env()`

**Assigned to: Copilot**

**Files:**
- Modify: `config.py`

`_credentials_present()` must no longer require `PUBLIC_ACCOUNT_NUMBER` — credentials are valid when a token exists and `accounts.json` has at least one entry. `_write_env()` must no longer write `PUBLIC_ACCOUNT_NUMBER`.

- [ ] **Step 1: Replace `_credentials_present()`**

Find (lines 180–188):
```python
def _credentials_present() -> bool:
    """Return True if both required env vars are set (from .env or environment)."""
    from dotenv import load_dotenv

    load_dotenv(ENV_FILE)
    token = os.environ.get("PUBLIC_ACCESS_TOKEN") or os.environ.get(
        "PUBLIC_API_SECRET_KEY"
    )
    return bool(token and os.environ.get("PUBLIC_ACCOUNT_NUMBER"))
```

Replace with:
```python
def _credentials_present() -> bool:
    """Return True if a token is set and at least one account is registered."""
    from dotenv import load_dotenv

    load_dotenv(ENV_FILE)
    token = os.environ.get("PUBLIC_ACCESS_TOKEN") or os.environ.get(
        "PUBLIC_API_SECRET_KEY"
    )
    return bool(token and get_accounts())
```

- [ ] **Step 2: Replace `_write_env()`**

Find (lines 191–203):
```python
def _write_env(access_token: str, account_number: str) -> None:
    """Write (or overwrite) PUBLIC_ACCESS_TOKEN and PUBLIC_ACCOUNT_NUMBER in .env."""
    lines: list[str] = []
    if ENV_FILE.exists():
        for line in ENV_FILE.read_text().splitlines():
            key = line.split("=", 1)[0].strip()
            if key not in ("PUBLIC_ACCESS_TOKEN", "PUBLIC_ACCOUNT_NUMBER"):
                lines.append(line)
    lines.append(f"PUBLIC_ACCESS_TOKEN={access_token}")
    lines.append(f"PUBLIC_ACCOUNT_NUMBER={account_number}")
    ENV_FILE.write_text("\n".join(lines) + "\n")
    os.environ["PUBLIC_ACCESS_TOKEN"] = access_token
    os.environ["PUBLIC_ACCOUNT_NUMBER"] = account_number
```

Replace with:
```python
def _write_env(access_token: str) -> None:
    """Write (or overwrite) PUBLIC_ACCESS_TOKEN in .env. Token is shared across all accounts."""
    lines: list[str] = []
    if ENV_FILE.exists():
        for line in ENV_FILE.read_text().splitlines():
            key = line.split("=", 1)[0].strip()
            if key not in ("PUBLIC_ACCESS_TOKEN", "PUBLIC_ACCOUNT_NUMBER", "PUBLIC_API_SECRET_KEY"):
                lines.append(line)
    lines.append(f"PUBLIC_ACCESS_TOKEN={access_token}")
    ENV_FILE.write_text("\n".join(lines) + "\n")
    os.environ["PUBLIC_ACCESS_TOKEN"] = access_token
```

- [ ] **Step 3: Update `_load_rebalance_config()` and `_save_rebalance_config()` to accept `account_id`**

Replace:
```python
def _load_rebalance_config() -> dict:
    try:
        return json.loads(REBALANCE_CONFIG_FILE.read_text())
    except (FileNotFoundError, json.JSONDecodeError, OSError):
        return {"index": "SP500", "top_n": 500}


def _save_rebalance_config(
    index: str,
    top_n: int,
    margin_usage_pct: float,
    excluded_tickers: list[str],
    allocations: dict[str, float],
) -> None:
    REBALANCE_CONFIG_FILE.write_text(
        json.dumps(
            {
                "index": index,
                "top_n": top_n,
                "margin_usage_pct": margin_usage_pct,
                "excluded_tickers": sorted(
                    set(t.upper().strip() for t in excluded_tickers if t.strip())
                ),
                "allocations": allocations,
            },
            indent=2,
        )
    )
```

With:
```python
def _load_rebalance_config(account_id: str) -> dict:
    try:
        return json.loads(get_rebalance_config_path(account_id).read_text())
    except (FileNotFoundError, json.JSONDecodeError, OSError):
        return {"index": "SP500", "top_n": 500}


def _save_rebalance_config(
    account_id: str,
    index: str,
    top_n: int,
    margin_usage_pct: float,
    excluded_tickers: list[str],
    allocations: dict[str, float],
) -> None:
    get_rebalance_config_path(account_id).write_text(
        json.dumps(
            {
                "index": index,
                "top_n": top_n,
                "margin_usage_pct": margin_usage_pct,
                "excluded_tickers": sorted(
                    set(t.upper().strip() for t in excluded_tickers if t.strip())
                ),
                "allocations": allocations,
            },
            indent=2,
        )
    )
```

- [ ] **Step 4: Run compile check**

```bash
uv run python -m compileall config.py
```
Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add config.py
git commit -m "feat(config): update credentials check and env write for multi-account"
```

---

## Task 5: Tests for `config.py`

**Assigned to: Gemini**

**Files:**
- Create: `test_config.py`

Write `unittest.TestCase` tests using a temporary directory to isolate file system state. Each test creates a fresh temp dir and patches `config._APP_DIR`, `config.ACCOUNTS_FILE`, `config.SCHEMA_VERSION_FILE`, `config.ACCOUNTS_DIR`, and `config.ENV_FILE` to point into it.

- [ ] **Step 1: Create `test_config.py` with test harness**

```python
"""Tests for config.py schema migration and account management."""
from __future__ import annotations

import json
import os
import shutil
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch


def _patch_config_paths(tmp: Path):
    """Return a context manager that redirects all config paths into tmp."""
    accounts_file = tmp / "accounts.json"
    schema_file = tmp / "schema_version.json"
    accounts_dir = tmp / "accounts"
    env_file = tmp / ".env"
    import config
    return unittest.mock.patch.multiple(
        config,
        _APP_DIR=tmp,
        ACCOUNTS_FILE=accounts_file,
        SCHEMA_VERSION_FILE=schema_file,
        ACCOUNTS_DIR=accounts_dir,
        ENV_FILE=env_file,
        REBALANCE_CONFIG_FILE=tmp / "rebalance_config.json",
        CACHE_DIR=tmp / "cache",
    )
```

- [ ] **Step 2: Add migration tests**

```python
class TestMigration(unittest.TestCase):
    def setUp(self):
        self.tmp = Path(tempfile.mkdtemp())

    def tearDown(self):
        shutil.rmtree(self.tmp)

    def test_v0_to_v1_moves_rebalance_config(self):
        import config
        env_file = self.tmp / ".env"
        env_file.write_text("PUBLIC_ACCESS_TOKEN=tok123\nPUBLIC_ACCOUNT_NUMBER=ACCT001\n")
        old_config = self.tmp / "rebalance_config.json"
        old_config.write_text(json.dumps({"index": "SP500", "top_n": 500}))

        with _patch_config_paths(self.tmp):
            os.environ["PUBLIC_ACCESS_TOKEN"] = "tok123"
            os.environ["PUBLIC_ACCOUNT_NUMBER"] = "ACCT001"
            config._migrate_v0_to_v1()

        new_config = self.tmp / "accounts" / "ACCT001" / "rebalance_config.json"
        self.assertTrue(new_config.exists())
        self.assertFalse(old_config.exists())

    def test_v0_to_v1_moves_cache_dir(self):
        import config
        env_file = self.tmp / ".env"
        env_file.write_text("PUBLIC_ACCESS_TOKEN=tok123\nPUBLIC_ACCOUNT_NUMBER=ACCT001\n")
        old_cache = self.tmp / "cache"
        old_cache.mkdir()
        (old_cache / "portfolio_cache.json").write_text("{}")

        with _patch_config_paths(self.tmp):
            os.environ["PUBLIC_ACCESS_TOKEN"] = "tok123"
            os.environ["PUBLIC_ACCOUNT_NUMBER"] = "ACCT001"
            config._migrate_v0_to_v1()

        new_cache = self.tmp / "accounts" / "ACCT001" / "cache"
        self.assertTrue(new_cache.exists())
        self.assertFalse(old_cache.exists())

    def test_v0_to_v1_rewrites_env_token_only(self):
        import config
        env_file = self.tmp / ".env"
        env_file.write_text("PUBLIC_ACCESS_TOKEN=tok123\nPUBLIC_ACCOUNT_NUMBER=ACCT001\n")

        with _patch_config_paths(self.tmp):
            os.environ["PUBLIC_ACCESS_TOKEN"] = "tok123"
            os.environ["PUBLIC_ACCOUNT_NUMBER"] = "ACCT001"
            config._migrate_v0_to_v1()

        content = env_file.read_text()
        self.assertIn("PUBLIC_ACCESS_TOKEN=tok123", content)
        self.assertNotIn("PUBLIC_ACCOUNT_NUMBER", content)

    def test_v0_to_v1_writes_accounts_json(self):
        import config
        env_file = self.tmp / ".env"
        env_file.write_text("PUBLIC_ACCESS_TOKEN=tok123\nPUBLIC_ACCOUNT_NUMBER=ACCT001\n")

        with _patch_config_paths(self.tmp):
            os.environ["PUBLIC_ACCESS_TOKEN"] = "tok123"
            os.environ["PUBLIC_ACCOUNT_NUMBER"] = "ACCT001"
            config._migrate_v0_to_v1()

        accounts = json.loads((self.tmp / "accounts.json").read_text())
        self.assertEqual(accounts, ["ACCT001"])

    def test_migrate_if_needed_skips_when_accounts_dir_exists(self):
        import config
        accounts_dir = self.tmp / "accounts"
        accounts_dir.mkdir()

        with _patch_config_paths(self.tmp):
            config.migrate_if_needed()

        schema = json.loads((self.tmp / "schema_version.json").read_text())
        self.assertEqual(schema["version"], config.CURRENT_SCHEMA_VERSION)

    def test_migrate_if_needed_noop_when_current(self):
        import config
        schema_file = self.tmp / "schema_version.json"
        schema_file.write_text(json.dumps({"version": config.CURRENT_SCHEMA_VERSION}))

        with _patch_config_paths(self.tmp):
            config.migrate_if_needed()  # should not raise
```

- [ ] **Step 3: Add account CRUD tests**

```python
class TestAccountCRUD(unittest.TestCase):
    def setUp(self):
        self.tmp = Path(tempfile.mkdtemp())

    def tearDown(self):
        shutil.rmtree(self.tmp)

    def test_get_accounts_empty_when_missing(self):
        import config
        with _patch_config_paths(self.tmp):
            self.assertEqual(config.get_accounts(), [])

    def test_add_account_creates_entry_and_dir(self):
        import config
        with _patch_config_paths(self.tmp):
            config.add_account("ACCT001")
            self.assertEqual(config.get_accounts(), ["ACCT001"])
            self.assertTrue((self.tmp / "accounts" / "ACCT001").exists())

    def test_add_account_deduplicates(self):
        import config
        with _patch_config_paths(self.tmp):
            config.add_account("ACCT001")
            config.add_account("ACCT001")
            self.assertEqual(config.get_accounts(), ["ACCT001"])

    def test_add_account_normalizes_case(self):
        import config
        with _patch_config_paths(self.tmp):
            config.add_account("acct001")
            self.assertEqual(config.get_accounts(), ["ACCT001"])

    def test_remove_account_deletes_dir(self):
        import config
        with _patch_config_paths(self.tmp):
            config.add_account("ACCT001")
            config.add_account("ACCT002")
            config.remove_account("ACCT001")
            self.assertNotIn("ACCT001", config.get_accounts())
            self.assertFalse((self.tmp / "accounts" / "ACCT001").exists())

    def test_remove_last_account_raises(self):
        import config
        with _patch_config_paths(self.tmp):
            config.add_account("ACCT001")
            with self.assertRaises(ValueError):
                config.remove_account("ACCT001")

    def test_account_scoped_paths_use_correct_dirs(self):
        import config
        with _patch_config_paths(self.tmp):
            p = config.get_rebalance_config_path("ACCT001")
            self.assertEqual(p, self.tmp / "accounts" / "ACCT001" / "rebalance_config.json")
            p2 = config.get_portfolio_cache_path("ACCT001")
            self.assertEqual(p2, self.tmp / "accounts" / "ACCT001" / "cache" / "portfolio_cache.json")


if __name__ == "__main__":
    unittest.main()
```

- [ ] **Step 4: Run tests**

```bash
uv run python -m unittest test_config -v
```
Expected: all tests pass.

- [ ] **Step 5: Commit**

```bash
git add test_config.py
git commit -m "test(config): add migration and account CRUD unit tests"
```

---

## Task 6: Update `client.py` — accept explicit `account_id`

**Assigned to: Codex**

**Files:**
- Modify: `client.py`

- [ ] **Step 1: Update `get_client()` signature**

Replace lines 45–71 in `client.py`:
```python
def get_client() -> PublicApiClient:
    """Create and return an authenticated PublicApiClient.

    Both env vars are API secret keys — the SDK exchanges them for short-lived
    bearer tokens automatically via ApiKeyAuthConfig.

    Required for this app:
      PUBLIC_ACCOUNT_NUMBER — default account used by portfolio/order calls
    """
    access_token = os.environ.get("PUBLIC_ACCESS_TOKEN")
    api_secret_key = os.environ.get("PUBLIC_API_SECRET_KEY")

    if not access_token and not api_secret_key:
        raise RuntimeError(
            "No credentials found. Set PUBLIC_ACCESS_TOKEN or PUBLIC_API_SECRET_KEY in .env"
        )

    account_number = os.environ.get("PUBLIC_ACCOUNT_NUMBER")
    if not account_number:
        raise RuntimeError("No account number found. Set PUBLIC_ACCOUNT_NUMBER in .env")

    config = PublicApiClientConfiguration(default_account_number=account_number)

    secret = access_token or api_secret_key
    auth = ApiKeyAuthConfig(api_secret_key=secret)

    return PublicApiClient(auth_config=auth, config=config)
```

With:
```python
def get_client(account_id: str) -> PublicApiClient:
    """Create and return an authenticated PublicApiClient for the given account."""
    access_token = os.environ.get("PUBLIC_ACCESS_TOKEN")
    api_secret_key = os.environ.get("PUBLIC_API_SECRET_KEY")

    if not access_token and not api_secret_key:
        raise RuntimeError(
            "No credentials found. Set PUBLIC_ACCESS_TOKEN in .env"
        )

    if not account_id or not account_id.strip():
        raise RuntimeError("account_id must be a non-empty string.")

    cfg = PublicApiClientConfiguration(default_account_number=account_id.upper().strip())
    secret = access_token or api_secret_key
    auth = ApiKeyAuthConfig(api_secret_key=secret)
    return PublicApiClient(auth_config=auth, config=cfg)
```

- [ ] **Step 2: Run compile check**

```bash
uv run python -m compileall client.py
```
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add client.py
git commit -m "feat(client): accept explicit account_id in get_client()"
```

---

## Task 7: Wire `account_id` through `rebalance.py` and `main.py`

**Assigned to: Codex**

**Files:**
- Modify: `rebalance.py`
- Modify: `main.py`

`rebalance.py` runs headlessly via systemd. It should default to the first account in `accounts.json` and accept an optional `--account <id>` CLI flag. All per-account path constants must be replaced with the scoped functions from `config.py`.

- [ ] **Step 1: Update imports in `rebalance.py`**

Replace the config imports block (lines 47–57):
```python
from config import (
    _ACTIVE_ORDER_STATUSES,
    BROKER_TO_YF_SYMBOLS,
    CACHE_DIR,
    MARKET_CAP_CACHE_FILE,
    REBALANCE_CONFIG_FILE,
    REBALANCE_LOG_FILE,
    SKIP_FILE,
    TODAY_BUYS_FILE,
    YF_TO_BROKER_SYMBOLS,
)
```

With:
```python
from config import (
    _ACTIVE_ORDER_STATUSES,
    BROKER_TO_YF_SYMBOLS,
    YF_TO_BROKER_SYMBOLS,
    get_accounts,
    get_cache_dir,
    get_market_cap_cache_path,
    get_rebalance_config_path,
    get_rebalance_log_path,
    get_skip_file_path,
    get_today_buys_path,
)
```

- [ ] **Step 2: Update `rebalance()` function signature and path usage**

Find the `rebalance()` function definition (around line 1460) and update it to accept an `account_id` parameter. Replace every use of `REBALANCE_CONFIG_FILE`, `REBALANCE_LOG_FILE`, `SKIP_FILE`, `TODAY_BUYS_FILE`, `CACHE_DIR`, and `MARKET_CAP_CACHE_FILE` with the scoped equivalents:

```python
def rebalance(dry_run: bool = False, account_id: str | None = None) -> None:
    resolved_account = (account_id or "").strip().upper()
    if not resolved_account:
        accounts = get_accounts()
        if not accounts:
            print("No accounts configured. Run the TUI to set up an account.", file=sys.stderr)
            sys.exit(1)
        resolved_account = accounts[0]

    rebalance_config_file = get_rebalance_config_path(resolved_account)
    rebalance_log_file = get_rebalance_log_path(resolved_account)
    skip_file = get_skip_file_path(resolved_account)
    today_buys_file = get_today_buys_path(resolved_account)
    cache_dir = get_cache_dir(resolved_account)
    market_cap_cache_file = get_market_cap_cache_path(resolved_account)
    # ... rest of function unchanged, using these local variables instead of module-level constants
```

Replace all remaining references to the old module-level constants within the function body with the local variables defined above.

- [ ] **Step 3: Update `get_client()` call in `rebalance.py`** (line ~1491)

Find:
```python
client = get_client()
```

Replace with:
```python
client = get_client(resolved_account)
```

- [ ] **Step 4: Update `main.py` to pass `--account` flag**

Replace `main.py` entirely:
```python
"""Public Terminal — entry point."""
from __future__ import annotations

import sys


def main() -> None:
    args = sys.argv[1:]
    if "--rebalance" in args:
        account_id = None
        if "--account" in args:
            idx = args.index("--account")
            if idx + 1 < len(args):
                account_id = args[idx + 1]
        from rebalance import rebalance
        rebalance(dry_run="--dry-run" in args, account_id=account_id)
    elif "--install-service" in args:
        from config import _install_service_files
        try:
            print(_install_service_files())
        except Exception as exc:
            print(f"Error: {exc}", file=sys.stderr)
            sys.exit(1)
    elif "--remove-service" in args:
        from config import _remove_service_files
        try:
            print(_remove_service_files())
        except Exception as exc:
            print(f"Error: {exc}", file=sys.stderr)
            sys.exit(1)
    else:
        from config import migrate_if_needed
        migrate_if_needed()
        from app import PublicTerminal
        PublicTerminal().run()


if __name__ == "__main__":
    main()
```

- [ ] **Step 5: Run compile check**

```bash
uv run python -m compileall rebalance.py main.py
```
Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add rebalance.py main.py
git commit -m "feat(rebalance): wire account_id through rebalance and main"
```

---

## Task 8: Update `SetupModal` for multi-account entry

**Assigned to: Kiro**

**Files:**
- Modify: `modals.py`

**Spec reference:** Section 6 of `docs/superpowers/specs/2026-04-28-multi-account-design.md`

Replace the existing `SetupModal` with one that:
1. Prompts for `PUBLIC_ACCESS_TOKEN`
2. Prompts for a first account number, showing a running list of added accounts below the input
3. Has an "Add Another Account" button that appends valid entries to the list
4. Has a "Done" button (enabled only when ≥1 account is listed) that writes `.env` and `accounts.json`
5. Shows inline errors (non-empty label, alphanumeric 4–12 chars, no duplicates) without dismissing the modal

The `SetupModal` should call `_write_env(token)` (one argument — see Task 4) and `add_account(account_id)` for each account, then `dismiss(True)`.

- [ ] **Step 1: Write the new `SetupModal` CSS**

Replace the existing `DEFAULT_CSS` in `SetupModal` with:
```python
DEFAULT_CSS = """
SetupModal {
    align: center middle;
}
#setup-dialog {
    width: 76;
    height: auto;
    border: thick $warning;
    background: $surface;
    padding: 1 2;
}
#setup-title {
    text-align: center;
    text-style: bold;
    height: 1;
    margin-bottom: 1;
    color: $warning;
}
#setup-intro {
    color: $text-muted;
    margin-bottom: 1;
}
.field-label {
    height: 1;
    margin-top: 1;
    color: $text-muted;
}
#setup-account-list {
    margin-top: 1;
    color: $success;
    height: auto;
}
#setup-btn-row {
    margin-top: 1;
    height: 3;
    align: center middle;
}
#setup-error {
    height: 1;
    margin-top: 1;
    color: $error;
}
#setup-btn-add {
    margin-right: 1;
}
#setup-btn-save {
    margin-right: 2;
}
"""
```

- [ ] **Step 2: Rewrite `SetupModal.compose()` and handlers**

```python
_INTRO = (
    "No credentials found. Enter your Public.com API details below.\n"
    "They will be saved to ~/.config/public-terminal/.env"
)

def compose(self):
    self._accounts: list[str] = []
    with Grid(id="setup-dialog"):
        yield Label("WELCOME TO PUBLIC TERMINAL", id="setup-title")
        yield Label(self._INTRO, id="setup-intro")
        yield Label("API Access Token  (Settings → API → Secret Key)", classes="field-label")
        yield Input(placeholder="your-access-token", password=True, id="input-token")
        yield Label("Account Number  (e.g. ACCT0001)", classes="field-label")
        yield Input(placeholder="e.g. ACCT0001", id="input-account")
        yield Label("", id="setup-error")
        yield Label("", id="setup-account-list")
        with Horizontal(id="setup-btn-row"):
            yield Button("Add Another Account", variant="default", id="setup-btn-add")
            yield Button("Done", variant="success", id="setup-btn-save", disabled=True)
            yield Button("Quit", variant="error", id="setup-btn-quit")

@on(Button.Pressed, "#setup-btn-quit")
def do_quit(self) -> None:
    self.dismiss(False)

@on(Button.Pressed, "#setup-btn-add")
def do_add_account(self) -> None:
    account = self.query_one("#input-account", Input).value.strip().upper()
    error_label = self.query_one("#setup-error", Label)
    if not account:
        error_label.update("Account number is required.")
        return
    if not account.isalnum() or not (4 <= len(account) <= 12):
        error_label.update("Account number must be 4–12 alphanumeric characters.")
        return
    if account in self._accounts:
        error_label.update(f"{account} is already added.")
        return
    self._accounts.append(account)
    error_label.update("")
    self.query_one("#input-account", Input).value = ""
    account_list = self.query_one("#setup-account-list", Label)
    account_list.update("Accounts: " + ", ".join(self._accounts))
    self.query_one("#setup-btn-save", Button).disabled = False

@on(Button.Pressed, "#setup-btn-save")
def do_save(self) -> None:
    from config import _write_env, add_account
    token = self.query_one("#input-token", Input).value.strip()
    error_label = self.query_one("#setup-error", Label)
    # Try adding any account still in the input field
    pending = self.query_one("#input-account", Input).value.strip().upper()
    if pending and pending not in self._accounts:
        if pending.isalnum() and 4 <= len(pending) <= 12:
            self._accounts.append(pending)
    if not token:
        error_label.update("API access token is required.")
        self.query_one("#input-token", Input).focus()
        return
    if not self._accounts:
        error_label.update("Add at least one account number.")
        return
    error_label.update("")
    _write_env(token)
    for acct in self._accounts:
        add_account(acct)
    self.dismiss(True)
```

- [ ] **Step 3: Run compile check**

```bash
uv run python -m compileall modals.py
```
Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add modals.py
git commit -m "feat(modals): rebuild SetupModal for multi-account entry"
```

---

## Task 9: Add `AccountManagementModal` with validation

**Assigned to: Claude**

**Files:**
- Modify: `modals.py`

This new modal is opened via `ctrl+a` in the TUI. It lists existing accounts with remove buttons, and has an add-account input with API validation.

- [ ] **Step 1: Add `AccountManagementModal` CSS and class skeleton**

```python
class AccountManagementModal(ModalScreen[None]):
    """Manage accounts: add new ones (with API validation) or remove existing ones."""

    DEFAULT_CSS = """
    AccountManagementModal {
        align: center middle;
    }
    #acct-dialog {
        width: 72;
        height: auto;
        border: thick $primary;
        background: $surface;
        padding: 1 2;
    }
    #acct-title {
        text-align: center;
        text-style: bold;
        height: 1;
        margin-bottom: 1;
    }
    .acct-row {
        height: 3;
        margin-bottom: 1;
    }
    .acct-label {
        width: 1fr;
        content-align: left middle;
    }
    .acct-remove-btn {
        width: auto;
    }
    #acct-add-section {
        margin-top: 1;
    }
    .field-label {
        height: 1;
        color: $text-muted;
    }
    #acct-error {
        height: 1;
        margin-top: 1;
        color: $error;
    }
    #acct-status {
        height: 1;
        margin-top: 1;
        color: $text-muted;
    }
    #acct-btn-row {
        margin-top: 1;
        height: 3;
        align: center middle;
    }
    """
```

- [ ] **Step 2: Add `compose()` and account list rendering**

```python
    def compose(self):
        from config import get_accounts
        self._accounts = get_accounts()
        with Grid(id="acct-dialog"):
            yield Label("ACCOUNT MANAGEMENT", id="acct-title")
            for acct in self._accounts:
                with Horizontal(classes="acct-row"):
                    yield Label(acct, classes="acct-label", id=f"acct-label-{acct}")
                    yield Button(
                        "Remove",
                        variant="error",
                        classes="acct-remove-btn",
                        id=f"acct-remove-{acct}",
                        disabled=len(self._accounts) == 1,
                    )
            with Vertical(id="acct-add-section"):
                yield Label("Add Account Number", classes="field-label")
                yield Input(placeholder="e.g. ACCT0002", id="acct-input")
                yield Label("", id="acct-error")
                yield Label("", id="acct-status")
            with Horizontal(id="acct-btn-row"):
                yield Button("Add Account", variant="success", id="acct-btn-add")
                yield Button("Close", variant="default", id="acct-btn-close")
```

- [ ] **Step 3: Add remove handler**

```python
    @on(Button.Pressed)
    def on_button_pressed(self, event: Button.Pressed) -> None:
        from config import remove_account
        btn_id = event.button.id or ""
        if btn_id.startswith("acct-remove-"):
            acct = btn_id[len("acct-remove-"):]
            try:
                remove_account(acct)
                self._accounts.remove(acct)
            except ValueError as exc:
                self.query_one("#acct-error", Label).update(str(exc))
                return
            # Rebuild the modal to reflect removed account
            self.dismiss(None)
        elif btn_id == "acct-btn-close":
            self.dismiss(None)
        elif btn_id == "acct-btn-add":
            self._do_add_account()
```

- [ ] **Step 4: Add `_do_add_account()` with format + duplicate + API validation**

```python
    def _do_add_account(self) -> None:
        from config import add_account, get_accounts
        error_label = self.query_one("#acct-error", Label)
        status_label = self.query_one("#acct-status", Label)
        account = self.query_one("#acct-input", Input).value.strip().upper()

        error_label.update("")
        status_label.update("")

        if not account:
            error_label.update("Account number is required.")
            return
        if not account.isalnum() or not (4 <= len(account) <= 12):
            error_label.update("Account number must be 4–12 alphanumeric characters.")
            return
        if account in get_accounts():
            error_label.update(f"{account} is already registered.")
            return

        status_label.update("Validating with Public.com…")
        self._validate_and_add(account)

    @work(thread=True)
    def _validate_and_add(self, account: str) -> None:
        from client import get_client
        import os
        from dotenv import load_dotenv
        from config import ENV_FILE, add_account
        load_dotenv(ENV_FILE)

        network_error = False
        api_error = False
        try:
            client = get_client(account)
            client.get_portfolio()
        except RuntimeError:
            # credentials missing — can't validate, allow add
            network_error = True
        except Exception as exc:
            msg = str(exc).lower()
            if any(w in msg for w in ("404", "not found", "unauthorized", "forbidden", "invalid")):
                api_error = True
            else:
                network_error = True

        def _finish():
            error_label = self.query_one("#acct-error", Label)
            status_label = self.query_one("#acct-status", Label)
            status_label.update("")
            if api_error:
                error_label.update(
                    "Account not found or not accessible with the current token."
                )
                return
            if network_error:
                error_label.update(
                    "Network error — account added anyway. Verify when online."
                )
            add_account(account)
            self.dismiss(None)

        self.call_from_thread(_finish)
```

- [ ] **Step 5: Run compile check**

```bash
uv run python -m compileall modals.py
```
Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add modals.py
git commit -m "feat(modals): add AccountManagementModal with API validation"
```

---

## Task 10: Account tab bar and switching in `app.py`

**Assigned to: Claude**

**Files:**
- Modify: `app.py`

Add a persistent account tab bar using Textual's `Tabs`/`Tab` widgets, `ctrl+left`/`ctrl+right` bindings for switching, `ctrl+a` for opening `AccountManagementModal`, and account-aware data loading.

- [ ] **Step 1: Update imports in `app.py`**

Add `Tab, Tabs` to the textual.widgets import line:
```python
from textual.widgets import Footer, Header, Label, Tab, Tabs
```

Add to config imports:
```python
from config import (
    _ACTIVE_ORDER_STATUSES,
    _HAS_SYSTEMCTL,
    _credentials_present,
    _install_service_files,
    _load_rebalance_config,
    _remove_service_files,
    _save_rebalance_config,
    get_accounts,
    get_portfolio_cache_path,
    get_skip_file_path,
    migrate_if_needed,
)
```

Add to modals imports:
```python
from modals import (
    AccountManagementModal,
    CancelConfirmModal,
    HistoryModal,
    OrderModal,
    RebalanceConfigModal,
    RebalanceNowConfirmModal,
    SetupModal,
)
```

- [ ] **Step 2: Add bindings and update `__init__`**

Add to `BINDINGS` list:
```python
Binding("ctrl+left", "prev_account", "Prev Account", show=False),
Binding("ctrl+right", "next_account", "Next Account", show=False),
Binding("ctrl+a", "manage_accounts", "Accounts"),
```

Update `__init__`:
```python
def __init__(self) -> None:
    super().__init__()
    self._client = None
    self._active_account: str = ""
    self._margin_enabled: bool | None = None
    self._margin_capacity = Decimal(0)
    self._live_chart = False
    self._live_timer: Timer | None = None
```

- [ ] **Step 3: Update CSS to include tab bar**

Add to the `CSS` string:
```python
CSS = """
Screen { background: $surface; }
#account-tabs { height: 3; }
#main-layout { height: 1fr; }
#left-pane  { width: 2fr; border: tall $primary; }
#right-pane { width: 1fr; border: tall $accent; }
#pane-title    { background: $primary; color: $text; text-align: center; height: 1; text-style: bold; }
#orders-title  { background: $accent;  color: $text; text-align: center; height: 1; text-style: bold; }
"""
```

- [ ] **Step 4: Update `compose()` to yield `Tabs`**

```python
def compose(self) -> ComposeResult:
    yield Header(show_clock=True)
    accounts = get_accounts()
    yield Tabs(
        *[Tab(acct, id=f"tab-{acct}") for acct in accounts],
        id="account-tabs",
    )
    yield BalanceBar(id="balance-bar")
    yield RebalancerBar(id="rebalancer-bar")
    yield PortfolioChart(id="portfolio-chart")
    with Horizontal(id="main-layout"):
        with Vertical(id="left-pane"):
            yield Label(" HOLDINGS", id="pane-title")
            yield HoldingsTable(id="holdings-table")
        with Vertical(id="right-pane"):
            yield Label(" OPEN ORDERS", id="orders-title")
            yield OrdersTable(id="orders-table")
    yield StatusBar(id="status-bar")
    yield Footer()
```

- [ ] **Step 5: Update `on_mount()` to set active account**

`migrate_if_needed()` is already called in `main.py` before the app starts — do not call it again here.

```python
def on_mount(self) -> None:
    self._live_timer = self.set_interval(
        LIVE_PORTFOLIO_POLL_SECONDS, self._poll_live_portfolio, pause=True
    )
    if not _credentials_present():
        self.push_screen(SetupModal(), self._handle_setup)
    else:
        accounts = get_accounts()
        self._active_account = accounts[0] if accounts else ""
        self._start_loading()
```

Also remove `migrate_if_needed` from the config imports in `app.py` — it is not needed here.

- [ ] **Step 6: Add `on_tabs_tab_activated()` for tab switching**

```python
def on_tabs_tab_activated(self, event: Tabs.TabActivated) -> None:
    if event.tab is None:
        return
    account_id = event.tab.id or ""
    if account_id.startswith("tab-"):
        account_id = account_id[4:]
    if account_id and account_id != self._active_account:
        self._active_account = account_id
        self._client = None  # force new client for new account
        self._start_loading()
```

- [ ] **Step 7: Add `action_prev_account()`, `action_next_account()`, `action_manage_accounts()`**

```python
def action_prev_account(self) -> None:
    accounts = get_accounts()
    if len(accounts) <= 1:
        return
    idx = accounts.index(self._active_account) if self._active_account in accounts else 0
    new_acct = accounts[(idx - 1) % len(accounts)]
    self.query_one("#account-tabs", Tabs).active = f"tab-{new_acct}"

def action_next_account(self) -> None:
    accounts = get_accounts()
    if len(accounts) <= 1:
        return
    idx = accounts.index(self._active_account) if self._active_account in accounts else 0
    new_acct = accounts[(idx + 1) % len(accounts)]
    self.query_one("#account-tabs", Tabs).active = f"tab-{new_acct}"

def action_manage_accounts(self) -> None:
    self.push_screen(AccountManagementModal(), self._handle_account_management)

def _handle_account_management(self, _: None) -> None:
    accounts = get_accounts()
    tabs = self.query_one("#account-tabs", Tabs)
    # Rebuild tabs to reflect any add/remove
    tabs.clear()
    for acct in accounts:
        tabs.add_tab(Tab(acct, id=f"tab-{acct}"))
    if self._active_account not in accounts and accounts:
        self._active_account = accounts[0]
        tabs.active = f"tab-{self._active_account}"
        self._client = None
        self._start_loading()
```

- [ ] **Step 8: Update `_get_client()` to pass active account**

Find the `_get_client()` method (around line 285–291):
```python
def _get_client(self):
    if self._client is None:
        from client import get_client
        self._client = get_client()
    return self._client
```

Replace with:
```python
def _get_client(self):
    if self._client is None:
        from client import get_client
        self._client = get_client(self._active_account)
    return self._client
```

- [ ] **Step 9: Update `_load_portfolio_cache()` and `_save_portfolio_cache()` to use scoped path**

Find `_load_portfolio_cache()`:
```python
def _load_portfolio_cache(self) -> None:
    try:
        data = json.loads(PORTFOLIO_CACHE.read_text())
```

Replace with:
```python
def _load_portfolio_cache(self) -> None:
    try:
        data = json.loads(get_portfolio_cache_path(self._active_account).read_text())
```

Find `_save_portfolio_cache()` — it currently uses `PORTFOLIO_CACHE` as a module-level constant. Update it to accept and use the scoped path:

```python
@staticmethod
def _save_portfolio_cache(
    account_id: str,
    balance: dict,
    holdings: list[dict],
    orders: list[dict],
    positions: list[dict],
) -> None:
    from config import get_portfolio_cache_path
    path = get_portfolio_cache_path(account_id)
    try:
        path.parent.mkdir(exist_ok=True)
        path.write_text(
            json.dumps(
                {
                    "account_id": account_id,
                    "balance": balance,
                    "holdings": holdings,
                    "orders": orders,
                    "positions": positions,
                }
            )
        )
    except OSError:
        pass
```

Then find all call sites of `_save_portfolio_cache` and add `self._active_account` as the first argument.

- [ ] **Step 10: Update `_load_rebalance_config` and `_save_rebalance_config` calls in `app.py`**

Search for any call to `_load_rebalance_config()` (no args) and replace with `_load_rebalance_config(self._active_account)`.

Search for any call to `_save_rebalance_config(...)` and prepend `self._active_account` as the first argument.

- [ ] **Step 11: Update `SKIP_FILE` usage in `app.py`**

Search for `SKIP_FILE` in `app.py`. For each occurrence, replace with `get_skip_file_path(self._active_account)`. Remove `SKIP_FILE` from the config import in `app.py` and add `get_skip_file_path` to the import.

- [ ] **Step 12: Run full compile check**

```bash
uv run python -m compileall app.py config.py widgets.py modals.py rebalance.py main.py client.py test_audit.py test_rebalance.py test_config.py
```
Expected: no errors.

- [ ] **Step 13: Run all tests**

```bash
uv run python -m unittest discover -v -p "test_*.py"
```
Expected: all tests pass.

- [ ] **Step 14: Commit**

```bash
git add app.py
git commit -m "feat(app): add account tab bar, ctrl bindings, and account-scoped data loading"
```

---

## Task 11: Version bump and CI update

**Assigned to: Copilot**

**Files:**
- Modify: `pyproject.toml`
- Modify: `.github/workflows/ci.yml`

- [ ] **Step 1: Bump version in `pyproject.toml`**

Change:
```toml
version = "0.1.3"
```
To:
```toml
version = "0.2.0"
```

- [ ] **Step 2: Add `test_config.py` to CI compile and discover commands**

In `.github/workflows/ci.yml`, update the compile check step:
```yaml
- name: Compile check
  run: |
    uv run python -m compileall app.py config.py widgets.py modals.py rebalance.py main.py client.py test_audit.py test_rebalance.py test_config.py
```

The unit test discovery step already uses `test_*.py` pattern and will pick up `test_config.py` automatically — no change needed there.

- [ ] **Step 3: Commit**

```bash
git add pyproject.toml .github/workflows/ci.yml
git commit -m "chore: bump version to v0.2.0 and update CI for test_config"
```

---

## Final Verification

After all tasks are complete:

- [ ] Run full compile check: `uv run python -m compileall app.py config.py widgets.py modals.py rebalance.py main.py client.py test_audit.py test_rebalance.py test_config.py`
- [ ] Run all tests: `uv run python -m unittest discover -v -p "test_*.py"`
- [ ] Run import smoke check: `uv run python -c "import app, client, config, main, modals, rebalance, widgets"`
- [ ] Verify `pyproject.toml` version is `0.2.0`
