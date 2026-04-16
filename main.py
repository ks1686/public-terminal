"""Public Terminal — btop/htop-style trading TUI."""

from __future__ import annotations

import json
import os
import subprocess
import uuid
from pathlib import Path
from decimal import Decimal, InvalidOperation

import pandas as pd
import yfinance as yf

from rich.text import Text
from textual import on, work
from textual.app import App, ComposeResult
from textual.binding import Binding
from textual.containers import Horizontal, Vertical, Grid
from textual.screen import ModalScreen
from textual.widgets import (
    Button,
    DataTable,
    Footer,
    Header,
    Input,
    Label,
    Select,
    Static,
)

from public_api_sdk import (
    InstrumentType,
    OrderExpirationRequest,
    OrderInstrument,
    OrderRequest,
    OrderSide,
    OrderStatus,
    OrderType,
    TimeInForce,
)

INSTRUMENT_OPTIONS = [
    ("Equity / ETF / Stock", "EQUITY"),
    ("Crypto", "CRYPTO"),
    ("Corporate Bond", "BOND"),
    ("Treasury", "TREASURY"),
]

_HINT = "  |  [r] Refresh  [b] Buy  [s] Sell  [c] Cancel  [q] Quit"

PORTFOLIO_CACHE = Path(__file__).parent / "cache" / "portfolio_cache.json"

CHART_PERIODS = [
    ("1D", "1d",  "5m"),
    ("1W", "5d",  "1h"),
    ("1M", "1mo", "1d"),
    ("3M", "3mo", "1d"),
    ("1Y", "1y",  "1d"),
]

# Map Public.com crypto symbols to yfinance tickers
YF_TICKERS = {"BTC": "BTC-USD", "ETH": "ETH-USD"}

TIMER_UNIT             = "public-terminal-rebalance.timer"
SERVICE_UNIT           = "public-terminal-rebalance.service"
SKIP_FILE              = Path(__file__).parent / "cache" / "skip_next_rebalance"
REBALANCE_CONFIG_FILE  = Path(__file__).parent / "rebalance_config.json"


def _load_rebalance_config() -> dict:
    try:
        return json.loads(REBALANCE_CONFIG_FILE.read_text())
    except (FileNotFoundError, json.JSONDecodeError, OSError):
        return {"etf_ticker": "SPY", "top_n": 100}


def _save_rebalance_config(etf_ticker: str, top_n: int, margin_usage_pct: float) -> None:
    REBALANCE_CONFIG_FILE.write_text(
        json.dumps({"etf_ticker": etf_ticker, "top_n": top_n, "margin_usage_pct": margin_usage_pct}, indent=2)
    )


# ---------------------------------------------------------------------------
# Order entry modal
# ---------------------------------------------------------------------------

class OrderModal(ModalScreen[dict | None]):
    """Modal for entering a market buy or sell order."""

    DEFAULT_CSS = """
    OrderModal {
        align: center middle;
    }
    #dialog {
        width: 60;
        height: auto;
        border: thick $primary;
        background: $surface;
        padding: 1 2;
    }
    #dialog-title {
        text-align: center;
        text-style: bold;
        height: 1;
        margin-bottom: 1;
    }
    .field-label {
        height: 1;
        margin-top: 1;
        color: $text-muted;
    }
    #btn-row {
        margin-top: 1;
        height: 3;
        align: center middle;
    }
    #btn-confirm {
        margin-right: 2;
    }
    """

    def __init__(self, side: OrderSide) -> None:
        super().__init__()
        self._side = side

    def compose(self) -> ComposeResult:
        title = f"MARKET {self._side.value}"
        with Grid(id="dialog"):
            yield Label(title, id="dialog-title")
            yield Label("Symbol", classes="field-label")
            yield Input(placeholder="e.g. AAPL, BTC", id="input-symbol")
            yield Label("Instrument type", classes="field-label")
            yield Select(
                [(label, val) for label, val in INSTRUMENT_OPTIONS],
                value="EQUITY",
                id="select-type",
            )
            yield Label("Quantity (shares / units)", classes="field-label")
            yield Input(placeholder="e.g. 10 or 0.5", id="input-qty")
            with Horizontal(id="btn-row"):
                yield Button(f"Confirm {self._side.value}", variant="success" if self._side == OrderSide.BUY else "error", id="btn-confirm")
                yield Button("Cancel", variant="default", id="btn-cancel")

    @on(Button.Pressed, "#btn-cancel")
    def cancel(self) -> None:
        self.dismiss(None)

    @on(Button.Pressed, "#btn-confirm")
    def confirm(self) -> None:
        symbol = self.query_one("#input-symbol", Input).value.strip().upper()
        instrument_type_val = self.query_one("#select-type", Select).value
        qty_str = self.query_one("#input-qty", Input).value.strip()

        if not symbol:
            self.query_one("#input-symbol", Input).focus()
            return
        try:
            qty = Decimal(qty_str)
            if qty <= 0:
                raise ValueError
        except (InvalidOperation, ValueError):
            self.query_one("#input-qty", Input).focus()
            return

        self.dismiss({
            "symbol": symbol,
            "instrument_type": instrument_type_val,
            "quantity": qty,
            "side": self._side,
        })


# ---------------------------------------------------------------------------
# Confirm cancel modal
# ---------------------------------------------------------------------------

class CancelConfirmModal(ModalScreen[bool]):
    """Confirmation dialog before cancelling an open order."""

    DEFAULT_CSS = """
    CancelConfirmModal {
        align: center middle;
    }
    #cancel-dialog {
        width: 50;
        height: auto;
        border: thick $error;
        background: $surface;
        padding: 1 2;
    }
    #cancel-title {
        text-align: center;
        text-style: bold;
        height: 1;
        margin-bottom: 1;
    }
    #cancel-btn-row {
        margin-top: 1;
        height: 3;
        align: center middle;
    }
    """

    def __init__(self, order_id: str, symbol: str) -> None:
        super().__init__()
        self._order_id = order_id
        self._symbol = symbol

    def compose(self) -> ComposeResult:
        with Grid(id="cancel-dialog"):
            yield Label("CANCEL ORDER", id="cancel-title")
            yield Label(f"Cancel order for [bold]{self._symbol}[/bold]?  (ID: {self._order_id[:8]}…)")
            with Horizontal(id="cancel-btn-row"):
                yield Button("Yes, cancel", variant="error", id="btn-yes")
                yield Button("No", variant="default", id="btn-no")

    @on(Button.Pressed, "#btn-yes")
    def yes(self) -> None:
        self.dismiss(True)

    @on(Button.Pressed, "#btn-no")
    def no(self) -> None:
        self.dismiss(False)


class RunNowModal(ModalScreen[bool]):
    """Confirmation before triggering an immediate rebalance."""

    DEFAULT_CSS = """
    RunNowModal {
        align: center middle;
    }
    #run-dialog {
        width: 54;
        height: auto;
        border: thick $warning;
        background: $surface;
        padding: 1 2;
    }
    #run-title {
        text-align: center;
        text-style: bold;
        height: 1;
        margin-bottom: 1;
    }
    #run-btn-row {
        margin-top: 1;
        height: 3;
        align: center middle;
    }
    """

    def compose(self) -> ComposeResult:
        with Grid(id="run-dialog"):
            yield Label("RUN REBALANCER NOW", id="run-title")
            yield Label("This will immediately place market orders to\nrebalance your portfolio. Proceed?")
            with Horizontal(id="run-btn-row"):
                yield Button("Run Now", variant="warning", id="btn-run")
                yield Button("Cancel", variant="default", id="btn-cancel")

    @on(Button.Pressed, "#btn-run")
    def run(self) -> None:
        self.dismiss(True)

    @on(Button.Pressed, "#btn-cancel")
    def cancel(self) -> None:
        self.dismiss(False)


# ---------------------------------------------------------------------------
# Rebalance settings modal
# ---------------------------------------------------------------------------

class RebalanceConfigModal(ModalScreen):
    """Modal for configuring the index ETF and top-N stock count."""

    _SUPPORTED = (
        "S&P 500:    SPY  VOO  IVV  SPLG\n"
        "NASDAQ-100: QQQ  QQQM\n"
        "Dow Jones:  DIA\n"
        "Other tickers fall back to S&P 500."
    )

    DEFAULT_CSS = """
    RebalanceConfigModal {
        align: center middle;
    }
    #cfg-dialog {
        width: 64;
        height: auto;
        border: thick $primary;
        background: $surface;
        padding: 1 2;
    }
    #cfg-title {
        text-align: center;
        text-style: bold;
        height: 1;
        margin-bottom: 1;
    }
    #cfg-hint {
        color: $text-muted;
        margin-bottom: 1;
    }
    .field-label {
        height: 1;
        margin-top: 1;
        color: $text-muted;
    }
    #cfg-btn-row {
        margin-top: 1;
        height: 3;
        align: center middle;
    }
    #cfg-btn-save {
        margin-right: 2;
    }
    """

    def __init__(self, current_etf: str, current_top_n: int, current_margin_pct: float) -> None:
        super().__init__()
        self._current_etf = current_etf
        self._current_top_n = current_top_n
        self._current_margin_pct = current_margin_pct

    def compose(self) -> ComposeResult:
        with Grid(id="cfg-dialog"):
            yield Label("REBALANCE SETTINGS", id="cfg-title")
            yield Label(self._SUPPORTED, id="cfg-hint")
            yield Label("Index ETF ticker", classes="field-label")
            yield Input(value=self._current_etf, id="input-etf")
            yield Label("Top N stocks", classes="field-label")
            yield Input(value=str(self._current_top_n), id="input-top-n")
            yield Label("Margin usage (0.0 = cash only, 0.5 = 50% of margin, 1.0 = full)", classes="field-label")
            yield Input(value=str(self._current_margin_pct), id="input-margin")
            with Horizontal(id="cfg-btn-row"):
                yield Button("Save", variant="success", id="cfg-btn-save")
                yield Button("Cancel", variant="default", id="cfg-btn-cancel")

    @on(Button.Pressed, "#cfg-btn-cancel")
    def cancel(self) -> None:
        self.dismiss(None)

    @on(Button.Pressed, "#cfg-btn-save")
    def save(self) -> None:
        etf = self.query_one("#input-etf", Input).value.strip().upper()
        top_n_str = self.query_one("#input-top-n", Input).value.strip()
        margin_str = self.query_one("#input-margin", Input).value.strip()
        if not etf:
            self.query_one("#input-etf", Input).focus()
            return
        try:
            top_n = int(top_n_str)
            if top_n < 1:
                raise ValueError
        except ValueError:
            self.query_one("#input-top-n", Input).focus()
            return
        try:
            margin_pct = float(margin_str)
            if not 0.0 <= margin_pct <= 1.0:
                raise ValueError
        except ValueError:
            self.query_one("#input-margin", Input).focus()
            return
        self.dismiss({"etf_ticker": etf, "top_n": top_n, "margin_usage_pct": margin_pct})


# ---------------------------------------------------------------------------
# Widgets
# ---------------------------------------------------------------------------

class BalanceBar(Static):
    DEFAULT_CSS = """
    BalanceBar {
        height: 3;
        background: $panel;
        border: tall $primary;
        padding: 0 2;
        content-align: left middle;
    }
    """

    def on_mount(self) -> None:
        self.update_display("—", "—", "—", "—")

    def update_display(self, total: str, bp: str, obp: str, cash: str) -> None:
        t = Text()
        t.append("  TOTAL EQUITY ", style="bold cyan")
        t.append(total, style="bold green")
        t.append("   |   BUYING POWER ", style="dim")
        t.append(bp, style="bold white")
        t.append("   |   OPTIONS BP ", style="dim")
        t.append(obp, style="bold white")
        t.append("   |   CASH ", style="dim")
        t.append(cash, style="bold white")
        self.update(t)


class RebalancerBar(Static):
    """Shows the systemd rebalancer timer status and provides start/stop/enable controls."""

    DEFAULT_CSS = """
    RebalancerBar {
        height: 1;
        background: $panel;
        padding: 0 1;
    }
    """

    def on_mount(self) -> None:
        self.update_status(None, None, "—", "—")

    def update_status(
        self,
        active: bool | None,
        enabled: bool | None,
        last_run: str,
        next_run: str,
        skip_pending: bool = False,
        etf_ticker: str = "SPY",
        top_n: int = 100,
        margin_usage_pct: float = 0.5,
    ) -> None:
        t = Text()
        t.append("  REBALANCER ", style="bold magenta")
        if active is None:
            t.append("—", style="dim")
        elif active:
            t.append("● ACTIVE", style="bold green")
        else:
            t.append("○ INACTIVE", style="red")
        t.append("  ", style="dim")
        if enabled is None:
            pass
        elif enabled:
            t.append("ENABLED", style="cyan")
        else:
            t.append("DISABLED", style="dim")
        if skip_pending:
            t.append("  ⚠ NEXT RUN SKIPPED", style="bold yellow")
        t.append(f"  {etf_ticker} top-{top_n}  margin {int(margin_usage_pct * 100)}%", style="bold white")
        t.append(f"  |  Last: {last_run}  Next: {next_run}", style="dim")
        t.append("  |  [t] Start/Stop  [e] Enable/Disable  [x] Skip Next  [R] Run Now  [S] Settings", style="dim")
        self.update(t)


class PortfolioChart(Static):
    """ASCII line chart showing portfolio value history via yfinance."""

    DEFAULT_CSS = """
    PortfolioChart {
        height: 14;
        background: $panel;
        border: tall $primary;
        overflow-y: hidden;
    }
    """

    def __init__(self, **kwargs) -> None:
        super().__init__("  Loading chart…", **kwargs)
        self._period_idx = 1  # default: 1W
        self._positions: list[tuple[str, float]] = []

    def set_positions(self, positions) -> None:
        self._positions = [
            (pos.instrument.symbol, float(pos.quantity))
            for pos in positions
            if pos.quantity
        ]
        if self._positions:
            self._fetch_chart()

    def cycle_period(self, direction: int) -> None:
        self._period_idx = (self._period_idx + direction) % len(CHART_PERIODS)
        if self._positions:
            self._fetch_chart()

    @work(thread=True, exclusive=True)
    def _fetch_chart(self) -> None:
        import plotext as plt
        from rich.text import Text as RichText

        try:
            label, yf_period, yf_interval = CHART_PERIODS[self._period_idx]

            # Map broker symbols → yfinance symbols and build qty lookup
            qty_map: dict[str, float] = {}
            for sym, qty in self._positions:
                yf_sym = YF_TICKERS.get(sym, sym)
                qty_map[yf_sym] = qty_map.get(yf_sym, 0) + qty

            yf_symbols = list(qty_map.keys())

            # Single batched download — far faster than one request per symbol
            data = yf.download(
                yf_symbols,
                period=yf_period,
                interval=yf_interval,
                auto_adjust=True,
                progress=False,
                group_by="ticker",
            )

            portfolio_series: pd.Series | None = None
            for yf_sym, qty in qty_map.items():
                try:
                    if len(yf_symbols) == 1:
                        close = data["Close"]
                    else:
                        close = data[yf_sym]["Close"]
                    if isinstance(close, pd.DataFrame):
                        close = close.iloc[:, 0]
                    series = close.ffill() * qty
                    if series.dropna().empty:
                        continue
                    portfolio_series = (
                        series if portfolio_series is None
                        else portfolio_series.add(series, fill_value=0)
                    )
                except Exception:
                    continue

            if portfolio_series is None or portfolio_series.dropna().empty:
                self.app.call_from_thread(self.update, "  No chart data available for this period.")
                return

            portfolio_series = portfolio_series.dropna()
            n = len(portfolio_series)
            if n > 150:
                portfolio_series = portfolio_series.iloc[:: n // 150]

            # Use integer x-axis so plotext never tries to parse the label strings as dates
            tick_fmt = "%m-%d %H:%M" if yf_interval in ("5m", "15m", "30m", "1h") else "%b %d"
            timestamps = [dt.strftime(tick_fmt) for dt in portfolio_series.index.to_pydatetime()]
            values: list[float] = [float(v) for v in portfolio_series.values]
            x_nums = list(range(len(values)))
            tick_step = max(1, len(x_nums) // 8)
            x_ticks = x_nums[::tick_step]

            size = self.size
            w = size.width - 4 if size.width > 8 else 80
            h = size.height - 2 if size.height > 6 else 10

            tabs = "  ".join(
                f"[{p[0]}]" if i == self._period_idx else p[0]
                for i, p in enumerate(CHART_PERIODS)
            )

            plt.clf()
            plt.plotsize(w, h)
            plt.theme("dark")
            plt.plot(x_nums, values, marker="braille")
            plt.xticks(x_ticks, [timestamps[i] for i in x_ticks])
            plt.title(f"{tabs}    < [  ] >")
            plt.ylabel("$")
            chart_str = plt.build()

            self.app.call_from_thread(self.update, RichText.from_ansi(chart_str))
        except Exception as exc:
            self.app.call_from_thread(self.update, f"  Chart error: {exc}")


class HoldingsTable(DataTable):
    DEFAULT_CSS = "HoldingsTable { height: 1fr; }"

    def on_mount(self) -> None:
        self.add_columns("SYMBOL", "TYPE", "QTY", "LAST PRICE", "VALUE", "DAY GAIN")
        self.cursor_type = "row"

    def refresh_from_cache(self, rows: list[dict]) -> None:
        self.clear()
        for r in rows:
            gain = r["gain"]
            gain_style = "green" if r.get("gain_positive") else ("dim" if gain == "—" else "red")
            self.add_row(
                Text(r["symbol"], style="bold cyan"), Text(r["type"], style="dim"),
                r["qty"], r["price"], Text(r["value"], style="bold"),
                Text(gain, style=gain_style),
            )

    def refresh_from_portfolio(self, positions) -> None:
        self.clear()
        for pos in positions:
            sym = pos.instrument.symbol
            typ = pos.instrument.type.value
            qty = str(pos.quantity)
            price = f"${pos.last_price.last_price:,.2f}" if pos.last_price and pos.last_price.last_price else "—"
            value = f"${pos.current_value:,.2f}" if pos.current_value else "—"
            if pos.position_daily_gain and pos.position_daily_gain.gain_percentage is not None:
                pct = float(pos.position_daily_gain.gain_percentage)
                gain_str = f"{'+' if pct >= 0 else ''}{pct:.2f}%"
                gain_style = "green" if pct >= 0 else "red"
            else:
                gain_str, gain_style = "—", "dim"
            self.add_row(
                Text(sym, style="bold cyan"), Text(typ, style="dim"),
                qty, price, Text(value, style="bold"), Text(gain_str, style=gain_style),
            )


class OrdersTable(DataTable):
    DEFAULT_CSS = "OrdersTable { height: 1fr; }"

    def on_mount(self) -> None:
        self.add_columns("SIDE", "SYMBOL", "QTY", "TYPE", "STATUS")
        self.cursor_type = "row"

    def refresh_from_cache(self, rows: list[dict]) -> None:
        self.clear()
        for r in rows:
            side_style = "green" if r.get("side_buy") else "red"
            self.add_row(
                Text(r["side"], style=side_style), Text(r["symbol"], style="bold"),
                r["qty"], r["type"], Text(r["status"], style="yellow"),
                key=r["order_id"],
            )

    def refresh_from_orders(self, orders) -> None:
        self.clear()
        active_statuses = {
            OrderStatus.NEW, OrderStatus.PARTIALLY_FILLED,
            OrderStatus.PENDING_REPLACE, OrderStatus.PENDING_CANCEL,
        }
        for order in orders:
            if order.status not in active_statuses:
                continue
            side = order.side.value
            sym = order.instrument.symbol
            qty = str(order.quantity or order.notional_value or "—")
            typ = order.type.value
            status = order.status.value
            side_style = "green" if order.side == OrderSide.BUY else "red"
            self.add_row(
                Text(side, style=side_style), Text(sym, style="bold"),
                qty, typ, Text(status, style="yellow"),
                key=order.order_id,
            )

    def get_selected_order_id(self) -> tuple[str, str] | None:
        """Return (order_id, symbol) for the highlighted row, or None."""
        if self.cursor_row < 0 or self.row_count == 0:
            return None
        row_data = self.get_row_at(self.cursor_row)
        symbol = str(row_data[1])  # SYMBOL column
        order_id = str(list(self.rows.keys())[self.cursor_row].value)
        return order_id, symbol


class StatusBar(Static):
    DEFAULT_CSS = """
    StatusBar {
        height: 1;
        background: $panel;
        padding: 0 1;
        color: $text-muted;
    }
    """

    def set_status(self, msg: str, style: str = "dim") -> None:
        self.update(Text(msg, style=style))


# ---------------------------------------------------------------------------
# App
# ---------------------------------------------------------------------------

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
        self._load_portfolio_cache()
        self.query_one(StatusBar).set_status("  Connecting…" + _HINT)
        self.load_portfolio()
        self.load_rebalancer_status()

    def _load_portfolio_cache(self) -> None:
        """Populate widgets from the last saved portfolio snapshot (instant, no network)."""
        try:
            data = json.loads(PORTFOLIO_CACHE.read_text())
        except (FileNotFoundError, json.JSONDecodeError, OSError):
            return
        b = data.get("balance", {})
        if b:
            self.query_one(BalanceBar).update_display(
                b.get("total", "—"), b.get("bp", "—"),
                b.get("obp", "—"), b.get("cash", "—"),
            )
        holdings = data.get("holdings", [])
        if holdings:
            self.query_one(HoldingsTable).refresh_from_cache(holdings)
        orders = data.get("orders", [])
        self.query_one(OrdersTable).refresh_from_cache(orders)
        positions = data.get("positions", [])
        if positions:
            chart = self.query_one(PortfolioChart)
            chart._positions = [(p["symbol"], p["qty"]) for p in positions]
            chart._fetch_chart()
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
            PORTFOLIO_CACHE.write_text(json.dumps({
                "account_id": account_id,
                "balance": balance,
                "holdings": holdings,
                "orders": orders,
                "positions": positions,
            }))
        except OSError:
            pass

    def action_quit(self) -> None:
        self.workers.cancel_all()
        if self._client is not None:
            try:
                self._client.close()
            except Exception:
                pass
        os._exit(0)

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
        result = subprocess.run(
            ["systemctl", "--user", *args],
            capture_output=True,
            text=True,
        )
        return result.returncode, (result.stdout + result.stderr).strip()

    @work(thread=True)
    def load_rebalancer_status(self) -> None:
        rc_active, _ = self._systemctl("is-active", TIMER_UNIT)
        active = rc_active == 0

        rc_enabled, _ = self._systemctl("is-enabled", TIMER_UNIT)
        enabled = rc_enabled == 0

        _, last_out = self._systemctl("show", TIMER_UNIT, "--property=LastTriggerUSec")
        last_run = "never"
        if "=" in last_out:
            val = last_out.split("=", 1)[1].strip()
            if val and val not in ("n/a", "0", ""):
                last_run = val

        _, next_out = self._systemctl("show", TIMER_UNIT, "--property=NextElapseUSecRealtime")
        next_run = "—"
        if "=" in next_out:
            val = next_out.split("=", 1)[1].strip()
            if val and val not in ("n/a", "0", ""):
                next_run = val

        skip_pending = SKIP_FILE.exists()
        cfg = _load_rebalance_config()
        self.call_from_thread(
            self.query_one(RebalancerBar).update_status,
            active, enabled, last_run, next_run, skip_pending,
            cfg.get("etf_ticker", "SPY"), cfg.get("top_n", 100), cfg.get("margin_usage_pct", 0.5),
        )

    @work(thread=True)
    def load_portfolio(self) -> None:
        status = self.query_one(StatusBar)
        try:
            client = self._get_client()
            portfolio = client.get_portfolio()
            total = sum(e.value for e in portfolio.equity)
            bp = portfolio.buying_power.buying_power
            obp = portfolio.buying_power.options_buying_power
            cash = next((e.value for e in portfolio.equity if e.type.value == "CASH"), Decimal(0))

            # Build serializable snapshots for cache
            balance_data = {
                "total": f"${total:,.2f}", "bp": f"${bp:,.2f}",
                "obp": f"${obp:,.2f}", "cash": f"${cash:,.2f}",
            }
            holdings_data: list[dict] = []
            for pos in portfolio.positions:
                price = f"${pos.last_price.last_price:,.2f}" if pos.last_price and pos.last_price.last_price else "—"
                value = f"${pos.current_value:,.2f}" if pos.current_value else "—"
                if pos.position_daily_gain and pos.position_daily_gain.gain_percentage is not None:
                    pct = float(pos.position_daily_gain.gain_percentage)
                    gain_str = f"{'+' if pct >= 0 else ''}{pct:.2f}%"
                    gain_positive = pct >= 0
                else:
                    gain_str, gain_positive = "—", False
                holdings_data.append({
                    "symbol": pos.instrument.symbol,
                    "type": pos.instrument.type.value,
                    "qty": str(pos.quantity),
                    "price": price,
                    "value": value,
                    "gain": gain_str,
                    "gain_positive": gain_positive,
                })
            active_statuses = {
                OrderStatus.NEW, OrderStatus.PARTIALLY_FILLED,
                OrderStatus.PENDING_REPLACE, OrderStatus.PENDING_CANCEL,
            }
            orders_data: list[dict] = []
            for order in portfolio.orders:
                if order.status not in active_statuses:
                    continue
                orders_data.append({
                    "side": order.side.value,
                    "side_buy": order.side == OrderSide.BUY,
                    "symbol": order.instrument.symbol,
                    "qty": str(order.quantity or order.notional_value or "—"),
                    "type": order.type.value,
                    "status": order.status.value,
                    "order_id": order.order_id,
                })
            positions_data = [
                {"symbol": pos.instrument.symbol, "qty": float(pos.quantity)}
                for pos in portfolio.positions if pos.quantity
            ]
            self._save_portfolio_cache(
                str(portfolio.account_id), balance_data, holdings_data, orders_data, positions_data
            )

            self.call_from_thread(self.query_one(BalanceBar).update_display,
                balance_data["total"], balance_data["bp"], balance_data["obp"], balance_data["cash"])
            self.call_from_thread(self.query_one(HoldingsTable).refresh_from_cache, holdings_data)
            self.call_from_thread(self.query_one(PortfolioChart).set_positions, portfolio.positions)
            self.call_from_thread(self.query_one(OrdersTable).refresh_from_orders, portfolio.orders)
            self.call_from_thread(status.set_status,
                f"  {portfolio.account_id}" + _HINT)
        except Exception as exc:
            self.call_from_thread(status.set_status,
                f"  Error: {exc}  |  Check .env credentials", "red")

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
            self.call_from_thread(status.set_status,
                f"  Order submitted: {side.value} {quantity} {symbol} (ID: {new_order.order_id[:8]}…)" + _HINT,
                "green")
            self.call_from_thread(self.load_portfolio)
        except Exception as exc:
            self.call_from_thread(status.set_status, f"  Order failed: {exc}", "red")

    async def action_cancel_order(self) -> None:
        orders_table = self.query_one(OrdersTable)
        result = orders_table.get_selected_order_id()
        if result is None:
            self.query_one(StatusBar).set_status(
                "  No open order selected — use arrow keys to select a row in Orders", "yellow"
            )
            return
        order_id, symbol = result
        confirmed = await self.push_screen_wait(CancelConfirmModal(order_id, symbol))
        if confirmed:
            self._do_cancel(order_id, symbol)

    @work(thread=True)
    def _do_cancel(self, order_id: str, symbol: str) -> None:
        status = self.query_one(StatusBar)
        try:
            client = self._get_client()
            client.cancel_order(order_id)
            self.call_from_thread(status.set_status,
                f"  Cancellation submitted for {symbol} (ID: {order_id[:8]}…)" + _HINT,
                "yellow")
            self.call_from_thread(self.load_portfolio)
        except Exception as exc:
            self.call_from_thread(status.set_status, f"  Cancel failed: {exc}", "red")

    @work(thread=True)
    def action_toggle_rebalancer(self) -> None:
        """Start or stop the rebalancer timer for the current session."""
        status = self.query_one(StatusBar)
        rc, _ = self._systemctl("is-active", TIMER_UNIT)
        if rc == 0:
            rc2, out = self._systemctl("stop", TIMER_UNIT)
            msg = "  Rebalancer stopped." if rc2 == 0 else f"  Stop failed: {out}"
        else:
            rc2, out = self._systemctl("start", TIMER_UNIT)
            msg = "  Rebalancer started." if rc2 == 0 else f"  Start failed: {out}"
        self.call_from_thread(status.set_status, msg + _HINT, "green" if rc2 == 0 else "red")
        self.call_from_thread(self.load_rebalancer_status)

    @work(thread=True)
    def action_toggle_enable_rebalancer(self) -> None:
        """Enable or disable the rebalancer timer across reboots."""
        status = self.query_one(StatusBar)
        rc, _ = self._systemctl("is-enabled", TIMER_UNIT)
        if rc == 0:
            rc2, out = self._systemctl("disable", TIMER_UNIT)
            msg = "  Rebalancer disabled (won't start on login)." if rc2 == 0 else f"  Disable failed: {out}"
        else:
            rc2, out = self._systemctl("enable", TIMER_UNIT)
            msg = "  Rebalancer enabled (starts automatically on login)." if rc2 == 0 else f"  Enable failed: {out}"
        self.call_from_thread(status.set_status, msg, "cyan" if rc2 == 0 else "red")
        self.call_from_thread(self.load_rebalancer_status)

    def action_skip_next_rebalance(self) -> None:
        """Toggle the skip sentinel for the next rebalancer run."""
        status = self.query_one(StatusBar)
        if SKIP_FILE.exists():
            SKIP_FILE.unlink()
            status.set_status("  Skip cancelled — next run will proceed normally." + _HINT, "cyan")
        else:
            SKIP_FILE.parent.mkdir(exist_ok=True)
            SKIP_FILE.touch()
            status.set_status("  Next rebalance run will be skipped. Press [x] again to cancel." + _HINT, "yellow")
        self.load_rebalancer_status()

    def action_run_rebalancer_now(self) -> None:
        """Immediately trigger the rebalancer service, bypassing the schedule."""
        self.push_screen(RunNowModal(), self._handle_run_now)

    def _handle_run_now(self, confirmed: bool) -> None:
        if confirmed:
            self._do_run_now()

    @work(thread=True)
    def _do_run_now(self) -> None:
        status = self.query_one(StatusBar)
        rc, out = self._systemctl("start", SERVICE_UNIT)
        if rc == 0:
            self.call_from_thread(status.set_status,
                "  Rebalancer triggered — check cache/rebalance.log for progress." + _HINT, "green")
        else:
            self.call_from_thread(status.set_status, f"  Failed to start rebalancer: {out}", "red")
        self.call_from_thread(self.load_rebalancer_status)

    def action_rebalance_settings(self) -> None:
        cfg = _load_rebalance_config()
        self.push_screen(
            RebalanceConfigModal(
                cfg.get("etf_ticker", "SPY"),
                cfg.get("top_n", 100),
                cfg.get("margin_usage_pct", 0.5),
            ),
            self._handle_rebalance_settings,
        )

    def _handle_rebalance_settings(self, result: dict | None) -> None:
        if result is None:
            return
        try:
            _save_rebalance_config(result["etf_ticker"], result["top_n"], result["margin_usage_pct"])
            pct = int(result["margin_usage_pct"] * 100)
            self.query_one(StatusBar).set_status(
                f"  Rebalance config saved: {result['etf_ticker']} top-{result['top_n']}  margin {pct}%" + _HINT, "green"
            )
            self.load_rebalancer_status()
        except OSError as exc:
            self.query_one(StatusBar).set_status(f"  Failed to save config: {exc}", "red")

    def action_chart_prev(self) -> None:
        self.query_one(PortfolioChart).cycle_period(-1)

    def action_chart_next(self) -> None:
        self.query_one(PortfolioChart).cycle_period(1)

    def action_history(self) -> None:
        self.query_one(StatusBar).set_status(
            "  [h] History — not yet implemented", "yellow"
        )


def main() -> None:
    PublicTerminal().run()


if __name__ == "__main__":
    main()
