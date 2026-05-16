package api

import "github.com/shopspring/decimal"

// CashBalance returns the equity-row value for type == "CASH".
// Returns zero if no cash row is present.
func (p *Portfolio) CashBalance() decimal.Decimal {
	if p == nil {
		return decimal.Zero
	}
	for _, e := range p.Equity {
		if e.Type == "CASH" {
			return e.Value
		}
	}
	return decimal.Zero
}

// EquityExCash sums equity values for non-CASH rows.
func (p *Portfolio) EquityExCash() decimal.Decimal {
	if p == nil {
		return decimal.Zero
	}
	total := decimal.Zero
	for _, e := range p.Equity {
		if e.Type != "CASH" {
			total = total.Add(e.Value)
		}
	}
	return total
}

// TotalEquity sums all equity rows.
func (p *Portfolio) TotalEquity() decimal.Decimal {
	if p == nil {
		return decimal.Zero
	}
	total := decimal.Zero
	for _, e := range p.Equity {
		total = total.Add(e.Value)
	}
	return total
}

// MarginStatus mirrors Python's app._get_margin_status. Returns whether margin
// is enabled on this account and the current margin capacity (loan + headroom).
func (p *Portfolio) MarginStatus() (enabled bool, capacity decimal.Decimal) {
	if p == nil {
		return false, decimal.Zero
	}
	bp := p.BuyingPower.BuyingPower
	cashOnlyBP := p.BuyingPower.CashOnlyBuyingPower
	if cashOnlyBP.IsZero() && bp.IsPositive() {
		// Older payloads may omit cashOnlyBuyingPower; fall back to bp so the
		// margin delta is zero rather than spuriously positive.
		cashOnlyBP = bp
	}
	cash := p.CashBalance()

	marginBP := bp.Sub(cashOnlyBP)
	if marginBP.IsNegative() {
		marginBP = decimal.Zero
	}
	enabled = marginBP.IsPositive() || cash.IsNegative()
	if !enabled {
		return false, decimal.Zero
	}
	exCash := p.EquityExCash()
	var loan decimal.Decimal
	if cash.IsNegative() {
		loan = cash.Neg()
	} else {
		loan = exCash.Add(cash).Sub(marginBP)
		if loan.IsNegative() {
			loan = decimal.Zero
		}
	}
	capacity = loan.Add(marginBP)
	if capacity.IsNegative() {
		capacity = decimal.Zero
	}
	return enabled, capacity
}
