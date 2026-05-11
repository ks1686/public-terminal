package modals

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ks1686/public-terminal/internal/api"
	"github.com/ks1686/public-terminal/internal/tui/theme"
)

// CancelModal asks the user to confirm cancelling an open order.
type CancelModal struct {
	client  *api.Client
	orderID string
	symbol  string
	err     string
}

type OrderCancelledConfirmMsg struct{ OrderID string }

func NewCancelModal(client *api.Client, orderID, symbol string) CancelModal {
	return CancelModal{client: client, orderID: orderID, symbol: symbol}
}

func (m CancelModal) Init() tea.Cmd { return nil }

func (m CancelModal) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "y", "Y", "enter":
			return m, func() tea.Msg {
				if err := m.client.CancelOrder(m.orderID); err != nil {
					return errMsg{err}
				}
				return OrderCancelledConfirmMsg{OrderID: m.orderID}
			}
		case "n", "N", "esc":
			return m, func() tea.Msg { return OrderCancelledMsg{} }
		}
	}
	return m, nil
}

func (m CancelModal) View() string {
	lines := []string{
		theme.Warning.Render("Cancel Order"),
		"",
		fmt.Sprintf("Cancel %s order %s?", m.symbol, theme.Muted.Render(m.orderID)),
		"",
		theme.Muted.Render("y/enter: confirm  n/esc: back"),
	}
	if m.err != "" {
		lines = append(lines, theme.StatusErr.Render(m.err))
	}
	return theme.ModalBox.Render(strings.Join(lines, "\n"))
}
