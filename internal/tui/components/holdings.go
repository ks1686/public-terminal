package components

import (
	"fmt"
	"sort"
	"strings"

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

func NewHoldingsModel(width, height int) HoldingsModel {
	cols := makeHoldingsCols(width)
	t := table.New(
		table.WithColumns(cols),
		table.WithFocused(true),
		table.WithHeight(height),
	)
	t.SetStyles(defaultTableStyles())
	return HoldingsModel{tbl: t}
}

func makeHoldingsCols(width int) []table.Column {
	_ = width
	return []table.Column{
		{Title: "Symbol", Width: 8},
		{Title: "Qty", Width: 12},
		{Title: "Value", Width: 12},
		{Title: "Last", Width: 10},
		{Title: "Day %", Width: 10},
	}
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
			f, _ := pos.LastPrice.LastPrice.Float64()
			last = fmt.Sprintf("$%.2f", f)
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

func (m HoldingsModel) View() string {
	header := theme.TableHeader.Render(" Holdings")
	body := m.tbl.View()
	if len(m.rows) == 0 {
		body = theme.Muted.Render("  No equity positions.")
	}
	return strings.Join([]string{header, body}, "\n")
}
