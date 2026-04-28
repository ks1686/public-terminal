"""Textual App class for the Public Terminal TUI."""

from __future__ import annotations

import json
import os
import signal
import subprocess
import sys
import uuid
from decimal import Decimal
from pathlib import Path

from public_api_sdk import (
    InstrumentType,
    OrderExpirationRequest,
    OrderInstrument,
    OrderRequest,
    OrderSide,
    OrderType,
    TimeInForce,
)
from textual import work
from textual.app import App, ComposeResult
from textual.binding import Binding
from textual.containers import Horizontal, Vertical
from textual.timer import Timer
from textual.widgets import Footer, Header, Label, Tab, Tabs

from config import (
    _ACTIVE_ORDER_STATUSES,
    _HAS_SYSTEMCTL,
    _credentials_present,
    _install_service_files,
    _load_rebalance_config,
    _remove_service_files,
    _save_rebalance_config,
    get_accounts,
    get_portfolio_cache_path,
    get_skip_file_path,
)
from client import validate_order_instrument
from modals import (
    AccountManagementModal,
    CancelConfirmModal,
    HistoryModal,
    OrderModal,
    RebalanceConfigModal,
    RebalanceNowConfirmModal,
    SetupModal,
)
from widgets import (
    BalanceBar,
    HoldingsTable,
    OrdersTable,
    PortfolioChart,
    RebalancerBar,
    StatusBar,
)

_HINT = "  |  [r] Refresh  [l] Live  [b] Buy  [s] Sell  [c] Cancel  [q] Quit"
LIVE_PORTFOLIO_POLL_SECONDS = 30
TIMER_UNIT = "public-terminal-rebalance.timer"
SERVICE_UNIT = "public-terminal-rebalance.service"
USER_SYSTEMD_DIR = Path.home() / ".config" / "systemd" / "user"
TIMER_UNIT_PATH = USER_SYSTEMD_DIR / TIMER_UNIT
SERVICE_UNIT_PATH = USER_SYSTEMD_DIR / SERVICE_UNIT


class PublicTerminal(App):
    TITLE = "PUBLIC TERMINAL"
    CSS = """
    Screen { background: $surface; }
    #account-tabs { height: 3; }
    #main-layout { height: 1fr; }
    #left-pane  { width: 2fr; border: tall $primary; }
    #right-pane { width: 1fr; border: tall $accent; }
    #pane-title    { background: $primary; color: $text; text-align: center; height: 1; text-style: bold; }
    #orders-title  { background: $accent;  color: $text; text-align: center; height: 1; text-style: bold; }
    """

    BINDINGS = [
        Binding("q", "quit", "Quit"),
        Binding("r", "refresh", "Refresh"),
        Binding("b", "buy", "Buy"),
        Binding("s", "sell", "Sell"),
        Binding("c", "cancel_order", "Cancel Order"),
        Binding("h", "history", "History"),
        Binding("l", "toggle_live_chart", "Live Chart"),
        Binding("t", "toggle_rebalancer", "Pause/Resume Schedule"),
        Binding("e", "toggle_enable_rebalancer", "Install/Remove Schedule"),
        Binding("x", "skip_next_rebalance", "Skip Next Run"),
        Binding("R", "rebalance_now", "Rebalance Now"),
        Binding("S", "rebalance_settings", "Settings"),
        Binding("[", "chart_prev", "Chart ◄"),
        Binding("]", "chart_next", "Chart ►"),
        Binding("ctrl+left", "prev_account", "Prev Account", show=False),
        Binding("ctrl+right", "next_account", "Next Account", show=False),
        Binding("ctrl+a", "manage_accounts", "Accounts"),
    ]

    def __init__(self) -> None:
        super().__init__()
        self._client = None
        self._active_account: str = ""
        self._margin_enabled: bool | None = None
        self._margin_capacity = Decimal(0)
        self._live_chart = False
        self._live_timer: Timer | None = None

    def compose(self) -> ComposeResult:
        yield Header(show_clock=True)
        accounts = get_accounts()
        yield Tabs(
            *[Tab(acct, id=f"tab-{acct}") for acct in accounts],
            id="account-tabs",
        )
        yield BalanceBar(id="balance-bar")
        yield RebalancerBar(id="rebalancer-bar")
        yield PortfolioChart(id="portfolio-chart")
        with Horizontal(id="main-layout"):
            with Vertical(id="left-pane"):
                yield Label(" HOLDINGS", id="pane-title")
                yield HoldingsTable(id="holdings-table")
            with Vertical(id="right-pane"):
                yield Label(" OPEN ORDERS", id="orders-title")
                yield OrdersTable(id="orders-table")
        yield StatusBar(id="status-bar")
        yield Footer()

    def on_mount(self) -> None:
        self._live_timer = self.set_interval(
            LIVE_PORTFOLIO_POLL_SECONDS, self._poll_live_portfolio, pause=True
        )
        if not _credentials_present():
            self.push_screen(SetupModal(), self._handle_setup)
        else:
            accounts = get_accounts()
            self._active_account = accounts[0] if accounts else ""
            self._start_loading()

    def _sync_tabs(self, accounts: list[str]) -> None:
        """Diff-update the account tab bar without calling clear().

        Textual's clear() schedules DOM removal asynchronously, so calling
        add_tab() immediately after raises DuplicateIds. Removing and adding
        individual tabs avoids this.
        """
        tabs = self.query_one("#account-tabs", Tabs)
        current_ids = {tab.id for tab in tabs.query(Tab)}
        desired_ids = {f"tab-{acct}" for acct in accounts}
        for tab_id in current_ids - desired_ids:
            tabs.remove_tab(tab_id)
        for acct in accounts:
            if f"tab-{acct}" not in current_ids:
                tabs.add_tab(Tab(acct, id=f"tab-{acct}"))

    def _handle_setup(self, saved: bool) -> None:
        if not saved:
            self.exit()
            return
        accounts = get_accounts()
        self._active_account = accounts[0] if accounts else ""
        self._sync_tabs(accounts)
        self.query_one(StatusBar).set_status(
            "  Credentials saved to .env — connecting…" + _HINT
        )
        self._start_loading()

    def _start_loading(self) -> None:
        self._load_portfolio_cache()
        self.query_one(StatusBar).set_status("  Connecting…" + _HINT)
        self.load_portfolio()
        self.load_rebalancer_status()

    def on_tabs_tab_activated(self, event: Tabs.TabActivated) -> None:
        if event.tab is None:
            return
        account_id = event.tab.id or ""
        if account_id.startswith("tab-"):
            account_id = account_id[4:]
        # Guard: if _active_account is not yet set, on_mount will handle initial load
        if not account_id or not self._active_account:
            return
        if account_id != self._active_account:
            self._active_account = account_id
            self._client = None
            if self._live_chart:
                self._live_chart = False
                if self._live_timer is not None:
                    self._live_timer.pause()
            self.query_one(PortfolioChart).clear_for_account_switch()
            self._start_loading()

    def action_prev_account(self) -> None:
        accounts = get_accounts()
        if len(accounts) <= 1:
            return
        idx = accounts.index(self._active_account) if self._active_account in accounts else 0
        new_acct = accounts[(idx - 1) % len(accounts)]
        self.query_one("#account-tabs", Tabs).active = f"tab-{new_acct}"

    def action_next_account(self) -> None:
        accounts = get_accounts()
        if len(accounts) <= 1:
            return
        idx = accounts.index(self._active_account) if self._active_account in accounts else 0
        new_acct = accounts[(idx + 1) % len(accounts)]
        self.query_one("#account-tabs", Tabs).active = f"tab-{new_acct}"

    def action_manage_accounts(self) -> None:
        self.push_screen(AccountManagementModal(), self._handle_account_management)

    def _handle_account_management(self, _: None) -> None:
        accounts = get_accounts()
        self._sync_tabs(accounts)
        if self._active_account not in accounts and accounts:
            self._active_account = accounts[0]
            self.query_one("#account-tabs", Tabs).active = f"tab-{self._active_account}"
            self._client = None
            self._start_loading()

    def _load_portfolio_cache(self) -> None:
        try:
            data = json.loads(get_portfolio_cache_path(self._active_account).read_text())
        except (FileNotFoundError, json.JSONDecodeError, OSError):
            return
        b = data.get("balance", {})
        if b:
            self.query_one(BalanceBar).update_display(
                b.get("total", "—"),
                b.get("bp", "—"),
                b.get("obp", "—"),
                b.get("crypto_bp", "—"),
                b.get("cash", "—"),
                b.get("cash_label", "CASH"),
            )
            if "margin_enabled" in b:
                self._margin_enabled = bool(b["margin_enabled"])
            if "margin_capacity" in b:
                self._margin_capacity = Decimal(str(b["margin_capacity"]))
        holdings = data.get("holdings", [])
        if holdings:
            self.query_one(HoldingsTable).refresh_from_cache(holdings)
        orders = data.get("orders", [])
        self.query_one(OrdersTable).refresh_from_cache(orders)
        positions = data.get("positions", [])
        if positions:
            self.query_one(PortfolioChart).set_positions(positions)
        account_id = data.get("account_id", "")
        if account_id:
            self.query_one(StatusBar).set_status(f"  {account_id} (cached)" + _HINT)

    @staticmethod
    def _save_portfolio_cache(
        account_id: str,
        balance: dict,
        holdings: list[dict],
        orders: list[dict],
        positions: list[dict],
    ) -> None:
        from config import get_portfolio_cache_path
        path = get_portfolio_cache_path(account_id)
        try:
            path.parent.mkdir(exist_ok=True)
            path.write_text(
                json.dumps(
                    {
                        "account_id": account_id,
                        "balance": balance,
                        "holdings": holdings,
                        "orders": orders,
                        "positions": positions,
                    }
                )
            )
        except OSError:
            pass

    @staticmethod
    def _get_margin_status(portfolio) -> tuple[bool, Decimal]:
        buying_power_obj = getattr(portfolio, "buying_power", None)
        raw_buying_power = getattr(buying_power_obj, "buying_power", None)
        raw_cash_only_buying_power = getattr(
            buying_power_obj, "cash_only_buying_power", None
        )
        buying_power = (
            Decimal(str(raw_buying_power))
            if raw_buying_power is not None
            else Decimal("0")
        )
        cash_only_buying_power = (
            Decimal(str(raw_cash_only_buying_power))
            if raw_cash_only_buying_power is not None
            else buying_power
        )
        cash_balance = sum(
            (e.value for e in getattr(portfolio, "equity", []) or []
             if getattr(e, "type", None) is not None and e.type.value == "CASH"),
            Decimal("0"),
        )
        margin_buying_power = max(Decimal("0"), buying_power - cash_only_buying_power)
        margin_enabled = margin_buying_power > 0 or cash_balance < 0
        total_equity_ex_cash = sum(
            (e.value for e in getattr(portfolio, "equity", []) or []
             if getattr(e, "type", None) is not None and e.type.value != "CASH"),
            Decimal("0"),
        )
        if margin_enabled:
            margin_loan = (
                -cash_balance
                if cash_balance < 0
                else max(
                    Decimal("0"),
                    total_equity_ex_cash + cash_balance - margin_buying_power,
                )
            )
            margin_capacity = max(Decimal("0"), margin_loan + margin_buying_power)
        else:
            margin_capacity = Decimal("0")
        return margin_enabled, margin_capacity

    @staticmethod
    def _get_crypto_buying_power(buying_power_obj) -> Decimal | None:
        for field_name in (
            "crypto_buying_power",
            "cryptoBuyingPower",
            "crypto_bp",
            "cryptoBp",
        ):
            raw_value = getattr(buying_power_obj, field_name, None)
            if raw_value is not None:
                return Decimal(str(raw_value))

        if hasattr(buying_power_obj, "model_dump"):
            dumped = buying_power_obj.model_dump(by_alias=False)
            for field_name in ("crypto_buying_power", "crypto_bp"):
                raw_value = dumped.get(field_name)
                if raw_value is not None:
                    return Decimal(str(raw_value))

            dumped_alias = buying_power_obj.model_dump(by_alias=True)
            for field_name in ("cryptoBuyingPower", "cryptoBp"):
                raw_value = dumped_alias.get(field_name)
                if raw_value is not None:
                    return Decimal(str(raw_value))

        return None

    def action_quit(self) -> None:
        self.workers.cancel_all()
        if self._client is not None:
            try:
                self._client.close()
            except Exception:
                pass
        signal.signal(signal.SIGALRM, lambda *_: os._exit(0))
        signal.alarm(3)
        self.exit()

    def on_unmount(self) -> None:
        if self._client is not None:
            try:
                self._client.close()
            except Exception:
                pass

    def _get_client(self):
        if self._client is None:
            from client import get_client
            self._client = get_client(self._active_account)
        return self._client

    @staticmethod
    def _systemctl(*args: str) -> tuple[int, str]:
        if not _HAS_SYSTEMCTL:
            return 1, "systemctl not available on this platform"
        result = subprocess.run(
            ["systemctl", "--user", *args],
            capture_output=True,
            text=True,
        )
        return result.returncode, (result.stdout + result.stderr).strip()

    def _reload_timer_unit(self) -> tuple[bool, str]:
        """Ensure installed user-level units are visible to systemd."""
        if not _HAS_SYSTEMCTL:
            return False, "systemctl not available on this platform"

        if not (TIMER_UNIT_PATH.exists() and SERVICE_UNIT_PATH.exists()):
            return False, "schedule is not installed; press [e] to install it"

        rc, out = self._systemctl("daemon-reload")
        if rc != 0:
            return False, out or "daemon-reload failed"

        return True, ""

    @work(thread=True)
    def load_rebalancer_status(self) -> None:
        if not _HAS_SYSTEMCTL:
            skip_pending = get_skip_file_path(self._active_account).exists()
            cfg = _load_rebalance_config(self._active_account)
            self.call_from_thread(
                self.query_one(RebalancerBar).update_status,
                None,
                None,
                "N/A (no systemd)",
                "N/A",
                skip_pending,
                cfg.get("index", "SP500"),
                cfg.get("top_n", 500),
                cfg.get("margin_usage_pct", 0.5),
                len(cfg.get("excluded_tickers", [])),
            )
            return
        rc_active, _ = self._systemctl("is-active", TIMER_UNIT)
        active = rc_active == 0

        rc_enabled, _ = self._systemctl("is-enabled", TIMER_UNIT)
        enabled = rc_enabled == 0

        _, show_out = self._systemctl(
            "show",
            TIMER_UNIT,
            "--property=LastTriggerUSec",
            "--property=NextElapseUSecRealtime",
        )
        props = {}
        for line in show_out.splitlines():
            if "=" in line:
                key, value = line.split("=", 1)
                props[key.strip()] = value.strip()

        last_run = "never"
        val = props.get("LastTriggerUSec", "")
        if val and val not in ("n/a", "0", ""):
            last_run = val

        next_run = "—"
        val = props.get("NextElapseUSecRealtime", "")
        if val and val not in ("n/a", "0", ""):
            next_run = val

        skip_pending = get_skip_file_path(self._active_account).exists()
        cfg = _load_rebalance_config(self._active_account)
        self.call_from_thread(
            self.query_one(RebalancerBar).update_status,
            active,
            enabled,
            last_run,
            next_run,
            skip_pending,
            cfg.get("index", "SP500"),
            cfg.get("top_n", 500),
            cfg.get("margin_usage_pct", 0.5),
            len(cfg.get("excluded_tickers", [])),
        )

    @work(thread=True, exclusive=True)
    def load_portfolio(self) -> None:
        status = self.query_one(StatusBar)
        try:
            client = self._get_client()
            portfolio = client.get_portfolio()
            total = sum(e.value for e in portfolio.equity)
            buying_power = getattr(portfolio, "buying_power", None)
            bp = getattr(buying_power, "buying_power", None) or Decimal(0)
            obp = getattr(buying_power, "options_buying_power", None) or Decimal(0)
            crypto_bp = self._get_crypto_buying_power(buying_power)
            cash = next(
                (e.value for e in portfolio.equity if e.type.value == "CASH"),
                Decimal(0),
            )
            cash_label = "MARGIN BALANCE" if cash < 0 else "CASH"
            margin_enabled, margin_capacity = self._get_margin_status(portfolio)
            self._margin_enabled = margin_enabled
            self._margin_capacity = margin_capacity

            balance_data = {
                "total": f"${total:,.2f}",
                "bp": f"${bp:,.2f}",
                "obp": f"${obp:,.2f}",
                "crypto_bp": f"${crypto_bp:,.2f}" if crypto_bp is not None else "—",
                "cash": f"${cash:,.2f}",
                "cash_label": cash_label,
                "margin_enabled": margin_enabled,
                "margin_capacity": str(margin_capacity),
            }
            holdings_data: list[dict] = []
            for pos in portfolio.positions:
                current_value = pos.current_value or Decimal(0)
                price = (
                    f"${pos.last_price.last_price:,.2f}"
                    if pos.last_price and pos.last_price.last_price
                    else "—"
                )
                value = f"${current_value:,.2f}" if pos.current_value else "—"
                if (
                    pos.position_daily_gain
                    and pos.position_daily_gain.gain_percentage is not None
                ):
                    pct = float(pos.position_daily_gain.gain_percentage)
                    gain_str = f"{'+' if pct >= 0 else ''}{pct:.2f}%"
                    gain_positive = pct >= 0
                else:
                    gain_str, gain_positive = "—", False
                holdings_data.append(
                    {
                        "symbol": pos.instrument.symbol,
                        "type": pos.instrument.type.value,
                        "qty": str(pos.quantity),
                        "price": price,
                        "value": value,
                        "value_num": float(current_value),
                        "gain": gain_str,
                        "gain_positive": gain_positive,
                    }
                )
            orders_data: list[dict] = []
            for order in portfolio.orders:
                if order.status not in _ACTIVE_ORDER_STATUSES:
                    continue
                orders_data.append(
                    {
                        "side": order.side.value,
                        "side_buy": order.side == OrderSide.BUY,
                        "symbol": order.instrument.symbol,
                        "qty": str(order.quantity or order.notional_value or "—"),
                        "type": order.type.value,
                        "status": order.status.value,
                        "order_id": order.order_id,
                    }
                )
            positions_data = [
                {"symbol": pos.instrument.symbol, "qty": float(pos.quantity)}
                for pos in portfolio.positions
                if pos.quantity
            ]
            self._save_portfolio_cache(
                self._active_account,
                balance_data,
                holdings_data,
                orders_data,
                positions_data,
            )

            self.call_from_thread(
                self.query_one(BalanceBar).update_display,
                balance_data["total"],
                balance_data["bp"],
                balance_data["obp"],
                balance_data["crypto_bp"],
                balance_data["cash"],
                balance_data["cash_label"],
            )
            self.call_from_thread(
                self.query_one(HoldingsTable).refresh_from_cache, holdings_data
            )
            self.call_from_thread(
                self.query_one(PortfolioChart).set_positions, portfolio.positions
            )
            if self._live_chart:
                self.call_from_thread(
                    self.query_one(PortfolioChart).add_live_point, float(total)
                )
            self.call_from_thread(
                self.query_one(OrdersTable).refresh_from_orders, portfolio.orders
            )
            stream_suffix = (
                f"  |  LIVE 24H/{LIVE_PORTFOLIO_POLL_SECONDS}s"
                if self._live_chart
                else ""
            )
            self.call_from_thread(
                status.set_status, f"  {portfolio.account_id}{stream_suffix}" + _HINT
            )
        except Exception as exc:
            self.call_from_thread(
                status.set_status, f"  Error: {exc}  |  Check .env credentials", "red"
            )

    def action_refresh(self) -> None:
        self.load_portfolio()
        self.load_rebalancer_status()

    def _poll_live_portfolio(self) -> None:
        if self._live_chart:
            self.load_portfolio()

    def action_toggle_live_chart(self) -> None:
        self._live_chart = not self._live_chart
        chart = self.query_one(PortfolioChart)
        status = self.query_one(StatusBar)
        if self._live_chart:
            chart.set_live_enabled(True)
            if self._live_timer is not None:
                self._live_timer.resume()
            status.set_status(
                f"  Live 24H portfolio stream ON — polling every {LIVE_PORTFOLIO_POLL_SECONDS}s"
                + _HINT,
                "green",
            )
            self.load_portfolio()
        else:
            if self._live_timer is not None:
                self._live_timer.pause()
            chart.set_live_enabled(False)
            status.set_status("  Live portfolio stream OFF" + _HINT, "cyan")

    def action_buy(self) -> None:
        self.push_screen(OrderModal(OrderSide.BUY), self._handle_order_result)

    def action_sell(self) -> None:
        self.push_screen(OrderModal(OrderSide.SELL), self._handle_order_result)

    def _handle_order_result(self, result: dict | None) -> None:
        if result is None:
            return
        self._place_order(
            symbol=result["symbol"],
            instrument_type=InstrumentType(result["instrument_type"]),
            quantity=result["quantity"],
            side=result["side"],
        )

    @work(thread=True)
    def _place_order(
        self,
        symbol: str,
        instrument_type: InstrumentType,
        quantity: Decimal,
        side: OrderSide,
    ) -> None:
        status = self.query_one(StatusBar)
        try:
            client = self._get_client()
            validate_order_instrument(client, symbol, instrument_type, side)
            request = OrderRequest(
                order_id=str(uuid.uuid4()),
                instrument=OrderInstrument(symbol=symbol, type=instrument_type),
                order_side=side,
                order_type=OrderType.MARKET,
                expiration=OrderExpirationRequest(time_in_force=TimeInForce.DAY),
                quantity=quantity,
            )
            new_order = client.place_order(request)
            self.call_from_thread(
                status.set_status,
                f"  Order submitted: {side.value} {quantity} {symbol} (ID: {new_order.order_id[:8]}…)"
                + _HINT,
                "green",
            )
            self.call_from_thread(self.load_portfolio)
        except Exception as exc:
            self.call_from_thread(status.set_status, f"  Order failed: {exc}", "red")

    def action_cancel_order(self) -> None:
        orders_table = self.query_one(OrdersTable)
        result = orders_table.get_selected_order_id()
        if result is None:
            self.query_one(StatusBar).set_status(
                "  No open order selected — use arrow keys to select a row in Orders",
                "yellow",
            )
            return
        order_id, symbol = result
        self.push_screen(
            CancelConfirmModal(order_id, symbol),
            lambda confirmed: self._handle_cancel_order_result(
                confirmed, order_id, symbol
            ),
        )

    def _handle_cancel_order_result(
        self, confirmed: bool, order_id: str, symbol: str
    ) -> None:
        if confirmed:
            self._do_cancel(order_id, symbol)

    @work(thread=True)
    def _do_cancel(self, order_id: str, symbol: str) -> None:
        status = self.query_one(StatusBar)
        try:
            client = self._get_client()
            client.cancel_order(order_id)
            self.call_from_thread(
                status.set_status,
                f"  Cancellation submitted for {symbol} (ID: {order_id[:8]}…)" + _HINT,
                "yellow",
            )
            self.call_from_thread(self.load_portfolio)
        except Exception as exc:
            self.call_from_thread(status.set_status, f"  Cancel failed: {exc}", "red")

    @work(thread=True)
    def action_toggle_rebalancer(self) -> None:
        status = self.query_one(StatusBar)
        if not _HAS_SYSTEMCTL:
            self.call_from_thread(
                status.set_status,
                "  Pause/resume requires systemctl on this platform.",
                "red",
            )
            return

        rc_enabled, _ = self._systemctl("is-enabled", TIMER_UNIT)
        if rc_enabled != 0:
            self.call_from_thread(
                status.set_status,
                "  No rebalancer schedule installed — press [e] to install it."
                + _HINT,
                "yellow",
            )
            self.call_from_thread(self.load_rebalancer_status)
            return

        rc, _ = self._systemctl("is-active", TIMER_UNIT)
        if rc == 0:
            self.call_from_thread(
                status.set_status,
                "  Pausing rebalancer schedule…" + _HINT,
                "yellow",
            )
            rc2, out = self._systemctl("stop", TIMER_UNIT)
            msg = (
                "  Rebalancer schedule paused — press [t] to resume."
                if rc2 == 0
                else f"  Pause failed: {out}"
            )
        else:
            self.call_from_thread(
                status.set_status,
                "  Resuming rebalancer schedule…" + _HINT,
                "yellow",
            )
            ok, prep_msg = self._reload_timer_unit()
            if not ok:
                self.call_from_thread(
                    status.set_status,
                    f"  Could not resume schedule: {prep_msg}",
                    "red",
                )
                return
            rc2, out = self._systemctl("start", TIMER_UNIT)
            msg = (
                "  Rebalancer schedule resumed — next run follows the 12:00 ET schedule."
                if rc2 == 0
                else f"  Resume failed: {out}"
            )
        self.call_from_thread(
            status.set_status, msg + _HINT, "green" if rc2 == 0 else "red"
        )
        self.call_from_thread(self.load_rebalancer_status)

    @work(thread=True)
    def action_toggle_enable_rebalancer(self) -> None:
        status = self.query_one(StatusBar)
        if not _HAS_SYSTEMCTL:
            self.call_from_thread(
                status.set_status,
                "  Install/remove schedule requires systemctl on this platform.",
                "red",
            )
            return

        rc, _ = self._systemctl("is-enabled", TIMER_UNIT)
        if rc == 0:
            self.call_from_thread(
                status.set_status,
                "  Removing rebalancer schedule…" + _HINT,
                "yellow",
            )
            try:
                _remove_service_files()
                self.call_from_thread(
                    status.set_status,
                    "  Rebalancer schedule removed." + _HINT,
                    "cyan",
                )
            except Exception as exc:
                self.call_from_thread(
                    status.set_status, f"  Remove schedule failed: {exc}", "red"
                )
                return
        else:
            self.call_from_thread(
                status.set_status,
                "  Installing rebalancer schedule…" + _HINT,
                "yellow",
            )
            try:
                _install_service_files()
                rc2, out = self._systemctl("enable", "--now", TIMER_UNIT)
            except Exception as exc:
                self.call_from_thread(
                    status.set_status,
                    f"  Install/enable failed: {exc}",
                    "red",
                )
                return
            if rc2 != 0:
                self.call_from_thread(
                    status.set_status,
                    f"  Schedule activation failed: {out}" + _HINT,
                    "red",
                )
                return
            self.call_from_thread(
                status.set_status,
                "  Rebalancer timer enabled — scheduled Mon-Fri at 12:00 ET."
                + _HINT,
                "green",
            )
        self.call_from_thread(self.load_rebalancer_status)

    def action_skip_next_rebalance(self) -> None:
        status = self.query_one(StatusBar)
        skip_file = get_skip_file_path(self._active_account)
        try:
            skip_file.unlink()
            status.set_status(
                "  Skip cancelled — next run will proceed normally." + _HINT, "cyan"
            )
        except FileNotFoundError:
            skip_file.parent.mkdir(exist_ok=True)
            skip_file.touch()
            status.set_status(
                "  Next rebalance run will be skipped. Press [x] again to cancel."
                + _HINT,
                "yellow",
            )
        self.load_rebalancer_status()

    def action_rebalance_now(self) -> None:
        self.push_screen(
            RebalanceNowConfirmModal(), self._handle_rebalance_now_result
        )

    def _handle_rebalance_now_result(self, confirmed: bool) -> None:
        if confirmed:
            self._trigger_rebalance_now()

    @work(thread=True)
    def _trigger_rebalance_now(self) -> None:
        status = self.query_one(StatusBar)
        self.call_from_thread(
            status.set_status, "  Starting on-demand rebalance…" + _HINT, "yellow"
        )

        if _HAS_SYSTEMCTL and SERVICE_UNIT_PATH.exists():
            rc, out = self._systemctl("start", "--no-block", SERVICE_UNIT)
            if rc == 0:
                self.call_from_thread(
                    status.set_status,
                    f"  Rebalance started — tail logs with: journalctl --user -u {SERVICE_UNIT} -f"
                    + _HINT,
                    "green",
                )
            else:
                self.call_from_thread(
                    status.set_status,
                    f"  Failed to start rebalance: {out}" + _HINT,
                    "red",
                )
            self.call_from_thread(self.load_rebalancer_status)
            return

        try:
            if getattr(sys, "frozen", False):
                cmd = [sys.executable, "--rebalance"]
            else:
                main_py = (Path(__file__).parent / "main.py").resolve()
                cmd = [sys.executable, str(main_py), "--rebalance"]
            subprocess.Popen(
                cmd,
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
                stdin=subprocess.DEVNULL,
                start_new_session=True,
            )
        except Exception as exc:
            self.call_from_thread(
                status.set_status, f"  Failed to start rebalance: {exc}" + _HINT, "red"
            )
            return

        self.call_from_thread(
            status.set_status,
            "  Rebalance started — logs in cache/rebalance.log" + _HINT,
            "green",
        )

    def action_rebalance_settings(self) -> None:
        from rebalance import (
            _DEFAULT_ALLOCS,
            _ETF_TO_INDEX,
            _INDEX_SP500,
            SUPPORTED_INDEXES,
        )

        cfg = _load_rebalance_config(self._active_account)
        current_index = cfg.get("index") or cfg.get("etf_ticker", "SP500")
        if current_index not in SUPPORTED_INDEXES:
            current_index = _ETF_TO_INDEX.get(current_index, _INDEX_SP500)
        default_allocs = {k: float(v) for k, v in _DEFAULT_ALLOCS.items()}
        current_allocs = cfg.get("allocations", default_allocs)
        self.push_screen(
            RebalanceConfigModal(
                current_index,
                cfg.get("top_n", 500),
                cfg.get("margin_usage_pct", 0.5),
                cfg.get("excluded_tickers", []),
                current_allocs,
                self._margin_enabled,
                self._margin_capacity,
                cfg.get("rebalance_enabled", True),
            ),
            self._handle_rebalance_settings,
        )

    def _handle_rebalance_settings(self, result: dict | None) -> None:
        if result is None:
            return
        try:
            from rebalance import SUPPORTED_INDEXES

            _save_rebalance_config(
                self._active_account,
                result["index"],
                result["top_n"],
                result["margin_usage_pct"],
                result["excluded_tickers"],
                result["allocations"],
                result.get("rebalance_enabled", True),
            )
            pct = int(result["margin_usage_pct"] * 100)
            excl = result["excluded_tickers"]
            excl_str = f"  excl {len(excl)}" if excl else ""
            index_label = SUPPORTED_INDEXES.get(result["index"], result["index"])
            a = result["allocations"]
            alloc_summary = (
                f"stk {round(a['stocks'] * 100)}%  "
                f"btc {round(a['btc'] * 100)}%  "
                f"eth {round(a['eth'] * 100)}%  "
                f"gold {round(a['gold'] * 100)}%  "
                f"cash {round(a['cash'] * 100)}%"
            )
            enabled_str = "" if result.get("rebalance_enabled", True) else "  REBALANCING DISABLED"
            self.query_one(StatusBar).set_status(
                f"  Saved: {index_label} top-{result['top_n']}  margin {pct}%{excl_str}  |  {alloc_summary}{enabled_str}"
                + _HINT,
                "green",
            )
            self.load_rebalancer_status()
        except OSError as exc:
            self.query_one(StatusBar).set_status(
                f"  Failed to save config: {exc}", "red"
            )

    def action_chart_prev(self) -> None:
        self.query_one(PortfolioChart).cycle_period(-1)

    def action_chart_next(self) -> None:
        self.query_one(PortfolioChart).cycle_period(1)

    def action_history(self) -> None:
        self.push_screen(HistoryModal(self._get_client()))
