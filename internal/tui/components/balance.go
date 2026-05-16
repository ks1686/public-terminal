package components

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/shopspring/decimal"

	"github.com/ks1686/public-terminal/internal/api"
	"github.com/ks1686/public-terminal/internal/tui/theme"
)

// BalanceModel renders the top status bar: equity, buying power, cash/margin, daily gain.
type BalanceModel struct {
	TotalEquity    decimal.Decimal
	BuyingPower    decimal.Decimal
	OptionsBP      decimal.Decimal
	CryptoBP       decimal.Decimal
	Cash           decimal.Decimal
	MarginEnabled  bool
	MarginCapacity decimal.Decimal
	DailyGainAmt   decimal.Decimal
	DailyGainPct   float64
	AccountID      string
	Width          int
}

func NewBalanceModel() BalanceModel { return BalanceModel{} }

func (m *BalanceModel) FromPortfolio(p *api.Portfolio, accountID string) {
	m.AccountID = accountID
	m.BuyingPower = p.BuyingPower.BuyingPower
	m.OptionsBP = p.BuyingPower.OptionsBuyingPower
	if p.BuyingPower.CryptoBuyingPower != nil {
		m.CryptoBP = *p.BuyingPower.CryptoBuyingPower
	}
	m.TotalEquity = p.TotalEquity()
	m.Cash = p.CashBalance()
	m.MarginEnabled, m.MarginCapacity = p.MarginStatus()

	var dailyGainAmt decimal.Decimal
	for _, pos := range p.Positions {
		if pos.PositionDailyGain != nil && pos.CurrentValue != nil && pos.PositionDailyGain.GainPercentage != nil {
			pct := *pos.PositionDailyGain.GainPercentage
			// daily gain amount ≈ currentValue * pct / (100 + pct)
			denominator := decimal.NewFromInt(100).Add(pct)
			if denominator.IsPositive() {
				dailyGainAmt = dailyGainAmt.Add(pos.CurrentValue.Mul(pct).Div(denominator))
			}
		}
	}
	m.DailyGainAmt = dailyGainAmt
	if m.TotalEquity.IsPositive() {
		f, _ := dailyGainAmt.Div(m.TotalEquity).Mul(decimal.NewFromInt(100)).Float64()
		m.DailyGainPct = f
	}
}

func (m BalanceModel) View() string {
	// Line 1: title + equity + daily gain
	eq := lipgloss.NewStyle().Bold(true).Foreground(theme.ColorCyan).Render(formatMoney(m.TotalEquity))
	gainStr := theme.FormatGain(m.DailyGainPct)
	gainAmt := formatMoneyStyled(m.DailyGainAmt)

	line1 := theme.Title.Render("PORTFOLIO") + "  " + eq + "  " + gainStr + " (" + gainAmt + ")"

	// Line 2: buying power stats
	bp := fmt.Sprintf("BP %s", formatMoney(m.BuyingPower))
	optBP := fmt.Sprintf("OPT BP %s", formatMoney(m.OptionsBP))
	cBP := fmt.Sprintf("CRYPTO %s", formatMoney(m.CryptoBP))

	cashLabel := "CASH"
	cashValue := formatMoney(m.Cash)
	if m.Cash.IsNegative() {
		cashLabel = "MARGIN"
		cashValue = theme.Warning.Render(formatMoney(m.Cash.Neg()))
	}
	cash := fmt.Sprintf("%s %s", cashLabel, cashValue)

	sep := theme.Muted.Render(" │ ")
	line2 := bp + sep + optBP + sep + cBP + sep + cash

	return line1 + "\n" + line2
}

func formatMoney(d decimal.Decimal) string {
	f, _ := d.Float64()
	if f < 0 {
		return fmt.Sprintf("-$%.2f", -f)
	}
	return fmt.Sprintf("$%.2f", f)
}

func formatMoneyStyled(d decimal.Decimal) string {
	f, _ := d.Float64()
	if f >= 0 {
		return theme.Positive.Render(fmt.Sprintf("+$%.2f", f))
	}
	return theme.Negative.Render(fmt.Sprintf("-$%.2f", -f))
}
