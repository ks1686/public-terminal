package components

import (
	"fmt"
	"sort"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/ks1686/public-terminal/internal/tui/table"
	"github.com/shopspring/decimal"

	"github.com/ks1686/public-terminal/internal/api"
	"github.com/ks1686/public-terminal/internal/tui/theme"
)

// HoldingsModel renders the equities positions table.
type HoldingsModel struct {
	tbl  table.Model
	rows []table.Row
}

func NewHoldingsModel() HoldingsModel {
	cols := holdingsColumnsForWidth(80)
	t := table.New(
		table.WithColumns(cols),
		table.WithFocused(true),
		table.WithHeight(10),
	)
	t.SetStyles(defaultTableStyles())
	return HoldingsModel{tbl: t}
}

func holdingsColumnsForWidth(w int) []table.Column {
	cols := []table.Column{
		{Title: "Symbol", Width: 10},
		{Title: "Qty", Width: 14},
		{Title: "Value", Width: 14},
		{Title: "Last", Width: 12},
		{Title: "Day %", Width: 10},
	}
	// Hide Day% first (10), then Last (12), then shrink Qty/Value.
	// Thresholds are tuned for content area widths (pane width minus 2 for border).
	if w < 62 {
		cols[4].Width = 0 // total now 50
	}
	if w < 52 {
		cols[3].Width = 0 // total now 38
	}
	if w < 40 {
		cols[1].Width = 8
		cols[2].Width = 10 // total now 28
	}
	if w < 30 {
		cols[2].Width = 0 // total now 18
	}
	return cols
}

func defaultTableStyles() table.Styles {
	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("8")).
		BorderBottom(true).
		Bold(true).
		Foreground(lipgloss.Color("6"))
	s.Selected = s.Selected.
		Background(lipgloss.Color("237")).
		UnsetForeground().
		Bold(false)
	return s
}

// renderTablePane renders a section title above the table body, or an
// empty-state message when the table has no rows. The -2 accounts for the
// title line + table header row inside the surrounding pane border.
func renderTablePane(tbl *table.Model, h int, title, emptyMsg string, empty bool) string {
	tbl.SetHeight(h - 1)
	body := tbl.View()
	if empty {
		body = theme.Muted.Render(emptyMsg)
	}
	return title + "\n" + body
}

func (m *HoldingsModel) FromPortfolio(p *api.Portfolio) {
	type rowData struct {
		symbol string
		qty    string
		value  string
		last   string
		dayPct string
		total  decimal.Decimal
	}
	var rows []rowData
	for _, pos := range p.Positions {
		if pos.Instrument.Type != "EQUITY" {
			continue
		}
		sym := pos.Instrument.Symbol
		qty := pos.Quantity.StringFixed(4)
		val := ""
		total := decimal.Zero
		if pos.CurrentValue != nil {
			val = formatMoney(*pos.CurrentValue)
			total = *pos.CurrentValue
		}
		last := ""
		if pos.LastPrice != nil && pos.LastPrice.LastPrice != nil {
			last = formatMoney(*pos.LastPrice.LastPrice)
		}

		rowPositive := false
		rowNegative := false
		dayPct := ""
		if pos.PositionDailyGain != nil && pos.PositionDailyGain.GainPercentage != nil {
			pct, _ := pos.PositionDailyGain.GainPercentage.Float64()
			if pct >= 0 {
				rowPositive = true
			} else {
				rowNegative = true
			}
			if rowPositive {
				dayPct = theme.Positive.Render(fmt.Sprintf("%+.2f%%", pct))
			} else {
				dayPct = theme.Negative.Render(fmt.Sprintf("%+.2f%%", pct))
			}
		}
		paint := func(s string) string {
			if rowPositive {
				return theme.Positive.Render(s)
			}
			if rowNegative {
				return theme.Negative.Render(s)
			}
			return s
		}
		rows = append(rows, rowData{
			symbol: paint(sym),
			qty:    paint(qty),
			value:  paint(val),
			last:   paint(last),
			dayPct: dayPct,
			total:  total,
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].total.Equal(rows[j].total) {
			return rows[i].symbol < rows[j].symbol
		}
		return rows[i].total.GreaterThan(rows[j].total)
	})

	tRows := make([]table.Row, len(rows))
	for i, r := range rows {
		tRows[i] = table.Row{r.symbol, r.qty, r.value, r.last, r.dayPct}
	}
	m.rows = tRows
	m.tbl.SetRows(tRows)
}

func (m HoldingsModel) SelectedSymbol() string {
	r := m.tbl.SelectedRow()
	if len(r) == 0 {
		return ""
	}
	return r[0]
}

func (m HoldingsModel) Update(msg tea.Msg) (HoldingsModel, tea.Cmd) {
	var cmd tea.Cmd
	m.tbl, cmd = m.tbl.Update(msg)
	return m, cmd
}

func (m *HoldingsModel) SetWidth(w int) {
	m.tbl.SetWidth(max(1, w))
	m.tbl.SetColumns(holdingsColumnsForWidth(w))
}

func (m HoldingsModel) ViewWithHeight(h int) string {
	return renderTablePane(&m.tbl, h, theme.PaneTitleStocks.Render(" STOCKS"), "  No stock positions.", len(m.rows) == 0)
}
