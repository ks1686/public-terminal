package components

import (
	"sort"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ks1686/public-terminal/internal/api"
	"github.com/ks1686/public-terminal/internal/tui/theme"
)

// HoldingsModel renders the equities positions table.
type HoldingsModel struct {
	tbl  table.Model
	rows []table.Row
}

func NewHoldingsModel() HoldingsModel {
	cols := []table.Column{
		{Title: "Symbol", Width: 10},
		{Title: "Qty", Width: 14},
		{Title: "Value", Width: 14},
		{Title: "Last", Width: 12},
		{Title: "Day %", Width: 10},
	}
	t := table.New(
		table.WithColumns(cols),
		table.WithFocused(true),
		table.WithHeight(10),
	)
	t.SetStyles(defaultTableStyles())
	return HoldingsModel{tbl: t}
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
		Foreground(lipgloss.Color("15")).
		Background(lipgloss.Color("237")).
		Bold(false)
	return s
}

// renderTablePane renders a section title above the table body, or an
// empty-state message when the table has no rows. The -2 accounts for the
// title line + table header row inside the surrounding pane border.
func renderTablePane(tbl *table.Model, h int, title, emptyMsg string, empty bool) string {
	tbl.SetHeight(h - 2)
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
	}
	var rows []rowData
	for _, pos := range p.Positions {
		if pos.Instrument.Type != "EQUITY" {
			continue
		}
		sym := pos.Instrument.Symbol
		qty := pos.Quantity.StringFixed(4)
		val := ""
		if pos.CurrentValue != nil {
			val = formatMoney(*pos.CurrentValue)
		}
		last := ""
		if pos.LastPrice != nil && pos.LastPrice.LastPrice != nil {
			last = formatMoney(*pos.LastPrice.LastPrice)
		}
		dayPct := ""
		if pos.PositionDailyGain != nil && pos.PositionDailyGain.GainPercentage != nil {
			pct, _ := pos.PositionDailyGain.GainPercentage.Float64()
			dayPct = theme.FormatGain(pct)
		}
		rows = append(rows, rowData{sym, qty, val, last, dayPct})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].symbol < rows[j].symbol })

	tRows := make([]table.Row, len(rows))
	for i, r := range rows {
		tRows[i] = table.Row{r.symbol, r.qty, r.value, r.last, r.dayPct}
	}
	m.rows = tRows
	m.tbl.SetRows(tRows)
}

func (m HoldingsModel) SelectedSymbol() string {
	r := m.tbl.SelectedRow()
	if r == nil || len(r) == 0 {
		return ""
	}
	return r[0]
}

func (m HoldingsModel) Update(msg tea.Msg) (HoldingsModel, tea.Cmd) {
	var cmd tea.Cmd
	m.tbl, cmd = m.tbl.Update(msg)
	return m, cmd
}

func (m HoldingsModel) ViewWithHeight(h int) string {
	return renderTablePane(&m.tbl, h, theme.PaneTitle.Render(" HOLDINGS"), "  No equity positions.", len(m.rows) == 0)
}
