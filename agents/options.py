"""Options agent — chains, expirations, and greeks."""

from typing import List, Optional
from public_api_sdk import (
    PublicApiClient,
    OptionChainRequest,
    OptionChainResponse,
    OptionExpirationsRequest,
    OptionExpirationsResponse,
    OptionGreeks,
)


class OptionsAgent:
    """Handles options-specific market data: chains, expirations, and greeks."""

    def __init__(self, client: PublicApiClient) -> None:
        self._client = client

    def get_expirations(
        self,
        request: OptionExpirationsRequest,
        account_id: Optional[str] = None,
    ) -> OptionExpirationsResponse:
        """Fetch available expiration dates for an underlying symbol."""
        return self._client.get_option_expirations(request, account_id=account_id)

    def get_chain(
        self,
        request: OptionChainRequest,
        account_id: Optional[str] = None,
    ) -> OptionChainResponse:
        """Fetch the full option chain (calls + puts) for a given expiration."""
        return self._client.get_option_chain(request, account_id=account_id)

    def get_greeks(
        self,
        osi_symbol: str,
        account_id: Optional[str] = None,
    ) -> OptionGreeks:
        """Fetch delta, gamma, theta, vega, rho, and IV for a single option contract."""
        return self._client.get_option_greek(osi_symbol, account_id=account_id)

    def get_greeks_batch(
        self,
        osi_symbols: List[str],
        account_id: Optional[str] = None,
    ):
        """Fetch greeks for multiple option contracts at once."""
        return self._client.get_option_greeks(osi_symbols, account_id=account_id)
