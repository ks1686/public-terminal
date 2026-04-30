"""Options trading support for Public Terminal."""

from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime
from decimal import Decimal


@dataclass
class OptionPosition:
    """Represents a single options contract position."""

    underlying_symbol: str
    option_type: str  # "CALL" or "PUT"
    strike_price: Decimal
    expiration_date: str  # ISO format: YYYY-MM-DD
    quantity: Decimal
    entry_price: Decimal
    current_value: Decimal | None = None
    last_price: Decimal | None = None
    contract_value: Decimal | None = None  # Usually 100 for stock options
    position_daily_gain: Decimal | None = None
    position_daily_gain_pct: Decimal | None = None

    @property
    def symbol_display(self) -> str:
        """Return human-readable contract symbol (e.g., 'AAPL 2026-05-16 150C')."""
        option_code = "C" if self.option_type.upper() == "CALL" else "P"
        return f"{self.underlying_symbol} {self.expiration_date} {self.strike_price}{option_code}"

    @property
    def days_to_expiry(self) -> int:
        """Calculate days remaining until expiration."""
        try:
            expiry = datetime.strptime(self.expiration_date, "%Y-%m-%d").date()
            today = datetime.now().date()
            return (expiry - today).days
        except (ValueError, TypeError):
            return 0

    @property
    def is_near_expiry(self) -> bool:
        """Check if option is within 7 days of expiration."""
        return 0 <= self.days_to_expiry <= 7

    def to_dict(self) -> dict:
        """Convert to dictionary for caching/display."""
        return {
            "underlying_symbol": self.underlying_symbol,
            "option_type": self.option_type,
            "strike_price": str(self.strike_price),
            "expiration_date": self.expiration_date,
            "quantity": str(self.quantity),
            "entry_price": str(self.entry_price),
            "current_value": str(self.current_value) if self.current_value else None,
            "last_price": str(self.last_price) if self.last_price else None,
            "contract_value": str(self.contract_value) if self.contract_value else None,
            "position_daily_gain": str(self.position_daily_gain) if self.position_daily_gain else None,
            "position_daily_gain_pct": str(self.position_daily_gain_pct) if self.position_daily_gain_pct else None,
        }

    @staticmethod
    def from_dict(data: dict) -> OptionPosition:
        """Reconstruct from dictionary."""
        return OptionPosition(
            underlying_symbol=data.get("underlying_symbol", ""),
            option_type=data.get("option_type", ""),
            strike_price=Decimal(str(data.get("strike_price", "0"))),
            expiration_date=data.get("expiration_date", ""),
            quantity=Decimal(str(data.get("quantity", "0"))),
            entry_price=Decimal(str(data.get("entry_price", "0"))),
            current_value=(
                Decimal(str(data.get("current_value")))
                if data.get("current_value")
                else None
            ),
            last_price=(
                Decimal(str(data.get("last_price")))
                if data.get("last_price")
                else None
            ),
            contract_value=(
                Decimal(str(data.get("contract_value")))
                if data.get("contract_value")
                else None
            ),
            position_daily_gain=(
                Decimal(str(data.get("position_daily_gain")))
                if data.get("position_daily_gain")
                else None
            ),
            position_daily_gain_pct=(
                Decimal(str(data.get("position_daily_gain_pct")))
                if data.get("position_daily_gain_pct")
                else None
            ),
        )


def _parse_occ_symbol(symbol: str) -> tuple[str, str, str, Decimal] | None:
    """
    Parse an OCC option symbol into (underlying, expiration_date, option_type, strike).

    OCC format: 6-char padded root + YYMMDD + C/P + 8-digit strike (thousandths).
    Example: "AAPL  260516C00150000" → ("AAPL", "2026-05-16", "CALL", Decimal("150"))
    Returns None if the symbol cannot be parsed.
    """
    s = symbol.replace(" ", "")
    # After stripping spaces the minimum valid length is 6(root)+6(date)+1(type)+8(strike)=21,
    # but root may be shorter once spaces are removed; find the C/P boundary from the right.
    # The last 15 chars are always YYMMDD + C/P + 8-digit strike.
    if len(s) < 15:
        return None
    date_type_strike = s[-15:]
    underlying = s[:-15].strip()
    try:
        yy, mm, dd = date_type_strike[:2], date_type_strike[2:4], date_type_strike[4:6]
        expiration_date = f"20{yy}-{mm}-{dd}"
        option_type = "CALL" if date_type_strike[6].upper() == "C" else "PUT"
        strike = Decimal(date_type_strike[7:]) / Decimal("1000")
    except (ValueError, IndexError):
        return None
    if not underlying:
        return None
    return underlying, expiration_date, option_type, strike


def extract_options_from_positions(positions) -> list[OptionPosition]:
    """
    Parse options positions from Public API portfolio.positions list.

    The SDK's PortfolioInstrument has only symbol/name/type — no details field.
    Option details are decoded from the OCC-format symbol string.
    """
    options = []

    for pos in positions or []:
        instrument = getattr(pos, "instrument", None)
        if not instrument or instrument.type.value != "OPTION":
            continue

        parsed = _parse_occ_symbol(instrument.symbol)
        if not parsed:
            continue
        underlying_symbol, expiration_date, option_type, strike_price = parsed

        quantity = Decimal(str(pos.quantity)) if pos.quantity else Decimal("0")
        current_value = pos.current_value or Decimal("0")

        last_price = None
        if pos.last_price and pos.last_price.last_price is not None:
            try:
                last_price = Decimal(str(pos.last_price.last_price))
            except (ValueError, TypeError):
                pass

        entry_price = Decimal("0")
        if pos.cost_basis and pos.cost_basis.unit_cost is not None:
            try:
                entry_price = Decimal(str(pos.cost_basis.unit_cost))
            except (ValueError, TypeError):
                pass

        daily_gain = None
        daily_gain_pct = None
        if pos.position_daily_gain:
            gain_obj = pos.position_daily_gain
            try:
                if gain_obj.gain_value is not None:
                    daily_gain = Decimal(str(gain_obj.gain_value))
                if gain_obj.gain_percentage is not None:
                    daily_gain_pct = Decimal(str(gain_obj.gain_percentage))
            except (ValueError, TypeError):
                pass

        options.append(
            OptionPosition(
                underlying_symbol=underlying_symbol,
                option_type=option_type,
                strike_price=strike_price,
                expiration_date=expiration_date,
                quantity=quantity,
                entry_price=entry_price,
                current_value=current_value,
                last_price=last_price,
                contract_value=Decimal("100"),
                position_daily_gain=daily_gain,
                position_daily_gain_pct=daily_gain_pct,
            )
        )

    return options
