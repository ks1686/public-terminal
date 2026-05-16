package modals

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/ks1686/public-terminal/internal/tui/table"
	"github.com/shopspring/decimal"

	"github.com/ks1686/public-terminal/internal/api"
	"github.com/ks1686/public-terminal/internal/tui/theme"
)

// HistoryModal shows the 90-day transaction history.
type HistoryModal struct {
	tbl       table.Model
	count     int
	truncated bool
	width     int
	height    int
}

type HistoryClosedMsg struct{}

func NewHistoryModal(entries []api.HistoryEntry, truncated bool, width, height int) HistoryModal {
	innerW := width - 8
	if innerW < 24 {
		innerW = 24
	}
	cols := []table.Column{
		{Title: "Date", Width: 16},
		{Title: "Type", Width: 22},
		{Title: "Symbol", Width: 8},
		{Title: "Side", Width: 6},
		{Title: "Qty", Width: 12},
		{Title: "Net", Width: 12},
	}
	if innerW < 78 {
		cols[4].Width = 0
	}
	if innerW < 66 {
		cols[3].Width = 0
	}
	if innerW < 58 {
		cols[2].Width = 0
	}
	if innerW < 48 {
		cols[1].Width = 14
		cols[0].Width = 12
	}
	innerH := height - 8
	if innerH < 6 {
		innerH = 6
	}
	t := table.New(
		table.WithColumns(cols),
		table.WithFocused(true),
		table.WithHeight(innerH),
	)
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
	t.SetStyles(s)
	t.SetWidth(innerW)

	rows := make([]table.Row, len(entries))
	for i, e := range entries {
		rows[i] = table.Row{
			formatTimestamp(e.Timestamp),
			e.Type,
			dashIfEmpty(e.Symbol),
			styleSide(e.Side),
			dashIfDecimal(e.Quantity),
			dashIfMoney(e.NetAmount),
		}
	}
	t.SetRows(rows)

	return HistoryModal{tbl: t, count: len(entries), truncated: truncated, width: width, height: height}
}

func (m HistoryModal) Init() tea.Cmd { return nil }

func (m HistoryModal) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "esc", "q", "h":
			return m, func() tea.Msg { return HistoryClosedMsg{} }
		}
	}
	var cmd tea.Cmd
	m.tbl, cmd = m.tbl.Update(msg)
	return m, cmd
}

func (m HistoryModal) View() string {
	suffix := ""
	if m.truncated {
		suffix = " (truncated)"
	}
	status := theme.Muted.Render(
		fmt.Sprintf("%d transactions%s (newest first, last 90 days)  |  esc/h to close", m.count, suffix),
	)
	lines := []string{
		theme.Title.Render(" Transaction History"),
		m.tbl.View(),
		"",
		status,
	}
	boxW := m.width - 4
	if boxW < 24 {
		boxW = 24
	}
	return theme.ModalBox.Width(boxW).Render(strings.Join(lines, "\n"))
}

// formatTimestamp converts ISO 8601 to "YYYY-MM-DD HH:MM" in local time.
func formatTimestamp(ts string) string {
	if ts == "" {
		return "—"
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		// Try without nanoseconds
		t, err = time.Parse("2006-01-02T15:04:05Z", ts)
		if err != nil {
			return ts // fall back to raw
		}
	}
	return t.Local().Format("2006-01-02 15:04")
}

func dashIfEmpty(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func dashIfDecimal(d *decimal.Decimal) string {
	if d == nil {
		return "—"
	}
	return d.StringFixed(4)
}

func dashIfMoney(d *decimal.Decimal) string {
	if d == nil {
		return "—"
	}
	return "$" + d.StringFixed(2)
}
