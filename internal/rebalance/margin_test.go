package rebalance

import (
	"testing"

	"github.com/shopspring/decimal"

	"github.com/ks1686/public-terminal/internal/api"
)

func dec(s string) decimal.Decimal {
	d, err := decimal.NewFromString(s)
	if err != nil {
		panic(err)
	}
	return d
}

func TestEstimateMarginState_ExistingLoan(t *testing.T) {
	snap := &PortfolioSnapshot{
		TotalEquity: dec("2000"),
		CashBalance: dec("-500"),
		BuyingPower: dec("1500"),
		CashOnlyBP:  decimal.Zero,
	}
	got := EstimateMarginState(snap, dec("0.5"))
	// capacity = loan 500 + headroom 1500 = 2000; allowed = 1000
	if !got.AllowedMarginLoan.Equal(dec("1000")) {
		t.Fatalf("AllowedMarginLoan = %s, want 1000", got.AllowedMarginLoan)
	}
	if !got.EffectiveBP.Equal(dec("500")) {
		t.Fatalf("EffectiveBP = %s, want 500 (cashOnly + max(0, allowed-loan))", got.EffectiveBP)
	}
	if !got.InvestmentBase.Equal(dec("2500")) {
		t.Fatalf("InvestmentBase = %s, want 2500", got.InvestmentBase)
	}
}

func TestEstimateMarginState_ClampsUsage(t *testing.T) {
	snap := &PortfolioSnapshot{
		TotalEquity: dec("1000"),
		CashBalance: dec("100"),
		BuyingPower: dec("200"),
		CashOnlyBP:  dec("100"),
	}
	got := EstimateMarginState(snap, dec("4"))
	if got.AllowedMarginLoan.GreaterThan(dec("100")) {
		t.Fatalf("usage > 1 should clamp; AllowedMarginLoan = %s", got.AllowedMarginLoan)
	}
}

func TestSnapshotFromPortfolio_SumsDuplicatesAndSkipsNilValue(t *testing.T) {
	val := dec("10")
	p := &api.Portfolio{
		Equity: []api.Equity{
			{Type: "STOCK", Value: dec("100")},
			{Type: "OPTION", Value: dec("25")},
			{Type: "CASH", Value: dec("5")},
		},
		Positions: []api.Position{
			{Instrument: api.Instrument{Type: "EQUITY", Symbol: "AAPL"}, Quantity: dec("1"), CurrentValue: &val},
			{Instrument: api.Instrument{Type: "EQUITY", Symbol: "AAPL"}, Quantity: dec("2"), CurrentValue: &val},
			{Instrument: api.Instrument{Type: "EQUITY", Symbol: "MSFT"}, Quantity: dec("3")},
		},
	}
	snap := snapshotFromPortfolio(p)
	if !snap.EquityPos["AAPL"].Equal(dec("20")) {
		t.Fatalf("AAPL value = %s, want 20", snap.EquityPos["AAPL"])
	}
	if !snap.EquityQty["AAPL"].Equal(dec("3")) {
		t.Fatalf("AAPL qty = %s, want 3", snap.EquityQty["AAPL"])
	}
	if _, ok := snap.EquityPos["MSFT"]; ok {
		t.Fatal("MSFT with nil CurrentValue must not be treated as $0")
	}
	if !snap.UnknownEquityValue["MSFT"] {
		t.Fatal("MSFT should be marked unknown value")
	}
	if !snap.OptionsValue.Equal(dec("25")) {
		t.Fatalf("OptionsValue = %s, want 25", snap.OptionsValue)
	}
}

func TestAllocationsValid(t *testing.T) {
	if err := validateAllocations(map[string]float64{"stocks": 0.65, "btc": 0.12, "eth": 0.04, "sol": 0.04, "gold": 0.10, "cash": 0.05}); err != nil {
		t.Fatalf("valid alloc: %v", err)
	}
	if err := validateAllocations(map[string]float64{"stocks": 0.9, "cash": 0.2}); err == nil {
		t.Fatal("expected error when allocations do not sum to 1")
	}
}
