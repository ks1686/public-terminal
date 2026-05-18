package api

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestPortfolio_TotalEquity(t *testing.T) {
	tests := []struct {
		name      string
		portfolio *Portfolio
		want      decimal.Decimal
	}{
		{
			name:      "nil portfolio",
			portfolio: nil,
			want:      decimal.Zero,
		},
		{
			name:      "empty portfolio",
			portfolio: &Portfolio{},
			want:      decimal.Zero,
		},
		{
			name: "single equity row",
			portfolio: &Portfolio{
				Equity: []Equity{
					{Type: "STOCK", Value: decimal.NewFromFloat(100.50)},
				},
			},
			want: decimal.NewFromFloat(100.50),
		},
		{
			name: "multiple equity rows",
			portfolio: &Portfolio{
				Equity: []Equity{
					{Type: "STOCK", Value: decimal.NewFromFloat(150.25)},
					{Type: "CASH", Value: decimal.NewFromFloat(50.75)},
					{Type: "CRYPTO", Value: decimal.NewFromFloat(200.00)},
				},
			},
			want: decimal.NewFromFloat(401.00),
		},
		{
			name: "negative equity values",
			portfolio: &Portfolio{
				Equity: []Equity{
					{Type: "STOCK", Value: decimal.NewFromFloat(100.00)},
					{Type: "CASH", Value: decimal.NewFromFloat(-50.00)},
				},
			},
			want: decimal.NewFromFloat(50.00),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.portfolio.TotalEquity(); !got.Equal(tt.want) {
				t.Errorf("Portfolio.TotalEquity() = %v, want %v", got, tt.want)
			}
		})
	}
}
