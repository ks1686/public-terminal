package modals

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ks1686/public-terminal/internal/api"
	"github.com/ks1686/public-terminal/internal/tui/theme"
)

// HistoryModal shows the 90-day transaction history.
type HistoryModal struct {
	tbl    table.Model
	width  int
	height int
}

type HistoryClosedMsg struct{}

func NewHistoryModal(entries []api.HistoryEntry, width, height int) HistoryModal {
	cols := []table.Column{
		{Title: "Date", Width: 12},
		{Title: "Type", Width: 12},
		{Title: "Symbol", Width: 8},
		{Title: "Description", Width: 28},
		{Title: "Amount", Width: 12},
		{Title: "Qty", Width: 10},
		{Title: "Price", Width: 10},
	}
	t := table.New(
		table.WithColumns(cols),
		table.WithFocused(true),
		table.WithHeight(height-6),
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

	rows := make([]table.Row, len(entries))
	for i, e := range entries {
		amt := ""
		if e.Amount != nil {
			f, _ := e.Amount.Float64()
			amt = fmt.Sprintf("$%.2f", f)
		}
		qty := ""
		if e.Quantity != nil {
			qty = e.Quantity.StringFixed(4)
		}
		price := ""
		if e.Price != nil {
			f, _ := e.Price.Float64()
			price = fmt.Sprintf("$%.2f", f)
		}
		rows[i] = table.Row{e.Date, e.Type, e.Symbol, e.Description, amt, qty, price}
	}
	t.SetRows(rows)

	return HistoryModal{tbl: t, width: width, height: height}
}

func (m HistoryModal) Init() tea.Cmd { return nil }

func (m HistoryModal) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "esc", "q":
			return m, func() tea.Msg { return HistoryClosedMsg{} }
		}
	}
	var cmd tea.Cmd
	m.tbl, cmd = m.tbl.Update(msg)
	return m, cmd
}

func (m HistoryModal) View() string {
	lines := []string{
		theme.Title.Render(" Transaction History (90 days)"),
		m.tbl.View(),
		"",
		theme.Muted.Render("↑↓ scroll  esc: close"),
	}
	return theme.ModalBox.Width(m.width - 4).Render(strings.Join(lines, "\n"))
}
