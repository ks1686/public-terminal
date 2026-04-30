# Design Spec: Standardized ETF Data Retrieval

**Date:** 2026-04-29  
**Status:** Approved  
**Topic:** ETF Holdings Retrieval Standardization

## Overview
Standardize the method for pulling ETF holdings/weights for all supported indexes (S&P 500, NASDAQ-100, DJIA, VT, and SPUS). The goal is to maximize reliability by providing multiple fallback layers and caching.

## Goals
- Primary source: Direct from the fund curator/company.
- Secondary source: Wikipedia (for major indexes where available).
- Tertiary source: Stale cache from the previous day/run.
- High-visibility notification to the user when falling back to stale cache, requesting a bug report.
- Per-index cache files to ensure data integrity and isolation.

## Architecture

### 1. Per-Index Cache Files
Instead of a shared `fund_weights.json`, each index will have its own cache file:
- `constituents_SP500.json`
- `constituents_NASDAQ100.json`
- `constituents_DJIA.json`
- `constituents_FTSE_GLOBAL_ALL_CAP.json`
- `constituents_SPUS.json`

Location: `~/.config/public-terminal/accounts/<account_id>/cache/`

### 2. Standardized Retrieval Flow
A generic retrieval wrapper will be implemented for all indexes:
1. **Try Official Source:** Call the existing `_fetch_<index>_tickers_official()` functions.
2. **Try Wikipedia (if applicable):** Call the `_fetch_<index>_tickers_wikipedia()` functions.
3. **Try Stale Cache:**
   - Attempt to load from the index-specific cache file.
   - If successful, log a **WARNING** with the age of the cache and a link to report the issue: `https://github.com/ks1686/public-terminal/issues`.
4. **Failure:** Raise a `RuntimeError` if all layers fail.

### 3. Data Structure
The cache files will store:
- `updated_at`: ISO timestamp of the last successful fetch.
- `tickers`: List of symbols.
- `weights`: Optional dictionary of `{ticker: weight}` (null for Wikipedia fallbacks).

## Implementation Plan

### Phase 1: Configuration Updates (`config.py`)
- Add `get_index_cache_path(account_id, index_id)` to return the per-index file path.

### Phase 2: Retrieval Refactoring (`rebalance.py`)
- Implement `_load_index_cache(index_id)` and `_save_index_cache(index_id, tickers, weights)`.
- Refactor `fetch_constituents(index)` to use the standardized flow.
- Ensure all official fetchers return `(tickers, weights)` consistently.

### Phase 3: Validation
- Update `test_rebalance.py` to cover the new fallback logic and per-index caching.
- Verify that `SPUS` continues to function correctly with its existing CSV source.

## Testing Strategy
- **Unit Tests:** Mock network failures for official and Wikipedia sources to verify cache fallback.
- **Integration Tests:** Run `rebalance --dry-run` for various indexes to ensure weights are correctly loaded or derived.
