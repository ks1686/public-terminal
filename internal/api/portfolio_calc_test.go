package api

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestPortfolio_EquityExCash(t *testing.T) {
	tests := []struct {
		name      string
		portfolio *Portfolio
		expected  decimal.Decimal
	}{
		{
			name:      "nil portfolio",
			portfolio: nil,
			expected:  decimal.Zero,
		},
		{
			name:      "empty equity",
			portfolio: &Portfolio{Equity: []Equity{}},
			expected:  decimal.Zero,
		},
		{
			name: "only cash",
			portfolio: &Portfolio{
				Equity: []Equity{
					{Type: "CASH", Value: decimal.NewFromFloat(100.50)},
				},
			},
			expected: decimal.Zero,
		},
		{
			name: "cash and other types",
			portfolio: &Portfolio{
				Equity: []Equity{
					{Type: "CASH", Value: decimal.NewFromFloat(100.50)},
					{Type: "STOCK", Value: decimal.NewFromFloat(200.25)},
					{Type: "CRYPTO", Value: decimal.NewFromFloat(50.25)},
				},
			},
			expected: decimal.NewFromFloat(250.50), // 200.25 + 50.25
		},
		{
			name: "no cash, multiple other types",
			portfolio: &Portfolio{
				Equity: []Equity{
					{Type: "STOCK", Value: decimal.NewFromFloat(300.00)},
					{Type: "OPTION", Value: decimal.NewFromFloat(150.00)},
				},
			},
			expected: decimal.NewFromFloat(450.00), // 300.00 + 150.00
		},
		{
			name: "negative non-cash equity",
			portfolio: &Portfolio{
				Equity: []Equity{
					{Type: "STOCK", Value: decimal.NewFromFloat(300.00)},
					{Type: "OPTION", Value: decimal.NewFromFloat(-50.00)},
				},
			},
			expected: decimal.NewFromFloat(250.00), // 300.00 + -50.00
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.portfolio.EquityExCash()
			if !result.Equal(tt.expected) {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}
