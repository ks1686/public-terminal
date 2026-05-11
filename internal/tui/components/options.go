package components

import (
	"fmt"
	"sort"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/ks1686/public-terminal/internal/api"
	"github.com/ks1686/public-terminal/internal/options"
	"github.com/ks1686/public-terminal/internal/tui/theme"
)

type OptionsModel struct {
	tbl  table.Model
	rows []table.Row
}

func NewOptionsModel() OptionsModel {
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
		table.WithFocused(true),
		table.WithHeight(10),
	)
	t.SetStyles(defaultTableStyles())
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
		val := formatMoney(o.CurrentValue)

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

func (m OptionsModel) ViewWithHeight(h int) string {
	return renderTablePane(&m.tbl, h, theme.PaneTitle.Render(" OPTIONS"), "  No option positions.", len(m.rows) == 0)
}
