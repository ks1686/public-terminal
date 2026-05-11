package components

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ks1686/public-terminal/internal/api"
	"github.com/ks1686/public-terminal/internal/options"
	"github.com/ks1686/public-terminal/internal/tui/theme"
)

// OptionsModel renders the options positions table.
type OptionsModel struct {
	tbl  table.Model
	rows []table.Row
}

func NewOptionsModel(width, height int) OptionsModel {
	cols := []table.Column{
		{Title: "Symbol", Width: 22},
		{Title: "Type", Width: 6},
		{Title: "Strike", Width: 10},
		{Title: "Expiry", Width: 10},
		{Title: "Qty", Width: 6},
		{Title: "Value", Width: 10},
		{Title: "Day %", Width: 10},
		{Title: "DTE", Width: 5},
	}
	t := table.New(
		table.WithColumns(cols),
		table.WithFocused(false),
		table.WithHeight(height),
	)
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
	t.SetStyles(s)
	return OptionsModel{tbl: t}
}

func (m *OptionsModel) FromPortfolio(p *api.Portfolio) {
	opts := options.ExtractOptionsFromPositions(p.Positions)
	sort.Slice(opts, func(i, j int) bool { return opts[i].OCCSymbol < opts[j].OCCSymbol })

	tRows := make([]table.Row, len(opts))
	for i, o := range opts {
		dayPct := ""
		if o.DailyGainPct != nil {
			f, _ := o.DailyGainPct.Float64()
			dayPct = theme.FormatGain(f)
		}
		f, _ := o.CurrentValue.Float64()
		val := fmt.Sprintf("$%.2f", f)

		dteStyle := theme.Muted
		if o.IsNearExpiry() {
			dteStyle = theme.Warning
		}
		dte := dteStyle.Render(fmt.Sprintf("%d", o.DaysToExpiry))

		tRows[i] = table.Row{
			o.SymbolDisplay(),
			o.OptionType,
			"$" + o.StrikePrice.StringFixed(2),
			o.ExpirationDate,
			o.Quantity.StringFixed(0),
			val,
			dayPct,
			dte,
		}
	}
	m.rows = tRows
	m.tbl.SetRows(tRows)
}

func (m OptionsModel) Update(msg tea.Msg) (OptionsModel, tea.Cmd) {
	var cmd tea.Cmd
	m.tbl, cmd = m.tbl.Update(msg)
	return m, cmd
}

func (m OptionsModel) View() string {
	header := theme.TableHeader.Render(" Options")
	body := m.tbl.View()
	if len(m.rows) == 0 {
		body = theme.Muted.Render("  No option positions.")
	}
	return strings.Join([]string{header, body}, "\n")
}
