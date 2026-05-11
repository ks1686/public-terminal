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
// ─────────────────────────────────────────────────────────────────────────────

type Bar struct {
	Timestamp int64   `json:"timestamp"` // unix seconds
	Open      float64 `json:"open"`
	High      float64 `json:"high"`
	Low       float64 `json:"low"`
	Close     float64 `json:"close"`
	Volume    float64 `json:"volume"`
}

type BarsResponse struct {
	Bars []Bar `json:"bars"`
}
