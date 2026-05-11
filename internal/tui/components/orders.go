package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/ks1686/public-terminal/internal/api"
	"github.com/ks1686/public-terminal/internal/tui/theme"
)

// OrdersModel renders the open orders table and tracks order IDs per row.
type OrdersModel struct {
	tbl      table.Model
	orderIDs []string
}

func NewOrdersModel() OrdersModel {
	cols := []table.Column{
		{Title: "Symbol", Width: 8},
		{Title: "Side", Width: 6},
		{Title: "Type", Width: 10},
		{Title: "Status", Width: 14},
		{Title: "Qty", Width: 10},
		{Title: "Amount", Width: 10},
	}
	t := table.New(
		table.WithColumns(cols),
		table.WithFocused(true),
		table.WithHeight(10),
	)
	t.SetStyles(defaultTableStyles())
	return OrdersModel{tbl: t}
}

func (m *OrdersModel) FromPortfolio(p *api.Portfolio) {
	var tRows []table.Row
	var ids []string

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
			f, _ := o.NotionalValue.Float64()
			amount = fmt.Sprintf("$%.2f", f)
		}
		tRows = append(tRows, table.Row{sym, side, o.Type, o.Status, qty, amount})
		ids = append(ids, o.OrderID)
	}

	m.orderIDs = ids
	m.tbl.SetRows(tRows)
}

// SelectedOrderID returns the order_id of the currently highlighted row.
func (m OrdersModel) SelectedOrderID() string {
	idx := m.tbl.Cursor()
	if idx < 0 || idx >= len(m.orderIDs) {
		return ""
	}
	return m.orderIDs[idx]
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

func (m OrdersModel) ViewWithHeight(h int) string {
	m.tbl.SetHeight(h - 2)
	header := theme.PaneTitleAccent.Render(" OPEN ORDERS")
	body := m.tbl.View()
	if len(m.orderIDs) == 0 {
		body = theme.Muted.Render("  No open orders.")
	}
	return strings.Join([]string{header, body}, "\n")
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
