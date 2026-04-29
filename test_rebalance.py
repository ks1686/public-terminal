from __future__ import annotations

import json
import tempfile
import unittest
from datetime import date
from decimal import Decimal
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch

from public_api_sdk import InstrumentType, OrderSide

import rebalance as rebalance_mod
from rebalance import (
    MIN_ORDER_DOLLARS,
    BUYING_POWER_BUFFER,
    _clean_tickers,
    _is_intraday_margin_error,
    _is_pdt_error,
    cap_buy_orders_to_buying_power,
    compute_delta,
    compute_stock_weights,
    compute_unallocated_buy_delta,
    estimate_margin_state,
    load_allocation_config,
    load_rebalance_config,
    load_today_buys,
    rank_by_market_cap,
    record_today_buys,
    top_n_by_market_cap,
)


# ---------------------------------------------------------------------------
# Config loading
# ---------------------------------------------------------------------------


class TestLoadRebalanceConfig(unittest.TestCase):
    def test_empty_config_returns_sp500_defaults(self) -> None:
        index, top_n, margin_pct, excluded = load_rebalance_config({})
        self.assertEqual(index, "SP500")
        self.assertGreater(top_n, 0)
        self.assertEqual(margin_pct, Decimal("0.5"))
        self.assertEqual(excluded, frozenset())

    def test_legacy_etf_ticker_migrates_to_index(self) -> None:
        cases = {
            "SPY": "SP500",
            "VOO": "SP500",
            "IVV": "SP500",
            "QQQ": "NASDAQ100",
            "DIA": "DJIA",
        }
        for etf, expected_index in cases.items():
            with self.subTest(etf=etf):
                index, *_ = load_rebalance_config({"etf_ticker": etf})
                self.assertEqual(index, expected_index)

    def test_new_index_key_takes_precedence_over_etf_ticker(self) -> None:
        index, *_ = load_rebalance_config(
            {"index": "NASDAQ100", "etf_ticker": "SPY"}
        )
        self.assertEqual(index, "NASDAQ100")

    def test_unknown_index_falls_back_to_sp500(self) -> None:
        index, *_ = load_rebalance_config({"index": "BOGUS_INDEX"})
        self.assertEqual(index, "SP500")

    def test_index_key_is_case_insensitive(self) -> None:
        index, *_ = load_rebalance_config({"index": "nasdaq100"})
        self.assertEqual(index, "NASDAQ100")

    def test_top_n_is_read_from_config(self) -> None:
        _, top_n, *_ = load_rebalance_config({"top_n": 42})
        self.assertEqual(top_n, 42)

    def test_top_n_minimum_is_one(self) -> None:
        _, top_n, *_ = load_rebalance_config({"top_n": 0})
        self.assertEqual(top_n, 1)

    def test_margin_usage_pct_is_clamped_to_zero_one(self) -> None:
        _, _, pct_low, _ = load_rebalance_config({"margin_usage_pct": "-1"})
        _, _, pct_high, _ = load_rebalance_config({"margin_usage_pct": "2"})
        self.assertEqual(pct_low, Decimal("0"))
        self.assertEqual(pct_high, Decimal("1"))

    def test_invalid_margin_usage_pct_falls_back_to_default(self) -> None:
        _, _, pct, _ = load_rebalance_config({"margin_usage_pct": "not-a-number"})
        self.assertEqual(pct, Decimal("0.5"))

    def test_excluded_tickers_are_uppercased_and_stripped(self) -> None:
        _, _, _, excluded = load_rebalance_config(
            {"excluded_tickers": ["goog", " AMZN ", "meta"]}
        )
        self.assertEqual(excluded, frozenset({"GOOG", "AMZN", "META"}))

    def test_blank_excluded_tickers_are_dropped(self) -> None:
        _, _, _, excluded = load_rebalance_config(
            {"excluded_tickers": ["AAPL", "", "  "]}
        )
        self.assertEqual(excluded, frozenset({"AAPL"}))

    def test_missing_config_returns_defaults(self) -> None:
        with patch.object(rebalance_mod, "_load_config_json", return_value={}):
            index, top_n, pct, excluded = load_rebalance_config()
        self.assertEqual(index, "SP500")
        self.assertEqual(excluded, frozenset())


class TestLoadAllocationConfig(unittest.TestCase):
    def test_empty_config_returns_defaults(self) -> None:
        allocs = load_allocation_config({})
        self.assertIn("stocks", allocs)
        self.assertIn("btc", allocs)
        self.assertIn("cash", allocs)
        total = sum(allocs.values())
        self.assertAlmostEqual(float(total), 1.0, places=4)

    def test_valid_custom_allocations_are_accepted(self) -> None:
        cfg = {
            "allocations": {
                "stocks": "0.50",
                "btc": "0.20",
                "eth": "0.10",
                "gold": "0.10",
                "cash": "0.10",
            }
        }
        allocs = load_allocation_config(cfg)
        self.assertEqual(allocs["stocks"], Decimal("0.50"))
        self.assertEqual(allocs["cash"], Decimal("0.10"))

    def test_allocations_not_summing_to_one_fall_back_to_defaults(self) -> None:
        cfg = {
            "allocations": {
                "stocks": "0.80",
                "btc": "0.80",
                "eth": "0.00",
                "gold": "0.00",
                "cash": "0.00",
            }
        }
        defaults = load_allocation_config({})
        allocs = load_allocation_config(cfg)
        self.assertEqual(allocs, defaults)

    def test_out_of_range_allocation_falls_back_to_defaults(self) -> None:
        cfg = {
            "allocations": {
                "stocks": "1.5",
                "btc": "0.00",
                "eth": "0.00",
                "gold": "0.00",
                "cash": "0.00",
            }
        }
        defaults = load_allocation_config({})
        allocs = load_allocation_config(cfg)
        self.assertEqual(allocs, defaults)

    def test_missing_allocations_key_returns_defaults(self) -> None:
        defaults = load_allocation_config({})
        allocs = load_allocation_config({"top_n": 100})
        self.assertEqual(allocs, defaults)


# ---------------------------------------------------------------------------
# Delta computation
# ---------------------------------------------------------------------------


class TestComputeDelta(unittest.TestCase):
    def _eq(self, symbol: str, typ, target: str, current: str, threshold: str = "0"):
        return compute_delta(
            symbol, typ, Decimal(target), Decimal(current), Decimal(threshold)
        )

    def test_buy_returned_when_above_drift_threshold(self) -> None:
        result = self._eq("AAPL", InstrumentType.EQUITY, "1000", "900", "0")
        self.assertIsNotNone(result)
        assert result is not None
        symbol, _, side, amount = result
        self.assertEqual(symbol, "AAPL")
        self.assertEqual(side, OrderSide.BUY)
        self.assertGreater(amount, Decimal("0"))

    def test_sell_returned_when_below_drift_threshold(self) -> None:
        result = self._eq("AAPL", InstrumentType.EQUITY, "900", "1000", "0")
        self.assertIsNotNone(result)
        assert result is not None
        symbol, _, side, amount = result
        self.assertEqual(side, OrderSide.SELL)
        self.assertGreater(amount, Decimal("0"))

    def test_none_returned_when_within_tolerance(self) -> None:
        # target == current → zero delta
        result = self._eq("AAPL", InstrumentType.EQUITY, "1000", "1000", "0")
        self.assertIsNone(result)

    def test_buy_below_min_order_dollars_returns_none(self) -> None:
        # delta of $2 is below the $5 minimum
        result = self._eq("AAPL", InstrumentType.EQUITY, "102", "100", "0")
        self.assertIsNone(result)

    def test_sell_below_min_order_dollars_returns_none(self) -> None:
        result = self._eq("AAPL", InstrumentType.EQUITY, "100", "102", "0")
        self.assertIsNone(result)

    def test_min_order_dollars_is_five(self) -> None:
        self.assertEqual(MIN_ORDER_DOLLARS, Decimal("5.00"))

    def test_custom_threshold_overrides_min_order_dollars(self) -> None:
        # threshold of $20 means a $15 delta is not enough
        result = self._eq("AAPL", InstrumentType.EQUITY, "115", "100", "20")
        self.assertIsNone(result)

    def test_buy_amount_is_quantized_to_cents(self) -> None:
        result = self._eq("AAPL", InstrumentType.EQUITY, "110.999", "100", "0")
        self.assertIsNotNone(result)
        assert result is not None
        _, _, _, amount = result
        self.assertEqual(amount, amount.quantize(Decimal("0.01")))


class TestComputeUnallocatedBuyDelta(unittest.TestCase):
    def test_returns_delta_for_small_buys_below_threshold(self) -> None:
        # target=101, current=100 → delta=1 which is <= drift_threshold
        delta = compute_unallocated_buy_delta(
            Decimal("101"), Decimal("100"), Decimal("0")
        )
        self.assertGreater(delta, Decimal("0"))

    def test_returns_zero_when_no_drift(self) -> None:
        delta = compute_unallocated_buy_delta(
            Decimal("100"), Decimal("100"), Decimal("0")
        )
        self.assertEqual(delta, Decimal("0"))

    def test_returns_zero_when_current_exceeds_target(self) -> None:
        delta = compute_unallocated_buy_delta(
            Decimal("100"), Decimal("110"), Decimal("0")
        )
        self.assertEqual(delta, Decimal("0"))

    def test_returns_zero_for_large_drift_above_threshold(self) -> None:
        # Large drift → compute_delta handles it, not unallocated
        delta = compute_unallocated_buy_delta(
            Decimal("200"), Decimal("100"), Decimal("0")
        )
        self.assertEqual(delta, Decimal("0"))


# ---------------------------------------------------------------------------
# Market cap ranking and stock weights
# ---------------------------------------------------------------------------


class TestMarketCapRanking(unittest.TestCase):
    _CAPS = {"AAPL": 3e12, "MSFT": 2.5e12, "NVDA": 2e12, "GOOG": 1.5e12}

    def test_rank_by_market_cap_sorts_descending(self) -> None:
        ranked = rank_by_market_cap(list(self._CAPS), self._CAPS)
        self.assertEqual(ranked, ["AAPL", "MSFT", "NVDA", "GOOG"])

    def test_rank_by_market_cap_excludes_missing_caps(self) -> None:
        caps = {"AAPL": 3e12}
        ranked = rank_by_market_cap(["AAPL", "UNKNOWN"], caps)
        self.assertEqual(ranked, ["AAPL"])

    def test_top_n_by_market_cap_returns_at_most_n(self) -> None:
        top2 = top_n_by_market_cap(list(self._CAPS), self._CAPS, 2)
        self.assertEqual(len(top2), 2)
        self.assertEqual(top2[0], "AAPL")

    def test_top_n_by_market_cap_returns_empty_list_when_no_caps(self) -> None:
        top = top_n_by_market_cap(["UNKNOWN"], {}, 5)
        self.assertEqual(top, [])

    def test_compute_stock_weights_sum_to_one(self) -> None:
        weights = compute_stock_weights(list(self._CAPS), self._CAPS)
        total = sum(weights.values())
        self.assertAlmostEqual(float(total), 1.0, places=6)

    def test_compute_stock_weights_are_proportional(self) -> None:
        caps = {"A": 100.0, "B": 300.0}
        weights = compute_stock_weights(["A", "B"], caps)
        self.assertAlmostEqual(float(weights["A"]), 0.25, places=6)
        self.assertAlmostEqual(float(weights["B"]), 0.75, places=6)

    def test_compute_stock_weights_raises_on_all_zero_caps(self) -> None:
        with self.assertRaises(RuntimeError):
            compute_stock_weights(["A"], {"A": 0.0})


# ---------------------------------------------------------------------------
# Margin state estimation
# ---------------------------------------------------------------------------


class TestEstimateMarginState(unittest.TestCase):
    def test_cash_only_account_no_margin_loan(self) -> None:
        nav, loan, allowed, base, bp = estimate_margin_state(
            total_equity=Decimal("10000"),
            cash_balance=Decimal("500"),
            buying_power=Decimal("500"),
            cash_only_buying_power=Decimal("500"),
            margin_usage_pct=Decimal("0"),
        )
        self.assertEqual(loan, Decimal("0"))
        self.assertEqual(allowed, Decimal("0"))
        self.assertEqual(bp, Decimal("500"))
        self.assertEqual(nav, Decimal("10500"))
        self.assertEqual(base, Decimal("10500"))

    def test_negative_cash_balance_does_not_reduce_effective_buying_power(self) -> None:
        # Negative cash = unsettled T+1 trades, NOT a margin loan.
        # broker reports buying_power == cash_only_buying_power → no margin offered.
        _, loan, _, _, bp = estimate_margin_state(
            total_equity=Decimal("10000"),
            cash_balance=Decimal("-2000"),
            buying_power=Decimal("3000"),
            cash_only_buying_power=Decimal("3000"),
            margin_usage_pct=Decimal("0.5"),
        )
        # No margin capacity → loan display is 0, effective BP = cash_only_bp
        self.assertEqual(loan, Decimal("0"))
        self.assertEqual(bp, Decimal("3000"))

    def test_margin_allowed_scales_with_usage_pct(self) -> None:
        # margin_capacity = buying_power - cash_only_buying_power = 1000
        _, _, allowed_50, _, _ = estimate_margin_state(
            total_equity=Decimal("10000"),
            cash_balance=Decimal("500"),
            buying_power=Decimal("1500"),
            cash_only_buying_power=Decimal("500"),
            margin_usage_pct=Decimal("0.5"),
        )
        _, _, allowed_100, _, _ = estimate_margin_state(
            total_equity=Decimal("10000"),
            cash_balance=Decimal("500"),
            buying_power=Decimal("1500"),
            cash_only_buying_power=Decimal("500"),
            margin_usage_pct=Decimal("1.0"),
        )
        self.assertEqual(allowed_50, Decimal("500"))
        self.assertEqual(allowed_100, Decimal("1000"))
        self.assertGreater(allowed_100, allowed_50)

    def test_effective_bp_includes_allowed_margin(self) -> None:
        # cash_only_bp=500, margin_capacity=1000, usage=50% → allowed=500 → effective=1000
        _, _, _, _, bp = estimate_margin_state(
            total_equity=Decimal("10000"),
            cash_balance=Decimal("500"),
            buying_power=Decimal("1500"),
            cash_only_buying_power=Decimal("500"),
            margin_usage_pct=Decimal("0.5"),
        )
        self.assertEqual(bp, Decimal("1000"))

    def test_zero_margin_usage_ignores_margin_capacity(self) -> None:
        _, _, allowed, _, bp = estimate_margin_state(
            total_equity=Decimal("10000"),
            cash_balance=Decimal("500"),
            buying_power=Decimal("2000"),  # broker offers $1500 margin
            cash_only_buying_power=Decimal("500"),
            margin_usage_pct=Decimal("0"),
        )
        self.assertEqual(allowed, Decimal("0"))
        self.assertEqual(bp, Decimal("500"))  # only cash

    def test_portfolio_nav_is_non_negative(self) -> None:
        nav, *_ = estimate_margin_state(
            total_equity=Decimal("0"),
            cash_balance=Decimal("-5000"),
            buying_power=Decimal("0"),
            cash_only_buying_power=Decimal("0"),
            margin_usage_pct=Decimal("0"),
        )
        self.assertGreaterEqual(nav, Decimal("0"))

    def test_margin_loan_display_uses_negative_cash_when_margin_offered(self) -> None:
        # Broker offers margin (buying_power > cash_only_buying_power) AND cash is negative
        # → displayed loan = abs(cash_balance)
        _, loan, _, _, _ = estimate_margin_state(
            total_equity=Decimal("10000"),
            cash_balance=Decimal("-1500"),
            buying_power=Decimal("2000"),
            cash_only_buying_power=Decimal("500"),
            margin_usage_pct=Decimal("0"),
        )
        self.assertEqual(loan, Decimal("1500"))



# ---------------------------------------------------------------------------
# Buy order budget capping
# ---------------------------------------------------------------------------


class TestCapBuyOrders(unittest.TestCase):
    _EQUITY = InstrumentType.EQUITY

    def _order(self, symbol: str, amount: str):
        return (symbol, self._EQUITY, OrderSide.BUY, Decimal(amount))

    def test_all_orders_fit_within_budget(self) -> None:
        orders = [self._order("AAPL", "10"), self._order("MSFT", "10")]
        result = cap_buy_orders_to_buying_power(orders, Decimal("30"))
        self.assertEqual(len(result), 2)

    def test_orders_exceeding_budget_are_dropped(self) -> None:
        orders = [self._order("AAPL", "50"), self._order("MSFT", "10")]
        # budget after buffer = 30 - 1 = 29; AAPL needs 50 so only MSFT fits
        result = cap_buy_orders_to_buying_power(orders, Decimal("30"))
        self.assertEqual(len(result), 1)
        self.assertEqual(result[0][0], "MSFT")

    def test_empty_result_when_buying_power_below_buffer(self) -> None:
        orders = [self._order("AAPL", "5")]
        result = cap_buy_orders_to_buying_power(orders, BUYING_POWER_BUFFER)
        self.assertEqual(result, [])

    def test_empty_orders_returns_empty(self) -> None:
        result = cap_buy_orders_to_buying_power([], Decimal("1000"))
        self.assertEqual(result, [])

    def test_buying_power_buffer_is_one_dollar(self) -> None:
        self.assertEqual(BUYING_POWER_BUFFER, Decimal("1.00"))


# ---------------------------------------------------------------------------
# Priority fill buy orders
# ---------------------------------------------------------------------------


class TestFillBuyOrders(unittest.TestCase):
    _EQ = InstrumentType.EQUITY
    _CR = InstrumentType.CRYPTO

    def _order(self, symbol: str, amount: str, inst_type=None):
        return (symbol, inst_type or self._EQ, OrderSide.BUY, Decimal(amount))

    def test_all_orders_fully_filled_when_budget_covers_all(self) -> None:
        from rebalance import fill_buy_orders
        orders = [self._order("AAPL", "50"), self._order("MSFT", "30")]
        result = fill_buy_orders(orders, Decimal("100"))
        self.assertEqual(len(result), 2)
        self.assertEqual(result[0][3], Decimal("50"))
        self.assertEqual(result[1][3], Decimal("30"))

    def test_partial_fill_on_first_order_that_does_not_fit(self) -> None:
        from rebalance import fill_buy_orders
        # budget after $1 buffer = $29; AAPL needs $50 → partial at $29
        orders = [self._order("AAPL", "50"), self._order("MSFT", "10")]
        result = fill_buy_orders(orders, Decimal("30"))
        self.assertEqual(len(result), 1)
        self.assertEqual(result[0][0], "AAPL")
        self.assertEqual(result[0][3], Decimal("29"))

    def test_stops_after_partial_fill_even_if_next_order_fits(self) -> None:
        # After partial fill remaining < $5; MSFT ($3) would fit but we stop
        from rebalance import fill_buy_orders
        orders = [self._order("AAPL", "50"), self._order("MSFT", "3")]
        # budget = 10 - 1 = 9; AAPL partial at $9; remaining = 0 < $5 → stop
        result = fill_buy_orders(orders, Decimal("10"))
        self.assertEqual(len(result), 1)
        self.assertEqual(result[0][0], "AAPL")
        self.assertEqual(result[0][3], Decimal("9"))

    def test_empty_result_when_buying_power_at_or_below_buffer(self) -> None:
        from rebalance import fill_buy_orders, BUYING_POWER_BUFFER
        orders = [self._order("AAPL", "5")]
        result = fill_buy_orders(orders, BUYING_POWER_BUFFER)
        self.assertEqual(result, [])

    def test_empty_orders_returns_empty(self) -> None:
        from rebalance import fill_buy_orders
        self.assertEqual(fill_buy_orders([], Decimal("1000")), [])

    def test_skips_partial_when_remaining_below_min_order_dollars(self) -> None:
        from rebalance import fill_buy_orders
        # budget = 4 + 1(buffer) = 5 total; after buffer: remaining = 4 < $5 min
        orders = [self._order("AAPL", "50")]
        result = fill_buy_orders(orders, Decimal("5"))
        # after buffer $1: remaining = $4, AAPL needs $50, $4 < MIN_ORDER_DOLLARS ($5) → stop
        self.assertEqual(result, [])

    def test_full_then_partial_sequence(self) -> None:
        from rebalance import fill_buy_orders
        # budget = 101 - 1 = 100; BTC $60 full; ETH $50 > $40 remaining but $40 >= $5 → partial $40
        orders = [
            self._order("BTC", "60", self._CR),
            self._order("ETH", "50", self._CR),
        ]
        result = fill_buy_orders(orders, Decimal("101"))
        self.assertEqual(len(result), 2)
        self.assertEqual(result[0][3], Decimal("60"))
        self.assertEqual(result[1][3], Decimal("40"))


# ---------------------------------------------------------------------------
# Day-trade ledger
# ---------------------------------------------------------------------------


class TestDayTradeLedger(unittest.TestCase):
    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self._ledger = Path(self._tmp.name) / "today_buys.json"

    def tearDown(self) -> None:
        self._tmp.cleanup()

    def _patch(self):
        return patch.object(rebalance_mod, "_first_account_path", return_value=self._ledger)

    def test_load_returns_empty_when_file_missing(self) -> None:
        with self._patch():
            result = load_today_buys()
        self.assertEqual(result, frozenset())

    def test_load_returns_symbols_for_today(self) -> None:
        self._ledger.write_text(
            json.dumps({"date": date.today().isoformat(), "symbols": ["AAPL", "MSFT"]})
        )
        with self._patch():
            result = load_today_buys()
        self.assertEqual(result, frozenset({"AAPL", "MSFT"}))

    def test_load_returns_empty_for_stale_date(self) -> None:
        self._ledger.write_text(
            json.dumps({"date": "2000-01-01", "symbols": ["AAPL"]})
        )
        with self._patch():
            result = load_today_buys()
        self.assertEqual(result, frozenset())

    def test_record_creates_file_with_symbols(self) -> None:
        with self._patch():
            record_today_buys({"AAPL", "GOOG"})
            result = load_today_buys()
        self.assertEqual(result, frozenset({"AAPL", "GOOG"}))

    def test_record_appends_to_existing_symbols(self) -> None:
        with self._patch():
            record_today_buys({"AAPL"})
            record_today_buys({"MSFT"})
            result = load_today_buys()
        self.assertEqual(result, frozenset({"AAPL", "MSFT"}))

    def test_record_with_empty_set_does_nothing(self) -> None:
        with self._patch():
            record_today_buys(set())
        self.assertFalse(self._ledger.exists())


# ---------------------------------------------------------------------------
# Broker error classification
# ---------------------------------------------------------------------------


class TestBrokerErrorClassification(unittest.TestCase):
    def test_pdt_error_detected_by_keyword(self) -> None:
        cases = [
            "You have been flagged as a pattern day trader",
            "PDT restriction applies",
            "day trade limit exceeded",
        ]
        for msg in cases:
            with self.subTest(msg=msg):
                self.assertTrue(_is_pdt_error(Exception(msg)))

    def test_non_pdt_error_not_detected(self) -> None:
        self.assertFalse(_is_pdt_error(Exception("insufficient funds")))

    def test_intraday_margin_error_detected(self) -> None:
        cases = [
            "intraday margin requirement not met",
            "intraday buying power exceeded",
            "margin call issued",
        ]
        for msg in cases:
            with self.subTest(msg=msg):
                self.assertTrue(_is_intraday_margin_error(Exception(msg)))

    def test_non_margin_error_not_detected(self) -> None:
        self.assertFalse(_is_intraday_margin_error(Exception("order rejected")))


# ---------------------------------------------------------------------------
# Ticker cleaning
# ---------------------------------------------------------------------------


class TestCleanTickers(unittest.TestCase):
    def test_strips_whitespace(self) -> None:
        self.assertEqual(_clean_tickers([" AAPL "]), ["AAPL"])

    def test_drops_empty_strings(self) -> None:
        self.assertEqual(_clean_tickers(["", "MSFT"]), ["MSFT"])

    def test_drops_placeholder_values(self) -> None:
        self.assertEqual(_clean_tickers(["-", "N/A", "AAPL"]), ["AAPL"])

    def test_drops_non_string_values(self) -> None:
        self.assertEqual(_clean_tickers([None, 123, "AAPL"]), ["AAPL"])

    def test_preserves_order(self) -> None:
        tickers = ["MSFT", "AAPL", "NVDA"]
        self.assertEqual(_clean_tickers(tickers), tickers)


# ---------------------------------------------------------------------------
# Constituent pre-filtering integration
# ---------------------------------------------------------------------------


class TestConstituentPreFiltering(unittest.TestCase):
    """
    Verify that rebalance() filters index constituents to only Public-tradable
    tickers before calling fetch_market_caps, so foreign/delisted tickers that
    slip through the alpha filter don't inflate the ticker count and prevent
    the market cap cache from being written.
    """

    def _run_rebalance_with_constituents(
        self, all_constituents: list[str], tradable: set[str]
    ) -> list[str]:
        """
        Run rebalance(dry_run=True) with a controlled constituent list and
        tradable symbol set, then return the ticker list that fetch_market_caps
        was called with.
        """
        fake_client = SimpleNamespace(
            get_portfolio=lambda: SimpleNamespace(orders=[]),
            close=lambda: None,
        )

        captured: list[list[str]] = []

        def fake_fetch_market_caps(tickers, index, cache_file=None):
            captured.append(list(tickers))
            return {t: float(i + 1) * 1e12 for i, t in enumerate(tickers)}

        with (
            patch.object(rebalance_mod, "_load_config_json", return_value={}),
            patch.object(
                rebalance_mod,
                "load_rebalance_config",
                return_value=("SP500", 10, Decimal("0"), frozenset()),
            ),
            patch.object(
                rebalance_mod,
                "load_allocation_config",
                return_value={
                    "stocks": Decimal("1"),
                    "btc": Decimal("0"),
                    "eth": Decimal("0"),
                    "gold": Decimal("0"),
                    "cash": Decimal("0"),
                },
            ),
            patch.object(rebalance_mod, "get_accounts", return_value=["TEST001"]),
            patch.object(rebalance_mod, "get_client", return_value=fake_client),
            patch.object(
                rebalance_mod,
                "get_tradable_instrument_symbols",
                return_value=tradable,
            ),
            patch.object(
                rebalance_mod, "fetch_constituents", return_value=all_constituents
            ),
            patch.object(
                rebalance_mod, "fetch_market_caps", side_effect=fake_fetch_market_caps
            ),
            patch.object(
                rebalance_mod,
                "select_public_tradable_stocks",
                side_effect=lambda _client, tickers, _caps, _n, _excl, _buyable=None: [
                    t for t in tickers if t in tradable
                ][:3],
            ),
            patch.object(
                rebalance_mod,
                "get_portfolio_snapshot",
                return_value=(
                    Decimal("0"),
                    Decimal("10000"),
                    Decimal("10000"),
                    Decimal("10000"),
                    {},
                    {},
                    {},
                    {},
                ),
            ),
            patch.object(rebalance_mod, "load_today_buys", return_value=frozenset()),
            patch.object(
                rebalance_mod,
                "filter_orders_by_public_tradability",
                side_effect=lambda client, orders: orders,
            ),
            patch.object(rebalance_mod, "fetch_crypto_price") as _mock_crypto,
            patch.object(rebalance_mod, "cancel_open_orders"),
            patch.object(rebalance_mod, "place_orders", return_value=([], [])),
            patch.object(rebalance_mod, "record_today_buys"),
        ):
            rebalance_mod.rebalance(dry_run=True)

        return captured[0] if captured else []

    def test_only_tradable_tickers_passed_to_fetch_market_caps(self) -> None:
        all_constituents = ["AAPL", "MSFT", "ADYEN", "SIKA", "NVDA"]
        tradable = {"AAPL", "MSFT", "NVDA"}  # foreign tickers excluded

        passed = self._run_rebalance_with_constituents(all_constituents, tradable)

        self.assertEqual(set(passed), tradable)
        self.assertNotIn("ADYEN", passed)
        self.assertNotIn("SIKA", passed)

    def test_all_tradable_when_no_foreign_tickers(self) -> None:
        constituents = ["AAPL", "MSFT", "NVDA"]
        tradable = {"AAPL", "MSFT", "NVDA"}

        passed = self._run_rebalance_with_constituents(constituents, tradable)

        self.assertEqual(set(passed), tradable)

    def test_empty_result_when_no_overlap_between_constituents_and_tradable(
        self,
    ) -> None:
        passed = self._run_rebalance_with_constituents(
            ["ADYEN", "SIKA"], {"AAPL", "MSFT"}
        )
        self.assertEqual(passed, [])


if __name__ == "__main__":
    unittest.main()
