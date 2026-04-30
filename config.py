"""Runtime paths, service management, credential helpers, and rebalancer config I/O."""

from __future__ import annotations

import json
import os
import shutil
import subprocess
import sys
from pathlib import Path

from public_api_sdk import OrderStatus

# ---------------------------------------------------------------------------
# Runtime paths — computed once at import time
# ---------------------------------------------------------------------------


def _app_dir() -> Path:
    """Base directory for config, cache, and data (frozen-binary aware)."""
    if getattr(sys, "frozen", False):
        return Path(sys.executable).parent
    source_dir = Path(__file__).resolve().parent
    if (source_dir / "pyproject.toml").exists():
        return source_dir

    xdg_config_home = Path(os.environ.get("XDG_CONFIG_HOME", Path.home() / ".config"))
    app_dir = xdg_config_home / "public-terminal"
    app_dir.mkdir(parents=True, exist_ok=True)
    return app_dir


_APP_DIR = _app_dir()

ACCOUNTS_FILE = _APP_DIR / "accounts.json"
SCHEMA_VERSION_FILE = _APP_DIR / "schema_version.json"
ACCOUNTS_DIR = _APP_DIR / "accounts"
CURRENT_SCHEMA_VERSION = 1


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


def get_index_cache_path(account_id: str, index_id: str) -> Path:
    """Return path to index-specific cache file, e.g. cache/constituents_SP500.json"""
    return get_cache_dir(account_id) / f"constituents_{index_id.upper()}.json"


def get_rebalance_log_path(account_id: str) -> Path:
    return get_cache_dir(account_id) / "rebalance.log"


def get_today_buys_path(account_id: str) -> Path:
    return get_cache_dir(account_id) / "today_buys.json"


def get_skip_file_path(account_id: str) -> Path:
    return get_cache_dir(account_id) / "skip_next_rebalance"


def get_market_cap_cache_path(account_id: str) -> Path:
    return get_cache_dir(account_id) / "market_caps.json"


def _read_schema_version() -> int:
    """Return the current on-disk schema version, or 0 if absent."""
    try:
        return int(json.loads(SCHEMA_VERSION_FILE.read_text()).get("version", 0))
    except (FileNotFoundError, json.JSONDecodeError, OSError, TypeError, ValueError):
        return 0


def _write_schema_version(version: int) -> None:
    SCHEMA_VERSION_FILE.write_text(json.dumps({"version": version}))


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


ENV_FILE = _APP_DIR / ".env"


# ---------------------------------------------------------------------------
# Shared constants
# ---------------------------------------------------------------------------

_HAS_SYSTEMCTL = bool(shutil.which("systemctl"))

_ACTIVE_ORDER_STATUSES = {
    OrderStatus.NEW,
    OrderStatus.PARTIALLY_FILLED,
    OrderStatus.PENDING_REPLACE,
    OrderStatus.PENDING_CANCEL,
}

# Shared broker-to-yfinance symbol aliases (crypto + share-class equities)
BROKER_TO_YF_SYMBOLS = {
    "BF.B": "BF-B",
    "BRK.B": "BRK-B",
    "BTC": "BTC-USD",
    "ETH": "ETH-USD",
}

YF_TO_BROKER_SYMBOLS = {
    yf_symbol: broker_symbol
    for broker_symbol, yf_symbol in BROKER_TO_YF_SYMBOLS.items()
}


# ---------------------------------------------------------------------------
# Service management
# ---------------------------------------------------------------------------


def _generate_service_content() -> str:
    """Return a systemd service file body for the current runtime."""
    if getattr(sys, "frozen", False):
        exec_start = f"{sys.executable} --rebalance"
        work_dir = str(Path(sys.executable).parent)
    else:
        main_py = (Path(__file__).parent / "main.py").resolve()
        exec_start = f"{sys.executable} {main_py} --rebalance"
        work_dir = str(Path(__file__).parent.resolve())
    return (
        "[Unit]\n"
        "Description=Public Terminal — daily portfolio rebalance\n"
        "After=network-online.target\n"
        "Wants=network-online.target\n"
        "\n"
        "[Service]\n"
        "Type=oneshot\n"
        f"ExecStart={exec_start}\n"
        f"WorkingDirectory={work_dir}\n"
        "StandardOutput=journal\n"
        "StandardError=journal\n"
        "Restart=on-failure\n"
        "RestartSec=60\n"
        "StartLimitBurst=3\n"
        "StartLimitIntervalSec=300\n"
    )


def _install_service_files() -> str:
    """Write the service + timer to ~/.config/systemd/user/ and reload the daemon."""
    systemd_dir = Path.home() / ".config" / "systemd" / "user"
    systemd_dir.mkdir(parents=True, exist_ok=True)

    service_path = systemd_dir / "public-terminal-rebalance.service"
    service_path.write_text(_generate_service_content())

    timer_dst = systemd_dir / "public-terminal-rebalance.timer"
    timer_dst.write_text(
        "[Unit]\n"
        "Description=Run portfolio rebalance daily at 12:00 ET\n"
        "\n"
        "[Timer]\n"
        "OnCalendar=Mon..Fri *-*-* 12:00:00\n"
        "TimeZone=America/New_York\n"
        "Persistent=true\n"
        "\n"
        "[Install]\n"
        "WantedBy=timers.target\n"
    )

    if _HAS_SYSTEMCTL:
        subprocess.run(["systemctl", "--user", "daemon-reload"], check=True)
        return (
            f"Service installed → {service_path}\n"
            f"Timer   installed → {timer_dst}\n"
            "daemon-reload OK.  Run: systemctl --user enable --now public-terminal-rebalance.timer"
        )
    return (
        f"Service installed → {service_path}\n"
        f"Timer   installed → {timer_dst}\n"
        "(systemctl not found — skipping daemon-reload)"
    )


def _remove_service_files() -> str:
    """Stop, disable, and remove the service + timer from ~/.config/systemd/user/."""
    systemd_dir = Path.home() / ".config" / "systemd" / "user"
    service_path = systemd_dir / "public-terminal-rebalance.service"
    timer_path = systemd_dir / "public-terminal-rebalance.timer"

    if _HAS_SYSTEMCTL:
        subprocess.run(
            ["systemctl", "--user", "stop", "public-terminal-rebalance.timer"],
            check=False,
        )
        subprocess.run(
            ["systemctl", "--user", "disable", "public-terminal-rebalance.timer"],
            check=False,
        )
        subprocess.run(
            ["systemctl", "--user", "stop", "public-terminal-rebalance.service"],
            check=False,
        )

    removed = []
    for path in (service_path, timer_path):
        if path.exists():
            path.unlink()
            removed.append(str(path))

    if _HAS_SYSTEMCTL:
        subprocess.run(["systemctl", "--user", "daemon-reload"], check=True)

    if removed:
        return "Removed: " + ", ".join(removed) + ".  daemon-reload OK."
    return "Nothing to remove — service files were not installed."


# ---------------------------------------------------------------------------
# Credentials
# ---------------------------------------------------------------------------


def _credentials_present() -> bool:
    """Return True if a token is set and at least one account is registered."""
    from dotenv import load_dotenv

    load_dotenv(ENV_FILE)
    token = os.environ.get("PUBLIC_ACCESS_TOKEN") or os.environ.get(
        "PUBLIC_API_SECRET_KEY"
    )
    return bool(token and get_accounts())


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


# ---------------------------------------------------------------------------
# Rebalancer config I/O
# ---------------------------------------------------------------------------


def _load_rebalance_config(account_id: str) -> dict:
    try:
        return json.loads(get_rebalance_config_path(account_id).read_text())
    except (FileNotFoundError, json.JSONDecodeError, OSError):
        return {"index": "SP500", "top_n": 500, "rebalance_enabled": True}


def _save_rebalance_config(
    account_id: str,
    index: str,
    top_n: int,
    margin_usage_pct: float,
    excluded_tickers: list[str],
    allocations: dict[str, float],
    rebalance_enabled: bool = True,
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
                "rebalance_enabled": rebalance_enabled,
            },
            indent=2,
        )
    )
