package components

import (
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/ks1686/public-terminal/internal/api"
)

func TestHoldingsSelectedSymbol_BareTicker(t *testing.T) {
	pct := decimal.NewFromFloat(1.5)
	val := decimal.NewFromFloat(100)
	p := &api.Portfolio{Positions: []api.Position{{
		Instrument:        api.Instrument{Type: "EQUITY", Symbol: "AAPL"},
		Quantity:          decimal.NewFromInt(1),
		CurrentValue:      &val,
		PositionDailyGain: &api.Gain{GainPercentage: &pct},
	}}}
	m := NewHoldingsModel()
	m.FromPortfolio(p)
	got := m.SelectedSymbol()
	if got != "AAPL" {
		t.Fatalf("SelectedSymbol() = %q, want AAPL", got)
	}
	if strings.Contains(got, "\x1b") {
		t.Fatalf("SelectedSymbol contains ANSI: %q", got)
	}
}

func TestCryptoSelectedSymbol_BareTicker(t *testing.T) {
	pct := decimal.NewFromFloat(-2)
	val := decimal.NewFromFloat(50)
	p := &api.Portfolio{Positions: []api.Position{{
		Instrument:        api.Instrument{Type: "CRYPTO", Symbol: "BTC"},
		Quantity:          decimal.NewFromFloat(0.1),
		CurrentValue:      &val,
		PositionDailyGain: &api.Gain{GainPercentage: &pct},
	}}}
	m := NewCryptoModel()
	m.FromPortfolio(p)
	got := m.SelectedSymbol()
	if got != "BTC" {
		t.Fatalf("SelectedSymbol() = %q, want BTC", got)
	}
}
