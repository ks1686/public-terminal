"""Textual App class for the Public Terminal TUI."""

from __future__ import annotations

import json
import os
import signal
import subprocess
import uuid
from decimal import Decimal

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
from textual.widgets import Footer, Header, Label

from config import (
    _ACTIVE_ORDER_STATUSES,
    _HAS_SYSTEMCTL,
    PORTFOLIO_CACHE,
    SKIP_FILE,
    _credentials_present,
    _install_service_files,
    _load_rebalance_config,
    _remove_service_files,
    _save_rebalance_config,
)
from modals import (
    CancelConfirmModal,
    HistoryModal,
    OrderModal,
    RebalanceConfigModal,
    RunNowModal,
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

_HINT = "  |  [r] Refresh  [b] Buy  [s] Sell  [c] Cancel  [q] Quit"
TIMER_UNIT = "public-terminal-rebalance.timer"
SERVICE_UNIT = "public-terminal-rebalance.service"


class PublicTerminal(App):
    TITLE = "PUBLIC TERMINAL"
    CSS = """
    Screen { background: $surface; }
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
        Binding("t", "toggle_rebalancer", "Start/Stop Rebalancer"),
        Binding("e", "toggle_enable_rebalancer", "Enable/Disable Rebalancer"),
        Binding("x", "skip_next_rebalance", "Skip Next Run"),
        Binding("R", "run_rebalancer_now", "Run Now"),
        Binding("S", "rebalance_settings", "Settings"),
        Binding("I", "install_service", "Install Service"),
        Binding("D", "remove_service", "Remove Service"),
        Binding("[", "chart_prev", "Chart ◄"),
        Binding("]", "chart_next", "Chart ►"),
    ]

    def __init__(self) -> None:
        super().__init__()
        self._client = None

    def compose(self) -> ComposeResult:
        yield Header(show_clock=True)
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
        if not _credentials_present():
            self.push_screen(SetupModal(), self._handle_setup)
        else:
            self._start_loading()

    def _handle_setup(self, saved: bool) -> None:
        if not saved:
            self.exit()
            return
        self.query_one(StatusBar).set_status(
            "  Credentials saved to .env — connecting…" + _HINT
        )
        self._start_loading()

    def _start_loading(self) -> None:
        self._load_portfolio_cache()
        self.query_one(StatusBar).set_status("  Connecting…" + _HINT)
        self.load_portfolio()
        self.load_rebalancer_status()

    def _load_portfolio_cache(self) -> None:
        try:
            data = json.loads(PORTFOLIO_CACHE.read_text())
        except (FileNotFoundError, json.JSONDecodeError, OSError):
            return
        b = data.get("balance", {})
        if b:
            self.query_one(BalanceBar).update_display(
                b.get("total", "—"),
                b.get("bp", "—"),
                b.get("obp", "—"),
                b.get("cash", "—"),
            )
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
        try:
            PORTFOLIO_CACHE.parent.mkdir(exist_ok=True)
            PORTFOLIO_CACHE.write_text(
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

            self._client = get_client()
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

    @work(thread=True)
    def load_rebalancer_status(self) -> None:
        if not _HAS_SYSTEMCTL:
            skip_pending = SKIP_FILE.exists()
            cfg = _load_rebalance_config()
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

        skip_pending = SKIP_FILE.exists()
        cfg = _load_rebalance_config()
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

    @work(thread=True)
    def load_portfolio(self) -> None:
        status = self.query_one(StatusBar)
        try:
            client = self._get_client()
            portfolio = client.get_portfolio()
            total = sum(e.value for e in portfolio.equity)
            buying_power = getattr(portfolio, "buying_power", None)
            bp = getattr(buying_power, "buying_power", None) or Decimal(0)
            obp = getattr(buying_power, "options_buying_power", None) or Decimal(0)
            cash = next(
                (e.value for e in portfolio.equity if e.type.value == "CASH"),
                Decimal(0),
            )

            balance_data = {
                "total": f"${total:,.2f}",
                "bp": f"${bp:,.2f}",
                "obp": f"${obp:,.2f}",
                "cash": f"${cash:,.2f}",
            }
            holdings_data: list[dict] = []
            for pos in portfolio.positions:
                price = (
                    f"${pos.last_price.last_price:,.2f}"
                    if pos.last_price and pos.last_price.last_price
                    else "—"
                )
                value = f"${pos.current_value:,.2f}" if pos.current_value else "—"
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
                str(portfolio.account_id),
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
                balance_data["cash"],
            )
            self.call_from_thread(
                self.query_one(HoldingsTable).refresh_from_cache, holdings_data
            )
            self.call_from_thread(
                self.query_one(PortfolioChart).set_positions, portfolio.positions
            )
            self.call_from_thread(
                self.query_one(OrdersTable).refresh_from_orders, portfolio.orders
            )
            self.call_from_thread(
                status.set_status, f"  {portfolio.account_id}" + _HINT
            )
        except Exception as exc:
            self.call_from_thread(
                status.set_status, f"  Error: {exc}  |  Check .env credentials", "red"
            )

    def action_refresh(self) -> None:
        self.load_portfolio()
        self.load_rebalancer_status()

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
        rc, _ = self._systemctl("is-active", TIMER_UNIT)
        if rc == 0:
            rc2, out = self._systemctl("stop", TIMER_UNIT)
            msg = "  Rebalancer stopped." if rc2 == 0 else f"  Stop failed: {out}"
        else:
            rc2, out = self._systemctl("start", TIMER_UNIT)
            msg = "  Rebalancer started." if rc2 == 0 else f"  Start failed: {out}"
        self.call_from_thread(
            status.set_status, msg + _HINT, "green" if rc2 == 0 else "red"
        )
        self.call_from_thread(self.load_rebalancer_status)

    @work(thread=True)
    def action_toggle_enable_rebalancer(self) -> None:
        status = self.query_one(StatusBar)
        rc, _ = self._systemctl("is-enabled", TIMER_UNIT)
        if rc == 0:
            rc2, out = self._systemctl("disable", TIMER_UNIT)
            msg = (
                "  Rebalancer disabled (won't start on login)."
                if rc2 == 0
                else f"  Disable failed: {out}"
            )
        else:
            rc2, out = self._systemctl("enable", TIMER_UNIT)
            msg = (
                "  Rebalancer enabled (starts automatically on login)."
                if rc2 == 0
                else f"  Enable failed: {out}"
            )
        self.call_from_thread(status.set_status, msg, "cyan" if rc2 == 0 else "red")
        self.call_from_thread(self.load_rebalancer_status)

    def action_skip_next_rebalance(self) -> None:
        status = self.query_one(StatusBar)
        try:
            SKIP_FILE.unlink()
            status.set_status(
                "  Skip cancelled — next run will proceed normally." + _HINT, "cyan"
            )
        except FileNotFoundError:
            SKIP_FILE.parent.mkdir(exist_ok=True)
            SKIP_FILE.touch()
            status.set_status(
                "  Next rebalance run will be skipped. Press [x] again to cancel."
                + _HINT,
                "yellow",
            )
        self.load_rebalancer_status()

    def action_run_rebalancer_now(self) -> None:
        self.push_screen(RunNowModal(), self._handle_run_now)

    def _handle_run_now(self, confirmed: bool) -> None:
        if confirmed:
            self._do_run_now()

    @work(thread=True)
    def _do_run_now(self) -> None:
        status = self.query_one(StatusBar)
        if _HAS_SYSTEMCTL:
            rc, out = self._systemctl("start", SERVICE_UNIT)
            if rc == 0:
                self.call_from_thread(
                    status.set_status,
                    "  Rebalancer triggered — check cache/rebalance.log for progress."
                    + _HINT,
                    "green",
                )
                self.call_from_thread(self.load_rebalancer_status)
                return
        # systemctl unavailable or unit not installed — run rebalancer directly in this thread
        self.call_from_thread(
            status.set_status,
            "  Rebalancer running (no systemd service) — this may take several minutes…"
            + _HINT,
            "yellow",
        )
        try:
            from rebalance import rebalance

            rebalance()
            self.call_from_thread(
                status.set_status,
                "  Rebalance complete — check cache/rebalance.log for details." + _HINT,
                "green",
            )
        except Exception as exc:
            self.call_from_thread(status.set_status, f"  Rebalance error: {exc}", "red")
        self.call_from_thread(self.load_rebalancer_status)

    def action_rebalance_settings(self) -> None:
        from rebalance import (
            _DEFAULT_ALLOCS,
            _ETF_TO_INDEX,
            _INDEX_SP500,
            SUPPORTED_INDEXES,
        )

        cfg = _load_rebalance_config()
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
            ),
            self._handle_rebalance_settings,
        )

    def _handle_rebalance_settings(self, result: dict | None) -> None:
        if result is None:
            return
        try:
            from rebalance import SUPPORTED_INDEXES

            _save_rebalance_config(
                result["index"],
                result["top_n"],
                result["margin_usage_pct"],
                result["excluded_tickers"],
                result["allocations"],
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
            self.query_one(StatusBar).set_status(
                f"  Saved: {index_label} top-{result['top_n']}  margin {pct}%{excl_str}  |  {alloc_summary}"
                + _HINT,
                "green",
            )
            self.load_rebalancer_status()
        except OSError as exc:
            self.query_one(StatusBar).set_status(
                f"  Failed to save config: {exc}", "red"
            )

    @work(thread=True)
    def action_install_service(self) -> None:
        status = self.query_one(StatusBar)
        self.call_from_thread(
            status.set_status, "  Installing systemd service…" + _HINT, "yellow"
        )
        try:
            msg = _install_service_files()
            short = msg.splitlines()[0]
            self.call_from_thread(status.set_status, f"  {short}" + _HINT, "green")
            self.call_from_thread(self.load_rebalancer_status)
        except Exception as exc:
            self.call_from_thread(status.set_status, f"  Install failed: {exc}", "red")

    @work(thread=True)
    def action_remove_service(self) -> None:
        status = self.query_one(StatusBar)
        self.call_from_thread(
            status.set_status, "  Removing systemd service…" + _HINT, "yellow"
        )
        try:
            msg = _remove_service_files()
            self.call_from_thread(status.set_status, f"  {msg}" + _HINT, "cyan")
            self.call_from_thread(self.load_rebalancer_status)
        except Exception as exc:
            self.call_from_thread(status.set_status, f"  Remove failed: {exc}", "red")

    def action_chart_prev(self) -> None:
        self.query_one(PortfolioChart).cycle_period(-1)

    def action_chart_next(self) -> None:
        self.query_one(PortfolioChart).cycle_period(1)

    def action_history(self) -> None:
        self.push_screen(HistoryModal(self._get_client()))
