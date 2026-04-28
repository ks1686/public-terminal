import pytest
from unittest.mock import patch, MagicMock
from app import PublicTerminal
from public_api_sdk import OrderStatus, OrderSide

@pytest.mark.asyncio
async def test_app_startup_and_mocked_portfolio():
    # Create a completely mocked client to ensure no real network or order calls
    mock_client = MagicMock()
    mock_portfolio = MagicMock()
    mock_portfolio.equity = "10000.00"
    mock_portfolio.cash = "500.00"
    mock_portfolio.positions = []
    mock_portfolio.orders = []
    mock_client.get_portfolio.return_value = mock_portfolio
    mock_client.get_accounts.return_value = [{"accountNumber": "TEST_ACCT"}]

    # Mock the client and config in app.py to prevent any real IO
    mock_path = MagicMock()
    mock_path.read_text.return_value = "{}"
    with patch("app.get_accounts", return_value=["TEST_ACCT"]), \
         patch("client.get_client", return_value=mock_client), \
         patch("app.get_portfolio_cache_path", return_value=mock_path):

        app = PublicTerminal()
        # We need to mock _credentials_present to return True to avoid SetupModal
        with patch("app._credentials_present", return_value=True), \
             patch("app.LIVE_PORTFOLIO_POLL_SECONDS", 0.1): # Speed up poll if it happens
            async with app.run_test() as pilot:
                # Verify the app starts up and has the correct title
                assert app.title == "PUBLIC TERMINAL"
                # We just want to ensure it reaches the main screen without crashing
                await pilot.pause()
