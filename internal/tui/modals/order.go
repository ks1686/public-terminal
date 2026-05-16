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

// OrderModal is the buy/sell order form.
type OrderModal struct {
	client     *api.Client
	side       string // "BUY" or "SELL"
	symInput   textinput.Model
	typeInput  textinput.Model
	qtyInput   textinput.Model
	orderType  int // 0=MARKET 1=LIMIT 2=STOP 3=STOP_LIMIT
	limitInput textinput.Model
	stopInput  textinput.Model
	focus      int
	err        string
}

var orderTypes = []string{"MARKET", "LIMIT", "STOP", "STOP_LIMIT"}

// OrderPlacedMsg is returned when an order is successfully placed.
type OrderPlacedMsg struct{ Symbol string }

// OrderCancelledMsg is returned when the user cancels the form.
type OrderCancelledMsg struct{}

func NewOrderModal(client *api.Client, side, defaultSymbol, defaultType string) OrderModal {
	sym := textinput.New()
	sym.Placeholder = "Symbol (e.g. AAPL)"
	sym.SetValue(strings.ToUpper(defaultSymbol))
	sym.Focus()

	instr := textinput.New()
	instr.Placeholder = "EQUITY or CRYPTO"
	instr.SetValue(strings.ToUpper(defaultType))

	qty := textinput.New()
	qty.Placeholder = "Dollar amount (e.g. 100)"

	limit := textinput.New()
	limit.Placeholder = "Limit price"

	stop := textinput.New()
	stop.Placeholder = "Stop price"

	return OrderModal{
		client:     client,
		side:       side,
		symInput:   sym,
		typeInput:  instr,
		qtyInput:   qty,
		limitInput: limit,
		stopInput:  stop,
	}
}

func (m OrderModal) Init() tea.Cmd { return textinput.Blink }

func (m OrderModal) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return m, func() tea.Msg { return OrderCancelledMsg{} }

		case "tab":
			maxFocus := 3
			if m.orderType >= 1 {
				maxFocus = 4
			}
			if m.orderType == 3 {
				maxFocus = 5
			}
			m.focus = (m.focus + 1) % maxFocus
			m.refocus()

		case "[":
			m.orderType = (m.orderType + len(orderTypes) - 1) % len(orderTypes)
		case "]":
			m.orderType = (m.orderType + 1) % len(orderTypes)

		case "ctrl+s", "enter":
			if m.focus == 0 || m.focus == 1 || m.focus == 2 || m.focus == 3 || m.focus == 4 {
				// handled by trySubmit
			}
			return m.trySubmit()
		}
	}

	var cmd tea.Cmd
	switch m.focus {
	case 0:
		m.symInput, cmd = m.symInput.Update(msg)
	case 1:
		m.typeInput, cmd = m.typeInput.Update(msg)
	case 2:
		m.qtyInput, cmd = m.qtyInput.Update(msg)
	case 3:
		m.limitInput, cmd = m.limitInput.Update(msg)
	case 4:
		m.stopInput, cmd = m.stopInput.Update(msg)
	}
	return m, cmd
}

func (m *OrderModal) refocus() {
	inputs := []*textinput.Model{&m.symInput, &m.typeInput, &m.qtyInput, &m.limitInput, &m.stopInput}
	for i, inp := range inputs {
		if i == m.focus {
			inp.Focus()
		} else {
			inp.Blur()
		}
	}
}

func (m *OrderModal) trySubmit() (tea.Model, tea.Cmd) {
	sym := strings.ToUpper(strings.TrimSpace(m.symInput.Value()))
	if sym == "" {
		m.err = "Symbol is required."
		m.focus = 0
		m.refocus()
		return m, nil
	}
	instrType := strings.ToUpper(strings.TrimSpace(m.typeInput.Value()))
	if instrType == "" {
		instrType = "EQUITY"
	}
	amtStr := strings.TrimSpace(m.qtyInput.Value())
	if amtStr == "" {
		m.err = "Amount is required."
		m.focus = 2
		m.refocus()
		return m, nil
	}
	amt, err := decimal.NewFromString(amtStr)
	if err != nil || !amt.IsPositive() {
		m.err = "Amount must be a positive number."
		m.focus = 2
		m.refocus()
		return m, nil
	}

	ot := orderTypes[m.orderType]
	req := api.OrderRequest{
		Instrument: api.OrderInstrument{Symbol: sym, Type: instrType},
		OrderSide:  m.side,
		OrderType:  ot,
		Expiration: api.OrderExpiration{TimeInForce: "DAY"},
		Amount:     &amt,
	}

	if ot == "LIMIT" || ot == "STOP_LIMIT" {
		lpStr := strings.TrimSpace(m.limitInput.Value())
		if lpStr == "" {
			m.err = "Limit price is required for this order type."
			m.focus = 3
			m.refocus()
			return m, nil
		}
		lp, err := decimal.NewFromString(lpStr)
		if err != nil || !lp.IsPositive() {
			m.err = "Limit price must be a positive number."
			m.focus = 3
			m.refocus()
			return m, nil
		}
		req.LimitPrice = &lp
	}
	if ot == "STOP" || ot == "STOP_LIMIT" {
		spStr := strings.TrimSpace(m.stopInput.Value())
		if spStr == "" {
			m.err = "Stop price is required for this order type."
			m.focus = 4
			m.refocus()
			return m, nil
		}
		sp, err := decimal.NewFromString(spStr)
		if err != nil || !sp.IsPositive() {
			m.err = "Stop price must be a positive number."
			m.focus = 4
			m.refocus()
			return m, nil
		}
		req.StopPrice = &sp
	}

	m.err = ""
	return m, func() tea.Msg {
		if err := m.client.PlaceOrder(req); err != nil {
			return errMsg{err}
		}
		return OrderPlacedMsg{Symbol: sym}
	}
}

type errMsg struct{ err error }

func (m OrderModal) View() string {
	title := fmt.Sprintf("%s Order", m.side)
	if m.side == "BUY" {
		title = theme.Positive.Render(title)
	} else {
		title = theme.Negative.Render(title)
	}

	typeTabs := make([]string, len(orderTypes))
	for i, ot := range orderTypes {
		if i == m.orderType {
			typeTabs[i] = theme.Title.Render("[" + ot + "]")
		} else {
			typeTabs[i] = theme.Muted.Render(" " + ot + " ")
		}
	}

	lines := []string{
		title,
		"",
		"Symbol:   " + m.symInput.View(),
		"Type:     " + m.typeInput.View(),
		"Amount $: " + m.qtyInput.View(),
		"Order:    " + strings.Join(typeTabs, " ") + "  ([ / ] to change)",
	}
	if m.orderType >= 1 {
		lines = append(lines, "Limit $:  "+m.limitInput.View())
	}
	if m.orderType == 2 || m.orderType == 3 {
		lines = append(lines, "Stop $:   "+m.stopInput.View())
	}
	lines = append(lines, "")
	lines = append(lines, theme.Muted.Render("tab: next field  ctrl+s/enter: place  esc: cancel"))
	if m.err != "" {
		lines = append(lines, theme.StatusErr.Render(m.err))
	}
	return theme.ModalBox.Render(strings.Join(lines, "\n"))
}
