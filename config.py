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
    return Path(__file__).parent


_APP_DIR = _app_dir()

CACHE_DIR = _APP_DIR / "cache"
PORTFOLIO_CACHE = CACHE_DIR / "portfolio_cache.json"
MARKET_CAP_CACHE_FILE = CACHE_DIR / "market_caps.json"
REBALANCE_LOG_FILE = CACHE_DIR / "rebalance.log"
TODAY_BUYS_FILE = CACHE_DIR / "today_buys.json"
ENV_FILE = _APP_DIR / ".env"
SKIP_FILE = CACHE_DIR / "skip_next_rebalance"
REBALANCE_CONFIG_FILE = _APP_DIR / "rebalance_config.json"


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
    """Return True if both required env vars are set (from .env or environment)."""
    from dotenv import load_dotenv

    load_dotenv(ENV_FILE)
    token = os.environ.get("PUBLIC_ACCESS_TOKEN") or os.environ.get(
        "PUBLIC_API_SECRET_KEY"
    )
    return bool(token and os.environ.get("PUBLIC_ACCOUNT_NUMBER"))


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


# ---------------------------------------------------------------------------
# Rebalancer config I/O
# ---------------------------------------------------------------------------


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
