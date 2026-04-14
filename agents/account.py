"""Account agent — balance, holdings, and transaction history."""

from typing import Optional
from public_api_sdk import PublicApiClient, Portfolio, HistoryRequest, HistoryResponsePage


class AccountAgent:
    """Handles account portfolio and history queries."""

    def __init__(self, client: PublicApiClient) -> None:
        self._client = client

    def get_portfolio(self, account_id: Optional[str] = None) -> Portfolio:
        """Return full portfolio snapshot: positions, buying power, and open orders."""
        return self._client.get_portfolio(account_id=account_id)

    def get_history(
        self,
        history_request: Optional[HistoryRequest] = None,
        account_id: Optional[str] = None,
    ) -> HistoryResponsePage:
        """Return paginated account transaction history."""
        return self._client.get_history(
            history_request=history_request,
            account_id=account_id,
        )

    def get_accounts(self):
        """Return all accounts associated with the authenticated user."""
        return self._client.get_accounts()
