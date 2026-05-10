import pytest
from unittest.mock import patch, MagicMock
from app import PublicTerminal
from decimal import Decimal
import asyncio

@pytest.mark.asyncio
async def test_app_startup_and_mocked_portfolio(app_with_mocks):
    async with app_with_mocks.run_test() as pilot:
        # Verify the app starts up and has the correct title
        assert app_with_mocks.title == "PUBLIC TERMINAL"
        await pilot.pause()

@pytest.mark.asyncio
async def test_modal_bindings(app_with_mocks):
    async with app_with_mocks.run_test() as pilot:
        for key, modal_name in [
            ("b", "OrderModal"), 
            ("s", "OrderModal"), 
            ("h", "HistoryModal"), 
            ("ctrl+a", "AccountManagementModal"),
            ("S", "RebalanceConfigModal")
        ]:
            await pilot.press(key)
            await pilot.pause()
            assert app_with_mocks.screen.__class__.__name__ == modal_name
            # Dismiss modal
            if hasattr(app_with_mocks.screen, "dismiss"):
                app_with_mocks.screen.dismiss(None)
            else:
                await pilot.press("escape")
            await pilot.pause()
            # Ensure we are back on the main screen (not a modal)
            assert not hasattr(app_with_mocks.screen, "is_modal") or not app_with_mocks.screen.is_modal

@pytest.mark.asyncio
async def test_place_order_success(app_with_mocks, mock_client):
    async with app_with_mocks.run_test() as pilot:
        with patch.object(app_with_mocks, "notify") as mock_notify:
            from public_api_sdk import InstrumentType, OrderSide
            # _place_order returns a Worker. We don't await it directly.
            app_with_mocks._place_order(
                symbol="AAPL", 
                instrument_type=InstrumentType.EQUITY, 
                quantity=Decimal("10"), 
                side=OrderSide.BUY, 
                order_type="MARKET"
            )
            # Wait for the background worker to finish
            await asyncio.sleep(0.5)
            await pilot.pause()
            
            # Check notifications - stripping ID check because it's random
            args, kwargs = mock_notify.call_args
            assert "Order submitted: BUY 10 AAPL (MARKET)" in args[0]
            assert kwargs["title"] == "Order Placed"
            assert kwargs["severity"] == "information"
            
            mock_client.place_order.assert_called_once()

@pytest.mark.asyncio
async def test_cancel_order_no_selection_notifies(app_with_mocks):
    async with app_with_mocks.run_test() as pilot:
        with patch.object(app_with_mocks, "notify") as mock_notify:
            await pilot.pause()
            # Trigger the action when no order is selected
            await pilot.press("c")
            mock_notify.assert_called_with("No open order selected", severity="warning")

@pytest.mark.asyncio
async def test_skip_rebalance_toggle(app_with_mocks):
    async with app_with_mocks.run_test() as pilot:
        with patch("app.get_skip_file_path") as mock_skip_path:
            mock_file = MagicMock()
            mock_skip_path.return_value = mock_file
            
            # First press: Create skip file
            mock_file.unlink.side_effect = FileNotFoundError()
            await pilot.press("x")
            mock_file.touch.assert_called_once()
            
            # Second press: Remove skip file
            mock_file.unlink.side_effect = None
            await pilot.press("x")
            assert mock_file.unlink.call_count == 2 # One failing, one succeeding

@pytest.mark.asyncio
async def test_startup_no_credentials():
    # Use a patch on get_accounts to return empty list and _credentials_present False
    with patch("app._credentials_present", return_value=False), \
         patch("app.get_accounts", return_value=[]):
        from app import PublicTerminal
        app = PublicTerminal()
        async with app.run_test() as pilot:
            # SetupModal is usually the first screen if no credentials
            assert app.screen.__class__.__name__ == "SetupModal"

@pytest.mark.asyncio
async def test_chart_cycle(app_with_mocks):
    async with app_with_mocks.run_test() as pilot:
        from widgets import PortfolioChart
        chart = app_with_mocks.query_one(PortfolioChart)
        initial_idx = chart._period_idx
        await pilot.press("]")
        assert chart._period_idx == (initial_idx + 1) % 5
        await pilot.press("[")
        assert chart._period_idx == initial_idx

@pytest.mark.asyncio
async def test_toggle_live_chart(app_with_mocks):
    async with app_with_mocks.run_test() as pilot:
        assert app_with_mocks._live_chart is False
        await pilot.press("l")
        assert app_with_mocks._live_chart is True
        await pilot.press("l")
        assert app_with_mocks._live_chart is False

@pytest.mark.asyncio
async def test_rebalance_now_triggers_subprocess(app_with_mocks):
    async with app_with_mocks.run_test() as pilot:
        with patch("subprocess.Popen") as mock_popen, \
             patch("app._HAS_SYSTEMCTL", False), \
             patch.object(app_with_mocks, "notify") as mock_notify:
            
            # Directly trigger rebalance now path (bypassing confirmation modal for simplicity in unit test)
            app_with_mocks._trigger_rebalance_now()
            await asyncio.sleep(0.5)
            await pilot.pause()
            
            mock_popen.assert_called_once()
            mock_notify.assert_called_with(
                "Rebalance started — logs in cache/rebalance.log",
                severity="information"
            )

@pytest.mark.asyncio
async def test_toggle_rebalancer_no_systemctl(app_with_mocks):
    async with app_with_mocks.run_test() as pilot:
        with patch("app._HAS_SYSTEMCTL", False), \
             patch.object(app_with_mocks, "notify") as mock_notify:
            
            await pilot.press("t")
            await asyncio.sleep(0.5)
            await pilot.pause()
            mock_notify.assert_called_with(
                "Pause/resume requires systemctl on this platform.",
                severity="error"
            )

@pytest.mark.asyncio
async def test_settings_save_notifies(app_with_mocks):
    async with app_with_mocks.run_test() as pilot:
        with patch("app._save_rebalance_config") as mock_save, \
             patch.object(app_with_mocks, "notify") as mock_notify:
            
            result = {
                "index": "SP500",
                "top_n": 500,
                "margin_usage_pct": 0.5,
                "excluded_tickers": [],
                "allocations": {"stocks": 1.0, "btc": 0.0, "eth": 0.0, "gold": 0.0, "cash": 0.0},
                "rebalance_enabled": True
            }
            app_with_mocks._handle_rebalance_settings(result)
            
            mock_save.assert_called_once()
            args, kwargs = mock_notify.call_args
            assert "Saved: S&P 500 top-500" in args[0]
            assert kwargs["severity"] == "information"

@pytest.mark.asyncio
async def test_account_switching_resets_client(app_with_mocks):
    async with app_with_mocks.run_test() as pilot:
        with patch("app.get_accounts", return_value=["ACCT1", "ACCT2"]), \
             patch.object(app_with_mocks, "load_portfolio") as mock_load:
            
            app_with_mocks._active_account = "ACCT1"
            app_with_mocks._client = MagicMock()
            
            # Simulate tab activation for ACCT2
            from textual.widgets import Tabs
            event = Tabs.TabActivated(
                tabs=app_with_mocks.query_one("#account-tabs"),
                tab=MagicMock(id="tab-ACCT2")
            )
            app_with_mocks.on_tabs_tab_activated(event)
            
            assert app_with_mocks._active_account == "ACCT2"
            assert app_with_mocks._client is None
            mock_load.assert_called_once()
