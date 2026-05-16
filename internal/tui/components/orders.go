package components

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ks1686/public-terminal/internal/tui/table"

	"github.com/ks1686/public-terminal/internal/api"
	"github.com/ks1686/public-terminal/internal/tui/theme"
)

// OrdersModel renders the open orders table and tracks the underlying orders per row.
type OrdersModel struct {
	tbl    table.Model
	orders []api.Order
}

func NewOrdersModel() OrdersModel {
	cols := ordersColumnsForWidth(80)
	t := table.New(
		table.WithColumns(cols),
		table.WithFocused(true),
		table.WithHeight(10),
	)
	t.SetStyles(defaultTableStyles())
	return OrdersModel{tbl: t}
}

func ordersColumnsForWidth(w int) []table.Column {
	cols := []table.Column{
		{Title: "Symbol", Width: 8},
		{Title: "Side", Width: 6},
		{Title: "Type", Width: 10},
		{Title: "Status", Width: 14},
		{Title: "Qty", Width: 10},
		{Title: "Amount", Width: 10},
	}
	// Thresholds tuned for content area widths (pane width minus 2 for border).
	// Right pane is narrower (40% on small terminals).
	if w < 58 {
		cols[5].Width = 0 // total now 48
	}
	if w < 48 {
		cols[4].Width = 0 // total now 38
	}
	if w < 38 {
		cols[2].Width = 0 // total now 28
	}
	if w < 28 {
		cols[3].Width = 10 // total now 24
	}
	return cols
}

func (m *OrdersModel) FromPortfolio(p *api.Portfolio) {
	var tRows []table.Row
	var orders []api.Order

	for _, o := range p.Orders {
		if !api.ActiveOrderStatuses[o.Status] {
			continue
		}
		sym := o.Instrument.Symbol
		side := sideStyle(o.Side)
		qty := ""
		if o.Quantity != nil {
			qty = o.Quantity.StringFixed(4)
		}
		amount := ""
		if o.NotionalValue != nil {
			amount = formatMoney(*o.NotionalValue)
		}
		tRows = append(tRows, table.Row{sym, side, o.Type, o.Status, qty, amount})
		orders = append(orders, o)
	}

	m.orders = orders
	m.tbl.SetRows(tRows)
}

// SelectedOrder returns the api.Order under the cursor, or nil if none.
func (m OrdersModel) SelectedOrder() *api.Order {
	idx := m.tbl.Cursor()
	if idx < 0 || idx >= len(m.orders) {
		return nil
	}
	o := m.orders[idx]
	return &o
}

// SelectedOrderID returns the order_id of the currently highlighted row.
func (m OrdersModel) SelectedOrderID() string {
	if o := m.SelectedOrder(); o != nil {
		return o.OrderID
	}
	return ""
}

// SelectedRow returns the currently highlighted table row.
func (m OrdersModel) SelectedRow() table.Row {
	return m.tbl.SelectedRow()
}

// Rows returns all rows (for checking if empty).
func (m OrdersModel) Rows() []table.Row { return m.tbl.Rows() }

func (m OrdersModel) Update(msg tea.Msg) (OrdersModel, tea.Cmd) {
	var cmd tea.Cmd
	m.tbl, cmd = m.tbl.Update(msg)
	return m, cmd
}

func (m *OrdersModel) SetWidth(w int) {
	m.tbl.SetWidth(max(1, w))
	m.tbl.SetColumns(ordersColumnsForWidth(w))
}

func (m OrdersModel) ViewWithHeight(h int) string {
	return renderTablePane(&m.tbl, h, theme.PaneTitleOrders.Render(" OPEN ORDERS"), "  No open orders.", len(m.orders) == 0)
}

func sideStyle(side string) string {
	switch strings.ToUpper(side) {
	case "BUY":
		return theme.Positive.Render("BUY")
	case "SELL":
		return theme.Negative.Render("SELL")
	}
	return side
}
