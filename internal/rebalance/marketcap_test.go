package rebalance

import (
	"fmt"
	"testing"
)

func TestValidateMarketCapCoverage(t *testing.T) {
	tests := []struct {
		name       string
		tickers    []string
		marketCaps map[string]float64
		topN       int
		want       bool
	}{
		{
			name:       "Happy Path - Full Coverage",
			tickers:    []string{"A", "B", "C"},
			marketCaps: map[string]float64{"A": 100, "B": 200, "C": 300},
			topN:       3,
			want:       true,
		},
		{
			name: "Sufficient Coverage - Meets Min and TopN",
			// Let's create 100 tickers to test MarketCapMinCoveragePct (0.95)
			tickers:    generateTickers(100),
			marketCaps: generateMarketCaps(100, 96), // 96 available > 95 min
			topN:       50,                          // 96 > 50 desired
			want:       true,
		},
		{
			name:       "Insufficient Coverage - Below Min",
			tickers:    generateTickers(100),
			marketCaps: generateMarketCaps(100, 94), // 94 available < 95 min
			topN:       50,
			want:       false,
		},
		{
			name:       "Insufficient Coverage - Below Desired",
			tickers:    generateTickers(100),
			marketCaps: generateMarketCaps(100, 96), // 96 available > 95 min
			topN:       98,                          // 96 available < 98 desired
			want:       false,
		},
		{
			name:       "Desired Exceeds Source Count",
			tickers:    []string{"A", "B", "C"},
			marketCaps: map[string]float64{"A": 100, "B": 200, "C": 300},
			topN:       5,    // Will be capped to 3 (source count)
			want:       true, // 3 available >= 3 desired and >= 2 min required (3 * 0.95 = 2.85 -> 2)
		},
		{
			name:       "Empty tickers",
			tickers:    []string{},
			marketCaps: map[string]float64{},
			topN:       0,
			want:       false, // 0 < 1 min required
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ValidateMarketCapCoverage(tt.tickers, tt.marketCaps, tt.topN); got != tt.want {
				t.Errorf("ValidateMarketCapCoverage() = %v, want %v", got, tt.want)
			}
		})
	}
}

func generateTickers(n int) []string {
	tickers := make([]string, n)
	for i := 0; i < n; i++ {
		tickers[i] = fmt.Sprintf("T%d", i)
	}
	return tickers
}

func generateMarketCaps(n, available int) map[string]float64 {
	caps := make(map[string]float64)
	for i := 0; i < available && i < n; i++ {
		caps[fmt.Sprintf("T%d", i)] = 100.0
	}
	return caps
}
