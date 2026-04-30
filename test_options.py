"""Tests for options trading support."""

from decimal import Decimal
from datetime import datetime, timedelta
from unittest.mock import MagicMock

import pytest

from options import OptionPosition, _parse_occ_symbol, extract_options_from_positions


class TestOptionPosition:
    """Test OptionPosition dataclass."""

    def test_symbol_display_call(self):
        """Test symbol display for call options."""
        opt = OptionPosition(
            underlying_symbol="AAPL",
            option_type="CALL",
            strike_price=Decimal("150"),
            expiration_date="2026-05-16",
            quantity=Decimal("2"),
            entry_price=Decimal("2.50"),
        )
        assert opt.symbol_display == "AAPL 2026-05-16 150C"

    def test_symbol_display_put(self):
        """Test symbol display for put options."""
        opt = OptionPosition(
            underlying_symbol="AAPL",
            option_type="PUT",
            strike_price=Decimal("150"),
            expiration_date="2026-05-16",
            quantity=Decimal("1"),
            entry_price=Decimal("2.50"),
        )
        assert opt.symbol_display == "AAPL 2026-05-16 150P"

    def test_days_to_expiry(self):
        """Test days to expiry calculation."""
        today = datetime.now().date()
        future_date = today + timedelta(days=7)
        expiry_str = future_date.isoformat()

        opt = OptionPosition(
            underlying_symbol="AAPL",
            option_type="CALL",
            strike_price=Decimal("150"),
            expiration_date=expiry_str,
            quantity=Decimal("1"),
            entry_price=Decimal("2.50"),
        )
        assert opt.days_to_expiry == 7

    def test_is_near_expiry_true(self):
        """Test near expiry detection when true."""
        today = datetime.now().date()
        close_date = today + timedelta(days=3)
        expiry_str = close_date.isoformat()

        opt = OptionPosition(
            underlying_symbol="AAPL",
            option_type="CALL",
            strike_price=Decimal("150"),
            expiration_date=expiry_str,
            quantity=Decimal("1"),
            entry_price=Decimal("2.50"),
        )
        assert opt.is_near_expiry is True

    def test_is_near_expiry_false(self):
        """Test near expiry detection when false."""
        today = datetime.now().date()
        far_date = today + timedelta(days=30)
        expiry_str = far_date.isoformat()

        opt = OptionPosition(
            underlying_symbol="AAPL",
            option_type="CALL",
            strike_price=Decimal("150"),
            expiration_date=expiry_str,
            quantity=Decimal("1"),
            entry_price=Decimal("2.50"),
        )
        assert opt.is_near_expiry is False

    def test_to_dict(self):
        """Test conversion to dictionary."""
        opt = OptionPosition(
            underlying_symbol="AAPL",
            option_type="CALL",
            strike_price=Decimal("150"),
            expiration_date="2026-05-16",
            quantity=Decimal("2"),
            entry_price=Decimal("2.50"),
            current_value=Decimal("350"),
            last_price=Decimal("1.75"),
        )
        opt_dict = opt.to_dict()

        assert opt_dict["underlying_symbol"] == "AAPL"
        assert opt_dict["option_type"] == "CALL"
        assert opt_dict["strike_price"] == "150"
        assert opt_dict["quantity"] == "2"
        assert opt_dict["entry_price"] == "2.50"
        assert opt_dict["current_value"] == "350"

    def test_from_dict(self):
        """Test reconstruction from dictionary."""
        opt_dict = {
            "underlying_symbol": "AAPL",
            "option_type": "PUT",
            "strike_price": "150",
            "expiration_date": "2026-05-16",
            "quantity": "1",
            "entry_price": "3.00",
            "current_value": "250",
            "last_price": "2.50",
        }
        opt = OptionPosition.from_dict(opt_dict)

        assert opt.underlying_symbol == "AAPL"
        assert opt.option_type == "PUT"
        assert opt.strike_price == Decimal("150")
        assert opt.quantity == Decimal("1")
        assert opt.current_value == Decimal("250")

    def test_roundtrip_to_from_dict(self):
        """Test to_dict -> from_dict roundtrip."""
        original = OptionPosition(
            underlying_symbol="TSLA",
            option_type="CALL",
            strike_price=Decimal("250.50"),
            expiration_date="2026-06-20",
            quantity=Decimal("5"),
            entry_price=Decimal("4.75"),
            current_value=Decimal("950"),
            last_price=Decimal("1.90"),
            position_daily_gain=Decimal("50"),
            position_daily_gain_pct=Decimal("5.26"),
        )

        dict_form = original.to_dict()
        restored = OptionPosition.from_dict(dict_form)

        assert restored.underlying_symbol == original.underlying_symbol
        assert restored.option_type == original.option_type
        assert restored.strike_price == original.strike_price
        assert restored.quantity == original.quantity
        assert restored.entry_price == original.entry_price
        assert restored.current_value == original.current_value
        assert restored.last_price == original.last_price
        assert restored.symbol_display == original.symbol_display


class TestParseOccSymbol:
    """Test OCC option symbol parsing."""

    def test_call_standard(self):
        underlying, expiry, opt_type, strike = _parse_occ_symbol("AAPL  260516C00150000")
        assert underlying == "AAPL"
        assert expiry == "2026-05-16"
        assert opt_type == "CALL"
        assert strike == Decimal("150")

    def test_put_standard(self):
        underlying, expiry, opt_type, strike = _parse_occ_symbol("AAPL  260516P00145000")
        assert underlying == "AAPL"
        assert expiry == "2026-05-16"
        assert opt_type == "PUT"
        assert strike == Decimal("145")

    def test_fractional_strike(self):
        _, _, _, strike = _parse_occ_symbol("SPY   260516C00512500")
        assert strike == Decimal("512.500")

    def test_long_underlying(self):
        underlying, _, _, _ = _parse_occ_symbol("GOOGL 260516C02000000")
        assert underlying == "GOOGL"

    def test_returns_none_for_short_symbol(self):
        assert _parse_occ_symbol("ABC") is None

    def test_returns_none_for_empty(self):
        assert _parse_occ_symbol("") is None


class TestExtractOptionsFromPositions:
    """Test extract_options_from_positions with mock SDK objects."""

    def _make_position(self, symbol, current_value=None, last_price_val=None,
                       unit_cost=None, gain_value=None, gain_pct=None):
        pos = MagicMock()
        pos.instrument.symbol = symbol
        pos.instrument.type.value = "OPTION"
        pos.quantity = Decimal("2")
        pos.current_value = current_value
        pos.last_price = MagicMock()
        pos.last_price.last_price = last_price_val
        pos.cost_basis = MagicMock()
        pos.cost_basis.unit_cost = unit_cost
        pos.position_daily_gain = MagicMock()
        pos.position_daily_gain.gain_value = gain_value
        pos.position_daily_gain.gain_percentage = gain_pct
        return pos

    def test_parses_call_position(self):
        pos = self._make_position("AAPL  260516C00150000", current_value=Decimal("350"))
        result = extract_options_from_positions([pos])
        assert len(result) == 1
        opt = result[0]
        assert opt.underlying_symbol == "AAPL"
        assert opt.option_type == "CALL"
        assert opt.strike_price == Decimal("150")
        assert opt.expiration_date == "2026-05-16"
        assert opt.current_value == Decimal("350")

    def test_parses_put_position(self):
        pos = self._make_position("TSLA  260620P00250000")
        result = extract_options_from_positions([pos])
        assert len(result) == 1
        assert result[0].option_type == "PUT"
        assert result[0].underlying_symbol == "TSLA"

    def test_skips_non_option_positions(self):
        pos = MagicMock()
        pos.instrument.type.value = "EQUITY"
        result = extract_options_from_positions([pos])
        assert result == []

    def test_skips_unparseable_symbol(self):
        pos = MagicMock()
        pos.instrument.symbol = "BADDATA"
        pos.instrument.type.value = "OPTION"
        result = extract_options_from_positions([pos])
        assert result == []

    def test_reads_last_price(self):
        pos = self._make_position("AAPL  260516C00150000", last_price_val=Decimal("1.75"))
        result = extract_options_from_positions([pos])
        assert result[0].last_price == Decimal("1.75")

    def test_reads_entry_price_from_cost_basis(self):
        pos = self._make_position("AAPL  260516C00150000", unit_cost=Decimal("2.50"))
        result = extract_options_from_positions([pos])
        assert result[0].entry_price == Decimal("2.50")

    def test_reads_daily_gain(self):
        pos = self._make_position("AAPL  260516C00150000",
                                  gain_value=Decimal("50"), gain_pct=Decimal("5.26"))
        result = extract_options_from_positions([pos])
        assert result[0].position_daily_gain == Decimal("50")
        assert result[0].position_daily_gain_pct == Decimal("5.26")

    def test_empty_positions_returns_empty(self):
        assert extract_options_from_positions([]) == []

    def test_none_positions_returns_empty(self):
        assert extract_options_from_positions(None) == []
