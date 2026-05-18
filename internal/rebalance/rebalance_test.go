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

func TestComputeDelta_Crypto(t *testing.T) {
	// target is 10, current is 8. Delta is 2.
	// For EQUITY, min order is $5, so delta=2 is ignored.
	// For CRYPTO, min order is $1, so delta=2 triggers a BUY.
	targetVal := decimal.NewFromFloat(10)
	currentVal := decimal.NewFromFloat(8)
	threshold := decimal.NewFromFloat(1) // small threshold

	specCrypto := ComputeDelta("BTC", "CRYPTO", targetVal, currentVal, threshold)
	if specCrypto == nil {
		t.Fatal("expected non-nil spec for CRYPTO delta > $1")
	}
	if specCrypto.Side != "BUY" {
		t.Errorf("Side = %q, want BUY", specCrypto.Side)
	}
	if specCrypto.DollarAmount.String() != "2" {
		t.Errorf("DollarAmount = %s, want 2", specCrypto.DollarAmount)
	}

	specEquity := ComputeDelta("AAPL", "EQUITY", targetVal, currentVal, threshold)
	if specEquity != nil {
		t.Fatalf("expected nil spec for EQUITY delta < $5, got %v", specEquity)
	}
}

func TestComputeDelta_ThresholdPercentage(t *testing.T) {
	// Target is 10000. RebalanceThresholdPct is 0.005. Target threshold is 10000 * 0.005 = 50.
	// Current is 9960. Delta is 40.
	// 40 is greater than $5 min order and threshold parameter (0), but less than 50 (percentage threshold).
	targetVal := decimal.NewFromFloat(10000)
	currentVal := decimal.NewFromFloat(9960)
	threshold := decimal.NewFromFloat(0)

	spec := ComputeDelta("SPY", "EQUITY", targetVal, currentVal, threshold)
	if spec != nil {
		t.Fatalf("expected nil spec since delta (40) < percentage threshold (50), got %v", spec)
	}

	// Current is 9940. Delta is 60.
	// 60 is > 50, so it should trigger a BUY.
	currentVal2 := decimal.NewFromFloat(9940)
	spec2 := ComputeDelta("SPY", "EQUITY", targetVal, currentVal2, threshold)
	if spec2 == nil {
		t.Fatal("expected non-nil spec since delta (60) > percentage threshold (50)")
	}
	if spec2.Side != "BUY" {
		t.Errorf("Side = %q, want BUY", spec2.Side)
	}
	if spec2.DollarAmount.String() != "60" {
		t.Errorf("DollarAmount = %s, want 60", spec2.DollarAmount)
	}
}

func TestComputeDelta_SellWithThreshold(t *testing.T) {
	// target is 10000, current is 10060. Delta is -60.
	// Percentage threshold is 10000 * 0.005 = 50.
	// The delta -60 is < -50 (which is the negative driftThreshold).
	targetVal := decimal.NewFromFloat(10000)
	currentVal := decimal.NewFromFloat(10060)
	threshold := decimal.NewFromFloat(0)

	spec := ComputeDelta("MSFT", "EQUITY", targetVal, currentVal, threshold)

	if spec == nil {
		t.Fatal("expected non-nil spec since delta (-60) triggers a SELL")
	}
	if spec.Side != "SELL" {
		t.Errorf("Side = %q, want SELL", spec.Side)
	}
	if spec.DollarAmount.String() != "60" {
		t.Errorf("DollarAmount = %s, want 60", spec.DollarAmount)
	}

	// Current is 10040. Delta is -40.
	// -40 is not < -50, so it should be ignored.
	currentVal2 := decimal.NewFromFloat(10040)
	spec2 := ComputeDelta("MSFT", "EQUITY", targetVal, currentVal2, threshold)
	if spec2 != nil {
		t.Fatalf("expected nil spec since delta (-40) does not trigger a SELL against threshold 50")
	}
}
