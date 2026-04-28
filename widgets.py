"""Textual widget classes for the Public Terminal TUI."""

from __future__ import annotations

from datetime import datetime, timedelta

import pandas as pd
from public_api_sdk import OrderSide
from rich.text import Text
from textual import work
from textual.widgets import DataTable, Static

from config import _ACTIVE_ORDER_STATUSES, BROKER_TO_YF_SYMBOLS

CHART_PERIODS = [
    ("24H", "1d", "5m"),
    ("1W", "5d", "1h"),
    ("1M", "1mo", "1d"),
    ("3M", "3mo", "1d"),
    ("1Y", "1y", "1d"),
]
YFINANCE_DOWNLOAD_TIMEOUT_SECONDS = 15
LIVE_CHART_WINDOW = timedelta(hours=24)
LIVE_CHART_MAX_RENDER_POINTS = 150


def _holding_value_sort_key(row: dict) -> float:
    value = row.get("value_num")
    if isinstance(value, int | float):
        return float(value)

    value_text = str(row.get("value", ""))
    cleaned = value_text.replace("$", "").replace(",", "").strip()
    try:
        return float(cleaned)
    except ValueError:
        return 0.0


def _format_period_change(start: float, end: float) -> str:
    change = end - start
    sign = "+" if change >= 0 else ""
    if start:
        pct = (change / start) * 100
        pct_sign = "+" if pct >= 0 else ""
        return f"{sign}${change:,.2f} ({pct_sign}{pct:.2f}%)"
    return f"{sign}${change:,.2f} (n/a)"


def _extract_close_series(data: pd.DataFrame, yf_symbol: str) -> pd.Series | None:
    """Return a Close series from yfinance data for flat or multi-index columns."""
    if data.empty:
        return None

    if isinstance(data.columns, pd.MultiIndex):
        for col in data.columns:
            if isinstance(col, tuple) and yf_symbol in col and "Close" in col:
                close = data[col]
                if isinstance(close, pd.DataFrame):
                    close = close.iloc[:, 0]
                return close

        close_cols = [
            col for col in data.columns if isinstance(col, tuple) and "Close" in col
        ]
        if len(close_cols) == 1:
            close = data[close_cols[0]]
            if isinstance(close, pd.DataFrame):
                close = close.iloc[:, 0]
            return close
        return None

    if "Close" not in data.columns:
        return None
    close = data["Close"]
    if isinstance(close, pd.DataFrame):
        close = close.iloc[:, 0]
    return close


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
        self.update_display("—", "—", "—", "—", "—", "CASH")

    def update_display(
        self,
        total: str,
        bp: str,
        obp: str,
        crypto_bp: str,
        cash: str,
        cash_label: str = "CASH",
    ) -> None:
        t = Text()
        t.append("  TOTAL EQUITY ", style="bold cyan")
        t.append(total, style="bold green")
        t.append("   |   BUYING POWER ", style="dim")
        t.append(bp, style="bold white")
        t.append("   |   OPTIONS BP ", style="dim")
        t.append(obp, style="bold white")
        t.append("   |   CRYPTO BP ", style="dim")
        t.append(crypto_bp, style="bold white")
        t.append(f"   |   {cash_label} ", style="dim")
        t.append(cash, style="bold white")
        self.update(t)


class RebalancerBar(Static):
    """Shows the rebalancer schedule status and available controls."""

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
        index: str = "SP500",
        top_n: int = 500,
        margin_usage_pct: float = 0.5,
        excluded_count: int = 0,
    ) -> None:
        from rebalance import SUPPORTED_INDEXES

        t = Text()
        t.append("  REBALANCER ", style="bold magenta")

        if enabled is None:
            t.append("— NO SYSTEMD", style="dim")
        elif enabled and active:
            t.append("● SCHEDULED", style="bold green")
        elif enabled:
            t.append("Ⅱ PAUSED", style="bold yellow")
        elif active:
            t.append("● ACTIVE", style="bold cyan")
            t.append("  NOT INSTALLED", style="dim")
        else:
            t.append("○ NOT SCHEDULED", style="dim")

        t.append("  ", style="dim")
        if enabled is True:
            t.append("INSTALLED", style="cyan")
        elif enabled is False:
            t.append("NOT INSTALLED", style="dim")
        else:
            t.append("NOT AVAILABLE", style="dim")

        if skip_pending:
            t.append("  ⚠ NEXT RUN SKIPPED", style="bold yellow")
        index_label = SUPPORTED_INDEXES.get(index, index)
        excl_str = f"  excl {excluded_count}" if excluded_count else ""
        t.append(
            f"  {index_label} top-{top_n}  margin {int(margin_usage_pct * 100)}%{excl_str}",
            style="bold white",
        )
        t.append(f"  |  Last: {last_run}  Next: {next_run}", style="dim")
        t.append(
            "  |  [t] Pause/Resume  [e] Install/Remove  [x] Skip  [S] Settings",
            style="dim",
        )
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
        self._period_idx = 0  # default: 1D
        self._positions: list[tuple[str, float]] = []
        self._live_enabled = False
        self._live_points: list[tuple[datetime, float]] = []
        self._last_content = None  # last rendered output for snapshot/restore

    @staticmethod
    def _normalize_positions(positions) -> list[tuple[str, float]]:
        by_symbol: dict[str, float] = {}
        for pos in positions:
            symbol = None
            qty_raw = None

            if isinstance(pos, dict):
                symbol = pos.get("symbol")
                qty_raw = pos.get("qty")
            elif isinstance(pos, tuple) and len(pos) == 2:
                symbol, qty_raw = pos
            else:
                instrument = getattr(pos, "instrument", None)
                symbol = getattr(instrument, "symbol", None)
                qty_raw = getattr(pos, "quantity", None)

            if not symbol or qty_raw is None:
                continue
            try:
                qty = float(qty_raw)
            except (TypeError, ValueError):
                continue
            if qty:
                by_symbol[str(symbol)] = by_symbol.get(str(symbol), 0.0) + qty

        return sorted(by_symbol.items())

    def get_snapshot(self) -> dict:
        return {
            "positions": self._positions[:],
            "period_idx": self._period_idx,
            "content": self._last_content,
        }

    def restore_snapshot(self, snapshot: dict) -> None:
        """Restore a previously saved chart state without triggering a re-fetch."""
        self._positions = snapshot["positions"]
        self._period_idx = snapshot["period_idx"]
        self._live_enabled = False
        self._live_points.clear()
        if snapshot["content"] is not None:
            super().update(snapshot["content"])
        else:
            super().update("  Loading chart…")

    def clear_for_account_switch(self) -> None:
        """Reset chart state when switching to an account with no cached snapshot."""
        self._positions = []
        self._live_points.clear()
        self._live_enabled = False
        self._last_content = None
        super().update("  Loading chart…")

    def set_positions(self, positions) -> None:
        normalized = self._normalize_positions(positions)
        if normalized == self._positions:
            return
        self._positions = normalized
        if self._live_enabled:
            return
        if self._positions:
            self._fetch_chart()
        else:
            self.update("  No chart data available for this period.")

    def cycle_period(self, direction: int) -> None:
        if self._live_enabled:
            return
        self._period_idx = (self._period_idx + direction) % len(CHART_PERIODS)
        if self._positions:
            self._fetch_chart()

    def set_live_enabled(self, enabled: bool) -> None:
        self._live_enabled = enabled
        if enabled:
            self._live_points.clear()
            self.update("  LIVE portfolio stream waiting for first balance point…")
        elif self._positions:
            self._fetch_chart()
        else:
            self.update("  No chart data available for this period.")

    def add_live_point(
        self, total_equity: float, timestamp: datetime | None = None
    ) -> None:
        if not self._live_enabled:
            return
        self._live_points.append((timestamp or datetime.now(), total_equity))
        cutoff = self._live_points[-1][0] - LIVE_CHART_WINDOW
        self._live_points = [
            point for point in self._live_points if point[0] >= cutoff
        ]
        self._render_live_chart()

    def _render_live_chart(self) -> None:
        import plotext as plt
        from rich.text import Text as RichText

        if not self._live_points:
            self.update("  LIVE portfolio stream waiting for first balance point…")
            return

        points = self._live_points
        if len(points) > LIVE_CHART_MAX_RENDER_POINTS:
            step = max(1, len(points) // LIVE_CHART_MAX_RENDER_POINTS)
            points = points[::step]
            if points[-1] != self._live_points[-1]:
                points = [*points, self._live_points[-1]]

        timestamps = [point[0].strftime("%m-%d %H:%M") for point in points]
        values = [point[1] for point in points]
        x_nums = list(range(len(values)))
        tick_step = max(1, len(x_nums) // 8)
        x_ticks = x_nums[::tick_step]

        size = self.size
        w = size.width - 4 if size.width > 8 else 80
        h = size.height - 2 if size.height > 6 else 10

        change_summary = _format_period_change(values[0], values[-1])
        title = (
            f"LIVE 24H portfolio equity ({len(self._live_points)} point"
            f"{'' if len(self._live_points) == 1 else 's'})  "
            f"change {change_summary}"
        )

        plt.clf()
        plt.plotsize(w, h)
        plt.theme("dark")
        plt.plot(x_nums, values, marker="braille")
        plt.xticks(x_ticks, [timestamps[i] for i in x_ticks])
        plt.title(title)
        plt.ylabel("$")
        chart_str = plt.build()
        rendered = RichText.from_ansi(chart_str)
        self._last_content = rendered
        self.update(rendered)

    @work(thread=True, exclusive=True)
    def _fetch_chart(self) -> None:
        import plotext as plt
        import yfinance as yf
        from rich.text import Text as RichText

        try:
            label, yf_period, yf_interval = CHART_PERIODS[self._period_idx]

            qty_map: dict[str, float] = {}
            for sym, qty in self._positions:
                yf_sym = BROKER_TO_YF_SYMBOLS.get(sym, sym)
                qty_map[yf_sym] = qty_map.get(yf_sym, 0) + qty

            yf_symbols = list(qty_map.keys())

            data = yf.download(
                yf_symbols,
                period=yf_period,
                interval=yf_interval,
                auto_adjust=True,
                prepost=True,
                progress=False,
                group_by="ticker",
                timeout=YFINANCE_DOWNLOAD_TIMEOUT_SECONDS,
            )

            portfolio_series: pd.Series | None = None
            for yf_sym, qty in qty_map.items():
                try:
                    close = _extract_close_series(data, yf_sym)
                    if close is None:
                        continue
                    series = close.ffill() * qty
                    if series.dropna().empty:
                        continue
                    portfolio_series = (
                        series
                        if portfolio_series is None
                        else portfolio_series.add(series, fill_value=0)
                    )
                except Exception:
                    continue

            if portfolio_series is None or portfolio_series.dropna().empty:
                self.app.call_from_thread(
                    self.update, "  No chart data available for this period."
                )
                return

            portfolio_series = portfolio_series.dropna()
            n = len(portfolio_series)
            if n > 150:
                portfolio_series = portfolio_series.iloc[:: n // 150]

            tick_fmt = (
                "%m-%d %H:%M" if yf_interval in ("5m", "15m", "30m", "1h") else "%b %d"
            )
            timestamps = [
                dt.strftime(tick_fmt) for dt in portfolio_series.index.to_pydatetime()
            ]
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
            period_change = _format_period_change(values[0], values[-1])

            plt.clf()
            plt.plotsize(w, h)
            plt.theme("dark")
            plt.plot(x_nums, values, marker="braille")
            plt.xticks(x_ticks, [timestamps[i] for i in x_ticks])
            plt.title(
                f"{tabs}    change {period_change}    extended hours + crypto 24/7    < [  ] >"
            )
            plt.ylabel("$")
            chart_str = plt.build()

            rendered = RichText.from_ansi(chart_str)
            self._last_content = rendered
            self.app.call_from_thread(self.update, rendered)
        except Exception as exc:
            self.app.call_from_thread(self.update, f"  Chart error: {exc}")


class HoldingsTable(DataTable):
    DEFAULT_CSS = "HoldingsTable { height: 1fr; }"

    def on_mount(self) -> None:
        self.add_columns("SYMBOL", "TYPE", "QTY", "LAST PRICE", "VALUE", "DAY GAIN")
        self.cursor_type = "row"

    def refresh_from_cache(self, rows: list[dict]) -> None:
        self.clear()
        for r in sorted(rows, key=_holding_value_sort_key, reverse=True):
            try:
                gain = r.get("gain", "—")
                gain_style = (
                    "green"
                    if r.get("gain_positive")
                    else ("dim" if gain == "—" else "red")
                )
                self.add_row(
                    Text(r.get("symbol", "?"), style="bold cyan"),
                    Text(r.get("type", "?"), style="dim"),
                    r.get("qty", "—"),
                    r.get("price", "—"),
                    Text(r.get("value", "—"), style="bold"),
                    Text(gain, style=gain_style),
                )
            except Exception:
                continue


class OrdersTable(DataTable):
    DEFAULT_CSS = "OrdersTable { height: 1fr; }"

    def on_mount(self) -> None:
        self.add_columns("SIDE", "SYMBOL", "QTY", "TYPE", "STATUS")
        self.cursor_type = "row"

    def refresh_from_cache(self, rows: list[dict]) -> None:
        self.clear()
        for r in rows:
            try:
                order_id = r.get("order_id")
                if not order_id:
                    continue
                side_style = "green" if r.get("side_buy") else "red"
                self.add_row(
                    Text(r.get("side", "?"), style=side_style),
                    Text(r.get("symbol", "?"), style="bold"),
                    r.get("qty", "—"),
                    r.get("type", "—"),
                    Text(r.get("status", "—"), style="yellow"),
                    key=str(order_id),
                )
            except Exception:
                continue

    def refresh_from_orders(self, orders) -> None:
        self.clear()
        for order in orders:
            if order.status not in _ACTIVE_ORDER_STATUSES:
                continue
            side = order.side.value
            sym = order.instrument.symbol
            qty = str(order.quantity or order.notional_value or "—")
            typ = order.type.value
            status = order.status.value
            side_style = "green" if order.side == OrderSide.BUY else "red"
            self.add_row(
                Text(side, style=side_style),
                Text(sym, style="bold"),
                qty,
                typ,
                Text(status, style="yellow"),
                key=order.order_id,
            )

    def get_selected_order_id(self) -> tuple[str, str] | None:
        """Return (order_id, symbol) for the highlighted row, or None."""
        if self.cursor_row < 0 or self.row_count == 0:
            return None
        row_data = self.get_row_at(self.cursor_row)
        symbol = str(row_data[1])
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
