// Package api wraps the public CLI (public --json) and provides typed Go structs.
//
// All field names use the CLI's camelCase JSON shape — verified against the
// live /userapigateway endpoints. Decimal-valued fields are encoded as JSON
// strings (e.g. "132.46"), so decimal.Decimal is used (it accepts both string
// and number JSON encodings).
package api

import "github.com/shopspring/decimal"

// ─────────────────────────────────────────────────────────────────────────────
// Portfolio
// ─────────────────────────────────────────────────────────────────────────────

type Portfolio struct {
	AccountID   string      `json:"accountId"`
	AccountType string      `json:"accountType"`
	BuyingPower BuyingPower `json:"buyingPower"`
	Equity      []Equity    `json:"equity"`
	Positions   []Position  `json:"positions"`
	Orders      []Order     `json:"orders"`
}

type Equity struct {
	Type                  string          `json:"type"` // CRYPTO, CASH, STOCK, OPTION
	Value                 decimal.Decimal `json:"value"`
	PercentageOfPortfolio decimal.Decimal `json:"percentageOfPortfolio"`
}

type BuyingPower struct {
	BuyingPower         decimal.Decimal  `json:"buyingPower"`
	CashOnlyBuyingPower decimal.Decimal  `json:"cashOnlyBuyingPower"`
	OptionsBuyingPower  decimal.Decimal  `json:"optionsBuyingPower"`
	CryptoBuyingPower   *decimal.Decimal `json:"cryptoBuyingPower"`
}

type Position struct {
	Instrument         Instrument       `json:"instrument"`
	Quantity           decimal.Decimal  `json:"quantity"`
	OpenedAt           string           `json:"openedAt"`
	CurrentValue       *decimal.Decimal `json:"currentValue"`
	PercentOfPortfolio *decimal.Decimal `json:"percentOfPortfolio"`
	LastPrice          *LastPrice       `json:"lastPrice"`
	InstrumentGain     *Gain            `json:"instrumentGain"`
	PositionDailyGain  *Gain            `json:"positionDailyGain"`
	CostBasis          *CostBasis       `json:"costBasis"`
	StrategyIDs        []string         `json:"strategyIds"`
}

type Instrument struct {
	Symbol string `json:"symbol"`
	Name   string `json:"name"`
	Type   string `json:"type"` // EQUITY, CRYPTO, OPTION
}

type LastPrice struct {
	LastPrice *decimal.Decimal `json:"lastPrice"`
	Timestamp string           `json:"timestamp"`
}

type Gain struct {
	GainValue      *decimal.Decimal `json:"gainValue"`
	GainPercentage *decimal.Decimal `json:"gainPercentage"`
	Timestamp      *string          `json:"timestamp"`
}

type CostBasis struct {
	TotalCost      *decimal.Decimal `json:"totalCost"`
	UnitCost       *decimal.Decimal `json:"unitCost"`
	GainValue      *decimal.Decimal `json:"gainValue"`
	GainPercentage *decimal.Decimal `json:"gainPercentage"`
	LastUpdate     string           `json:"lastUpdate"`
}

// DailyGain is retained as a thin shim for callers that still reach for
// PositionDailyGain.GainPercentage. New code should use the Gain type directly.
type DailyGain = Gain

type Order struct {
	OrderID       string           `json:"orderId"`
	Side          string           `json:"side"`   // BUY, SELL
	Type          string           `json:"type"`   // MARKET, LIMIT, STOP, STOP_LIMIT
	Status        string           `json:"status"` // NEW, PARTIALLY_FILLED, PENDING_REPLACE, PENDING_CANCEL, FILLED, CANCELLED
	Instrument    Instrument       `json:"instrument"`
	Quantity      *decimal.Decimal `json:"quantity"`
	NotionalValue *decimal.Decimal `json:"notionalValue"`
	LimitPrice    *decimal.Decimal `json:"limitPrice"`
	StopPrice     *decimal.Decimal `json:"stopPrice"`
}

// ActiveOrderStatuses matches Python's _ACTIVE_ORDER_STATUSES.
var ActiveOrderStatuses = map[string]bool{
	"NEW":              true,
	"PARTIALLY_FILLED": true,
	"PENDING_REPLACE":  true,
	"PENDING_CANCEL":   true,
}

// ─────────────────────────────────────────────────────────────────────────────
// Order placement
// ─────────────────────────────────────────────────────────────────────────────

type OrderRequest struct {
	OrderID    string           `json:"order_id"`
	Instrument OrderInstrument  `json:"instrument"`
	OrderSide  string           `json:"order_side"`
	OrderType  string           `json:"order_type"`
	Expiration OrderExpiration  `json:"expiration"`
	Quantity   *decimal.Decimal `json:"quantity,omitempty"`
	Amount     *decimal.Decimal `json:"amount,omitempty"`
	LimitPrice *decimal.Decimal `json:"limit_price,omitempty"`
	StopPrice  *decimal.Decimal `json:"stop_price,omitempty"`
}

type OrderInstrument struct {
	Symbol string `json:"symbol"`
	Type   string `json:"type"`
}

type OrderExpiration struct {
	TimeInForce string `json:"time_in_force"`
}

// ─────────────────────────────────────────────────────────────────────────────
// History
// ─────────────────────────────────────────────────────────────────────────────

type HistoryEntry struct {
	Timestamp       string           `json:"timestamp"` // ISO 8601 UTC with Z
	ID              string           `json:"id"`
	Type            string           `json:"type"`
	SubType         string           `json:"subType"`
	AccountNumber   string           `json:"accountNumber"`
	Symbol          string           `json:"symbol"`
	SecurityType    string           `json:"securityType"`
	Side            string           `json:"side"`
	Description     string           `json:"description"`
	NetAmount       *decimal.Decimal `json:"netAmount"`
	PrincipalAmount *decimal.Decimal `json:"principalAmount"`
	Quantity        *decimal.Decimal `json:"quantity"`
	Direction       string           `json:"direction"`
	Fees            *decimal.Decimal `json:"fees"`
}

type HistoryResponse struct {
	Transactions []HistoryEntry `json:"transactions"`
	NextToken    string         `json:"nextToken"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Instruments
// ─────────────────────────────────────────────────────────────────────────────

type InstrumentDetail struct {
	Instrument        Instrument `json:"instrument"`
	Trading           string     `json:"trading"`           // BUY_AND_SELL, LIQUIDATION_ONLY, DISABLED
	FractionalTrading string     `json:"fractionalTrading"` // BUY_AND_SELL, SELL_ONLY, DISABLED
}

func (d InstrumentDetail) IsBuyable() bool {
	return d.Trading == "BUY_AND_SELL"
}

func (d InstrumentDetail) IsSellable() bool {
	return d.Trading == "BUY_AND_SELL" || d.Trading == "LIQUIDATION_ONLY"
}

type InstrumentsListResponse struct {
	Instruments []InstrumentDetail `json:"instruments"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Market quotes
// ─────────────────────────────────────────────────────────────────────────────

type Quote struct {
	Instrument Instrument       `json:"instrument"`
	Outcome    string           `json:"outcome"` // SUCCESS, INVALID, ...
	Last       *decimal.Decimal `json:"last"`
	Bid        *decimal.Decimal `json:"bid"`
	Ask        *decimal.Decimal `json:"ask"`
}

func (q Quote) Symbol() string { return q.Instrument.Symbol }

type QuotesResponse struct {
	Quotes []Quote `json:"quotes"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Historic bars (for chart)
//
// CLI returns:
//   { "symbol": "...", "period": "...",
//     "preMarket":     { "expectedBars": N, "bars": [...] },
//     "regularMarket": { "expectedBars": N, "bars": [...] },
//     "afterMarket":   { "expectedBars": N, "bars": [...] } }
// We flatten them chronologically for charting.
// ─────────────────────────────────────────────────────────────────────────────

type Bar struct {
	Timestamp string          `json:"timestamp"` // ISO 8601 (with offset, e.g. -04:00)
	Open      decimal.Decimal `json:"open"`
	High      decimal.Decimal `json:"high"`
	Low       decimal.Decimal `json:"low"`
	Close     decimal.Decimal `json:"close"`
	Value     decimal.Decimal `json:"value"`
	Volume    decimal.Decimal `json:"volume"` // some sessions return as JSON number, others as string
}

type marketSessionBars struct {
	Bars []Bar `json:"bars"`
}

type BarsResponse struct {
	Symbol        string            `json:"symbol"`
	Period        string            `json:"period"`
	PreMarket     marketSessionBars `json:"preMarket"`
	RegularMarket marketSessionBars `json:"regularMarket"`
	AfterMarket   marketSessionBars `json:"afterMarket"`
}

// Flatten returns all bars across sessions in chronological order
// (pre-market → regular → after-market).
func (b BarsResponse) Flatten() []Bar {
	out := make([]Bar, 0, len(b.PreMarket.Bars)+len(b.RegularMarket.Bars)+len(b.AfterMarket.Bars))
	out = append(out, b.PreMarket.Bars...)
	out = append(out, b.RegularMarket.Bars...)
	out = append(out, b.AfterMarket.Bars...)
	return out
}
