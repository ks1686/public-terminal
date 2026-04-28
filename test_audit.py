from __future__ import annotations

import unittest
from decimal import Decimal
from types import SimpleNamespace
from unittest.mock import patch

from public_api_sdk import InstrumentType, OrderSide, OrderStatus

from client import (
    get_instrument_lookup,
    get_tradable_instrument_symbols,
    validate_order_instrument,
)
from rebalance import (
    _make_order,
    filter_orders_by_public_tradability,
    place_orders,
    select_public_tradable_stocks,
)
import rebalance as rebalance_mod


class _FakeAuthManager:
    def __init__(self) -> None:
        self.refresh_count = 0

    def refresh_token_if_needed(self) -> None:
        self.refresh_count += 1


class _FakeApiClient:
    def __init__(
        self,
        responses: dict[tuple[str, str], dict],
        instrument_list: list[dict] | None = None,
    ) -> None:
        self.responses = responses
        self.instrument_list = instrument_list or list(responses.values())
        self.paths: list[str] = []

    def get(self, path: str, params=None):
        self.paths.append(path)
        if path == "/userapigateway/trading/instruments":
            type_filter = set((params or {}).get("typeFilter") or [])
            trading_filter = set((params or {}).get("tradingFilter") or [])
            instruments = []
            for item in self.instrument_list:
                instrument = item.get("instrument") or {}
                if type_filter and instrument.get("type") not in type_filter:
                    continue
                if trading_filter and item.get("trading") not in trading_filter:
                    continue
                instruments.append(item)
            return {"instruments": instruments}
        parts = path.rsplit("/", 2)
        key = (parts[-2], parts[-1])
        if key not in self.responses:
            raise RuntimeError("not found")
        return self.responses[key]


class _FakeClient:
    def __init__(self, responses: dict[tuple[str, str], dict]) -> None:
        self.auth_manager = _FakeAuthManager()
        self.api_client = _FakeApiClient(responses)
        self.placed = []

    def place_order(self, request):
        self.placed.append(request)
        return SimpleNamespace(order_id="12345678-1234-1234-1234-123456789abc")


def _instrument(symbol: str, typ: str, trading: str = "BUY_AND_SELL") -> dict:
    return {
        "instrument": {"symbol": symbol, "type": typ},
        "trading": trading,
        "fractionalTrading": trading,
    }


class InstrumentLookupTests(unittest.TestCase):
    def test_raw_lookup_reads_tradability_without_typed_sdk_models(self) -> None:
        client = _FakeClient({("AAPL", "EQUITY"): _instrument("AAPL", "EQUITY")})

        lookup = get_instrument_lookup(client, "aapl", InstrumentType.EQUITY)

        self.assertEqual(lookup.symbol, "AAPL")
        self.assertEqual(lookup.instrument_type, InstrumentType.EQUITY)
        self.assertTrue(lookup.is_buyable)
        self.assertEqual(
            client.api_client.paths,
            ["/userapigateway/trading/instruments/AAPL/EQUITY"],
        )

    def test_buy_validation_rejects_liquidation_only_symbols(self) -> None:
        client = _FakeClient(
            {("XYZ", "EQUITY"): _instrument("XYZ", "EQUITY", "LIQUIDATION_ONLY")}
        )

        with self.assertRaisesRegex(ValueError, "not buyable"):
            validate_order_instrument(
                client, "XYZ", InstrumentType.EQUITY, OrderSide.BUY
            )

    def test_sell_validation_allows_liquidation_only_symbols(self) -> None:
        client = _FakeClient(
            {("XYZ", "EQUITY"): _instrument("XYZ", "EQUITY", "LIQUIDATION_ONLY")}
        )

        lookup = validate_order_instrument(
            client, "XYZ", InstrumentType.EQUITY, OrderSide.SELL
        )

        self.assertEqual(lookup.trading, "LIQUIDATION_ONLY")

    def test_raw_tradable_symbol_list_filters_by_side_and_type(self) -> None:
        client = _FakeClient(
            {
                ("AAPL", "EQUITY"): _instrument("AAPL", "EQUITY", "BUY_AND_SELL"),
                ("XYZ", "EQUITY"): _instrument("XYZ", "EQUITY", "LIQUIDATION_ONLY"),
                ("BTC", "CRYPTO"): _instrument("BTC", "CRYPTO", "BUY_AND_SELL"),
            }
        )

        buyable = get_tradable_instrument_symbols(
            client, InstrumentType.EQUITY, OrderSide.BUY
        )
        sellable = get_tradable_instrument_symbols(
            client, InstrumentType.EQUITY, OrderSide.SELL
        )

        self.assertEqual(buyable, {"AAPL"})
        self.assertEqual(sellable, {"AAPL", "XYZ"})


class OrderSafetyTests(unittest.TestCase):
    def test_place_orders_validates_before_submitting(self) -> None:
        client = _FakeClient({})

        order_ids, submitted = place_orders(
            client,
            [("NOPE", InstrumentType.EQUITY, OrderSide.BUY, Decimal("5.00"))],
        )

        self.assertEqual(order_ids, [])
        self.assertEqual(submitted, [])
        self.assertEqual(client.placed, [])

    def test_filter_orders_removes_invalid_buys_before_submission_phase(self) -> None:
        client = _FakeClient(
            {("AAPL", "EQUITY"): _instrument("AAPL", "EQUITY", "BUY_AND_SELL")}
        )
        orders = [
            ("AAPL", InstrumentType.EQUITY, OrderSide.BUY, Decimal("5.00")),
            ("NOPE", InstrumentType.EQUITY, OrderSide.BUY, Decimal("5.00")),
        ]

        filtered = filter_orders_by_public_tradability(client, orders)

        self.assertEqual(filtered, [orders[0]])
        self.assertEqual(client.placed, [])

    def test_stock_selection_validates_public_before_excluding_or_selecting(self) -> None:
        client = _FakeClient(
            {
                ("AAA", "EQUITY"): _instrument("AAA", "EQUITY", "BUY_AND_SELL"),
                ("BBB", "EQUITY"): _instrument("BBB", "EQUITY", "DISABLED"),
                ("CCC", "EQUITY"): _instrument("CCC", "EQUITY", "BUY_AND_SELL"),
                ("DDD", "EQUITY"): _instrument("DDD", "EQUITY", "BUY_AND_SELL"),
            }
        )

        selected = select_public_tradable_stocks(
            client,
            ["AAA", "BBB", "CCC", "DDD"],
            {"AAA": 4, "BBB": 3, "CCC": 2, "DDD": 1},
            2,
            frozenset({"AAA"}),
        )

        self.assertEqual(selected, ["CCC", "DDD"])
        self.assertEqual(
            client.api_client.paths,
            [
                "/userapigateway/trading/instruments",
            ],
        )

    def test_crypto_sell_quantity_rounds_down_to_held_precision(self) -> None:
        request = _make_order(
            "BTC",
            InstrumentType.CRYPTO,
            OrderSide.SELL,
            Decimal("100.00"),
            crypto_price=Decimal("1"),
            crypto_held_quantity=Decimal("0.123456"),
        )

        self.assertEqual(request.quantity, Decimal("0.12345"))

    def test_equity_amount_rounds_down_to_cents(self) -> None:
        request = _make_order(
            "AAPL",
            InstrumentType.EQUITY,
            OrderSide.BUY,
            Decimal("1.239"),
        )

        self.assertEqual(request.amount, Decimal("1.23"))

    def test_rebalance_dry_run_does_not_cancel_place_or_record(self) -> None:
        fake_client = SimpleNamespace(
            get_portfolio=lambda: SimpleNamespace(
                orders=[
                    SimpleNamespace(
                        status=OrderStatus.NEW,
                        side=OrderSide.BUY,
                        instrument=SimpleNamespace(symbol="OLD"),
                        order_id="old-order-id",
                    )
                ]
            ),
            close=lambda: None,
        )

        with (
            patch.object(rebalance_mod, "_load_config_json", return_value={}),
            patch.object(
                rebalance_mod,
                "load_rebalance_config",
                return_value=("SP500", 1, Decimal("0"), frozenset()),
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
                return_value={"AAPL"},
            ),
            patch.object(rebalance_mod, "fetch_constituents", return_value=["AAPL"]),
            patch.object(
                rebalance_mod, "fetch_market_caps", return_value={"AAPL": 1_000_000}
            ),
            patch.object(
                rebalance_mod, "select_public_tradable_stocks", return_value=["AAPL"]
            ),
            patch.object(
                rebalance_mod,
                "get_portfolio_snapshot",
                return_value=(
                    Decimal("0"),
                    Decimal("100"),
                    Decimal("100"),
                    Decimal("100"),
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
            patch.object(rebalance_mod, "fetch_crypto_price") as fetch_crypto_price,
            patch.object(rebalance_mod, "cancel_open_orders") as cancel_open_orders,
            patch.object(rebalance_mod, "place_orders") as place_orders_mock,
            patch.object(rebalance_mod, "record_today_buys") as record_today_buys,
        ):
            rebalance_mod.rebalance(dry_run=True)

        fetch_crypto_price.assert_not_called()
        cancel_open_orders.assert_not_called()
        place_orders_mock.assert_not_called()
        record_today_buys.assert_not_called()


if __name__ == "__main__":
    unittest.main()
