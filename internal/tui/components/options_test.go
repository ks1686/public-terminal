package components

import (
	"testing"

	"github.com/shopspring/decimal"

	"github.com/ks1686/public-terminal/internal/api"
)

func TestOptionsSelectedContract(t *testing.T) {
	qty := decimal.RequireFromString("2")
	val := decimal.RequireFromString("350")
	m := NewOptionsModel()
	m.FromPortfolio(&api.Portfolio{
		Positions: []api.Position{
			{
				Instrument:   api.Instrument{Type: "OPTION", Symbol: "AAPL  260516C00150000"},
				Quantity:     qty,
				CurrentValue: &val,
			},
		},
	})
	occ, gotQty := m.SelectedContract()
	if occ != "AAPL  260516C00150000" {
		t.Fatalf("OCC = %q", occ)
	}
	if gotQty != "2" {
		t.Fatalf("qty = %q, want 2", gotQty)
	}
}

func TestOptionsSelectedContract_Empty(t *testing.T) {
	m := NewOptionsModel()
	m.FromPortfolio(&api.Portfolio{})
	occ, qty := m.SelectedContract()
	if occ != "" || qty != "" {
		t.Fatalf("expected empty selection, got %q %q", occ, qty)
	}
}
