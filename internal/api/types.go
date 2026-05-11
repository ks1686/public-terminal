// Package api wraps the public CLI (public --json) and provides typed Go structs.
package api

import "github.com/shopspring/decimal"

// ─────────────────────────────────────────────────────────────────────────────
// Portfolio
// ─────────────────────────────────────────────────────────────────────────────

type Portfolio struct {
	AccountID   string       `json:"account_id"`
	Equity      []Equity     `json:"equity"`
	BuyingPower BuyingPower  `json:"buying_power"`
	Positions   []Position   `json:"positions"`
	Orders      []Order      `json:"orders"`
}

type Equity struct {
	Type  string          `json:"type"`
	Value decimal.Decimal `json:"value"`
}

type BuyingPower struct {
	BuyingPower         decimal.Decimal  `json:"buying_power"`
	OptionsBuyingPower  decimal.Decimal  `json:"options_buying_power"`
	CryptoBuyingPower   *decimal.Decimal `json:"crypto_buying_power"`
}

type Position struct {
	Instrument        Instrument       `json:"instrument"`
	Quantity          decimal.Decimal  `json:"quantity"`
	CurrentValue      *decimal.Decimal `json:"current_value"`
	LastPrice         *LastPrice       `json:"last_price"`
	PositionDailyGain *DailyGain       `json:"position_daily_gain"`
}

type Instrument struct {
	Symbol string `json:"symbol"`
	Type   string `json:"type"` // EQUITY, CRYPTO, OPTION
}

type LastPrice struct {
	LastPrice *decimal.Decimal `json:"last_price"`
}

type DailyGain struct {
	GainPercentage *decimal.Decimal `json:"gain_percentage"`
}

type Order struct {
	OrderID      string           `json:"order_id"`
	Side         string           `json:"side"`   // BUY, SELL
	Type         string           `json:"type"`   // MARKET, LIMIT, STOP, STOP_LIMIT
	Status       string           `json:"status"` // NEW, PARTIALLY_FILLED, PENDING_REPLACE, PENDING_CANCEL, FILLED, CANCELLED
	Instrument   Instrument       `json:"instrument"`
	Quantity     *decimal.Decimal `json:"quantity"`
	NotionalValue *decimal.Decimal `json:"notional_value"`
	LimitPrice   *decimal.Decimal `json:"limit_price"`
	StopPrice    *decimal.Decimal `json:"stop_price"`
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
	OrderID    string          `json:"order_id"`
	Instrument OrderInstrument `json:"instrument"`
	OrderSide  string          `json:"order_side"`
	OrderType  string          `json:"order_type"`
	Expiration OrderExpiration `json:"expiration"`
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
	Date        string           `json:"date"`
	Type        string           `json:"type"`
	Symbol      string           `json:"symbol"`
	Description string           `json:"description"`
	Amount      *decimal.Decimal `json:"amount"`
	Price       *decimal.Decimal `json:"price"`
	Quantity    *decimal.Decimal `json:"quantity"`
}

// The CLI wraps results in a pagination envelope.
type HistoryResponse struct {
	Items     []HistoryEntry `json:"items"`
	NextToken string         `json:"next_token"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Instruments
// ─────────────────────────────────────────────────────────────────────────────

type InstrumentDetail struct {
	Instrument        Instrument `json:"instrument"`
	Trading           string     `json:"trading"`            // BUY_AND_SELL, LIQUIDATION_ONLY, DISABLED
	FractionalTrading string     `json:"fractional_trading"` // BUY_AND_SELL, SELL_ONLY, DISABLED
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
	Symbol string           `json:"symbol"`
	Last   *decimal.Decimal `json:"last"`
	Bid    *decimal.Decimal `json:"bid"`
	Ask    *decimal.Decimal `json:"ask"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Historic bars
//
// The CLI returns bars partitioned into three market sessions:
//   { "symbol": "...", "period": "...",
//     "preMarket":     { "expectedBars": N, "bars": [...] },
//     "regularMarket": { "expectedBars": N, "bars": [...] },
//     "afterMarket":   { "expectedBars": N, "bars": [...] } }
// We flatten them chronologically for charting.
// ─────────────────────────────────────────────────────────────────────────────

// Bar holds OHLC + volume for one historicdata interval. The API returns
// decimal prices as JSON strings (e.g. "293.05"), so decimal.Decimal is used
// for the price fields — it accepts both string and number JSON encodings.
type Bar struct {
	Timestamp string          `json:"timestamp"` // ISO 8601
	Open      decimal.Decimal `json:"open"`
	High      decimal.Decimal `json:"high"`
	Low       decimal.Decimal `json:"low"`
	Close     decimal.Decimal `json:"close"`
	Value     decimal.Decimal `json:"value"`
	Volume    int64           `json:"volume"`
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
