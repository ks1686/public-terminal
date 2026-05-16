package components

import (
	"fmt"
	"sort"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/shopspring/decimal"

	"github.com/ks1686/public-terminal/internal/api"
	"github.com/ks1686/public-terminal/internal/tui/table"
	"github.com/ks1686/public-terminal/internal/tui/theme"
)

// CryptoModel renders the crypto positions table.
type CryptoModel struct {
	tbl  table.Model
	rows []table.Row
}

func NewCryptoModel() CryptoModel {
	cols := cryptoColumnsForWidth(80)
	t := table.New(
		table.WithColumns(cols),
		table.WithFocused(true),
		table.WithHeight(10),
	)
	t.SetStyles(defaultTableStyles())
	return CryptoModel{tbl: t}
}

func cryptoColumnsForWidth(w int) []table.Column {
	cols := []table.Column{
		{Title: "Symbol", Width: 9},
		{Title: "Qty", Width: 10},
		{Title: "Value", Width: 12},
		{Title: "Last", Width: 10},
		{Title: "Day %", Width: 8},
	}
	// Thresholds tuned for content area widths (pane width minus 2 for border).
	// Right pane is narrower (40% on small terminals).
	if w < 52 {
		cols[3].Width = 0 // hide Last, total now 39
	}
	if w < 42 {
		cols[4].Width = 0 // hide Day%, total now 31
	}
	if w < 34 {
		cols[1].Width = 8
		cols[2].Width = 10 // shrink Qty/Value, total now 27
	}
	if w < 28 {
		cols[1].Width = 0
		cols[2].Width = 9 // hide Qty, shrink Value, total now 18
	}
	return cols
}

func (m *CryptoModel) FromPortfolio(p *api.Portfolio) {
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
		if pos.Instrument.Type != "CRYPTO" {
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

func (m CryptoModel) Update(msg tea.Msg) (CryptoModel, tea.Cmd) {
	var cmd tea.Cmd
	m.tbl, cmd = m.tbl.Update(msg)
	return m, cmd
}

func (m CryptoModel) SelectedSymbol() string {
	r := m.tbl.SelectedRow()
	if len(r) == 0 {
		return ""
	}
	return r[0]
}

func (m *CryptoModel) SetWidth(w int) {
	m.tbl.SetWidth(max(1, w))
	m.tbl.SetColumns(cryptoColumnsForWidth(w))
}

func (m CryptoModel) ViewWithHeight(h int) string {
	return renderTablePane(&m.tbl, h, theme.PaneTitleCrypto.Render(" CRYPTO"), "  No crypto positions.", len(m.rows) == 0)
}
