package rebalance

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestSupportedIndexes(t *testing.T) {
	if _, ok := SupportedIndexes["SP500"]; !ok {
		t.Error("SP500 missing from SupportedIndexes")
	}
	if _, ok := SupportedIndexes["NASDAQ100"]; !ok {
		t.Error("NASDAQ100 missing from SupportedIndexes")
	}
	if _, ok := SupportedIndexes["DJIA"]; !ok {
		t.Error("DJIA missing from SupportedIndexes")
	}
}

func TestComputeDelta_Buy(t *testing.T) {
	targetVal := decimal.NewFromFloat(1000)
	currentVal := decimal.NewFromFloat(800)
	threshold := decimal.NewFromFloat(50)

	spec := ComputeDelta("AAPL", "EQUITY", targetVal, currentVal, threshold)

	if spec == nil {
		t.Fatal("expected non-nil spec for buy delta")
	}
	if spec.Side != "BUY" {
		t.Errorf("Side = %q, want BUY", spec.Side)
	}
	if !spec.DollarAmount.IsPositive() {
		t.Errorf("DollarAmount = %s, expected positive", spec.DollarAmount)
	}
	if spec.Symbol != "AAPL" {
		t.Errorf("Symbol = %q, want AAPL", spec.Symbol)
	}
}

func TestComputeDelta_Sell(t *testing.T) {
	targetVal := decimal.NewFromFloat(500)
	currentVal := decimal.NewFromFloat(1000)
	threshold := decimal.NewFromFloat(50)

	spec := ComputeDelta("TSLA", "EQUITY", targetVal, currentVal, threshold)

	if spec == nil {
		t.Fatal("expected non-nil spec for sell delta")
	}
	if spec.Side != "SELL" {
		t.Errorf("Side = %q, want SELL", spec.Side)
	}
}

func TestComputeDelta_BelowThreshold(t *testing.T) {
	targetVal := decimal.NewFromFloat(1000)
	currentVal := decimal.NewFromFloat(980)
	threshold := decimal.NewFromFloat(50)

	spec := ComputeDelta("AAPL", "EQUITY", targetVal, currentVal, threshold)

	if spec != nil {
		t.Error("expected nil spec when delta is below threshold")
	}
}

func TestComputeStockWeights(t *testing.T) {
	tickers := []string{"AAPL", "MSFT", "GOOGL"}
	caps := map[string]float64{
		"AAPL":  3000,
		"MSFT":  2500,
		"GOOGL": 1500,
	}
	weights, err := ComputeStockWeights(tickers, caps)
	if err != nil {
		t.Fatalf("ComputeStockWeights: %v", err)
	}
	if len(weights) != 3 {
		t.Errorf("expected 3 weights, got %d", len(weights))
	}
	// Check that weights sum to ~1.0
	var sum decimal.Decimal
	for _, w := range weights {
		sum = sum.Add(w)
	}
	one := decimal.NewFromFloat(1.0)
	diff := sum.Sub(one).Abs()
	if diff.GreaterThan(decimal.NewFromFloat(0.01)) {
		t.Errorf("weights sum to %s, want ~1.0", sum)
	}
	// Largest market cap should have largest weight
	if weights["AAPL"].LessThan(weights["MSFT"]) {
		t.Error("AAPL should have higher weight than MSFT")
	}
}

func TestTopNByMarketCap(t *testing.T) {
	tickers := []string{"A", "B", "C", "D", "E"}
	caps := map[string]float64{"A": 100, "B": 300, "C": 200, "D": 500, "E": 400}

	result := TopNByMarketCap(tickers, caps, 3)
	if len(result) != 3 {
		t.Fatalf("expected 3, got %d", len(result))
	}
	// Top 3 by market cap: D(500), E(400), B(300)
	if result[0] != "D" || result[1] != "E" || result[2] != "B" {
		t.Errorf("unexpected top 3: %v", result)
	}
}
