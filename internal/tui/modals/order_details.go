package modals

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/shopspring/decimal"

	"github.com/ks1686/public-terminal/internal/api"
	"github.com/ks1686/public-terminal/internal/tui/theme"
)

// OrderDetailsModal lets the user inspect an open order, request a cancel, or
// submit a modify request (cancel + replace, surfaced via ModifyRequestedMsg).
type OrderDetailsModal struct {
	order        api.Order
	qtyInput     textinput.Model
	limitInput   textinput.Model
	stopInput    textinput.Model
	focus        int // 0..n-1: inputs (variable count) then buttons
	err          string
	wantsLimit   bool
	wantsStop    bool
	inputCount   int
	buttonStart  int // first focus index that points at a button
	buttonLabels []string
}

// CancelRequestedMsg asks the app to swap to a CancelModal for the given order.
type CancelRequestedMsg struct {
	OrderID string
	Symbol  string
}

// ModifyRequestedMsg is emitted when the user submits the modify form.
// New* fields are nil if left blank.
type ModifyRequestedMsg struct {
	OrderID  string
	Symbol   string
	NewQty   *decimal.Decimal
	NewLimit *decimal.Decimal
	NewStop  *decimal.Decimal
}

func NewOrderDetailsModal(order api.Order) OrderDetailsModal {
	wantsLimit := order.Type == "LIMIT" || order.Type == "STOP_LIMIT"
	wantsStop := order.Type == "STOP" || order.Type == "STOP_LIMIT"

	qty := textinput.New()
	qty.Placeholder = "(keep current)"
	qty.CharLimit = 16
	qty.Width = 20
	qty.Focus()

	limit := textinput.New()
	limit.Placeholder = "e.g. 150.50"
	limit.CharLimit = 16
	limit.Width = 20

	stop := textinput.New()
	stop.Placeholder = "e.g. 145.00"
	stop.CharLimit = 16
	stop.Width = 20

	inputCount := 1
	if wantsLimit {
		inputCount++
	}
	if wantsStop {
		inputCount++
	}

	return OrderDetailsModal{
		order:        order,
		qtyInput:     qty,
		limitInput:   limit,
		stopInput:    stop,
		wantsLimit:   wantsLimit,
		wantsStop:    wantsStop,
		inputCount:   inputCount,
		buttonStart:  inputCount,
		buttonLabels: []string{"Modify", "Cancel Order", "Close"},
	}
}

func (m OrderDetailsModal) Init() tea.Cmd { return textinput.Blink }

func (m OrderDetailsModal) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "esc":
			return m, func() tea.Msg { return OrderCancelledMsg{} }

		case "tab", "down":
			m.focus = (m.focus + 1) % (m.inputCount + len(m.buttonLabels))
			m.refocus()
			return m, nil

		case "shift+tab", "up":
			total := m.inputCount + len(m.buttonLabels)
			m.focus = (m.focus - 1 + total) % total
			m.refocus()
			return m, nil

		case "enter":
			// On a button: activate. On an input: jump to the first button.
			if m.focus < m.buttonStart {
				m.focus = m.buttonStart
				m.refocus()
				return m, nil
			}
			return m.activateButton()
		}
	}

	var cmd tea.Cmd
	switch m.focus {
	case 0:
		m.qtyInput, cmd = m.qtyInput.Update(msg)
	case 1:
		if m.wantsLimit {
			m.limitInput, cmd = m.limitInput.Update(msg)
		} else if m.wantsStop {
			m.stopInput, cmd = m.stopInput.Update(msg)
		}
	case 2:
		if m.wantsLimit && m.wantsStop {
			m.stopInput, cmd = m.stopInput.Update(msg)
		}
	}
	return m, cmd
}

func (m *OrderDetailsModal) refocus() {
	m.qtyInput.Blur()
	m.limitInput.Blur()
	m.stopInput.Blur()
	switch m.focus {
	case 0:
		m.qtyInput.Focus()
	case 1:
		if m.wantsLimit {
			m.limitInput.Focus()
		} else if m.wantsStop {
			m.stopInput.Focus()
		}
	case 2:
		if m.wantsLimit && m.wantsStop {
			m.stopInput.Focus()
		}
	}
}

func (m OrderDetailsModal) activateButton() (tea.Model, tea.Cmd) {
	btn := m.focus - m.buttonStart
	switch btn {
	case 0: // Modify
		return m.submitModify()
	case 1: // Cancel Order
		return m, func() tea.Msg {
			return CancelRequestedMsg{OrderID: m.order.OrderID, Symbol: m.order.Instrument.Symbol}
		}
	case 2: // Close
		return m, func() tea.Msg { return OrderCancelledMsg{} }
	}
	return m, nil
}

func (m OrderDetailsModal) submitModify() (tea.Model, tea.Cmd) {
	qtyStr := strings.TrimSpace(m.qtyInput.Value())
	limitStr := strings.TrimSpace(m.limitInput.Value())
	stopStr := strings.TrimSpace(m.stopInput.Value())

	if qtyStr == "" && limitStr == "" && stopStr == "" {
		m.err = "Enter at least one value to modify."
		return m, nil
	}

	var newQty, newLimit, newStop *decimal.Decimal
	if qtyStr != "" {
		v, err := decimal.NewFromString(qtyStr)
		if err != nil || !v.IsPositive() {
			m.err = "Quantity must be a positive number."
			m.focus = 0
			m.refocus()
			return m, nil
		}
		newQty = &v
	}
	if m.wantsLimit && limitStr != "" {
		v, err := decimal.NewFromString(limitStr)
		if err != nil || !v.IsPositive() {
			m.err = "Limit price must be a positive number."
			return m, nil
		}
		newLimit = &v
	}
	if m.wantsStop && stopStr != "" {
		v, err := decimal.NewFromString(stopStr)
		if err != nil || !v.IsPositive() {
			m.err = "Stop price must be a positive number."
			return m, nil
		}
		newStop = &v
	}

	return m, func() tea.Msg {
		return ModifyRequestedMsg{
			OrderID:  m.order.OrderID,
			Symbol:   m.order.Instrument.Symbol,
			NewQty:   newQty,
			NewLimit: newLimit,
			NewStop:  newStop,
		}
	}
}

func (m OrderDetailsModal) View() string {
	o := m.order
	header := theme.Title.Render("ORDER DETAILS")

	qtyStr := "—"
	if o.Quantity != nil {
		qtyStr = o.Quantity.StringFixed(4)
	}

	rows := []string{
		header,
		"",
		fmt.Sprintf("%s %s  %s %s",
			theme.Muted.Render("Symbol:"), o.Instrument.Symbol,
			theme.Muted.Render("Side:"), styleSide(o.Side),
		),
		fmt.Sprintf("%s %s  %s %s",
			theme.Muted.Render("Type:"), o.Type,
			theme.Muted.Render("Status:"), o.Status,
		),
		fmt.Sprintf("%s %s", theme.Muted.Render("Quantity:"), qtyStr),
	}
	if o.LimitPrice != nil {
		rows = append(rows, fmt.Sprintf("%s $%s", theme.Muted.Render("Limit:"), o.LimitPrice.StringFixed(2)))
	}
	if o.StopPrice != nil {
		rows = append(rows, fmt.Sprintf("%s $%s", theme.Muted.Render("Stop:"), o.StopPrice.StringFixed(2)))
	}

	rows = append(rows, "", theme.Muted.Render("New quantity:"), m.qtyInput.View())
	if m.wantsLimit {
		rows = append(rows, theme.Muted.Render("New limit price:"), m.limitInput.View())
	}
	if m.wantsStop {
		rows = append(rows, theme.Muted.Render("New stop price:"), m.stopInput.View())
	}

	rows = append(rows, "", m.renderButtons())
	rows = append(rows, "", theme.Muted.Render("tab: next  enter: activate  esc: close"))
	if m.err != "" {
		rows = append(rows, theme.StatusErr.Render(m.err))
	}
	return theme.ModalBox.Render(strings.Join(rows, "\n"))
}

func (m OrderDetailsModal) renderButtons() string {
	parts := make([]string, len(m.buttonLabels))
	for i, label := range m.buttonLabels {
		if m.focus == m.buttonStart+i {
			parts[i] = theme.Title.Render("[ " + label + " ]")
		} else {
			parts[i] = theme.Muted.Render("  " + label + "  ")
		}
	}
	return strings.Join(parts, "  ")
}

func styleSide(side string) string {
	switch strings.ToUpper(side) {
	case "BUY":
		return theme.Positive.Render("BUY")
	case "SELL":
		return theme.Negative.Render("SELL")
	}
	return side
}
