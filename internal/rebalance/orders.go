package rebalance

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/ks1686/public-terminal/internal/api"
)

// ─────────────────────────────────────────────────────────────────────────────
// Constants (match Python rebalance.py)
// ─────────────────────────────────────────────────────────────────────────────

var (
	MinOrderDollars      = decimal.NewFromFloat(5.00)
	MinCryptoOrderDollars = decimal.NewFromFloat(1.00)
	RebalanceThresholdPct = decimal.NewFromFloat(0.005)
	BuyingPowerBuffer    = decimal.NewFromFloat(1.00)
	SellWaitTimeoutSecs  = 300
	OrderPollSecs        = 2
)

const (
	GoldSymbol = "GLDM"
	BTCSymbol  = "BTC"
	ETHSymbol  = "ETH"
	SOLSymbol  = "SOL"
)

// nonStockETFs lists equity symbols excluded from the stock index slice.
var nonStockETFs = map[string]bool{GoldSymbol: true}

// cryptoToYF maps Public broker crypto symbols to Yahoo Finance tickers.
var cryptoToYF = map[string]string{
	"BTC": "BTC-USD", "ETH": "ETH-USD", "SOL": "SOL-USD",
}

// ─────────────────────────────────────────────────────────────────────────────
// Order spec
// ─────────────────────────────────────────────────────────────────────────────

type OrderSpec struct {
	Symbol         string
	InstrumentType string // "EQUITY" or "CRYPTO"
	Side           string // "BUY" or "SELL"
	DollarAmount   decimal.Decimal
	// For equity full-liquidations: sell by share quantity
	EquityQty *decimal.Decimal
	// For crypto sells: cap quantity to held balance
	CryptoQty *decimal.Decimal
}

// PatternDayTradingError is returned when the broker rejects due to PDT.
type PatternDayTradingError struct{ msg string }

func (e PatternDayTradingError) Error() string { return e.msg }

func isPDTError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, kw := range []string{"pattern day trad", "pdt", "day trade limit", "day trading restriction", "flagged as a pattern day trader"} {
		if strings.Contains(msg, kw) {
			return true
		}
	}
	return false
}

func isIntradayMarginError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, kw := range []string{"intraday margin", "intraday buying power", "margin call", "margin maintenance", "day trading margin", "margin deficiency"} {
		if strings.Contains(msg, kw) {
			return true
		}
	}
	return false
}

// ─────────────────────────────────────────────────────────────────────────────
// Delta computation
// ─────────────────────────────────────────────────────────────────────────────

// ComputeDelta returns an OrderSpec if drift exceeds the threshold, or nil.
func ComputeDelta(symbol, instrType string, targetValue, currentValue, threshold decimal.Decimal) *OrderSpec {
	minOrder := MinOrderDollars
	if instrType == "CRYPTO" {
		minOrder = MinCryptoOrderDollars
	}
	delta := targetValue.Sub(currentValue)
	driftThreshold := maxDecimal(targetValue.Mul(RebalanceThresholdPct), minOrder, threshold)
	if delta.GreaterThan(driftThreshold) {
		amt := delta.RoundBank(2)
		return &OrderSpec{Symbol: symbol, InstrumentType: instrType, Side: "BUY", DollarAmount: amt}
	}
	if delta.LessThan(driftThreshold.Neg()) {
		amt := delta.Abs().RoundBank(2)
		return &OrderSpec{Symbol: symbol, InstrumentType: instrType, Side: "SELL", DollarAmount: amt}
	}
	return nil
}

// ComputeUnallocatedBuyDelta returns a small positive delta below the threshold.
func ComputeUnallocatedBuyDelta(targetValue, currentValue, threshold decimal.Decimal) decimal.Decimal {
	delta := targetValue.Sub(currentValue)
	driftThreshold := maxDecimal(targetValue.Mul(RebalanceThresholdPct), MinOrderDollars, threshold)
	if delta.IsPositive() && delta.LessThanOrEqual(driftThreshold) {
		return delta.RoundBank(2)
	}
	return decimal.Zero
}

func maxDecimal(a, b, c decimal.Decimal) decimal.Decimal {
	m := a
	if b.GreaterThan(m) {
		m = b
	}
	if c.GreaterThan(m) {
		m = c
	}
	return m
}

// ─────────────────────────────────────────────────────────────────────────────
// Stock weight computation
// ─────────────────────────────────────────────────────────────────────────────

// ComputeStockWeights returns within-slice market-cap weights summing to 1.0.
func ComputeStockWeights(tickers []string, marketCaps map[string]float64) (map[string]decimal.Decimal, error) {
	caps := map[string]float64{}
	total := 0.0
	for _, t := range tickers {
		if c, ok := marketCaps[t]; ok && c > 0 {
			caps[t] = c
			total += c
		}
	}
	if total == 0 {
		return nil, fmt.Errorf("all market caps are zero — data problem")
	}
	weights := make(map[string]decimal.Decimal, len(caps))
	for t, c := range caps {
		weights[t] = decimal.NewFromFloat(c / total)
	}
	log.Printf("INFO     Stock weights computed for %d positions.", len(weights))
	return weights, nil
}

// TopNByMarketCap returns the top-N tickers by market cap.
func TopNByMarketCap(tickers []string, marketCaps map[string]float64, n int) []string {
	ranked := make([]string, 0, len(tickers))
	for _, t := range tickers {
		if _, ok := marketCaps[t]; ok {
			ranked = append(ranked, t)
		}
	}
	sort.Slice(ranked, func(i, j int) bool {
		return marketCaps[ranked[i]] > marketCaps[ranked[j]]
	})
	if n < len(ranked) {
		ranked = ranked[:n]
	}
	if len(ranked) > 0 {
		largest := marketCaps[ranked[0]]
		smallest := marketCaps[ranked[len(ranked)-1]]
		if largest >= 1e9 {
			log.Printf("INFO     Top %d stocks: largest=%s ($%.2fT), smallest=%s ($%.2fB)",
				len(ranked), ranked[0], largest/1e12, ranked[len(ranked)-1], smallest/1e9)
		}
	}
	return ranked
}

// SelectPublicTradableStocks filters the ranked list to Public-buyable symbols.
func SelectPublicTradableStocks(
	client *api.Client,
	tickers []string,
	marketCaps map[string]float64,
	n int,
	excludedTickers map[string]bool,
) ([]string, error) {
	ranked := TopNByMarketCap(tickers, marketCaps, len(tickers))

	// Fetch all Public-buyable equity symbols in one call
	tradable, err := client.ListTradableInstruments("EQUITY", "BUY_AND_SELL")
	if err != nil {
		return nil, fmt.Errorf("listing tradable instruments: %w", err)
	}
	buyable := map[string]bool{}
	for _, d := range tradable {
		if d.IsBuyable() {
			buyable[d.Instrument.Symbol] = true
		}
	}
	log.Printf("INFO     Loaded %d Public-buyable equity symbols.", len(buyable))

	var selected, excluded, untradable []string
	for _, symbol := range ranked {
		if !buyable[symbol] {
			untradable = append(untradable, symbol)
			log.Printf("INFO       Skipping %s — not buyable or missing on Public.", symbol)
			continue
		}
		if excludedTickers[symbol] {
			excluded = append(excluded, symbol)
			log.Printf("INFO       Skipping %s — excluded by config.", symbol)
			continue
		}
		selected = append(selected, symbol)
		if len(selected) >= n {
			break
		}
	}
	if len(excluded) > 0 {
		log.Printf("INFO     Excluded after Public validation (%d): %s", len(excluded), strings.Join(excluded[:min10(len(excluded))], ", "))
	}
	if len(untradable) > 0 {
		log.Printf("WARNING  Skipped %d non-buyable constituent(s): %s", len(untradable), strings.Join(untradable[:min10(len(untradable))], ", "))
	}
	if len(selected) < n {
		log.Printf("WARNING  Only %d Public-buyable stocks available (wanted %d).", len(selected), n)
	}
	return selected, nil
}

func min10(n int) int {
	if n < 10 {
		return n
	}
	return 10
}

// ─────────────────────────────────────────────────────────────────────────────
// Buy priority sorting
// ─────────────────────────────────────────────────────────────────────────────

func SortBuysByPriority(buys []OrderSpec, stockWeights map[string]decimal.Decimal, allocStocks, investmentBase decimal.Decimal) []OrderSpec {
	tier := map[string]int{BTCSymbol: 0, ETHSymbol: 1, SOLSymbol: 2, GoldSymbol: 3}
	sorted := make([]OrderSpec, len(buys))
	copy(sorted, buys)
	sort.SliceStable(sorted, func(i, j int) bool {
		si, sj := sorted[i].Symbol, sorted[j].Symbol
		ti, tj := tieredKey(si, tier, stockWeights, allocStocks, investmentBase)
		tk, tl := tieredKey(sj, tier, stockWeights, allocStocks, investmentBase)
		if ti != tk {
			return ti < tk
		}
		return tl.LessThan(tj) // descending target value within tier 4
	})
	return sorted
}

func tieredKey(symbol string, tier map[string]int, stockWeights map[string]decimal.Decimal, allocStocks, investmentBase decimal.Decimal) (int, decimal.Decimal) {
	if t, ok := tier[symbol]; ok {
		return t, decimal.Zero
	}
	target := stockWeights[symbol].Mul(allocStocks).Mul(investmentBase)
	return 4, target
}

// ─────────────────────────────────────────────────────────────────────────────
// Order placement
// ─────────────────────────────────────────────────────────────────────────────

// FilterByTradability removes orders the Public API would reject.
func FilterByTradability(client *api.Client, specs []OrderSpec) []OrderSpec {
	var valid []OrderSpec
	var skipped []string
	for _, s := range specs {
		d, err := client.GetInstrument(s.Symbol, s.InstrumentType)
		if err != nil {
			skipped = append(skipped, s.Symbol)
			log.Printf("WARNING  Skipping %s %s — instrument lookup failed: %v", s.Side, s.Symbol, err)
			continue
		}
		if s.Side == "BUY" && !d.IsBuyable() {
			skipped = append(skipped, s.Symbol)
			log.Printf("WARNING  Skipping BUY %s — not buyable (trading=%s).", s.Symbol, d.Trading)
			continue
		}
		if s.Side == "SELL" && !d.IsSellable() {
			skipped = append(skipped, s.Symbol)
			log.Printf("WARNING  Skipping SELL %s — not sellable (trading=%s).", s.Symbol, d.Trading)
			continue
		}
		valid = append(valid, s)
	}
	if len(skipped) > 0 {
		log.Printf("WARNING  Removed %d order(s) after tradability check: %s", len(skipped), strings.Join(skipped, ", "))
	}
	return valid
}

// PlaceBatch places orders serially. Returns (submittedOrderIDs, submittedSpecs).
// Raises PatternDayTradingError on PDT; stops on intraday margin error.
func PlaceBatch(client *api.Client, specs []OrderSpec, cryptoPrices map[string]decimal.Decimal, dryRun bool) ([]string, []OrderSpec, error) {
	if dryRun {
		LogDryRunOrders(specs)
		return nil, nil, nil
	}
	var orderIDs []string
	var submitted []OrderSpec
	success, fail := 0, 0

	for _, s := range specs {
		req, err := buildOrderRequest(s, cryptoPrices)
		if err != nil {
			log.Printf("ERROR    ✗ %s %-6s $%.2f → build error: %v", s.Side, s.Symbol, must2(s.DollarAmount.Float64()), err)
			fail++
			continue
		}
		if err := client.PlaceOrder(req); err != nil {
			if isPDTError(err) {
				log.Printf("ERROR    ✗ %s %-6s — PATTERN DAY TRADING restriction: %v", s.Side, s.Symbol, err)
				log.Printf("ERROR    PDT restriction detected — aborting remaining orders (%d placed so far).", success)
				fail++
				return orderIDs, submitted, PatternDayTradingError{msg: err.Error()}
			}
			if isIntradayMarginError(err) {
				log.Printf("WARNING  ✗ %s %-6s — intraday margin limit: %v", s.Side, s.Symbol, err)
				log.Printf("WARNING  Stopping further orders (%d placed so far).", success)
				fail++
				break
			}
			log.Printf("ERROR    ✗ %s %-6s $%.2f → %v", s.Side, s.Symbol, must2(s.DollarAmount.Float64()), err)
			fail++
		} else {
			log.Printf("INFO       ✓ %s %-6s $%.2f", s.Side, s.Symbol, must2(s.DollarAmount.Float64()))
			orderIDs = append(orderIDs, req.OrderID)
			submitted = append(submitted, s)
			success++
		}
		time.Sleep(100 * time.Millisecond)
	}
	log.Printf("INFO     Orders placed: %d  |  failed: %d", success, fail)
	return orderIDs, submitted, nil
}

func buildOrderRequest(s OrderSpec, cryptoPrices map[string]decimal.Decimal) (api.OrderRequest, error) {
	import_uuid := newUUID()
	base := api.OrderRequest{
		OrderID: import_uuid,
		Instrument: api.OrderInstrument{
			Symbol: s.Symbol,
			Type:   s.InstrumentType,
		},
		OrderSide: s.Side,
		OrderType: "MARKET",
		Expiration: api.OrderExpiration{TimeInForce: "DAY"},
	}

	if s.InstrumentType == "CRYPTO" {
		price, ok := cryptoPrices[s.Symbol]
		if !ok || !price.IsPositive() {
			return api.OrderRequest{}, fmt.Errorf("crypto_price required for CRYPTO order (%s)", s.Symbol)
		}
		if s.DollarAmount.LessThan(MinCryptoOrderDollars) {
			return api.OrderRequest{}, fmt.Errorf("CRYPTO order below minimum ($%.2f < $%.2f)", must2(s.DollarAmount.Float64()), must2(MinCryptoOrderDollars.Float64()))
		}
		qty := s.DollarAmount.Div(price).RoundDown(5)
		if !qty.IsPositive() {
			return api.OrderRequest{}, fmt.Errorf("CRYPTO order quantity rounds to zero (%s: $%.2f at $%.2f/coin)", s.Symbol, must2(s.DollarAmount.Float64()), must2(price.Float64()))
		}
		if s.Side == "SELL" && s.CryptoQty != nil {
			qty = decimal.Min(qty, *s.CryptoQty).RoundDown(5)
			if !qty.IsPositive() {
				return api.OrderRequest{}, fmt.Errorf("CRYPTO SELL quantity rounds to zero after cap (%s)", s.Symbol)
			}
		}
		base.Quantity = &qty
		return base, nil
	}

	// Equity
	if s.EquityQty != nil {
		if !s.EquityQty.IsPositive() {
			return api.OrderRequest{}, fmt.Errorf("equity quantity is zero for full-liquidation sell (%s)", s.Symbol)
		}
		base.Quantity = s.EquityQty
		return base, nil
	}
	amt := s.DollarAmount.RoundDown(2)
	if !amt.IsPositive() {
		return api.OrderRequest{}, fmt.Errorf("equity order amount rounds to zero (%s: $%.2f)", s.Symbol, must2(s.DollarAmount.Float64()))
	}
	base.Amount = &amt
	return base, nil
}

func must2(f float64, _ bool) float64 { return f }

// ─────────────────────────────────────────────────────────────────────────────
// Cancel open orders
// ─────────────────────────────────────────────────────────────────────────────

func CancelOpenOrders(client *api.Client, orders []api.Order, dryRun bool) {
	open := filterActive(orders)
	if len(open) == 0 {
		log.Printf("INFO     No open orders to cancel.")
		return
	}
	log.Printf("INFO     Cancelling %d open order(s) before rebalancing…", len(open))
	if dryRun {
		log.Printf("INFO     DRY RUN — would cancel %d open order(s).", len(open))
		return
	}
	for _, o := range open {
		if err := client.CancelOrder(o.OrderID); err != nil {
			log.Printf("WARNING    ✗ Could not cancel %s (ID: %.8s): %v", o.Instrument.Symbol, o.OrderID, err)
		} else {
			log.Printf("INFO       ✓ Cancelled %s %s (ID: %.8s)", o.Side, o.Instrument.Symbol, o.OrderID)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func filterActive(orders []api.Order) []api.Order {
	var out []api.Order
	for _, o := range orders {
		if api.ActiveOrderStatuses[o.Status] {
			out = append(out, o)
		}
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// Wait for orders to clear
// ─────────────────────────────────────────────────────────────────────────────

func WaitForOrdersToClear(client *api.Client, orderIDs []string, label string, timeoutSecs int) bool {
	if len(orderIDs) == 0 {
		return true
	}
	pending := map[string]bool{}
	for _, id := range orderIDs {
		pending[id] = true
	}
	deadline := time.Now().Add(time.Duration(timeoutSecs) * time.Second)
	for time.Now().Before(deadline) {
		p, err := client.GetPortfolio()
		if err != nil {
			log.Printf("WARNING  Could not refresh portfolio while waiting for %s orders: %v", label, err)
			time.Sleep(time.Duration(OrderPollSecs) * time.Second)
			continue
		}
		for _, o := range p.Orders {
			if api.ActiveOrderStatuses[o.Status] {
				delete(pending, o.OrderID)
			}
		}
		// pending = intersection(pending, active)
		newPending := map[string]bool{}
		active := map[string]bool{}
		for _, o := range p.Orders {
			if api.ActiveOrderStatuses[o.Status] {
				active[o.OrderID] = true
			}
		}
		for id := range pending {
			if active[id] {
				newPending[id] = true
			}
		}
		pending = newPending
		if len(pending) == 0 {
			log.Printf("INFO     All %s orders are no longer active.", label)
			return true
		}
		log.Printf("INFO     Waiting for %d %s order(s) to clear…", len(pending), label)
		time.Sleep(time.Duration(OrderPollSecs) * time.Second)
	}
	log.Printf("WARNING  Timed out waiting for %d %s order(s) to clear.", len(pending), label)
	return false
}

// ─────────────────────────────────────────────────────────────────────────────
// Fill buy orders (budget-constrained)
// ─────────────────────────────────────────────────────────────────────────────

func FillBuyOrders(orders []OrderSpec, availableBP decimal.Decimal) []OrderSpec {
	remaining := decimal.Max(decimal.Zero, availableBP.Sub(BuyingPowerBuffer))
	if !remaining.IsPositive() {
		log.Printf("WARNING  No buy orders will be placed: buying power is only $%.2f.", must2(availableBP.Float64()))
		return nil
	}
	var result []OrderSpec
	for _, o := range orders {
		if remaining.GreaterThanOrEqual(o.DollarAmount) {
			result = append(result, o)
			remaining = remaining.Sub(o.DollarAmount)
		} else if remaining.GreaterThanOrEqual(MinOrderDollars) {
			partial := remaining.RoundDown(2)
			o.DollarAmount = partial
			result = append(result, o)
			log.Printf("INFO       Partial fill %s $%.2f (buying power limit).", o.Symbol, must2(partial.Float64()))
			break
		} else {
			break
		}
	}
	total := decimal.Zero
	for _, o := range result {
		total = total.Add(o.DollarAmount)
	}
	log.Printf("INFO     Buy budget: $%.2f available, $%.2f allocated across %d order(s).",
		must2(decimal.Max(decimal.Zero, availableBP.Sub(BuyingPowerBuffer)).Float64()),
		must2(total.Float64()), len(result))
	return result
}

// ─────────────────────────────────────────────────────────────────────────────
// Supplemental sells
// ─────────────────────────────────────────────────────────────────────────────

func ComputeSupplementalSells(
	shortfall decimal.Decimal,
	equityPos map[string]decimal.Decimal,
	alreadySelling, alreadyBuying, todayBuys map[string]bool,
	stockWeights map[string]decimal.Decimal,
	allocStocks, investmentBase decimal.Decimal,
) []OrderSpec {
	protected := map[string]bool{BTCSymbol: true, ETHSymbol: true, SOLSymbol: true, GoldSymbol: true}
	var candidates []string
	for symbol := range equityPos {
		if alreadySelling[symbol] || alreadyBuying[symbol] || todayBuys[symbol] || protected[symbol] {
			continue
		}
		candidates = append(candidates, symbol)
	}
	sort.Slice(candidates, func(i, j int) bool {
		ti := stockWeights[candidates[i]].Mul(allocStocks).Mul(investmentBase)
		tj := stockWeights[candidates[j]].Mul(allocStocks).Mul(investmentBase)
		return ti.LessThan(tj)
	})
	var result []OrderSpec
	remaining := shortfall
	for _, symbol := range candidates {
		if !remaining.IsPositive() {
			break
		}
		sellAmount := decimal.Min(equityPos[symbol], remaining).RoundDown(2)
		if sellAmount.GreaterThanOrEqual(MinOrderDollars) {
			result = append(result, OrderSpec{
				Symbol:         symbol,
				InstrumentType: "EQUITY",
				Side:           "SELL",
				DollarAmount:   sellAmount,
			})
			remaining = remaining.Sub(sellAmount)
		}
	}
	return result
}

// ─────────────────────────────────────────────────────────────────────────────
// Day-trade ledger
// ─────────────────────────────────────────────────────────────────────────────

type todayBuysFile struct {
	Date    string   `json:"date"`
	Symbols []string `json:"symbols"`
}

func LoadTodayBuys(path string) map[string]bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return map[string]bool{}
	}
	var f todayBuysFile
	if err := json.Unmarshal(b, &f); err != nil {
		return map[string]bool{}
	}
	today := time.Now().Format("2006-01-02")
	if f.Date != today {
		return map[string]bool{}
	}
	out := map[string]bool{}
	for _, s := range f.Symbols {
		out[s] = true
	}
	return out
}

func RecordTodayBuys(path string, symbols map[string]bool) {
	if len(symbols) == 0 {
		return
	}
	existing := LoadTodayBuys(path)
	for s := range symbols {
		existing[s] = true
	}
	out := make([]string, 0, len(existing))
	for s := range existing {
		out = append(out, s)
	}
	sort.Strings(out)
	f := todayBuysFile{
		Date:    time.Now().Format("2006-01-02"),
		Symbols: out,
	}
	b, _ := json.Marshal(f)
	_ = os.WriteFile(path, b, 0o644)
	log.Printf("INFO     Day-trade ledger updated: %d symbol(s) bought today total.", len(existing))
}

// ─────────────────────────────────────────────────────────────────────────────
// Dry-run logging
// ─────────────────────────────────────────────────────────────────────────────

func LogDryRunOrders(specs []OrderSpec) {
	if len(specs) == 0 {
		return
	}
	total := decimal.Zero
	for _, s := range specs {
		total = total.Add(s.DollarAmount)
	}
	label := specs[0].Side
	log.Printf("INFO     DRY RUN — would submit %d %s order(s), total notional $%.2f.", len(specs), label, must2(total.Float64()))
	max := 25
	if len(specs) < max {
		max = len(specs)
	}
	for _, s := range specs[:max] {
		log.Printf("INFO       DRY RUN  %-6s %-6s %-6s $%.2f", s.Side, s.Symbol, s.InstrumentType, must2(s.DollarAmount.Float64()))
	}
	if len(specs) > 25 {
		log.Printf("INFO       DRY RUN  … %d additional %s order(s)", len(specs)-25, label)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// UUID helper
// ─────────────────────────────────────────────────────────────────────────────

func newUUID() string {
	// Simple UUID v4 via crypto/rand
	b := make([]byte, 16)
	_, _ = randRead(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}
