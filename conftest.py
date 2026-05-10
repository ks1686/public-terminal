import pytest
from unittest.mock import MagicMock, patch
from decimal import Decimal

# ==============================================================================
# DEFENSE IN DEPTH: FAIL-SAFE TEST ISOLATION
# These fixtures run automatically for EVERY test to guarantee that real money,
# real API keys, and real orders can NEVER be used during testing, even if a
# test crashes and leaves background threads running.
# ==============================================================================

@pytest.fixture(autouse=True, scope="session")
def fail_safe_api_blocker():
    """
    GLOBAL LOCKOUT: Prevents the SDK from executing real orders.
    Patches the base SDK class methods globally for the entire test session.
    """
    def _block_real_order(*args, **kwargs):
        raise RuntimeError(
            "CRITICAL SECURITY VIOLATION: A test attempted to place a real order "
            "against the Public API. The global test fail-safe blocked this action."
        )

    with patch("public_api_sdk.PublicApiClient.place_order", side_effect=_block_real_order), \
         patch("public_api_sdk.PublicApiClient.cancel_order", side_effect=_block_real_order):
        yield

@pytest.fixture(autouse=True)
def wipe_environment_credentials(monkeypatch):
    """
    ENVIRONMENT LOCKOUT: Wipes API keys from the environment before every test.
    Ensures that if client.get_client() is accidentally called with real logic,
    it fails authentication instantly.
    """
    monkeypatch.delenv("PUBLIC_ACCESS_TOKEN", raising=False)
    monkeypatch.delenv("PUBLIC_API_SECRET_KEY", raising=False)

# ==============================================================================
# Standard Test Mocks
# ==============================================================================

@pytest.fixture
def mock_client():
    client = MagicMock()
    client.get_accounts.return_value = [{"accountNumber": "TEST_ACCT"}]
    portfolio = MagicMock()
    portfolio.account_id = "TEST_ACCT"
    portfolio.equity = Decimal("10000.00")
    portfolio.cash = Decimal("500.00")
    portfolio.positions = []
    portfolio.orders = []
    client.get_portfolio.return_value = portfolio
    
    # Properly mock history to avoid loops with MagicMocks
    history_page = MagicMock()
    history_page.transactions = []
    history_page.next_token = None
    client.get_history.return_value = history_page
    
    # Mock api_client for validate_order_instrument
    client.api_client.get.return_value = {
        "instrument": {"symbol": "AAPL", "type": "EQUITY"},
        "trading": "BUY_AND_SELL",
        "fractionalTrading": "BUY_AND_SELL"
    }
    
    return client

@pytest.fixture
def app_with_mocks(mock_client):
    with patch("app.get_accounts", return_value=["TEST_ACCT"]), \
         patch("client.get_client", return_value=mock_client), \
         patch("app._credentials_present", return_value=True), \
         patch("app.get_portfolio_cache_path") as mock_cache:
        mock_cache.return_value.read_text.return_value = "{}"
        from app import PublicTerminal
        yield PublicTerminal()
