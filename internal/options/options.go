// Package options parses OCC option symbols and extracts option positions from
// a portfolio. Direct port of options.py.
package options

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/shopspring/decimal"

	"github.com/ks1686/public-terminal/internal/api"
)

// OptionPosition holds data for a single options contract position.
type OptionPosition struct {
	UnderlyingSymbol string
	OptionType       string // "CALL" or "PUT"
	StrikePrice      decimal.Decimal
	ExpirationDate   string // "YYYY-MM-DD"
	Quantity         decimal.Decimal
	CurrentValue     decimal.Decimal
	LastPrice        decimal.Decimal
	DailyGainPct     *decimal.Decimal
	DaysToExpiry     int
	OCCSymbol        string
}

// SymbolDisplay returns a compact representation like "AAPL 260516C150.00".
func (o OptionPosition) SymbolDisplay() string {
	parts := strings.Fields(o.OCCSymbol)
	if len(parts) == 0 {
		return o.OCCSymbol
	}
	underlying := parts[0]
	rest := ""
	if len(parts) > 1 {
		rest = strings.Join(parts[1:], "")
	}
	// Try to produce a friendly label from parsed fields
	if o.OptionType != "" && !o.StrikePrice.IsZero() {
		t := strings.ToUpper(string(o.OptionType[0]))
		exp := strings.ReplaceAll(o.ExpirationDate, "-", "")[2:] // YYMMDD
		return fmt.Sprintf("%s %s%s%.2f", underlying, exp, t, must(o.StrikePrice.Float64()))
	}
	return fmt.Sprintf("%s %s", underlying, rest)
}

func must(f float64, _ bool) float64 { return f }

// IsNearExpiry returns true when DaysToExpiry is 0–7 (inclusive).
func (o OptionPosition) IsNearExpiry() bool {
	return o.DaysToExpiry >= 0 && o.DaysToExpiry <= 7
}

// ToDict returns a map suitable for the TUI's options table cache.
func (o OptionPosition) ToDict() map[string]any {
	var gainStr string
	gainPositive := false
	if o.DailyGainPct != nil {
		pct, _ := o.DailyGainPct.Float64()
		sign := ""
		if pct >= 0 {
			sign = "+"
			gainPositive = true
		}
		gainStr = fmt.Sprintf("%s%.2f%%", sign, pct)
	} else {
		gainStr = "—"
	}
	var lastPriceStr, valueStr string
	if !o.LastPrice.IsZero() {
		lastPriceStr = fmt.Sprintf("$%.2f", must(o.LastPrice.Float64()))
	} else {
		lastPriceStr = "—"
	}
	if !o.CurrentValue.IsZero() {
		valueStr = fmt.Sprintf("$%.2f", must(o.CurrentValue.Float64()))
	} else {
		valueStr = "—"
	}
	valueNum, _ := o.CurrentValue.Float64()
	return map[string]any{
		"symbol_display":  o.SymbolDisplay(),
		"underlying":      o.UnderlyingSymbol,
		"type":            o.OptionType,
		"strike":          o.StrikePrice.StringFixed(2),
		"expiry":          o.ExpirationDate,
		"qty":             o.Quantity.String(),
		"price":           lastPriceStr,
		"value":           valueStr,
		"value_num":       valueNum,
		"gain":            gainStr,
		"gain_positive":   gainPositive,
		"days_to_expiry":  o.DaysToExpiry,
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// OCC symbol parsing
// ─────────────────────────────────────────────────────────────────────────────

// parseOCCSymbol parses an OCC option symbol into its components.
// Format: "{Underlying}{YYMMDD}{C|P}{StrikePrice*1000:08d}"
// e.g. "AAPL  260516C00150000" → underlying="AAPL", expiry="2026-05-16", type="CALL", strike=150.00
func parseOCCSymbol(occ string) (underlying, optType, expiry string, strike decimal.Decimal, err error) {
	// Strip spaces and extract parts
	s := strings.TrimSpace(occ)
	// Find the transition from alpha (underlying) to digit (date)
	idx := -1
	for i, ch := range s {
		if unicode.IsDigit(ch) {
			idx = i
			break
		}
	}
	if idx < 1 || idx+6 > len(s) {
		return "", "", "", decimal.Zero, fmt.Errorf("invalid OCC symbol: %q", occ)
	}
	underlying = strings.TrimSpace(s[:idx])
	rest := s[idx:]
	if len(rest) < 15 {
		return "", "", "", decimal.Zero, fmt.Errorf("OCC symbol too short: %q", occ)
	}
	dateStr := rest[:6] // YYMMDD
	typeChar := rest[6]
	strikeStr := rest[7:15]

	year, _ := strconv.Atoi(dateStr[:2])
	month, _ := strconv.Atoi(dateStr[2:4])
	day, _ := strconv.Atoi(dateStr[4:6])
	fullYear := 2000 + year
	expiry = fmt.Sprintf("%04d-%02d-%02d", fullYear, month, day)

	switch typeChar {
	case 'C', 'c':
		optType = "CALL"
	case 'P', 'p':
		optType = "PUT"
	default:
		return "", "", "", decimal.Zero, fmt.Errorf("unknown option type %q in %q", string(typeChar), occ)
	}

	strikeInt, err := strconv.ParseInt(strikeStr, 10, 64)
	if err != nil {
		return "", "", "", decimal.Zero, fmt.Errorf("invalid strike in %q: %w", occ, err)
	}
	strike = decimal.NewFromInt(strikeInt).Div(decimal.NewFromInt(1000))
	return underlying, optType, expiry, strike, nil
}

func daysToExpiry(expiryDate string) int {
	t, err := time.Parse("2006-01-02", expiryDate)
	if err != nil {
		return -1
	}
	days := int(time.Until(t).Hours() / 24)
	return days
}

// ─────────────────────────────────────────────────────────────────────────────
// Extraction
// ─────────────────────────────────────────────────────────────────────────────

// ExtractOptionsFromPositions filters OPTION positions from a portfolio and
// returns parsed OptionPosition values.
func ExtractOptionsFromPositions(positions []api.Position) []OptionPosition {
	var out []OptionPosition
	for _, pos := range positions {
		if pos.Instrument.Type != "OPTION" {
			continue
		}
		occ := pos.Instrument.Symbol
		underlying, optType, expiry, strike, err := parseOCCSymbol(occ)
		if err != nil {
			// Fallback: surface the raw symbol
			underlying = occ
		}

		var lastPrice decimal.Decimal
		if pos.LastPrice != nil && pos.LastPrice.LastPrice != nil {
			lastPrice = *pos.LastPrice.LastPrice
		}

		var currentValue decimal.Decimal
		if pos.CurrentValue != nil {
			currentValue = *pos.CurrentValue
		}

		var gainPct *decimal.Decimal
		if pos.PositionDailyGain != nil && pos.PositionDailyGain.GainPercentage != nil {
			gainPct = pos.PositionDailyGain.GainPercentage
		}

		out = append(out, OptionPosition{
			UnderlyingSymbol: underlying,
			OptionType:       optType,
			StrikePrice:      strike,
			ExpirationDate:   expiry,
			Quantity:         pos.Quantity,
			CurrentValue:     currentValue,
			LastPrice:        lastPrice,
			DailyGainPct:     gainPct,
			DaysToExpiry:     daysToExpiry(expiry),
			OCCSymbol:        occ,
		})
	}
	return out
}
