#!/usr/bin/env python3
"""
Portfolio daily rebalancer.

Default target allocation (configurable via TUI settings)
  65%  Top-N index stocks, market-cap weighted within that slice
  15%  Bitcoin (BTC)
   5%  Ethereum (ETH)
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
from concurrent.futures import TimeoutError as _FuturesTimeoutError
from datetime import date, datetime, timedelta
from decimal import Decimal, InvalidOperation, ROUND_DOWN
from pathlib import Path

import pandas as pd
import yfinance as yf
from public_api_sdk import (
    InstrumentType,
    OrderExpirationRequest,
    OrderInstrument,
    OrderRequest,
    OrderSide,
    OrderType,
    TimeInForce,
)

from client import (
    get_client,
    get_tradable_instrument_symbols,
    validate_order_instrument,
)
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

# ---------------------------------------------------------------------------
# Allocation config
# ---------------------------------------------------------------------------

# Default allocations — overridden at runtime by load_allocation_config()
_DEFAULT_ALLOCS: dict[str, Decimal] = {
    "stocks": Decimal("0.65"),  # index stocks, market-cap weighted
    "btc": Decimal("0.15"),  # Bitcoin
    "eth": Decimal("0.05"),  # Ethereum
    "gold": Decimal("0.10"),  # GLDM ETF
    "cash": Decimal("0.05"),  # uninvested cash (no orders placed)
}
# Keep module-level names for use outside rebalance() (dry-run scripts, etc.)
ALLOC_STOCKS = _DEFAULT_ALLOCS["stocks"]
ALLOC_BTC = _DEFAULT_ALLOCS["btc"]
ALLOC_ETH = _DEFAULT_ALLOCS["eth"]
ALLOC_GOLD = _DEFAULT_ALLOCS["gold"]

SP500_TOP_N = 500  # default: full index (capped by actual constituent count)
GOLD_SYMBOL = "GLDM"
BTC_SYMBOL = "BTC"
ETH_SYMBOL = "ETH"
NON_STOCK_ETFS = {GOLD_SYMBOL}  # equity symbols excluded from the stock index slice

# ---------------------------------------------------------------------------
# Operational config
# ---------------------------------------------------------------------------

MARKET_CAP_CACHE_MAX_AGE_HOURS = 20  # same-day cache; refresh each noon run
MARKET_CAP_FETCH_WORKERS = 20  # parallel threads for fast_info calls
MARKET_CAP_FETCH_TIMEOUT_SECONDS = (
    300  # hard wall-clock deadline for all workers combined
)
YFINANCE_DOWNLOAD_TIMEOUT_SECONDS = 15
CRYPTO_PRICE_FETCH_TIMEOUT_SECONDS = YFINANCE_DOWNLOAD_TIMEOUT_SECONDS * 2

MIN_ORDER_DOLLARS = Decimal("5.00")  # Public.com API enforces a $5 minimum per order
MIN_CRYPTO_ORDER_DOLLARS = Decimal("1.00")  # minimum notional for any crypto order
REBALANCE_THRESHOLD_PCT = Decimal("0.005")  # only act if drift > 0.5% of target
BUYING_POWER_BUFFER = Decimal(
    "1.00"
)  # leave a small cushion to avoid broker-side shortfall errors
SELL_WAIT_TIMEOUT_SECONDS = 300  # wait up to 5 minutes for sell orders to clear
ORDER_STATUS_POLL_SECONDS = 2.0

# ---------------------------------------------------------------------------
# Logging
# ---------------------------------------------------------------------------

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s  %(levelname)-8s  %(message)s",
    datefmt="%Y-%m-%d %H:%M:%S",
    handlers=[logging.StreamHandler(sys.stdout)],
)
log = logging.getLogger(__name__)


def _attach_rebalance_log_file(log_path: Path) -> None:
    """Switch the file handler to log_path, closing any previous one."""
    root = logging.getLogger()
    resolved = str(log_path.resolve())
    for handler in list(root.handlers):
        if isinstance(handler, logging.FileHandler):
            if handler.baseFilename == resolved:
                return
            root.removeHandler(handler)
            handler.close()
    root.addHandler(logging.FileHandler(log_path))


# ---------------------------------------------------------------------------
# Rebalance config (index + top N), read at runtime
# ---------------------------------------------------------------------------

# Canonical index identifiers used in rebalance_config.json
_INDEX_SP500 = "SP500"
_INDEX_NASDAQ100 = "NASDAQ100"
_INDEX_DJIA = "DJIA"
_INDEX_VT = "FTSE_GLOBAL_ALL_CAP"

# Legacy ETF ticker → index name (for migrating old configs)
_ETF_TO_INDEX: dict[str, str] = {
    "SPY": _INDEX_SP500,
    "VOO": _INDEX_SP500,
    "IVV": _INDEX_SP500,
    "SPLG": _INDEX_SP500,
    "CSPX": _INDEX_SP500,
    "QQQ": _INDEX_NASDAQ100,
    "QQQM": _INDEX_NASDAQ100,
    "ONEQ": _INDEX_NASDAQ100,
    "DIA": _INDEX_DJIA,
    "VT": _INDEX_VT,
}


SUPPORTED_INDEXES: dict[str, str] = {
    _INDEX_SP500: "S&P 500",
    _INDEX_NASDAQ100: "NASDAQ-100",
    _INDEX_DJIA: "Dow Jones (DJIA)",
    _INDEX_VT: "Global equities (ACWI proxy)",
}


def _first_account_path(path_fn) -> Path | None:
    accounts = get_accounts()
    if not accounts:
        return None
    return path_fn(accounts[0])


def _load_config_json(config_file: Path | None = None) -> dict:
    """Load rebalance_config.json once; returns {} on any error."""
    if config_file is None:
        config_file = _first_account_path(get_rebalance_config_path)
        if config_file is None:
            return {}
    try:
        return json.loads(config_file.read_text())
    except (FileNotFoundError, json.JSONDecodeError, OSError):
        return {}


def load_rebalance_config(
    _cfg: dict | None = None,
) -> tuple[str, int, Decimal, frozenset[str]]:
    """Return (index, top_n, margin_usage_pct, excluded_tickers) from rebalance_config.json.

    index is one of: SP500, NASDAQ100, DJIA, FTSE_GLOBAL_ALL_CAP.
    Old configs using etf_ticker (SPY, QQQ, DIA, etc.) are migrated automatically.

    margin_usage_pct: 0.0 = cash only, 0.5 = 50% of margin capacity, 1.0 = full margin.
    excluded_tickers: symbols blocked from new buys; existing positions are liquidated.
    _cfg: pre-loaded config dict (avoids a second file read when called alongside load_allocation_config).
    """
    try:
        cfg = _cfg if _cfg is not None else _load_config_json()
        # Prefer new "index" key; fall back to legacy "etf_ticker" and migrate
        if "index" in cfg:
            index = str(cfg["index"]).upper().strip()
        else:
            legacy = str(cfg.get("etf_ticker", "SPY")).upper().strip()
            index = _ETF_TO_INDEX.get(legacy, _INDEX_SP500)
        if index not in SUPPORTED_INDEXES:
            index = _INDEX_SP500
        top_n = int(cfg.get("top_n", SP500_TOP_N))
        raw_margin_usage_pct = cfg.get("margin_usage_pct", "0.5")
        try:
            margin_usage_pct = (
                Decimal(str(raw_margin_usage_pct)).max(Decimal("0")).min(Decimal("1"))
            )
        except (InvalidOperation, ValueError, TypeError):
            log.warning(
                "Invalid margin_usage_pct=%r in rebalance config — using default 0.5.",
                raw_margin_usage_pct,
            )
            margin_usage_pct = Decimal("0.5")
        excluded_tickers = frozenset(
            str(t).upper().strip()
            for t in cfg.get("excluded_tickers", [])
            if str(t).strip()
        )
        return index, max(1, top_n), margin_usage_pct, excluded_tickers
    except (ValueError, KeyError, TypeError):
        return _INDEX_SP500, SP500_TOP_N, Decimal("0.5"), frozenset()


# ---------------------------------------------------------------------------
# Allocation config
# ---------------------------------------------------------------------------


def load_allocation_config(_cfg: dict | None = None) -> dict[str, Decimal]:
    """Return allocation fractions from rebalance_config.json.

    Keys: stocks, btc, eth, gold, cash.  Values sum to 1.0.
    'cash' is the uninvested fraction — no orders are placed for it.
    Falls back to _DEFAULT_ALLOCS if the config is missing, invalid, or doesn't sum to 1.
    _cfg: pre-loaded config dict (avoids a second file read when called alongside load_rebalance_config).
    """
    try:
        cfg = _cfg if _cfg is not None else _load_config_json()
        raw = cfg.get("allocations", {})
        if not raw:
            return _DEFAULT_ALLOCS.copy()
        allocs = {
            k: Decimal(str(raw.get(k, _DEFAULT_ALLOCS[k]))) for k in _DEFAULT_ALLOCS
        }
        if any(v < Decimal("0") or v > Decimal("1") for v in allocs.values()):
            log.warning(
                "One or more allocations are out of range (0..1) — using defaults."
            )
            return _DEFAULT_ALLOCS.copy()
        total = sum(allocs.values())
        if abs(total - Decimal("1.00")) > Decimal("0.005"):
            log.warning("Allocations sum to %.4f (not 1.0) — using defaults.", total)
            return _DEFAULT_ALLOCS.copy()
        return allocs
    except Exception:
        return _DEFAULT_ALLOCS.copy()


# ---------------------------------------------------------------------------
# Daily buy ledger  (day-trade prevention)
# ---------------------------------------------------------------------------


def load_today_buys(today_buys_file: Path | None = None) -> frozenset[str]:
    """
    Return the set of equity symbols that were bought in any rebalance run today.
    Used to prevent selling a position on the same day it was purchased (day trade).
    """
    if today_buys_file is None:
        today_buys_file = _first_account_path(get_today_buys_path)
        if today_buys_file is None:
            return frozenset()
    try:
        data = json.loads(today_buys_file.read_text())
        if data.get("date") == date.today().isoformat():
            return frozenset(data.get("symbols", []))
    except (FileNotFoundError, json.JSONDecodeError, KeyError):
        pass
    return frozenset()


def record_today_buys(symbols: set[str], today_buys_file: Path | None = None) -> None:
    """Append equity symbols to today's buy ledger (creates or updates the file)."""
    if not symbols:
        return
    if today_buys_file is None:
        today_buys_file = _first_account_path(get_today_buys_path)
        if today_buys_file is None:
            return
    existing = set(load_today_buys(today_buys_file))
    existing.update(symbols)
    today_buys_file.write_text(
        json.dumps(
            {
                "date": date.today().isoformat(),
                "symbols": sorted(existing),
            }
        )
    )
    log.info(
        "Day-trade ledger updated: %d symbol(s) bought today total.", len(existing)
    )


# ---------------------------------------------------------------------------
# Index constituents
# ---------------------------------------------------------------------------

_UA = "Mozilla/5.0 (compatible; public-terminal/1.0)"


def _fetch_bytes(url: str, extra_headers: dict | None = None) -> bytes:
    """Fetch URL with a shared User-Agent, returning raw bytes."""
    headers = {"User-Agent": _UA, **(extra_headers or {})}
    req = urllib.request.Request(url, headers=headers)
    with urllib.request.urlopen(req, timeout=30) as resp:
        return resp.read()


def _clean_tickers(raw: list) -> list[str]:
    """Strip whitespace and drop empty / placeholder values from a ticker list."""
    return [
        str(t).strip()
        for t in raw
        if isinstance(t, str) and str(t).strip() not in ("", "-", "N/A")
    ]


def _first_table_column(
    tables: list[pd.DataFrame],
    candidate_columns: tuple[str, ...],
    label: str,
) -> tuple[pd.DataFrame, str]:
    """Return the first Wikipedia table containing one of the expected ticker columns."""
    if not tables:
        raise RuntimeError(f"Could not find {label} constituent table on Wikipedia")
    for df in tables:
        for col in candidate_columns:
            if col in df.columns:
                return df, col
    expected = ", ".join(candidate_columns)
    raise RuntimeError(
        f"Could not find {label} constituent table with expected column(s): {expected}"
    )


# --- official sources ---


def _fetch_sp500_tickers_official() -> list[str]:
    """Fetch S&P 500 constituents from iShares IVV daily holdings CSV.

    iShares CSV structure: 9 metadata rows, then a header row, then data.
    We keep only rows where Asset Class == "Equity" to drop cash/futures lines.
    """
    url = (
        "https://www.ishares.com/us/products/239726/ISHARES-CORE-SP-500-ETF"
        "/1467271812596.ajax?fileType=csv&fileName=IVV_holdings&dataType=fund"
    )
    content = _fetch_bytes(url).decode("utf-8")
    df = pd.read_csv(io.StringIO(content), skiprows=9)
    if "Asset Class" in df.columns:
        df = df[df["Asset Class"].str.contains("Equity", na=False)]
    return _clean_tickers(df["Ticker"].tolist())


def _fetch_nasdaq100_tickers_official() -> list[str]:
    """Fetch NASDAQ-100 constituents from Invesco QQQ holdings JSON API.

    The API returns all holdings including index futures (securityTypeCode=IFUT)
    and cash entries — we filter to equity-only rows.
    """
    url = (
        "https://dng-api.invesco.com/cache/v1/accounts/en_US/shareclasses"
        "/QQQ/holdings/fund?idType=ticker&productType=ETF"
    )
    data = json.loads(
        _fetch_bytes(
            url, {"Referer": "https://www.invesco.com/qqq-etf/en/about.html"}
        ).decode("utf-8")
    )
    tickers = [
        h["ticker"]
        for h in data["holdings"]
        if h.get("ticker")
        and h.get("securityTypeCode") not in ("IFUT", "CASH", "FXFWD")
        and str(h["ticker"]).replace(".", "").isalpha()
    ]
    return _clean_tickers(tickers)


def _fetch_djia_tickers_official() -> list[str]:
    """Fetch DJIA constituents from SSGA DIA daily holdings xlsx.

    SSGA xlsx structure: 4 metadata rows, then a header row (Name, Ticker, ...),
    then data. We skip 4 rows so row 4 becomes the column header.
    """
    url = (
        "https://www.ssga.com/us/en/intermediary/etfs/library-content/products"
        "/fund-data/etfs/us/holdings-daily-us-en-dia.xlsx"
    )
    df = pd.read_excel(
        io.BytesIO(_fetch_bytes(url, {"Referer": "https://www.ssga.com/"})),
        skiprows=4,
        engine="openpyxl",
    )
    return _clean_tickers(df["Ticker"].tolist())


# --- Wikipedia fallbacks ---


def _fetch_sp500_tickers_wikipedia() -> list[str]:
    """Scrape the S&P 500 constituent list from Wikipedia (fallback)."""
    log.info("  Falling back to Wikipedia for S&P 500 constituents…")
    html = _fetch_bytes(
        "https://en.wikipedia.org/wiki/List_of_S%26P_500_companies"
    ).decode("utf-8")
    try:
        tables = pd.read_html(io.StringIO(html), attrs={"id": "constituents"})
    except ValueError as exc:
        raise RuntimeError(
            "Could not parse S&P 500 constituent table from Wikipedia"
        ) from exc
    df, col = _first_table_column(tables, ("Symbol",), "S&P 500")
    tickers = _clean_tickers(df[col].tolist())
    if not tickers:
        raise RuntimeError(
            "S&P 500 Wikipedia constituent table contained no usable tickers"
        )
    return tickers


def _fetch_nasdaq100_tickers_wikipedia() -> list[str]:
    """Scrape the NASDAQ-100 constituent list from Wikipedia (fallback)."""
    log.info("  Falling back to Wikipedia for NASDAQ-100 constituents…")
    html = _fetch_bytes("https://en.wikipedia.org/wiki/Nasdaq-100").decode("utf-8")
    try:
        tables = pd.read_html(io.StringIO(html), attrs={"id": "constituents"})
    except ValueError as exc:
        raise RuntimeError(
            "Could not parse NASDAQ-100 constituent table from Wikipedia"
        ) from exc
    df, col = _first_table_column(tables, ("Ticker", "Symbol"), "NASDAQ-100")
    tickers = _clean_tickers(df[col].tolist())
    if not tickers:
        raise RuntimeError(
            "NASDAQ-100 Wikipedia constituent table contained no usable tickers"
        )
    return tickers


def _fetch_djia_tickers_wikipedia() -> list[str]:
    """Scrape the DJIA constituent list from Wikipedia (fallback)."""
    log.info("  Falling back to Wikipedia for DJIA constituents…")
    html = _fetch_bytes(
        "https://en.wikipedia.org/wiki/Dow_Jones_Industrial_Average"
    ).decode("utf-8")
    tables = pd.read_html(io.StringIO(html))
    for df in tables:
        for col in ("Symbol", "Ticker"):
            if col in df.columns:
                tickers = _clean_tickers(
                    [
                        t
                        for t in df[col].tolist()
                        if isinstance(t, str) and t.replace(".", "").isalpha()
                    ]
                )
                if len(tickers) >= 20:
                    return tickers
    raise RuntimeError("Could not find DJIA constituent table on Wikipedia")


def _fetch_vt_tickers_official() -> list[str]:
    """Fetch a global equity proxy from iShares MSCI ACWI daily holdings CSV.

    iShares CSV: 9 metadata rows, then a header row, then data.
    We keep only Equity rows whose ticker is purely alphabetic (A–Z, 1–5 chars).
    Non-US holdings use numeric or exchange-qualified tickers ("2330", "005930"),
    which are dropped by the alpha filter.  Foreign companies whose primary-listing
    ticker matches their US ADR ticker (ASML, AZN, SHEL, SHOP, SAP, RY, …) pass
    through and resolve to their US listing when yfinance fetches market caps;
    purely-foreign tickers that have no matching US listing fail silently at the
    market-cap stage and are naturally excluded from top-N selection.
    """
    url = (
        "https://www.ishares.com/us/products/239600/ISHARES-MSCI-ACWI-ETF"
        "/1467271812596.ajax?fileType=csv&fileName=ACWI_holdings&dataType=fund"
    )
    content = _fetch_bytes(url).decode("utf-8")
    df = pd.read_csv(io.StringIO(content), skiprows=9)
    if "Asset Class" in df.columns:
        df = df[df["Asset Class"].str.contains("Equity", na=False)]
    raw = _clean_tickers(df["Ticker"].tolist())
    # Drop numeric/exchange-qualified foreign tickers; keep US-style alphabetic ones
    return [t for t in raw if t.isalpha() and len(t) <= 5]


# --- public fetch functions ---


def _fetch_sp500_tickers() -> list[str]:
    log.info("Fetching S&P 500 constituents (iShares IVV)…")
    try:
        tickers = _fetch_sp500_tickers_official()
        log.info("  → %d constituents", len(tickers))
        return tickers
    except Exception as exc:
        log.warning("iShares fetch failed (%s) — falling back to Wikipedia.", exc)
        tickers = _fetch_sp500_tickers_wikipedia()
        log.info("  → %d constituents (Wikipedia)", len(tickers))
        return tickers


def _fetch_nasdaq100_tickers() -> list[str]:
    log.info("Fetching NASDAQ-100 constituents (Invesco QQQ)…")
    try:
        tickers = _fetch_nasdaq100_tickers_official()
        log.info("  → %d constituents", len(tickers))
        return tickers
    except Exception as exc:
        log.warning("Invesco fetch failed (%s) — falling back to Wikipedia.", exc)
        tickers = _fetch_nasdaq100_tickers_wikipedia()
        log.info("  → %d constituents (Wikipedia)", len(tickers))
        return tickers


def _fetch_djia_tickers() -> list[str]:
    log.info("Fetching DJIA constituents (SSGA DIA)…")
    try:
        tickers = _fetch_djia_tickers_official()
        log.info("  → %d constituents", len(tickers))
        return tickers
    except Exception as exc:
        log.warning("SSGA fetch failed (%s) — falling back to Wikipedia.", exc)
        tickers = _fetch_djia_tickers_wikipedia()
        log.info("  → %d constituents (Wikipedia)", len(tickers))
        return tickers


def _fetch_vt_tickers() -> list[str]:
    log.info("Fetching global equity proxy constituents (iShares ACWI)…")
    tickers = _fetch_vt_tickers_official()
    log.info("  → %d US-listed constituents", len(tickers))
    return tickers


def fetch_constituents(index: str) -> list[str]:
    """Return the constituent list for the given index identifier."""
    if index == _INDEX_NASDAQ100:
        return _fetch_nasdaq100_tickers()
    if index == _INDEX_DJIA:
        return _fetch_djia_tickers()
    if index == _INDEX_SP500:
        return _fetch_sp500_tickers()
    if index == _INDEX_VT:
        return _fetch_vt_tickers()
    raise ValueError(
        f"Unsupported index '{index}'. Supported values: {', '.join(SUPPORTED_INDEXES)}"
    )


# ---------------------------------------------------------------------------
# Market caps (daily cache, parallel fetch)
# ---------------------------------------------------------------------------


def _load_market_cap_cache(
    index: str, market_cap_cache_file: Path | None = None
) -> dict[str, float] | None:
    if market_cap_cache_file is None:
        market_cap_cache_file = _first_account_path(get_market_cap_cache_path)
        if market_cap_cache_file is None:
            return None
    try:
        data = json.loads(market_cap_cache_file.read_text())
        raw_caps = data.get("caps")
        if not isinstance(raw_caps, dict):
            log.warning("Market cap cache has invalid caps payload — discarding.")
            return None
        if not raw_caps:
            log.info("Market cap cache is empty — discarding.")
            return None
        source_ticker_count = data.get("source_ticker_count")
        if not isinstance(source_ticker_count, int) or source_ticker_count < 1:
            log.info("Market cap cache missing coverage metadata — discarding.")
            return None
        min_required = max(1, source_ticker_count // 2)
        if len(raw_caps) < min_required:
            log.info(
                "Market cap cache coverage too low (%d/%d, threshold: %d) — discarding.",
                len(raw_caps),
                source_ticker_count,
                min_required,
            )
            return None
        # Support both new "index" key and legacy "etf_ticker" key in cache
        cached_raw = data.get("index") or _ETF_TO_INDEX.get(
            data.get("etf_ticker", ""), _INDEX_SP500
        )
        if cached_raw != index:
            log.info(
                "Index changed (%s → %s) — discarding market cap cache.",
                cached_raw,
                index,
            )
            return None
        age = datetime.now() - datetime.fromisoformat(data["updated_at"])
        if age > timedelta(hours=MARKET_CAP_CACHE_MAX_AGE_HOURS):
            log.info(
                "Market cap cache is %.1f hours old — will refresh.",
                age.total_seconds() / 3600,
            )
            return None
        log.info(
            "Using cached market caps (%d entries, %.0f min old).",
            len(raw_caps),
            age.total_seconds() / 60,
        )
        # Older caches stored yfinance-style share-class tickers (e.g. BRK-B).
        return {
            YF_TO_BROKER_SYMBOLS.get(symbol, symbol): cap
            for symbol, cap in raw_caps.items()
        }
    except Exception as exc:
        log.warning("Could not read market cap cache: %s", exc)
        return None


def _save_market_cap_cache(
    caps: dict[str, float],
    index: str,
    source_ticker_count: int,
    market_cap_cache_file: Path | None = None,
) -> None:
    if market_cap_cache_file is None:
        market_cap_cache_file = _first_account_path(get_market_cap_cache_path)
        if market_cap_cache_file is None:
            return
    market_cap_cache_file.write_text(
        json.dumps(
            {
                "updated_at": datetime.now().isoformat(),
                "index": index,
                "source_ticker_count": source_ticker_count,
                "caps": caps,
            },
            indent=2,
        )
    )
    log.info("Market cap cache saved (%d entries).", len(caps))


def _fetch_one_market_cap(ticker: str) -> tuple[str, float | None]:
    yf_ticker = BROKER_TO_YF_SYMBOLS.get(ticker, ticker)
    for attempt in range(3):
        try:
            mc = yf.Ticker(yf_ticker).fast_info.market_cap
            if mc and float(mc) > 0:
                return ticker, float(mc)
        except Exception:
            if attempt < 2:
                time.sleep(1.0 * (attempt + 1))
    return ticker, None


def fetch_market_caps(
    tickers: list[str], index: str, market_cap_cache_file: Path | None = None
) -> dict[str, float]:
    """
    Return {ticker: market_cap} for the given tickers.
    Same-day results are served from a local cache; otherwise fetched in parallel
    using fast_info (~30–60 s for 500 stocks with 20 workers).
    Cache is invalidated automatically when the index changes.
    """
    cached = _load_market_cap_cache(index, market_cap_cache_file)
    if cached is not None:
        return cached

    log.info(
        "Fetching market caps for %d tickers (%d workers)…",
        len(tickers),
        MARKET_CAP_FETCH_WORKERS,
    )
    caps: dict[str, float] = {}
    done = 0
    pool = ThreadPoolExecutor(max_workers=MARKET_CAP_FETCH_WORKERS)
    timed_out = False
    try:
        futures = {pool.submit(_fetch_one_market_cap, t): t for t in tickers}
        try:
            for future in as_completed(
                futures, timeout=MARKET_CAP_FETCH_TIMEOUT_SECONDS
            ):
                ticker, mc = future.result()
                if mc:
                    caps[ticker] = mc
                done += 1
                if done % 100 == 0:
                    log.info("  … %d / %d done", done, len(tickers))
        except _FuturesTimeoutError:
            timed_out = True
            log.warning(
                "Market cap fetch timed out after %ds — %d / %d tickers completed.",
                MARKET_CAP_FETCH_TIMEOUT_SECONDS,
                len(caps),
                len(tickers),
            )
    finally:
        if timed_out:
            pool.shutdown(wait=False, cancel_futures=True)
        else:
            pool.shutdown(wait=True)

    log.info("  → market caps for %d / %d tickers", len(caps), len(tickers))
    min_required = max(1, len(tickers) // 2)
    if len(caps) >= min_required:
        _save_market_cap_cache(caps, index, len(tickers), market_cap_cache_file)
    else:
        log.warning(
            "Market cap fetch returned only %d / %d results (threshold: %d) — skipping cache write.",
            len(caps),
            len(tickers),
            min_required,
        )
    return caps


# ---------------------------------------------------------------------------
# Top-N selection and weight computation
# ---------------------------------------------------------------------------


def top_n_by_market_cap(
    tickers: list[str],
    market_caps: dict[str, float],
    n: int,
) -> list[str]:
    """Return the top-N index tickers ranked by market cap."""
    ranked = sorted(
        [t for t in tickers if t in market_caps],
        key=lambda t: market_caps[t],
        reverse=True,
    )
    top = ranked[:n]
    if not top:
        log.error(
            "No usable market caps found for %d constituent(s); cannot select top %d.",
            len(tickers),
            n,
        )
        return []
    log.info(
        "Top %d stocks: largest=%s ($%.2fT), smallest=%s ($%.2fB)",
        len(top),
        top[0],
        market_caps[top[0]] / 1e12,
        top[-1],
        market_caps[top[-1]] / 1e9,
    )
    return top


def rank_by_market_cap(
    tickers: list[str],
    market_caps: dict[str, float],
) -> list[str]:
    """Return all tickers with usable market caps, ranked largest first."""
    return sorted(
        [t for t in tickers if t in market_caps],
        key=lambda t: market_caps[t],
        reverse=True,
    )


def select_public_tradable_stocks(
    client,
    tickers: list[str],
    market_caps: dict[str, float],
    n: int,
    excluded_tickers: frozenset[str],
    public_buyable_symbols: set[str] | None = None,
) -> list[str]:
    """Select top-N stocks that exist on Public and are buyable before planning orders."""
    ranked = rank_by_market_cap(tickers, market_caps)
    if not ranked:
        log.error(
            "No usable market caps found for %d constituent(s); cannot select top %d.",
            len(tickers),
            n,
        )
        return []

    selected: list[str] = []
    excluded_seen: list[str] = []
    untradable_seen: list[str] = []
    if public_buyable_symbols is None:
        public_buyable_symbols = get_tradable_instrument_symbols(
            client, InstrumentType.EQUITY, OrderSide.BUY
        )
        log.info(
            "Loaded %d Public-buyable equity symbol(s) for basket validation.",
            len(public_buyable_symbols),
        )
    for symbol in ranked:
        if symbol not in public_buyable_symbols:
            untradable_seen.append(symbol)
            log.info("  Skipping %s — not buyable or missing on Public.", symbol)
            continue

        if symbol in excluded_tickers:
            excluded_seen.append(symbol)
            log.info("  Skipping %s — excluded by config after Public validation.", symbol)
            continue

        selected.append(symbol)
        if len(selected) >= n:
            break

    if selected:
        log.info(
            "Top %d Public-tradable stocks: largest=%s ($%.2fT), smallest=%s ($%.2fB)",
            len(selected),
            selected[0],
            market_caps[selected[0]] / 1e12,
            selected[-1],
            market_caps[selected[-1]] / 1e9,
        )
    if excluded_seen:
        log.info(
            "  Excluded after Public validation (%d): %s",
            len(excluded_seen),
            ", ".join(excluded_seen[:10]) + ("…" if len(excluded_seen) > 10 else ""),
        )
    if untradable_seen:
        log.warning(
            "  Skipped %d non-buyable/Public-missing constituent(s): %s",
            len(untradable_seen),
            ", ".join(untradable_seen[:10])
            + ("…" if len(untradable_seen) > 10 else ""),
        )
    if len(selected) < n:
        log.warning(
            "  Only %d Public-buyable replacement stock(s) available; holding %d instead of %d positions.",
            len(selected),
            len(selected),
            n,
        )
    return selected


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
# Broker error classification
# ---------------------------------------------------------------------------


class PatternDayTradingError(RuntimeError):
    """Raised when the broker rejects an order due to PDT restrictions."""


def _is_pdt_error(exc: Exception) -> bool:
    msg = str(exc).lower()
    return any(
        kw in msg
        for kw in (
            "pattern day trad",
            "pdt",
            "day trade limit",
            "day trading restriction",
            "flagged as a pattern day trader",
        )
    )


def _is_intraday_margin_error(exc: Exception) -> bool:
    msg = str(exc).lower()
    return any(
        kw in msg
        for kw in (
            "intraday margin",
            "intraday buying power",
            "margin call",
            "margin maintenance",
            "day trading margin",
            "margin deficiency",
        )
    )


# ---------------------------------------------------------------------------
# Portfolio snapshot
# ---------------------------------------------------------------------------


def get_portfolio_snapshot(
    client,
) -> tuple[
    Decimal,
    Decimal,
    Decimal,
    Decimal,
    dict[str, Decimal],
    dict[str, Decimal],
    dict[str, Decimal],
    dict[str, Decimal],
]:
    """
    Fetch current portfolio from Public.com.

    Returns:
        total_equity           — sum of all non-cash asset values (investments only; cash excluded)
        cash_balance           — broker-reported CASH position value; negative means margin debt
        buying_power           — total available buying power (cash + margin if enabled)
        cash_only_buying_power — buying power from settled cash only (no margin)
        equity_pos             — {symbol: current_value} for EQUITY positions
        crypto_pos             — {symbol: current_value} for CRYPTO positions
        equity_qty             — {symbol: share_quantity} for EQUITY positions
        crypto_qty             — {symbol: coin_quantity} for CRYPTO positions
    """
    portfolio = client.get_portfolio()
    # Exclude CASH-type entries so total_equity = invested-asset value only.
    # Cash is captured via cash_only_buying_power, and the two are combined
    # into portfolio_nav in rebalance() to compute the true NAV.
    total_equity = sum(
        e.value for e in portfolio.equity if e.type.value != "CASH"
    )
    cash_balance = sum(
        (e.value for e in portfolio.equity if e.type.value == "CASH"),
        Decimal("0"),
    )
    buying_power_obj = getattr(portfolio, "buying_power", None)
    raw_buying_power = getattr(buying_power_obj, "buying_power", None)
    raw_cash_only_buying_power = getattr(
        buying_power_obj, "cash_only_buying_power", None
    )
    buying_power = (
        Decimal(str(raw_buying_power)) if raw_buying_power is not None else Decimal("0")
    )
    cash_only_buying_power = (
        Decimal(str(raw_cash_only_buying_power))
        if raw_cash_only_buying_power is not None
        else buying_power
    )

    equity_pos: dict[str, Decimal] = {}
    equity_qty: dict[str, Decimal] = {}
    crypto_pos: dict[str, Decimal] = {}
    crypto_qty: dict[str, Decimal] = {}
    for pos in portfolio.positions:
        if pos.instrument.type == InstrumentType.EQUITY:
            if pos.current_value is not None:
                equity_pos[pos.instrument.symbol] = pos.current_value
            if pos.quantity is not None:
                equity_qty[pos.instrument.symbol] = Decimal(str(pos.quantity))
        elif pos.instrument.type == InstrumentType.CRYPTO:
            if pos.current_value is not None:
                crypto_pos[pos.instrument.symbol] = pos.current_value
            if pos.quantity is not None:
                crypto_qty[pos.instrument.symbol] = Decimal(str(pos.quantity))

    return (
        total_equity,
        cash_balance,
        buying_power,
        cash_only_buying_power,
        equity_pos,
        crypto_pos,
        equity_qty,
        crypto_qty,
    )


def estimate_margin_state(
    total_equity: Decimal,
    cash_balance: Decimal,
    buying_power: Decimal,
    cash_only_buying_power: Decimal,
    margin_usage_pct: Decimal,
) -> tuple[Decimal, Decimal, Decimal, Decimal, Decimal]:
    """
    Return (portfolio_nav, current_margin_loan, allowed_margin_loan,
    investment_base, effective_buying_power).

    The configured margin percentage is applied to margin buying-power capacity:
    current margin debt plus currently available margin buying power. Existing
    margin debt, including withdrawal loans, consumes that allowance before any
    new buy orders are allowed.
    """
    margin_buying_power = max(Decimal("0"), buying_power - cash_only_buying_power)
    margin_available = margin_buying_power > Decimal("0") or cash_balance < Decimal("0")
    if cash_balance < Decimal("0"):
        current_margin_loan = -cash_balance
    elif margin_available:
        current_margin_loan = max(
            Decimal("0"),
            total_equity + cash_balance - margin_buying_power,
        )
    else:
        current_margin_loan = Decimal("0")

    portfolio_nav = max(Decimal("0"), total_equity + cash_balance)
    margin_capacity = current_margin_loan + margin_buying_power
    allowed_margin_loan = (
        margin_usage_pct * margin_capacity if margin_available else Decimal("0")
    )
    investment_base = portfolio_nav + allowed_margin_loan
    effective_buying_power = max(
        Decimal("0"),
        cash_only_buying_power + allowed_margin_loan - current_margin_loan,
    )
    return (
        portfolio_nav,
        current_margin_loan,
        allowed_margin_loan,
        investment_base,
        effective_buying_power,
    )


# ---------------------------------------------------------------------------
# Order helpers
# ---------------------------------------------------------------------------


def _make_order(
    symbol: str,
    instrument_type: InstrumentType,
    side: OrderSide,
    dollar_amount: Decimal,
    crypto_price: Decimal | None = None,
    equity_quantity: Decimal | None = None,
    crypto_held_quantity: Decimal | None = None,
) -> OrderRequest:
    """
    Build a market order request.

    EQUITY orders normally use `amount` (dollar notional). When `equity_quantity`
    is supplied (full-liquidation sells), a share-quantity order is used instead —
    required by the broker when the notional amount is nearly equal to the full
    position value.
    CRYPTO orders require `quantity` (coin units) — the API does not support
    dollar-notional amounts for crypto. `crypto_price` must be provided for CRYPTO.
    For CRYPTO SELL orders, `crypto_held_quantity` caps the submitted quantity to
    the actual held balance, preventing broker rejection from rounding overages.
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
        quantity = (dollar_amount / crypto_price).quantize(
            Decimal("0.00001"), rounding=ROUND_DOWN
        )
        if quantity <= 0:
            raise ValueError(
                f"CRYPTO order quantity rounds to zero ({symbol}: ${dollar_amount} at ${crypto_price}/coin)"
            )
        if side == OrderSide.SELL and crypto_held_quantity is not None:
            quantity = min(quantity, crypto_held_quantity).quantize(
                Decimal("0.00001"), rounding=ROUND_DOWN
            )
            if quantity <= 0:
                raise ValueError(
                    f"CRYPTO SELL quantity rounds to zero after capping to held balance ({symbol})"
                )
        return OrderRequest(**base, quantity=quantity)
    # Equity — prefer quantity ordering for full liquidations
    if equity_quantity is not None:
        if equity_quantity <= 0:
            raise ValueError(
                f"Equity quantity is zero for full-liquidation sell ({symbol})"
            )
        return OrderRequest(**base, quantity=equity_quantity)
    amount = dollar_amount.quantize(Decimal("0.01"), rounding=ROUND_DOWN)
    if amount <= 0:
        raise ValueError(
            f"Equity order amount rounds to zero ({symbol}: ${dollar_amount})"
        )
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
        log.warning(
            "Could not fetch %s price from Public API: %s — falling back to yfinance",
            symbol,
            exc,
        )

    data = yf.download(
        yf_ticker,
        period="1d",
        auto_adjust=True,
        progress=False,
        timeout=YFINANCE_DOWNLOAD_TIMEOUT_SECONDS,
        multi_level_index=False,
    )
    if not data.empty:
        return Decimal(str(float(data["Close"].iloc[-1])))

    raise RuntimeError(f"Unable to fetch {symbol} price from any source")


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
            log.info(
                "  ✓ Cancelled %s %s (ID: %s)",
                order.side.value,
                order.instrument.symbol,
                order.order_id[:8],
            )
        except Exception as exc:
            log.warning(
                "  ✗ Could not cancel %s (ID: %s): %s",
                order.instrument.symbol,
                order.order_id[:8],
                exc,
            )
        time.sleep(0.05)


def place_orders(
    client,
    orders: list[tuple[str, InstrumentType, OrderSide, Decimal]],
    crypto_prices: dict[str, Decimal] | None = None,
    liquidation_quantities: dict[str, Decimal] | None = None,
    crypto_quantities: dict[str, Decimal] | None = None,
) -> tuple[list[str], list[tuple[str, InstrumentType, OrderSide, Decimal]]]:
    """
    Place a list of (symbol, instrument_type, side, dollar_amount) market orders.

    liquidation_quantities: {symbol: share_count} for equity SELL orders that
        must use share-quantity ordering (full-position liquidations where the
        broker rejects notional orders whose value ≈ full position value).
    crypto_quantities: {symbol: coin_count} held balances used to cap SELL
        quantities and prevent broker rejection from rounding overages.

    Returns (submitted_order_ids, submitted_orders) — only orders that were
    accepted by the broker appear in these lists, so callers can record only
    what actually went through (e.g. PDT ledger should only log real buys).

    Raises PatternDayTradingError immediately if the broker signals a PDT
    restriction, so the caller can abort the entire rebalance cleanly.
    Stops placing further orders and logs a warning on intraday margin errors.
    """
    success = fail = 0
    submitted_order_ids: list[str] = []
    submitted_orders: list[tuple[str, InstrumentType, OrderSide, Decimal]] = []
    for symbol, inst_type, side, dollar_amount in orders:
        try:
            validate_order_instrument(client, symbol, inst_type, side)
            price = (crypto_prices or {}).get(symbol)
            eq_qty = (
                (liquidation_quantities or {}).get(symbol)
                if inst_type == InstrumentType.EQUITY and side == OrderSide.SELL
                else None
            )
            crypto_held = (
                (crypto_quantities or {}).get(symbol)
                if inst_type == InstrumentType.CRYPTO and side == OrderSide.SELL
                else None
            )
            req = _make_order(
                symbol, inst_type, side, dollar_amount, price, eq_qty, crypto_held
            )
            new_order = client.place_order(req)
            if inst_type == InstrumentType.CRYPTO:
                log.info(
                    "  ✓ %-6s %-6s $%9.2f  (%s coins @ $%.2f)  order id: %s",
                    side.value,
                    symbol,
                    dollar_amount,
                    req.quantity,
                    price,
                    new_order.order_id[:8],
                )
            elif eq_qty is not None:
                log.info(
                    "  ✓ %-6s %-6s %s shares (liquidation)  order id: %s",
                    side.value,
                    symbol,
                    eq_qty,
                    new_order.order_id[:8],
                )
            else:
                log.info(
                    "  ✓ %-6s %-6s $%9.2f  order id: %s",
                    side.value,
                    symbol,
                    dollar_amount,
                    new_order.order_id[:8],
                )
            submitted_order_ids.append(new_order.order_id)
            submitted_orders.append((symbol, inst_type, side, dollar_amount))
            success += 1
        except Exception as exc:
            if _is_pdt_error(exc):
                log.error(
                    "  ✗ %-6s %-6s — PATTERN DAY TRADING restriction: %s",
                    side.value,
                    symbol,
                    exc,
                )
                log.error(
                    "PDT restriction detected — aborting remaining orders (%d placed so far).",
                    success,
                )
                fail += 1
                raise PatternDayTradingError(str(exc)) from exc
            if _is_intraday_margin_error(exc):
                log.warning(
                    "  ✗ %-6s %-6s — intraday margin limit reached: %s",
                    side.value,
                    symbol,
                    exc,
                )
                log.warning(
                    "Stopping further orders to avoid margin breach (%d placed so far).",
                    success,
                )
                fail += 1
                break
            log.error(
                "  ✗ %-6s %-6s $%9.2f  → %s", side.value, symbol, dollar_amount, exc
            )
            fail += 1
        time.sleep(0.1)
    log.info("Orders placed: %d  |  failed: %d", success, fail)
    return submitted_order_ids, submitted_orders


def filter_orders_by_public_tradability(
    client,
    orders: list[tuple[str, InstrumentType, OrderSide, Decimal]],
) -> list[tuple[str, InstrumentType, OrderSide, Decimal]]:
    """Remove orders Public reports as unavailable before any submission attempt."""
    valid_orders: list[tuple[str, InstrumentType, OrderSide, Decimal]] = []
    skipped: list[str] = []
    for symbol, inst_type, side, dollar_amount in orders:
        try:
            validate_order_instrument(client, symbol, inst_type, side)
        except ValueError as exc:
            skipped.append(symbol)
            log.warning(
                "Skipping %s %s before order submission — Public validation failed: %s",
                side.value,
                symbol,
                exc,
            )
            continue
        valid_orders.append((symbol, inst_type, side, dollar_amount))

    if skipped:
        log.warning(
            "Removed %d order(s) before submission after Public validation: %s",
            len(skipped),
            ", ".join(skipped[:10]) + ("…" if len(skipped) > 10 else ""),
        )
    return valid_orders


def log_dry_run_orders(
    orders: list[tuple[str, InstrumentType, OrderSide, Decimal]],
    *,
    label: str,
    max_rows: int = 25,
) -> None:
    """Log planned orders without submitting anything."""
    total = sum(order[3] for order in orders)
    log.info(
        "DRY RUN — would submit %d %s order(s), total notional $%.2f.",
        len(orders),
        label,
        total,
    )
    for symbol, inst_type, side, amount in orders[:max_rows]:
        log.info(
            "  DRY RUN  %-6s %-6s %-6s $%9.2f",
            side.value,
            symbol,
            inst_type.value,
            amount,
        )
    if len(orders) > max_rows:
        log.info("  DRY RUN  … %d additional %s order(s)", len(orders) - max_rows, label)


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
        log.info(
            "Waiting for %d %s order(s) to clear before continuing…",
            len(pending),
            label,
        )
        time.sleep(ORDER_STATUS_POLL_SECONDS)

    if pending:
        log.warning(
            "Timed out waiting for %d %s order(s) to clear.", len(pending), label
        )
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
        log.warning(
            "No buy orders will be placed: buying power is only $%.2f.", buying_power
        )
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
    log.info(
        "Buy budget: $%.2f available after buffer, $%.2f selected, $%.2f skipped.",
        available_budget,
        total_selected,
        total_skipped,
    )
    if skipped:
        skipped_symbols = ", ".join(symbol for symbol, *_ in skipped[:10])
        suffix = "…" if len(skipped) > 10 else ""
        log.warning(
            "Skipped %d buy order(s) that exceed buying power: %s%s",
            len(skipped),
            skipped_symbols,
            suffix,
        )
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
        return (
            symbol,
            instrument_type,
            OrderSide.SELL,
            abs(delta).quantize(Decimal("0.01")),
        )
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


def rebalance(dry_run: bool = False, account_id: str | None = None) -> None:
    resolved_account = (account_id or "").strip().upper()
    if not resolved_account:
        accounts = get_accounts()
        if not accounts:
            print(
                "No accounts configured. Run the TUI to set up an account.",
                file=sys.stderr,
            )
            sys.exit(1)
        enabled = [
            a for a in accounts
            if _load_config_json(get_rebalance_config_path(a)).get("rebalance_enabled", True)
        ]
        if not enabled:
            print("No accounts have rebalancing enabled.", file=sys.stderr)
            sys.exit(0)
        if len(enabled) > 1:
            for acct in enabled:
                rebalance(dry_run=dry_run, account_id=acct)
            return
        resolved_account = enabled[0]

    rebalance_config_file = get_rebalance_config_path(resolved_account)
    rebalance_log_file = get_rebalance_log_path(resolved_account)
    skip_file = get_skip_file_path(resolved_account)
    today_buys_file = get_today_buys_path(resolved_account)
    cache_dir = get_cache_dir(resolved_account)
    market_cap_cache_file = get_market_cap_cache_path(resolved_account)
    cache_dir.mkdir(exist_ok=True)
    _attach_rebalance_log_file(rebalance_log_file)

    log.info("=" * 64)
    mode = "DRY-RUN PORTFOLIO REBALANCE" if dry_run else "PORTFOLIO REBALANCE"
    log.info("%s  —  %s", mode, datetime.now().strftime("%Y-%m-%d %H:%M:%S"))
    if dry_run:
        log.info(
            "DRY RUN ENABLED — no orders will be placed, no orders will be cancelled, "
            "and the day-trade ledger will not be modified."
        )
    _cfg = _load_config_json(rebalance_config_file)
    index, top_n, margin_usage_pct, excluded_tickers = load_rebalance_config(_cfg)
    alloc = load_allocation_config(_cfg)
    alloc_stocks = alloc["stocks"]
    alloc_btc = alloc["btc"]
    alloc_eth = alloc["eth"]
    alloc_gold = alloc["gold"]
    alloc_cash = alloc["cash"]
    log.info(
        "  Allocation: %.0f%% stocks  |  %.0f%% BTC  |  %.0f%% ETH  |  %.0f%% gold  |  %.0f%% cash",
        alloc_stocks * 100,
        alloc_btc * 100,
        alloc_eth * 100,
        alloc_gold * 100,
        alloc_cash * 100,
    )
    log.info(
        "  Index: %s (%s) — top %d by market cap",
        index,
        SUPPORTED_INDEXES.get(index, index),
        top_n,
    )
    log.info("  Margin usage: %.0f%% of margin capacity", margin_usage_pct * 100)
    if excluded_tickers:
        log.info(
            "  Excluded tickers (%d): %s",
            len(excluded_tickers),
            ", ".join(sorted(excluded_tickers)),
        )
    log.info("=" * 64)

    if skip_file.exists() and dry_run:
        log.info(
            "DRY RUN — skip sentinel is present but was not removed; continuing to show plan."
        )
    elif skip_file.exists():
        skip_file.unlink()
        log.info(
            "SKIPPED — skip sentinel was set. Removed sentinel; next run will proceed normally."
        )
        return

    log.info("Connecting to Public.com…")
    client = get_client(resolved_account)
    try:
        log.info("--- Selecting Public-tradable stock basket ---")
        public_buyable_symbols = get_tradable_instrument_symbols(
            client, InstrumentType.EQUITY, OrderSide.BUY
        )
        log.info(
            "Loaded %d Public-buyable equity symbol(s) for basket validation.",
            len(public_buyable_symbols),
        )

        constituents = fetch_constituents(index)
        tradable_constituents = [t for t in constituents if t in public_buyable_symbols]
        log.info(
            "Filtered constituents to %d / %d Public-tradable tickers.",
            len(tradable_constituents),
            len(constituents),
        )
        market_caps = fetch_market_caps(tradable_constituents, index, market_cap_cache_file)

        top_stocks = select_public_tradable_stocks(
            client,
            tradable_constituents,
            market_caps,
            top_n,
            excluded_tickers,
            public_buyable_symbols,
        )
        if not top_stocks:
            log.error(
                "REBALANCE ABORTED — no Public-buyable top stocks could be selected."
            )
            return
        stock_weights = compute_stock_weights(top_stocks, market_caps)

        log.info("Fetching portfolio from Public.com…")
        initial_portfolio = client.get_portfolio()
        if dry_run:
            open_orders = [
                o
                for o in (initial_portfolio.orders or [])
                if o.status in _ACTIVE_ORDER_STATUSES
            ]
            log.info(
                "DRY RUN — would cancel %d open order(s) before a live rebalance; no cancellations sent.",
                len(open_orders),
            )
        else:
            cancel_open_orders(client, initial_portfolio.orders or [])

        # Re-fetch after cancellations so snapshot reflects the clean state
        (
            total_equity,
            cash_balance,
            buying_power,
            cash_only_bp,
            equity_pos,
            crypto_pos,
            equity_qty,
            crypto_qty,
        ) = get_portfolio_snapshot(client)
        (
            portfolio_nav,
            margin_loan_estimate,
            allowed_margin_loan,
            investment_base,
            effective_buying_power,
        ) = estimate_margin_state(
            total_equity,
            cash_balance,
            buying_power,
            cash_only_bp,
            margin_usage_pct,
        )
        log.info(
            "  Portfolio NAV: $%.2f  |  margin loan est.: $%.2f  |  allowed margin: $%.2f  |  investment base: $%.2f  |  effective BP: $%.2f  |  equity positions: %d",
            portfolio_nav,
            margin_loan_estimate,
            allowed_margin_loan,
            investment_base,
            effective_buying_power,
            len(equity_pos),
        )

        sells: list[tuple[str, InstrumentType, OrderSide, Decimal]] = []
        buys: list[tuple[str, InstrumentType, OrderSide, Decimal]] = []

        today_buys = load_today_buys(today_buys_file)
        if today_buys:
            log.info(
                "Day-trade prevention: %d symbol(s) bought earlier today are protected from same-day sells.",
                len(today_buys),
            )

        def queue(order):
            if order is None:
                return
            symbol, inst_type, side, amount = order
            if side == OrderSide.BUY and symbol in excluded_tickers:
                log.info("  Skipping BUY %s — excluded by config (liquidate only).", symbol)
                return
            if (
                side == OrderSide.SELL
                and inst_type == InstrumentType.EQUITY
                and symbol in today_buys
            ):
                log.warning(
                    "Day-trade prevention: skipping SELL %s — position was opened earlier today.",
                    symbol,
                )
                return
            if side == OrderSide.SELL:
                sells.append(order)
            else:
                buys.append(order)

        log.info("--- Computing stock deltas (%s top-%d) ---", index, top_n)
        all_stock_symbols = set(top_stocks) | {
            s for s in equity_pos if s not in NON_STOCK_ETFS and s not in top_stocks
        }
        for symbol in all_stock_symbols:
            if symbol in NON_STOCK_ETFS:
                continue
            weight = stock_weights.get(symbol, Decimal("0"))
            if symbol in excluded_tickers:
                weight = Decimal("0")  # liquidate any held position
            target = (weight * alloc_stocks * investment_base).quantize(Decimal("0.01"))
            current = equity_pos.get(symbol, Decimal("0"))
            queue(
                compute_delta(
                    symbol, InstrumentType.EQUITY, target, current, Decimal("1.00")
                )
            )

        def queue_etf_delta(symbol: str, alloc_pct: Decimal) -> None:
            target = Decimal("0") if symbol in excluded_tickers else (alloc_pct * investment_base).quantize(Decimal("0.01"))
            current = equity_pos.get(symbol, Decimal("0"))
            log.info(
                "  %s  target=$%.2f  current=$%.2f  delta=$%.2f",
                symbol,
                target,
                current,
                target - current,
            )
            queue(
                compute_delta(
                    symbol, InstrumentType.EQUITY, target, current, Decimal("1.00")
                )
            )

        crypto_prices: dict[str, Decimal] = {}
        crypto_allocs = {BTC_SYMBOL: alloc_btc, ETH_SYMBOL: alloc_eth}
        crypto_to_fetch = {
            symbol: BROKER_TO_YF_SYMBOLS.get(symbol, f"{symbol}-USD")
            for symbol, alloc_pct in crypto_allocs.items()
            if (
                symbol not in excluded_tickers
                and (alloc_pct > Decimal("0") or crypto_pos.get(symbol, Decimal("0")) > 0)
            )
        }
        if crypto_to_fetch:
            pool = ThreadPoolExecutor(max_workers=len(crypto_to_fetch))
            futures = {
                pool.submit(fetch_crypto_price, client, symbol, yf_ticker): symbol
                for symbol, yf_ticker in crypto_to_fetch.items()
            }
            try:
                for future in as_completed(
                    futures, timeout=CRYPTO_PRICE_FETCH_TIMEOUT_SECONDS
                ):
                    symbol = futures[future]
                    try:
                        crypto_prices[symbol] = future.result()
                    except Exception as exc:
                        log.error(
                            "Could not fetch %s price — aborting rebalance: %s",
                            symbol,
                            exc,
                        )
                        raise RuntimeError(f"Could not fetch {symbol} price") from exc
            except _FuturesTimeoutError as exc:
                pending = [
                    symbol for future, symbol in futures.items() if not future.done()
                ]
                log.error(
                    "Timed out after %ds fetching crypto price(s): %s — aborting rebalance.",
                    CRYPTO_PRICE_FETCH_TIMEOUT_SECONDS,
                    ", ".join(sorted(pending)) or "unknown",
                )
                raise RuntimeError("Timed out fetching crypto prices") from exc
            finally:
                pool.shutdown(wait=False, cancel_futures=True)

        log.info("--- Computing BTC delta ---")
        btc_price = crypto_prices.get(BTC_SYMBOL, Decimal("0"))
        btc_target = Decimal("0") if BTC_SYMBOL in excluded_tickers else (alloc_btc * investment_base).quantize(Decimal("0.01"))
        btc_current = crypto_pos.get(BTC_SYMBOL, Decimal("0"))
        log.info(
            "  BTC  price=$%.2f  target=$%.2f  current=$%.2f  delta=$%.2f",
            btc_price,
            btc_target,
            btc_current,
            btc_target - btc_current,
        )
        queue(
            compute_delta(
                BTC_SYMBOL,
                InstrumentType.CRYPTO,
                btc_target,
                btc_current,
                Decimal("1.00"),
            )
        )

        log.info("--- Computing ETH delta ---")
        eth_price = crypto_prices.get(ETH_SYMBOL, Decimal("0"))
        eth_target = Decimal("0") if ETH_SYMBOL in excluded_tickers else (alloc_eth * investment_base).quantize(Decimal("0.01"))
        eth_current = crypto_pos.get(ETH_SYMBOL, Decimal("0"))
        log.info(
            "  ETH  price=$%.2f  target=$%.2f  current=$%.2f  delta=$%.2f",
            eth_price,
            eth_target,
            eth_current,
            eth_target - eth_current,
        )
        queue(
            compute_delta(
                ETH_SYMBOL,
                InstrumentType.CRYPTO,
                eth_target,
                eth_current,
                Decimal("1.00"),
            )
        )

        log.info("--- Computing GLDM delta ---")
        queue_etf_delta(GOLD_SYMBOL, alloc_gold)
        log.info(
            "  Cash allocation (%.0f%%) stays uninvested — no order placed.",
            alloc_cash * 100,
        )

        sells.sort(key=lambda order: (order[3], order[0]), reverse=True)
        buys.sort(key=lambda order: (order[3], order[0]), reverse=True)

        log.info("Rebalance plan: %d sells  |  %d buys", len(sells), len(buys))
        if not sells and not buys:
            log.info("Portfolio is within threshold on all buckets — nothing to do.")
            return

        # Stocks being fully liquidated (target=$0) must be sold by share quantity,
        # not by notional amount — the broker rejects notional orders when the amount
        # is nearly equal to the full position value.
        # NON_STOCK_ETFS (e.g. GLDM) are excluded: they always have a non-zero target
        # and are never in stock_weights, so checking stock_weights alone would
        # incorrectly treat every GLDM partial-sell as a full liquidation.
        liquidation_quantities = {
            symbol: equity_qty[symbol]
            for symbol, inst_type, side, _ in sells
            if (
                inst_type == InstrumentType.EQUITY
                and side == OrderSide.SELL
                and symbol in equity_qty
                and symbol not in NON_STOCK_ETFS
                and stock_weights.get(symbol, Decimal("0")) == Decimal("0")
            )
        }
        if liquidation_quantities:
            log.info(
                "  %d positions will be liquidated by share quantity: %s",
                len(liquidation_quantities),
                ", ".join(sorted(liquidation_quantities)[:10])
                + ("…" if len(liquidation_quantities) > 10 else ""),
            )

        if dry_run:
            sells = filter_orders_by_public_tradability(client, sells) if sells else []
            buys = filter_orders_by_public_tradability(client, buys) if buys else []
            buys = cap_buy_orders_to_buying_power(buys, effective_buying_power)
            log.info("--- DRY RUN ORDER PLAN ---")
            log_dry_run_orders(sells, label="sell")
            log_dry_run_orders(buys, label="buy")
            log.info("DRY RUN complete — no order, cancellation, or ledger mutation occurred.")
            return

        try:
            if sells:
                sells = filter_orders_by_public_tradability(client, sells)
            if sells:
                log.info("--- Placing SELL orders (%d) ---", len(sells))
                sell_order_ids, _ = place_orders(
                    client,
                    sells,
                    crypto_prices=crypto_prices,
                    liquidation_quantities=liquidation_quantities,
                    crypto_quantities=crypto_qty,
                )
                if not wait_for_orders_to_clear(client, sell_order_ids, label="sell"):
                    log.error(
                        "Sell orders did not clear within timeout — aborting buy phase "
                        "to avoid over-investing against unsettled proceeds."
                    )
                    return

            if buys:
                buys = filter_orders_by_public_tradability(client, buys)
            if buys:
                post_sell_effective_bp = effective_buying_power
                try:
                    (
                        post_equity,
                        post_cash_balance,
                        post_bp,
                        post_cash_bp,
                        _,
                        _,
                        _,
                        _,
                    ) = get_portfolio_snapshot(client)
                    (
                        post_portfolio_nav,
                        post_loan_estimate,
                        post_allowed_margin,
                        _,
                        post_sell_effective_bp,
                    ) = estimate_margin_state(
                        post_equity,
                        post_cash_balance,
                        post_bp,
                        post_cash_bp,
                        margin_usage_pct,
                    )
                    log.info(
                        "  Post-sell — NAV: $%.2f  margin loan est.: $%.2f  allowed margin: $%.2f  effective BP: $%.2f",
                        post_portfolio_nav,
                        post_loan_estimate,
                        post_allowed_margin,
                        post_sell_effective_bp,
                    )
                except Exception as exc:
                    log.warning("Could not refresh buying power after sells: %s", exc)
                buys = cap_buy_orders_to_buying_power(buys, post_sell_effective_bp)
            if buys:
                log.info("--- Placing BUY orders (%d) ---", len(buys))
                _, submitted_buys = place_orders(
                    client, buys, crypto_prices=crypto_prices
                )
                # Record only confirmed equity buys so the PDT ledger reflects
                # what actually went through, not what was merely planned.
                bought_today = {
                    symbol
                    for symbol, inst_type, _, _ in submitted_buys
                    if inst_type == InstrumentType.EQUITY
                }
                record_today_buys(bought_today, today_buys_file)

        except PatternDayTradingError:
            log.error(
                "REBALANCE ABORTED — pattern day trading restriction encountered.\n"
                "The PDT rule may have been re-instated by the broker. No further\n"
                "orders will be placed this session. Next scheduled run will retry."
            )
            return

        log.info("Rebalance complete.")
    finally:
        client.close()


if __name__ == "__main__":
    args = sys.argv[1:]
    account_arg = None
    if "--account" in args:
        idx = args.index("--account")
        if idx + 1 < len(args):
            account_arg = args[idx + 1]
    rebalance(dry_run="--dry-run" in args, account_id=account_arg)
