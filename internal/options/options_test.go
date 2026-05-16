package options

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/ks1686/public-terminal/internal/api"
)

func TestParseOCCSymbol_CallStandard(t *testing.T) {
	underlying, optType, expiry, strike, err := parseOCCSymbol("AAPL  260516C00150000")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if underlying != "AAPL" {
		t.Errorf("underlying = %q, want %q", underlying, "AAPL")
	}
	if optType != "CALL" {
		t.Errorf("optType = %q, want %q", optType, "CALL")
	}
	if expiry != "2026-05-16" {
		t.Errorf("expiry = %q, want %q", expiry, "2026-05-16")
	}
	wantStrike := decimal.NewFromFloat(150.0)
	if !strike.Equal(wantStrike) {
		t.Errorf("strike = %s, want %s", strike, wantStrike)
	}
}

func TestParseOCCSymbol_PutStandard(t *testing.T) {
	_, optType, _, _, err := parseOCCSymbol("AAPL  260516P00145000")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if optType != "PUT" {
		t.Errorf("optType = %q, want %q", optType, "PUT")
	}
}

func TestParseOCCSymbol_FractionalStrike(t *testing.T) {
	_, _, _, strike, err := parseOCCSymbol("SPY   260516C00512500")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := decimal.NewFromFloat(512.500)
	if !strike.Equal(want) {
		t.Errorf("strike = %s, want %s", strike, want)
	}
}

func TestParseOCCSymbol_LongUnderlying(t *testing.T) {
	underlying, _, _, _, err := parseOCCSymbol("GOOGL 260516C02000000")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if underlying != "GOOGL" {
		t.Errorf("underlying = %q, want %q", underlying, "GOOGL")
	}
}

func TestParseOCCSymbol_ShortSymbol(t *testing.T) {
	_, _, _, _, err := parseOCCSymbol("ABC")
	if err == nil {
		t.Error("expected error for short symbol")
	}
}

func TestParseOCCSymbol_Empty(t *testing.T) {
	_, _, _, _, err := parseOCCSymbol("")
	if err == nil {
		t.Error("expected error for empty symbol")
	}
}

func TestParseOCCSymbol_UnknownType(t *testing.T) {
	_, _, _, _, err := parseOCCSymbol("AAPL  260516X00150000")
	if err == nil {
		t.Error("expected error for unknown option type")
	}
}

func TestOptionPosition_SymbolDisplay_Call(t *testing.T) {
	opt := OptionPosition{
		UnderlyingSymbol: "AAPL",
		OptionType:       "CALL",
		StrikePrice:      decimal.NewFromFloat(150.0),
		ExpirationDate:   "2026-05-16",
		Quantity:         decimal.NewFromFloat(2.0),
		CurrentValue:     decimal.NewFromFloat(350.0),
		OCCSymbol:        "AAPL  260516C00150000",
	}
	got := opt.SymbolDisplay()
	want := "AAPL 260516C150.00"
	if got != want {
		t.Errorf("SymbolDisplay() = %q, want %q", got, want)
	}
}

func TestOptionPosition_SymbolDisplay_Put(t *testing.T) {
	opt := OptionPosition{
		UnderlyingSymbol: "TSLA",
		OptionType:       "PUT",
		StrikePrice:      decimal.NewFromFloat(250.5),
		ExpirationDate:   "2026-06-20",
		Quantity:         decimal.NewFromFloat(5.0),
		OCCSymbol:        "TSLA  260620P00250500",
	}
	got := opt.SymbolDisplay()
	want := "TSLA 260620P250.50"
	if got != want {
		t.Errorf("SymbolDisplay() = %q, want %q", got, want)
	}
}

func TestOptionPosition_IsNearExpiry_True(t *testing.T) {
	future := time.Now().AddDate(0, 0, 3).Format("2006-01-02")
	opt := OptionPosition{ExpirationDate: future}
	opt.DaysToExpiry = daysToExpiry(opt.ExpirationDate)
	if !opt.IsNearExpiry() {
		t.Error("expected IsNearExpiry() = true")
	}
}

func TestOptionPosition_IsNearExpiry_False(t *testing.T) {
	future := time.Now().AddDate(0, 0, 30).Format("2006-01-02")
	opt := OptionPosition{ExpirationDate: future}
	opt.DaysToExpiry = daysToExpiry(opt.ExpirationDate)
	if opt.IsNearExpiry() {
		t.Error("expected IsNearExpiry() = false")
	}
}

func TestOptionPosition_IsNearExpiry_Past(t *testing.T) {
	past := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	opt := OptionPosition{ExpirationDate: past}
	opt.DaysToExpiry = daysToExpiry(opt.ExpirationDate)
	if opt.IsNearExpiry() {
		t.Error("expected IsNearExpiry() = false for past date")
	}
}

func TestOptionPosition_ToDict(t *testing.T) {
	pct := decimal.NewFromFloat(5.26)
	opt := OptionPosition{
		UnderlyingSymbol: "AAPL",
		OptionType:       "CALL",
		StrikePrice:      decimal.NewFromFloat(150.0),
		ExpirationDate:   "2026-05-16",
		Quantity:         decimal.NewFromFloat(2.0),
		CurrentValue:     decimal.NewFromFloat(350.0),
		LastPrice:        decimal.NewFromFloat(1.75),
		DailyGainPct:     &pct,
		OCCSymbol:        "AAPL  260516C00150000",
	}
	d := opt.ToDict()
	if d["underlying"] != "AAPL" {
		t.Errorf("underlying = %v, want AAPL", d["underlying"])
	}
	if d["type"] != "CALL" {
		t.Errorf("type = %v, want CALL", d["type"])
	}
	if d["strike"] != "150.00" {
		t.Errorf("strike = %v, want 150.00", d["strike"])
	}
}

func TestOptionPosition_ToDict_NoGain(t *testing.T) {
	opt := OptionPosition{
		UnderlyingSymbol: "AAPL",
		OptionType:       "CALL",
		StrikePrice:      decimal.NewFromFloat(150.0),
		ExpirationDate:   "2026-05-16",
		Quantity:         decimal.NewFromFloat(2.0),
		OCCSymbol:        "AAPL  260516C00150000",
	}
	d := opt.ToDict()
	if d["gain"] != "—" {
		t.Errorf("gain = %v, want —", d["gain"])
	}
}

func TestExtractOptionsFromPositions_Empty(t *testing.T) {
	result := ExtractOptionsFromPositions(nil)
	if len(result) != 0 {
		t.Errorf("expected empty, got %d", len(result))
	}
	result = ExtractOptionsFromPositions([]api.Position{})
	if len(result) != 0 {
		t.Errorf("expected empty, got %d", len(result))
	}
}

func TestExtractOptionsFromPositions_SkipsNonOption(t *testing.T) {
	positions := []api.Position{
		{Instrument: api.Instrument{Type: "EQUITY", Symbol: "AAPL"}, Quantity: decimal.NewFromFloat(10)},
	}
	result := ExtractOptionsFromPositions(positions)
	if len(result) != 0 {
		t.Errorf("expected empty, got %d", len(result))
	}
}

func TestExtractOptionsFromPositions_ParsesCall(t *testing.T) {
	val := decimal.NewFromFloat(350.0)
	positions := []api.Position{
		{
			Instrument:   api.Instrument{Type: "OPTION", Symbol: "AAPL  260516C00150000"},
			Quantity:     decimal.NewFromFloat(2),
			CurrentValue: &val,
		},
	}
	result := ExtractOptionsFromPositions(positions)
	if len(result) != 1 {
		t.Fatalf("expected 1, got %d", len(result))
	}
	opt := result[0]
	if opt.UnderlyingSymbol != "AAPL" {
		t.Errorf("UnderlyingSymbol = %q, want AAPL", opt.UnderlyingSymbol)
	}
	if opt.OptionType != "CALL" {
		t.Errorf("OptionType = %q, want CALL", opt.OptionType)
	}
	if !opt.CurrentValue.Equal(val) {
		t.Errorf("CurrentValue = %s, want %s", opt.CurrentValue, val)
	}
}

func TestExtractOptionsFromPositions_SkipsInvalid(t *testing.T) {
	positions := []api.Position{
		{Instrument: api.Instrument{Type: "OPTION", Symbol: "BADDATA"}},
	}
	result := ExtractOptionsFromPositions(positions)
	if len(result) == 0 {
		t.Fatal("expected fallback entry")
	}
	// Invalid OCC symbol should fall back to using the raw symbol as underlying
	if result[0].UnderlyingSymbol != "BADDATA" {
		t.Errorf("UnderlyingSymbol = %q, want BADDATA", result[0].UnderlyingSymbol)
	}
}
