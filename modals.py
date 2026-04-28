"""Textual modal screens for the Public Terminal TUI."""

from __future__ import annotations

from datetime import datetime, timedelta, timezone
from decimal import Decimal, InvalidOperation

from public_api_sdk import HistoryRequest, OrderSide
from rich.text import Text
from textual import on, work
from textual.binding import Binding
from textual.containers import Grid, Horizontal, Vertical
from textual.screen import ModalScreen
from textual.widgets import Button, DataTable, Input, Label, Select

# config imports are deferred to handler methods to avoid circular imports at module load

INSTRUMENT_OPTIONS = [
    ("Equity / ETF / Stock", "EQUITY"),
    ("Crypto", "CRYPTO"),
]


# ---------------------------------------------------------------------------
# First-run setup
# ---------------------------------------------------------------------------


class SetupModal(ModalScreen[bool]):
    """Shown on first launch when credentials are missing. Writes .env and registers accounts."""

    DEFAULT_CSS = """
    SetupModal {
        align: center middle;
    }
    #setup-dialog {
        width: 76;
        height: auto;
        border: thick $warning;
        background: $surface;
        padding: 1 2;
    }
    #setup-title {
        text-align: center;
        text-style: bold;
        height: 1;
        margin-bottom: 1;
        color: $warning;
    }
    #setup-intro {
        color: $text-muted;
        margin-bottom: 1;
    }
    .field-label {
        height: 1;
        margin-top: 1;
        color: $text-muted;
    }
    #setup-account-list {
        margin-top: 1;
        color: $success;
        height: auto;
    }
    #setup-btn-row {
        margin-top: 1;
        height: 3;
        align: center middle;
    }
    #setup-error {
        height: 1;
        margin-top: 1;
        color: $error;
    }
    #setup-btn-add {
        margin-right: 1;
    }
    #setup-btn-save {
        margin-right: 2;
    }
    """

    _INTRO = (
        "No credentials found. Enter your Public.com API details below.\n"
        "They will be saved to ~/.config/public-terminal/.env"
    )

    def compose(self):
        self._accounts: list[str] = []
        with Grid(id="setup-dialog"):
            yield Label("WELCOME TO PUBLIC TERMINAL", id="setup-title")
            yield Label(self._INTRO, id="setup-intro")
            yield Label(
                "API Access Token  (Settings → API → Secret Key)", classes="field-label"
            )
            yield Input(placeholder="your-access-token", password=True, id="input-token")
            yield Label("Account Number  (e.g. ACCT0001)", classes="field-label")
            yield Input(placeholder="e.g. ACCT0001", id="input-account")
            yield Label("", id="setup-error")
            yield Label("", id="setup-account-list")
            with Horizontal(id="setup-btn-row"):
                yield Button("Add Another Account", variant="default", id="setup-btn-add")
                yield Button("Done", variant="success", id="setup-btn-save", disabled=True)
                yield Button("Quit", variant="error", id="setup-btn-quit")

    @on(Button.Pressed, "#setup-btn-quit")
    def do_quit(self) -> None:
        self.dismiss(False)

    @on(Button.Pressed, "#setup-btn-add")
    def do_add_account(self) -> None:
        account = self.query_one("#input-account", Input).value.strip().upper()
        error_label = self.query_one("#setup-error", Label)
        if not account:
            error_label.update("Account number is required.")
            return
        if not account.isalnum() or not (4 <= len(account) <= 12):
            error_label.update("Account number must be 4–12 alphanumeric characters.")
            return
        if account in self._accounts:
            error_label.update(f"{account} is already added.")
            return
        self._accounts.append(account)
        error_label.update("")
        self.query_one("#input-account", Input).value = ""
        self.query_one("#setup-account-list", Label).update(
            "Accounts: " + ", ".join(self._accounts)
        )
        self.query_one("#setup-btn-save", Button).disabled = False

    @on(Button.Pressed, "#setup-btn-save")
    def do_save(self) -> None:
        from config import _write_env, add_account

        token = self.query_one("#input-token", Input).value.strip()
        error_label = self.query_one("#setup-error", Label)
        # Auto-add any valid account still in the input field
        pending = self.query_one("#input-account", Input).value.strip().upper()
        if pending and pending not in self._accounts:
            if pending.isalnum() and 4 <= len(pending) <= 12:
                self._accounts.append(pending)
        if not token:
            error_label.update("API access token is required.")
            self.query_one("#input-token", Input).focus()
            return
        if not self._accounts:
            error_label.update("Add at least one account number.")
            return
        error_label.update("")
        _write_env(token)
        for acct in self._accounts:
            add_account(acct)
        self.dismiss(True)


# ---------------------------------------------------------------------------
# Manual order entry
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
    #order-error {
        height: 1;
        margin-top: 1;
        color: $error;
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

    def compose(self):
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
            yield Label("", id="order-error")
            with Horizontal(id="btn-row"):
                yield Button(
                    f"Confirm {self._side.value}",
                    variant="success" if self._side == OrderSide.BUY else "error",
                    id="btn-confirm",
                )
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
            self.query_one("#order-error", Label).update("Symbol is required.")
            self.query_one("#input-symbol", Input).focus()
            return
        try:
            qty = Decimal(qty_str)
            if qty <= 0:
                raise ValueError
        except (InvalidOperation, ValueError):
            self.query_one("#order-error", Label).update(
                "Quantity must be a positive number."
            )
            self.query_one("#input-qty", Input).focus()
            return
        self.query_one("#order-error", Label).update("")

        self.dismiss(
            {
                "symbol": symbol,
                "instrument_type": instrument_type_val,
                "quantity": qty,
                "side": self._side,
            }
        )


# ---------------------------------------------------------------------------
# Order cancellation confirmation
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

    def compose(self):
        with Grid(id="cancel-dialog"):
            yield Label("CANCEL ORDER", id="cancel-title")
            yield Label(
                f"Cancel order for [bold]{self._symbol}[/bold]?  (ID: {self._order_id[:8]}…)"
            )
            with Horizontal(id="cancel-btn-row"):
                yield Button("Yes, cancel", variant="error", id="btn-yes")
                yield Button("No", variant="default", id="btn-no")

    @on(Button.Pressed, "#btn-yes")
    def yes(self) -> None:
        self.dismiss(True)

    @on(Button.Pressed, "#btn-no")
    def no(self) -> None:
        self.dismiss(False)


class RebalanceNowConfirmModal(ModalScreen[bool]):
    """Confirmation dialog before triggering an on-demand rebalance."""

    DEFAULT_CSS = """
    RebalanceNowConfirmModal {
        align: center middle;
    }
    #rebnow-dialog {
        width: 60;
        height: auto;
        border: thick $warning;
        background: $surface;
        padding: 1 2;
    }
    #rebnow-title {
        text-align: center;
        text-style: bold;
        height: 1;
        margin-bottom: 1;
    }
    #rebnow-body {
        height: auto;
        margin-bottom: 1;
    }
    #rebnow-btn-row {
        margin-top: 1;
        height: 3;
        align: center middle;
    }
    """

    def compose(self):
        with Grid(id="rebnow-dialog"):
            yield Label("RUN REBALANCE NOW", id="rebnow-title")
            yield Label(
                "Rebalance the portfolio using current settings? "
                "Orders will be placed immediately against live markets.",
                id="rebnow-body",
            )
            with Horizontal(id="rebnow-btn-row"):
                yield Button("Yes, rebalance", variant="warning", id="btn-yes")
                yield Button("No", variant="default", id="btn-no")

    @on(Button.Pressed, "#btn-yes")
    def yes(self) -> None:
        self.dismiss(True)

    @on(Button.Pressed, "#btn-no")
    def no(self) -> None:
        self.dismiss(False)


# ---------------------------------------------------------------------------
# Transaction history
# ---------------------------------------------------------------------------


class HistoryModal(ModalScreen):
    """Scrollable transaction history modal."""

    BINDINGS = [Binding("escape,h", "dismiss_modal", "Close", show=False)]
    HISTORY_PAGE_SIZE = 100
    HISTORY_MAX_PAGES = 20
    HISTORY_MAX_TRANSACTIONS = 1000

    DEFAULT_CSS = """
    HistoryModal {
        align: center middle;
    }
    #hist-dialog {
        width: 96;
        height: 36;
        border: thick $primary;
        background: $surface;
        padding: 1 2;
    }
    #hist-title {
        text-align: center;
        text-style: bold;
        height: 1;
        margin-bottom: 1;
    }
    #hist-table {
        height: 1fr;
    }
    #hist-status {
        height: 1;
        margin-top: 1;
        color: $text-muted;
    }
    #hist-btn-row {
        height: 3;
        align: center middle;
    }
    """

    def __init__(self, client) -> None:
        super().__init__()
        self._client = client

    def compose(self):
        with Grid(id="hist-dialog"):
            yield Label("TRANSACTION HISTORY", id="hist-title")
            yield DataTable(id="hist-table")
            yield Label("Loading…", id="hist-status")
            with Horizontal(id="hist-btn-row"):
                yield Button("Close", variant="default", id="btn-close")

    def on_mount(self) -> None:
        tbl = self.query_one("#hist-table", DataTable)
        tbl.add_columns("DATE", "TYPE", "SYMBOL", "SIDE", "QTY", "NET")
        tbl.cursor_type = "row"
        self._load_history()

    @work(thread=True)
    def _load_history(self) -> None:
        status = self.query_one("#hist-status", Label)
        tbl = self.query_one("#hist-table", DataTable)
        try:
            start = datetime.now(timezone.utc) - timedelta(days=90)
            txns = []
            next_token = None
            truncated = False
            for _ in range(self.HISTORY_MAX_PAGES):
                page = self._client.get_history(
                    history_request=HistoryRequest(
                        start=start,
                        page_size=self.HISTORY_PAGE_SIZE,
                        next_token=next_token,
                    )
                )
                txns.extend(page.transactions or [])
                next_token = getattr(page, "next_token", None)
                if not next_token:
                    break
                if len(txns) >= self.HISTORY_MAX_TRANSACTIONS:
                    truncated = True
                    break
            else:
                truncated = bool(next_token)

            txns = sorted(txns, key=lambda t: t.timestamp, reverse=True)[
                : self.HISTORY_MAX_TRANSACTIONS
            ]
            rows = []
            for tx in txns:
                tx_date = tx.timestamp.strftime("%Y-%m-%d %H:%M")
                tx_type = tx.type.value if tx.type else "—"
                symbol = tx.symbol or "—"
                side = tx.side.value if tx.side else "—"
                qty = str(tx.quantity) if tx.quantity is not None else "—"
                net = f"${tx.net_amount:,.2f}" if tx.net_amount is not None else "—"
                side_style = (
                    "green"
                    if side.upper() == "BUY"
                    else "red"
                    if side.upper() == "SELL"
                    else "dim"
                )
                rows.append(
                    (tx_date, tx_type, symbol, Text(side, style=side_style), qty, net)
                )

            def _populate() -> None:
                for row in rows:
                    tbl.add_row(*row)
                suffix = " (truncated)" if truncated else ""
                status.update(
                    f"{len(rows)} transactions{suffix} (newest first, last 90 days)  |  ESC or [h] to close"
                )

            self.app.call_from_thread(_populate)
        except Exception as exc:
            self.app.call_from_thread(
                status.update, f"[red]Error loading history: {exc}[/red]"
            )

    def action_dismiss_modal(self) -> None:
        self.dismiss()

    @on(Button.Pressed, "#btn-close")
    def close(self) -> None:
        self.dismiss()


# ---------------------------------------------------------------------------
# Rebalance settings
# ---------------------------------------------------------------------------


class RebalanceConfigModal(ModalScreen):
    """Modal for configuring the rebalancer."""

    DEFAULT_CSS = """
    RebalanceConfigModal {
        align: center middle;
    }
    #cfg-dialog {
        width: 96;
        max-width: 95vw;
        height: auto;
        max-height: 90vh;
        border: thick $primary;
        background: $surface;
        padding: 1 2;
        overflow-y: auto;
    }
    #cfg-title {
        text-align: center;
        text-style: bold;
        height: 1;
        margin-bottom: 1;
    }
    #cfg-section-index, #cfg-section-margin, #cfg-section-alloc {
        text-style: bold;
        margin-top: 1;
        color: $primary;
        height: auto;
    }
    .field-label {
        width: 100%;
        height: auto;
        margin-top: 1;
        color: $text-muted;
    }
    .field-help {
        width: 100%;
        height: auto;
        color: $text-muted;
    }
    .field-blocked {
        color: $warning;
    }
    #input-margin:disabled {
        color: $text-muted;
    }
    #alloc-sum {
        height: 1;
        margin-top: 1;
    }
    #cfg-error {
        height: 1;
        margin-top: 1;
        color: $error;
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

    _ALLOC_INPUTS = (
        "input-stocks",
        "input-btc",
        "input-eth",
        "input-gold",
        "input-cash",
    )
    _ALLOC_LABELS = {
        "stocks": "Stocks %",
        "btc": "Bitcoin %",
        "eth": "Ethereum %",
        "gold": "Gold %",
        "cash": "Cash %",
    }

    def __init__(
        self,
        current_index: str,
        current_top_n: int,
        current_margin_pct: float,
        current_excluded: list[str],
        current_allocs: dict[str, float],
        margin_enabled: bool | None,
        margin_capacity: Decimal,
    ) -> None:
        super().__init__()
        self._current_index = current_index
        self._current_top_n = current_top_n
        self._current_margin_pct = current_margin_pct
        self._current_excluded = current_excluded
        self._current_allocs = current_allocs
        self._margin_enabled = margin_enabled
        self._margin_capacity = margin_capacity

    def compose(self):
        from rebalance import SUPPORTED_INDEXES

        a = self._current_allocs
        excluded_str = ", ".join(sorted(self._current_excluded))
        index_options = [(label, key) for key, label in SUPPORTED_INDEXES.items()]
        margin_available = self._margin_enabled is True
        margin_value = self._current_margin_pct if margin_available else 0.0
        margin_capacity = f"${self._margin_capacity:,.2f}"
        with Grid(id="cfg-dialog"):
            yield Label("REBALANCE SETTINGS", id="cfg-title")

            yield Label("Index & Stocks", id="cfg-section-index")
            yield Label("Index to track", classes="field-label")
            yield Select(index_options, value=self._current_index, id="select-index")
            yield Label(
                "Top N stocks by market cap",
                classes="field-label",
            )
            yield Label("Default: full index", classes="field-help")
            yield Input(value=str(self._current_top_n), id="input-top-n")
            yield Label(
                "Excluded tickers",
                classes="field-label",
            )
            yield Label("Comma-separated; leave blank for none", classes="field-help")
            yield Input(
                value=excluded_str, placeholder="e.g. TSLA, NVDA", id="input-excluded"
            )

            yield Label("Margin", id="cfg-section-margin")
            yield Label(
                "Margin usage",
                classes="field-label",
            )
            if margin_available:
                yield Label(
                    "0.0 = cash only | 0.5 = 50% of margin | "
                    f"1.0 = full | Capacity: {margin_capacity}",
                    classes="field-help",
                )
            else:
                yield Label(
                    "Margin buying power is not enabled on this account; "
                    "rebalancer will use cash only.",
                    classes="field-help field-blocked",
                )
            yield Input(
                value=str(margin_value),
                id="input-margin",
                disabled=not margin_available,
            )

            yield Label("Target Allocation (must sum to 100%)", id="cfg-section-alloc")
            yield Label(
                "These percentages are target portfolio weights for each bucket.",
                classes="field-label",
            )
            yield Label(
                "Stocks = Top N index basket | BTC = Bitcoin | ETH = Ethereum",
                classes="field-help",
            )
            yield Label(
                "Gold = GLDM ETF | Cash = left uninvested",
                classes="field-help",
            )
            yield Label("Stocks % (Top N index basket)", classes="field-label")
            yield Input(
                value=str(round(a.get("stocks", 0.65) * 100)), id="input-stocks"
            )
            yield Label("Bitcoin (BTC) %", classes="field-label")
            yield Input(value=str(round(a.get("btc", 0.15) * 100)), id="input-btc")
            yield Label("Ethereum (ETH) %", classes="field-label")
            yield Input(value=str(round(a.get("eth", 0.05) * 100)), id="input-eth")
            yield Label("Gold (GLDM ETF) %", classes="field-label")
            yield Input(value=str(round(a.get("gold", 0.10) * 100)), id="input-gold")
            yield Label("Cash (uninvested buying power) %", classes="field-label")
            yield Input(value=str(round(a.get("cash", 0.05) * 100)), id="input-cash")
            yield Label("", id="alloc-sum")
            yield Label("", id="cfg-error")

            with Horizontal(id="cfg-btn-row"):
                yield Button("Save", variant="success", id="cfg-btn-save")
                yield Button("Cancel", variant="default", id="cfg-btn-cancel")

    def on_mount(self) -> None:
        self._update_sum()

    def _parse_alloc_inputs(self) -> tuple[dict[str, int], int, str | None]:
        values: dict[str, int] = {}
        for input_id in self._ALLOC_INPUTS:
            key = input_id.removeprefix("input-")
            raw = self.query_one(f"#{input_id}", Input).value.strip()
            label = self._ALLOC_LABELS[key]
            try:
                value = Decimal(raw)
            except InvalidOperation:
                values[key] = 0
                return (
                    values,
                    sum(values.values()),
                    f"{label} must be a whole number like 65.",
                )
            if not value.is_finite() or value < 0 or value != value.to_integral_value():
                values[key] = 0
                return (
                    values,
                    sum(values.values()),
                    f"{label} must be a whole number like 65.",
                )
            values[key] = int(value)
        return values, sum(values.values()), None

    def _update_sum(self) -> None:
        _, total, error = self._parse_alloc_inputs()
        label = self.query_one("#alloc-sum", Label)
        if error:
            label.update(f"  {error}")
            label.styles.color = "red"
        elif total == 100:
            label.update(f"  Total: {total}%  ✓")
            label.styles.color = "green"
        else:
            label.update(f"  Total: {total}%  — must equal 100%")
            label.styles.color = "red"

    @on(Input.Changed)
    def on_input_changed(self, event: Input.Changed) -> None:
        if event.input.id in self._ALLOC_INPUTS:
            self._update_sum()

    @on(Button.Pressed, "#cfg-btn-cancel")
    def cancel(self) -> None:
        self.dismiss(None)

    @on(Button.Pressed, "#cfg-btn-save")
    def save(self) -> None:
        index = self.query_one("#select-index", Select).value
        top_n_str = self.query_one("#input-top-n", Input).value.strip()
        margin_str = self.query_one("#input-margin", Input).value.strip()
        excluded_raw = self.query_one("#input-excluded", Input).value
        try:
            top_n = int(top_n_str)
            if top_n < 1:
                raise ValueError
        except ValueError:
            self.query_one("#cfg-error", Label).update(
                "Top N must be a whole number ≥ 1."
            )
            self.query_one("#input-top-n", Input).focus()
            return
        if self._margin_enabled is True:
            try:
                margin_pct = float(margin_str)
                if not 0.0 <= margin_pct <= 1.0:
                    raise ValueError
            except ValueError:
                self.query_one("#cfg-error", Label).update(
                    "Margin must be a number between 0.0 and 1.0."
                )
                self.query_one("#input-margin", Input).focus()
                return
        else:
            margin_pct = 0.0
        alloc_pcts, total, alloc_error = self._parse_alloc_inputs()
        if alloc_error:
            self.query_one("#cfg-error", Label).update(alloc_error)
            self.query_one("#input-stocks", Input).focus()
            return
        if any(v < 0 or v > 100 for v in alloc_pcts.values()):
            self.query_one("#cfg-error", Label).update(
                "Each allocation percentage must be between 0 and 100."
            )
            self.query_one("#input-stocks", Input).focus()
            return
        if total != 100:
            self.query_one("#cfg-error", Label).update(
                f"Allocations sum to {total}% — must equal 100%."
            )
            self.query_one("#input-stocks", Input).focus()
            return
        self.query_one("#cfg-error", Label).update("")
        allocations = {k: round(v / 100, 4) for k, v in alloc_pcts.items()}
        excluded = [t.upper().strip() for t in excluded_raw.split(",") if t.strip()]
        self.dismiss(
            {
                "index": index,
                "top_n": top_n,
                "margin_usage_pct": margin_pct,
                "excluded_tickers": excluded,
                "allocations": allocations,
            }
        )


# ---------------------------------------------------------------------------
# Account management
# ---------------------------------------------------------------------------


class AccountManagementModal(ModalScreen[None]):
    """Manage accounts: add new ones (with API validation) or remove existing ones."""

    DEFAULT_CSS = """
    AccountManagementModal {
        align: center middle;
    }
    #acct-dialog {
        width: 72;
        height: auto;
        border: thick $primary;
        background: $surface;
        padding: 1 2;
    }
    #acct-title {
        text-align: center;
        text-style: bold;
        height: 1;
        margin-bottom: 1;
    }
    .acct-row {
        height: 3;
        margin-bottom: 1;
    }
    .acct-label {
        width: 1fr;
        content-align: left middle;
    }
    .acct-remove-btn {
        width: auto;
    }
    #acct-add-section {
        margin-top: 1;
    }
    .field-label {
        height: 1;
        color: $text-muted;
    }
    #acct-error {
        height: 1;
        margin-top: 1;
        color: $error;
    }
    #acct-status {
        height: 1;
        margin-top: 1;
        color: $text-muted;
    }
    #acct-btn-row {
        margin-top: 1;
        height: 3;
        align: center middle;
    }
    """

    def compose(self):
        from config import get_accounts
        self._accounts = get_accounts()
        with Grid(id="acct-dialog"):
            yield Label("ACCOUNT MANAGEMENT", id="acct-title")
            for acct in self._accounts:
                with Horizontal(classes="acct-row"):
                    yield Label(acct, classes="acct-label", id=f"acct-label-{acct}")
                    yield Button(
                        "Remove",
                        variant="error",
                        classes="acct-remove-btn",
                        id=f"acct-remove-{acct}",
                        disabled=len(self._accounts) == 1,
                    )
            with Vertical(id="acct-add-section"):
                yield Label("Add Account Number", classes="field-label")
                yield Input(placeholder="e.g. ACCT0002", id="acct-input")
                yield Label("", id="acct-error")
                yield Label("", id="acct-status")
            with Horizontal(id="acct-btn-row"):
                yield Button("Add Account", variant="success", id="acct-btn-add")
                yield Button("Close", variant="default", id="acct-btn-close")

    @on(Button.Pressed)
    def on_button_pressed(self, event: Button.Pressed) -> None:
        from config import remove_account
        btn_id = event.button.id or ""
        if btn_id.startswith("acct-remove-"):
            acct = btn_id[len("acct-remove-"):]
            try:
                remove_account(acct)
                self._accounts.remove(acct)
            except ValueError as exc:
                self.query_one("#acct-error", Label).update(str(exc))
                return
            self.dismiss(None)
        elif btn_id == "acct-btn-close":
            self.dismiss(None)
        elif btn_id == "acct-btn-add":
            self._do_add_account()

    def _do_add_account(self) -> None:
        from config import add_account, get_accounts
        error_label = self.query_one("#acct-error", Label)
        status_label = self.query_one("#acct-status", Label)
        account = self.query_one("#acct-input", Input).value.strip().upper()

        error_label.update("")
        status_label.update("")

        if not account:
            error_label.update("Account number is required.")
            return
        if not account.isalnum() or not (4 <= len(account) <= 12):
            error_label.update("Account number must be 4–12 alphanumeric characters.")
            return
        if account in get_accounts():
            error_label.update(f"{account} is already registered.")
            return

        status_label.update("Validating with Public.com…")
        self._validate_and_add(account)

    @work(thread=True)
    def _validate_and_add(self, account: str) -> None:
        from config import ENV_FILE, add_account
        from dotenv import load_dotenv
        load_dotenv(ENV_FILE)

        network_error = False
        api_error = False
        try:
            from client import get_client
            client = get_client(account)
            client.get_portfolio()
        except RuntimeError:
            network_error = True
        except Exception as exc:
            msg = str(exc).lower()
            if any(w in msg for w in ("404", "not found", "unauthorized", "forbidden", "invalid")):
                api_error = True
            else:
                network_error = True

        def _finish():
            error_label = self.query_one("#acct-error", Label)
            status_label = self.query_one("#acct-status", Label)
            status_label.update("")
            if api_error:
                error_label.update(
                    "Account not found or not accessible with the current token."
                )
                return
            if network_error:
                error_label.update(
                    "Network error — account added anyway. Verify when online."
                )
            add_account(account)
            self.dismiss(None)

        self.call_from_thread(_finish)
