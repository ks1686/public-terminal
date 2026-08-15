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
	confirming bool
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
	instr.Placeholder = "EQUITY, CRYPTO, or OPTION"
	instr.SetValue(strings.ToUpper(defaultType))

	qty := textinput.New()
	if strings.EqualFold(defaultType, "OPTION") {
		qty.Placeholder = "Contracts (e.g. 1)"
	} else {
		qty.Placeholder = "Dollar amount (e.g. 100)"
	}

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

// WithQuantity pre-fills the amount/quantity field (used for option closes).
func (m OrderModal) WithQuantity(qty string) OrderModal {
	m.qtyInput.SetValue(qty)
	return m
}

func (m *OrderModal) SetError(s string) { m.err = s }

func (m OrderModal) isOption() bool {
	return strings.EqualFold(strings.TrimSpace(m.typeInput.Value()), "OPTION")
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
			if !m.confirming {
				m.confirming = true
				m.err = ""
				return m, nil
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
	req, errMsgText := m.buildRequest()
	if errMsgText != "" {
		m.err = errMsgText
		m.confirming = false
		m.refocus()
		return m, nil
	}
	m.err = ""
	m.confirming = false
	sym := req.Instrument.Symbol
	return m, func() tea.Msg {
		if err := m.client.PlaceOrder(req); err != nil {
			return ErrMsg{Err: err}
		}
		return OrderPlacedMsg{Symbol: sym}
	}
}

func (m *OrderModal) buildRequest() (api.OrderRequest, string) {
	sym := strings.ToUpper(strings.TrimSpace(m.symInput.Value()))
	if sym == "" {
		m.focus = 0
		return api.OrderRequest{}, "Symbol is required."
	}
	instrType := strings.ToUpper(strings.TrimSpace(m.typeInput.Value()))
	if instrType == "" {
		instrType = "EQUITY"
	}
	amtStr := strings.TrimSpace(m.qtyInput.Value())
	if amtStr == "" {
		m.focus = 2
		if instrType == "OPTION" {
			return api.OrderRequest{}, "Contract quantity is required."
		}
		return api.OrderRequest{}, "Amount is required."
	}
	amt, err := decimal.NewFromString(amtStr)
	if err != nil || !amt.IsPositive() {
		m.focus = 2
		if instrType == "OPTION" {
			return api.OrderRequest{}, "Quantity must be a positive number of contracts."
		}
		return api.OrderRequest{}, "Amount must be a positive number."
	}

	orderID, err := api.NewOrderID()
	if err != nil {
		return api.OrderRequest{}, "Could not generate order id."
	}
	ot := orderTypes[m.orderType]
	req := api.OrderRequest{
		OrderID:    orderID,
		Instrument: api.OrderInstrument{Symbol: sym, Type: instrType},
		OrderSide:  m.side,
		OrderType:  ot,
		Expiration: api.OrderExpiration{TimeInForce: "DAY"},
	}
	if instrType == "OPTION" && m.side == "SELL" {
		req.OpenCloseIndicator = "CLOSE"
	}
	if instrType == "OPTION" {
		req.Quantity = &amt
	} else {
		req.Amount = &amt
	}

	if ot == "LIMIT" || ot == "STOP_LIMIT" {
		lpStr := strings.TrimSpace(m.limitInput.Value())
		if lpStr == "" {
			m.focus = 3
			return api.OrderRequest{}, "Limit price is required for this order type."
		}
		lp, err := decimal.NewFromString(lpStr)
		if err != nil || !lp.IsPositive() {
			m.focus = 3
			return api.OrderRequest{}, "Limit price must be a positive number."
		}
		req.LimitPrice = &lp
	}
	if ot == "STOP" || ot == "STOP_LIMIT" {
		spStr := strings.TrimSpace(m.stopInput.Value())
		if spStr == "" {
			m.focus = 4
			return api.OrderRequest{}, "Stop price is required for this order type."
		}
		sp, err := decimal.NewFromString(spStr)
		if err != nil || !sp.IsPositive() {
			m.focus = 4
			return api.OrderRequest{}, "Stop price must be a positive number."
		}
		req.StopPrice = &sp
	}
	return req, ""
}

// ErrMsg is returned when a modal command fails. The root model surfaces it.
type ErrMsg struct{ Err error }

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

	qtyLabel := "Amount $: "
	if m.isOption() {
		qtyLabel = "Contracts: "
	}
	lines := []string{
		title,
		"",
		"Symbol:   " + m.symInput.View(),
		"Type:     " + m.typeInput.View(),
		qtyLabel + m.qtyInput.View(),
		"Order:    " + strings.Join(typeTabs, " ") + "  ([ / ] to change)",
	}
	if m.orderType >= 1 {
		lines = append(lines, "Limit $:  "+m.limitInput.View())
	}
	if m.orderType == 2 || m.orderType == 3 {
		lines = append(lines, "Stop $:   "+m.stopInput.View())
	}
	lines = append(lines, "")
	hint := "tab: next field  enter: review  esc: cancel"
	if m.confirming {
		hint = "enter again to confirm  esc: back"
	}
	lines = append(lines, theme.Muted.Render(hint))
	if m.err != "" {
		lines = append(lines, theme.StatusErr.Render(m.err))
	}
	return theme.ModalBox.Render(strings.Join(lines, "\n"))
}
