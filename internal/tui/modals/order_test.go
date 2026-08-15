package modals

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/shopspring/decimal"
)

func TestNewOrderModal_OptionUsesContracts(t *testing.T) {
	m := NewOrderModal(nil, "SELL", "AAPL  260516C00150000", "OPTION").WithQuantity("2")
	view := m.View()
	if !strings.Contains(view, "Contracts:") {
		t.Fatalf("option modal view missing Contracts label:\n%s", view)
	}
	if !strings.Contains(view, "2") {
		t.Fatalf("option modal view missing prefilled qty:\n%s", view)
	}
	if m.qtyInput.Placeholder != "Contracts (e.g. 1)" {
		t.Fatalf("placeholder = %q", m.qtyInput.Placeholder)
	}
}

func TestOrderModal_OptionSubmitUsesQuantity(t *testing.T) {
	m := NewOrderModal(nil, "SELL", "AAPL  260516C00150000", "OPTION").WithQuantity("2")
	req, err := m.buildRequest()
	if err != "" {
		t.Fatalf("buildRequest error: %s", err)
	}
	if req.Quantity == nil {
		t.Fatal("expected Quantity for OPTION")
	}
	if req.Amount != nil {
		t.Fatal("did not expect Amount for OPTION")
	}
	if !req.Quantity.Equal(decimal.RequireFromString("2")) {
		t.Fatalf("quantity = %s, want 2", req.Quantity)
	}
	if req.Instrument.Type != "OPTION" {
		t.Fatalf("type = %q, want OPTION", req.Instrument.Type)
	}
}

func TestOrderModal_EquitySubmitUsesAmount(t *testing.T) {
	m := NewOrderModal(nil, "BUY", "AAPL", "EQUITY")
	m.qtyInput.SetValue("100")
	req, err := m.buildRequest()
	if err != "" {
		t.Fatalf("buildRequest error: %s", err)
	}
	if req.Amount == nil {
		t.Fatal("expected Amount for EQUITY")
	}
	if req.Quantity != nil {
		t.Fatal("did not expect Quantity for EQUITY")
	}
}

func TestOrderModal_EscapeCancels(t *testing.T) {
	m := NewOrderModal(nil, "SELL", "AAPL", "EQUITY")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("esc should return a command")
	}
}
