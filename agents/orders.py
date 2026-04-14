"""Orders agent — place, cancel, replace, and track orders."""

from typing import Optional
from public_api_sdk import (
    PublicApiClient,
    OrderRequest,
    MultilegOrderRequest,
    CancelAndReplaceRequest,
    PreflightRequest,
    PreflightResponse,
    PreflightMultiLegRequest,
    PreflightMultiLegResponse,
    NewOrder,
    Order,
)


class OrdersAgent:
    """Handles single-leg and multi-leg order lifecycle."""

    def __init__(self, client: PublicApiClient) -> None:
        self._client = client

    # --- Preflight (dry-run cost estimates) ---

    def preflight(
        self,
        request: PreflightRequest,
        account_id: Optional[str] = None,
    ) -> PreflightResponse:
        """Estimate the financial impact of a single-leg order before placing it."""
        return self._client.perform_preflight_calculation(request, account_id=account_id)

    def preflight_multileg(
        self,
        request: PreflightMultiLegRequest,
        account_id: Optional[str] = None,
    ) -> PreflightMultiLegResponse:
        """Estimate the financial impact of a multi-leg strategy before placing it."""
        return self._client.perform_multi_leg_preflight_calculation(
            request, account_id=account_id
        )

    # --- Order placement ---

    def place(
        self,
        request: OrderRequest,
        account_id: Optional[str] = None,
    ) -> NewOrder:
        """Place a single-leg order (stocks, ETFs, options, crypto, bonds)."""
        return self._client.place_order(request, account_id=account_id)

    def place_multileg(
        self,
        request: MultilegOrderRequest,
        account_id: Optional[str] = None,
    ) -> NewOrder:
        """Place a multi-leg options strategy order."""
        return self._client.place_multileg_order(request, account_id=account_id)

    # --- Order management ---

    def get(self, order_id: str, account_id: Optional[str] = None) -> Order:
        """Fetch current status and details of an order."""
        return self._client.get_order(order_id, account_id=account_id)

    def cancel(self, order_id: str, account_id: Optional[str] = None) -> None:
        """Submit a cancellation request for an open order."""
        self._client.cancel_order(order_id, account_id=account_id)

    def replace(
        self,
        request: CancelAndReplaceRequest,
        account_id: Optional[str] = None,
    ) -> NewOrder:
        """Atomically cancel an existing order and replace it with a new one."""
        return self._client.cancel_and_replace_order(request, account_id=account_id)
