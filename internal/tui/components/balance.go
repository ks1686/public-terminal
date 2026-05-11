package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/shopspring/decimal"

	"github.com/ks1686/public-terminal/internal/api"
	"github.com/ks1686/public-terminal/internal/tui/theme"
)

// BalanceModel renders the top status bar: equity, buying power, daily gain.
type BalanceModel struct {
	TotalEquity  decimal.Decimal
	BuyingPower  decimal.Decimal
	OptionsBP    decimal.Decimal
	CryptoBP     decimal.Decimal
	DailyGainAmt decimal.Decimal
	DailyGainPct float64
	AccountID    string
	Width        int
}

func NewBalanceModel() BalanceModel { return BalanceModel{} }

func (m *BalanceModel) FromPortfolio(p *api.Portfolio, accountID string) {
	m.AccountID = accountID
	m.BuyingPower = p.BuyingPower.BuyingPower
	m.OptionsBP = p.BuyingPower.OptionsBuyingPower
	if p.BuyingPower.CryptoBuyingPower != nil {
		m.CryptoBP = *p.BuyingPower.CryptoBuyingPower
	}

	var total, dailyGainAmt decimal.Decimal
	for _, e := range p.Equity {
		total = total.Add(e.Value)
	}
	m.TotalEquity = total

	for _, pos := range p.Positions {
		if pos.PositionDailyGain != nil && pos.CurrentValue != nil {
			if pos.PositionDailyGain.GainPercentage != nil {
				pct := *pos.PositionDailyGain.GainPercentage
				// daily gain amount ≈ currentValue * pct / (100 + pct)
				denominator := decimal.NewFromInt(100).Add(pct)
				if denominator.IsPositive() {
					dailyGainAmt = dailyGainAmt.Add(pos.CurrentValue.Mul(pct).Div(denominator))
				}
			}
		}
	}
	m.DailyGainAmt = dailyGainAmt
	if total.IsPositive() {
		f, _ := dailyGainAmt.Div(total).Mul(decimal.NewFromInt(100)).Float64()
		m.DailyGainPct = f
	}
}

func (m BalanceModel) View() string {
	sep := theme.Muted.Render(" │ ")

	eq := fmt.Sprintf("EQUITY %s", theme.Title.Render(formatMoney(m.TotalEquity)))
	bp := fmt.Sprintf("BP %s", formatMoney(m.BuyingPower))
	optBP := fmt.Sprintf("OPT BP %s", formatMoney(m.OptionsBP))
	cBP := fmt.Sprintf("CRYPTO BP %s", formatMoney(m.CryptoBP))

	gainStr := theme.FormatGain(m.DailyGainPct)
	gainAmt := fmt.Sprintf("(%s)", formatMoneyStyled(m.DailyGainAmt))
	daily := fmt.Sprintf("DAY %s %s", gainStr, gainAmt)

	acct := theme.Muted.Render(m.AccountID)

	parts := []string{eq, bp, optBP, cBP, daily}
	left := strings.Join(parts, sep)

	plain := lipgloss.Width(left)
	acctW := lipgloss.Width(acct)
	padding := m.Width - plain - acctW - 2
	if padding < 1 {
		padding = 1
	}
	line := left + strings.Repeat(" ", padding) + acct

	return theme.BalanceBar.Width(m.Width).Render(line)
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
