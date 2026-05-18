package api

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestPortfolio_CashBalance(t *testing.T) {
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
			name: "empty equity",
			portfolio: &Portfolio{
				Equity: []Equity{},
			},
			expected: decimal.Zero,
		},
		{
			name: "no cash equity",
			portfolio: &Portfolio{
				Equity: []Equity{
					{Type: "STOCK", Value: decimal.NewFromFloat(100.0)},
					{Type: "CRYPTO", Value: decimal.NewFromFloat(50.0)},
				},
			},
			expected: decimal.Zero,
		},
		{
			name: "with cash equity",
			portfolio: &Portfolio{
				Equity: []Equity{
					{Type: "STOCK", Value: decimal.NewFromFloat(100.0)},
					{Type: "CASH", Value: decimal.NewFromFloat(500.50)},
				},
			},
			expected: decimal.NewFromFloat(500.50),
		},
		{
			name: "with multiple cash equity (should return first)",
			portfolio: &Portfolio{
				Equity: []Equity{
					{Type: "CASH", Value: decimal.NewFromFloat(300.0)},
					{Type: "CASH", Value: decimal.NewFromFloat(200.0)},
				},
			},
			expected: decimal.NewFromFloat(300.0),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.portfolio.CashBalance()
			if !result.Equal(tt.expected) {
				t.Errorf("CashBalance() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

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
			name: "empty equity",
			portfolio: &Portfolio{
				Equity: []Equity{},
			},
			expected: decimal.Zero,
		},
		{
			name: "only cash",
			portfolio: &Portfolio{
				Equity: []Equity{
					{Type: "CASH", Value: decimal.NewFromFloat(500.0)},
				},
			},
			expected: decimal.Zero,
		},
		{
			name: "mixed equity",
			portfolio: &Portfolio{
				Equity: []Equity{
					{Type: "STOCK", Value: decimal.NewFromFloat(100.0)},
					{Type: "CASH", Value: decimal.NewFromFloat(50.0)},
					{Type: "CRYPTO", Value: decimal.NewFromFloat(200.0)},
				},
			},
			expected: decimal.NewFromFloat(300.0),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.portfolio.EquityExCash()
			if !result.Equal(tt.expected) {
				t.Errorf("EquityExCash() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestPortfolio_TotalEquity(t *testing.T) {
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
			name: "empty equity",
			portfolio: &Portfolio{
				Equity: []Equity{},
			},
			expected: decimal.Zero,
		},
		{
			name: "mixed equity",
			portfolio: &Portfolio{
				Equity: []Equity{
					{Type: "STOCK", Value: decimal.NewFromFloat(100.0)},
					{Type: "CASH", Value: decimal.NewFromFloat(50.0)},
					{Type: "CRYPTO", Value: decimal.NewFromFloat(200.0)},
				},
			},
			expected: decimal.NewFromFloat(350.0),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.portfolio.TotalEquity()
			if !result.Equal(tt.expected) {
				t.Errorf("TotalEquity() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestPortfolio_MarginStatus(t *testing.T) {
	tests := []struct {
		name             string
		portfolio        *Portfolio
		expectedEnabled  bool
		expectedCapacity decimal.Decimal
	}{
		{
			name:             "nil portfolio",
			portfolio:        nil,
			expectedEnabled:  false,
			expectedCapacity: decimal.Zero,
		},
		{
			name: "not enabled (marginBP <= 0 and cash >= 0)",
			portfolio: &Portfolio{
				BuyingPower: BuyingPower{
					BuyingPower:         decimal.NewFromFloat(100.0),
					CashOnlyBuyingPower: decimal.NewFromFloat(100.0),
				},
				Equity: []Equity{
					{Type: "CASH", Value: decimal.NewFromFloat(50.0)},
				},
			},
			expectedEnabled:  false,
			expectedCapacity: decimal.Zero,
		},
		{
			name: "enabled via marginBP > 0, cash > 0 (loan = 0)",
			portfolio: &Portfolio{
				BuyingPower: BuyingPower{
					BuyingPower:         decimal.NewFromFloat(200.0),
					CashOnlyBuyingPower: decimal.NewFromFloat(100.0), // MarginBP = 100
				},
				Equity: []Equity{
					{Type: "CASH", Value: decimal.NewFromFloat(50.0)}, // Cash = 50, ExCash = 0, ExCash+Cash = 50. loan = 50 - 100 = -50 -> 0.
				},
			},
			// capacity = loan + MarginBP = 0 + 100 = 100
			expectedEnabled:  true,
			expectedCapacity: decimal.NewFromFloat(100.0),
		},
		{
			name: "enabled via marginBP > 0, large exCash (loan > 0)",
			portfolio: &Portfolio{
				BuyingPower: BuyingPower{
					BuyingPower:         decimal.NewFromFloat(200.0),
					CashOnlyBuyingPower: decimal.NewFromFloat(100.0), // MarginBP = 100
				},
				Equity: []Equity{
					{Type: "STOCK", Value: decimal.NewFromFloat(200.0)},
					{Type: "CASH", Value: decimal.NewFromFloat(50.0)}, // Cash = 50, ExCash = 200. Total = 250. loan = 250 - 100 = 150.
				},
			},
			// capacity = loan + MarginBP = 150 + 100 = 250
			expectedEnabled:  true,
			expectedCapacity: decimal.NewFromFloat(250.0),
		},
		{
			name: "enabled via negative cash",
			portfolio: &Portfolio{
				BuyingPower: BuyingPower{
					BuyingPower:         decimal.NewFromFloat(100.0),
					CashOnlyBuyingPower: decimal.NewFromFloat(100.0), // MarginBP = 0
				},
				Equity: []Equity{
					{Type: "CASH", Value: decimal.NewFromFloat(-50.0)}, // loan = 50
				},
			},
			// capacity = loan + MarginBP = 50 + 0 = 50
			expectedEnabled:  true,
			expectedCapacity: decimal.NewFromFloat(50.0),
		},
		{
			name: "marginBP is negative (should be zeroed)",
			portfolio: &Portfolio{
				BuyingPower: BuyingPower{
					BuyingPower:         decimal.NewFromFloat(50.0),
					CashOnlyBuyingPower: decimal.NewFromFloat(100.0), // MarginBP = -50 -> 0
				},
				Equity: []Equity{
					{Type: "CASH", Value: decimal.NewFromFloat(-50.0)},
				},
			},
			// MarginBP is 0. enabled via cash.IsNegative(). loan = 50. capacity = 50 + 0 = 50.
			expectedEnabled:  true,
			expectedCapacity: decimal.NewFromFloat(50.0),
		},
		{
			name: "loan is negative (should be zeroed)",
			portfolio: &Portfolio{
				BuyingPower: BuyingPower{
					BuyingPower:         decimal.NewFromFloat(150.0),
					CashOnlyBuyingPower: decimal.NewFromFloat(50.0), // MarginBP = 100
				},
				Equity: []Equity{
					{Type: "STOCK", Value: decimal.NewFromFloat(20.0)},
					{Type: "CASH", Value: decimal.NewFromFloat(30.0)},
				}, // ExCash=20, Cash=30. ExCash+Cash = 50. loan = 50 - 100 = -50 -> 0. capacity = 0 + 100 = 100
			},
			expectedEnabled:  true,
			expectedCapacity: decimal.NewFromFloat(100.0),
		},
		{
			name: "capacity is negative (should be zeroed)",
			portfolio: &Portfolio{
				BuyingPower: BuyingPower{
					BuyingPower:         decimal.NewFromFloat(50.0),
					CashOnlyBuyingPower: decimal.NewFromFloat(100.0), // MarginBP = -50 -> 0
				},
				Equity: []Equity{
					{Type: "CASH", Value: decimal.NewFromFloat(0.0)},
				},
				// enabled is false... Wait, if marginBP=0 and cash=0, enabled=false -> returns early.
				// We need enabled=true, meaning cash is negative or marginBP is positive.
				// If cash < 0, enabled=true. marginBP=0. loan = cash.Neg() = >0. capacity = loan + marginBP > 0.
				// If marginBP > 0, enabled=true. loan >= 0. capacity = loan + marginBP >= marginBP > 0.
				// Actually, capacity can never be negative because loan >= 0 and marginBP >= 0!
				// Line 79-81 `if capacity.IsNegative() { capacity = decimal.Zero }` is defensively written and practically unreachable.
			},
			expectedEnabled:  false,
			expectedCapacity: decimal.Zero,
		},
		{
			name: "missing CashOnlyBuyingPower fallback to BuyingPower",
			portfolio: &Portfolio{
				BuyingPower: BuyingPower{
					BuyingPower:         decimal.NewFromFloat(100.0),
					CashOnlyBuyingPower: decimal.Zero, // Fallback makes it 100, so MarginBP = 0
				},
				Equity: []Equity{
					{Type: "CASH", Value: decimal.NewFromFloat(50.0)},
				},
			},
			expectedEnabled:  false,
			expectedCapacity: decimal.Zero,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enabled, capacity := tt.portfolio.MarginStatus()
			if enabled != tt.expectedEnabled {
				t.Errorf("MarginStatus() enabled = %v, expected %v", enabled, tt.expectedEnabled)
			}
			if !capacity.Equal(tt.expectedCapacity) {
				t.Errorf("MarginStatus() capacity = %v, expected %v", capacity, tt.expectedCapacity)
			}
		})
	}
}
