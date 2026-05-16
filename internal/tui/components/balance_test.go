package components

import (
	"testing"

	"github.com/shopspring/decimal"

	"github.com/ks1686/public-terminal/internal/api"
)

func TestBalanceModel_New(t *testing.T) {
	b := NewBalanceModel()
	if b.Width != 0 {
		t.Errorf("default width = %d, want 0", b.Width)
	}
}

func TestBalanceModel_SetWidth(t *testing.T) {
	b := NewBalanceModel()
	b.Width = 120
	if b.Width != 120 {
		t.Error("width not set")
	}
}

func TestBalanceModel_FromPortfolio_Empty(t *testing.T) {
	b := NewBalanceModel()
	p := &api.Portfolio{}
	b.FromPortfolio(p, "ACCT001")
	// Should not panic with nil portfolio fields
}

func TestBalanceModel_FromPortfolio_WithValues(t *testing.T) {
	b := NewBalanceModel()
	total := decimal.NewFromFloat(50000)
	cash := decimal.NewFromFloat(10000)
	bp := decimal.NewFromFloat(20000)
	obp := decimal.NewFromFloat(5000)
	cbp := decimal.NewFromFloat(3000)

	p := &api.Portfolio{
		BuyingPower: api.BuyingPower{
			BuyingPower:         bp,
			CashOnlyBuyingPower: cash,
			OptionsBuyingPower:  obp,
			CryptoBuyingPower:   &cbp,
		},
	}
	_ = total
	b.FromPortfolio(p, "ACCT001")
	// Should not panic
	if b.AccountID != "ACCT001" {
		t.Errorf("AccountID = %q, want ACCT001", b.AccountID)
	}
}
