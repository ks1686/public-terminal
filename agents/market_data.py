"""Market data agent — quotes and instrument lookup."""

from typing import List, Optional
from public_api_sdk import (
    PublicApiClient,
    Quote,
    OrderInstrument,
    InstrumentsRequest,
    InstrumentsResponse,
    InstrumentType,
)


class MarketDataAgent:
    """Handles real-time quotes and instrument discovery."""

    def __init__(self, client: PublicApiClient) -> None:
        self._client = client

    def get_quotes(
        self,
        instruments: List[OrderInstrument],
        account_id: Optional[str] = None,
    ) -> List[Quote]:
        """Fetch live quotes for a list of instruments (stocks, ETFs, crypto, bonds)."""
        return self._client.get_quotes(instruments=instruments, account_id=account_id)

    def get_quote(
        self,
        symbol: str,
        instrument_type: InstrumentType,
        account_id: Optional[str] = None,
    ) -> Quote:
        """Convenience: fetch a single quote by symbol and type."""
        instrument = OrderInstrument(symbol=symbol, type=instrument_type)
        quotes = self.get_quotes([instrument], account_id=account_id)
        if not quotes:
            raise ValueError(f"No quote returned for {symbol}")
        return quotes[0]

    def get_instruments(
        self,
        request: Optional[InstrumentsRequest] = None,
        account_id: Optional[str] = None,
    ) -> InstrumentsResponse:
        """List all tradeable instruments, optionally filtered by type."""
        return self._client.get_all_instruments(
            instruments_request=request,
            account_id=account_id,
        )
