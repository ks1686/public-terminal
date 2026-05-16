package components

import (
	"fmt"
	"strings"

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
	width := m.Width
	if width < 20 {
		width = 20
	}
	contentW := max(1, width-4)
	boxW := max(1, width-2)

	title := theme.Muted.Render("PORTFOLIO EQUITY")

	eqStyle := lipgloss.NewStyle().Bold(true).Foreground(theme.ColorCyan)
	eq := eqStyle.Render(formatMoney(m.TotalEquity))

	gainStr := theme.FormatGain(m.DailyGainPct)
	gainAmt := formatMoneyStyled(m.DailyGainAmt)
	daily := fmt.Sprintf("%s (%s)", gainStr, gainAmt)

	top := lipgloss.JoinVertical(lipgloss.Center, title, eq, daily)
	top = lipgloss.NewStyle().Width(contentW).Align(lipgloss.Center).Render(top)

	sep := theme.Muted.Render("  │  ")
	bp := fmt.Sprintf("BP %s", formatMoney(m.BuyingPower))
	optBP := fmt.Sprintf("OPT BP %s", formatMoney(m.OptionsBP))
	cBP := fmt.Sprintf("CRYPTO BP %s", formatMoney(m.CryptoBP))

	cashLabel := "CASH"
	cashValue := formatMoney(m.Cash)
	if m.Cash.IsNegative() {
		cashLabel = "MARGIN"
		cashValue = theme.Warning.Render(formatMoney(m.Cash.Neg()))
	}
	cash := fmt.Sprintf("%s %s", cashLabel, cashValue)

	parts := []string{bp, optBP, cBP, cash}
	bottom := strings.Join(parts, sep)
	bottom = lipgloss.NewStyle().Width(contentW).Align(lipgloss.Center).Foreground(theme.ColorGray).Render(bottom)

	// Combine into a box
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.ColorCyan).
		Width(boxW). // Account for borders
		Render(lipgloss.JoinVertical(lipgloss.Center, top, "", bottom))

	return box
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

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
