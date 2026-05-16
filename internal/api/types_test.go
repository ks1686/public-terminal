package api

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestActiveOrderStatuses(t *testing.T) {
	active := []string{"NEW", "PARTIALLY_FILLED", "PENDING_REPLACE", "PENDING_CANCEL"}
	for _, s := range active {
		if !ActiveOrderStatuses[s] {
			t.Errorf("%s should be active", s)
		}
	}
	if ActiveOrderStatuses["FILLED"] {
		t.Error("FILLED should not be active")
	}
	if ActiveOrderStatuses["CANCELLED"] {
		t.Error("CANCELLED should not be active")
	}
}

func TestPositionFromAPI(t *testing.T) {
	val := decimal.NewFromFloat(1000.0)
	last := decimal.NewFromFloat(50.0)
	pos := Position{
		Instrument:   Instrument{Type: "EQUITY", Symbol: "AAPL"},
		Quantity:     decimal.NewFromFloat(10),
		CurrentValue: &val,
		LastPrice:    &LastPrice{LastPrice: &last},
	}
	if pos.Instrument.Type != "EQUITY" {
		t.Errorf("type = %s, want EQUITY", pos.Instrument.Type)
	}
	if !pos.Quantity.Equal(decimal.NewFromFloat(10)) {
		t.Errorf("quantity mismatch")
	}
}

func TestBarsResponse_Flatten(t *testing.T) {
	resp := BarsResponse{
		PreMarket: marketSessionBars{
			Bars: []Bar{
				{Open: decimal.NewFromFloat(100), Close: decimal.NewFromFloat(101)},
			},
		},
		RegularMarket: marketSessionBars{
			Bars: []Bar{
				{Open: decimal.NewFromFloat(101), Close: decimal.NewFromFloat(102)},
			},
		},
	}
	flat := resp.Flatten()
	if len(flat) != 2 {
		t.Errorf("expected 2 bars, got %d", len(flat))
	}
}
