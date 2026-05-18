package rebalance

import (
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/ks1686/public-terminal/internal/api"
	"github.com/ks1686/public-terminal/internal/config"
)

// ─────────────────────────────────────────────────────────────────────────────
// Portfolio snapshot
// ─────────────────────────────────────────────────────────────────────────────

type PortfolioSnapshot struct {
	TotalEquity        decimal.Decimal
	CashBalance        decimal.Decimal
	BuyingPower        decimal.Decimal
	CashOnlyBP         decimal.Decimal
	EquityPos          map[string]decimal.Decimal // symbol → current value
	CryptoPos          map[string]decimal.Decimal
	EquityQty          map[string]decimal.Decimal // symbol → share count
	CryptoQty          map[string]decimal.Decimal
	Orders             []api.Order
}

func GetPortfolioSnapshot(client *api.Client) (*PortfolioSnapshot, error) {
	p, err := client.GetPortfolio()
	if err != nil {
		return nil, err
	}
	var totalEquity, cashBalance decimal.Decimal
	for _, e := range p.Equity {
		if e.Type == "CASH" {
			cashBalance = cashBalance.Add(e.Value)
		} else {
			totalEquity = totalEquity.Add(e.Value)
		}
	}
	bp := p.BuyingPower.BuyingPower
	cashBP := p.BuyingPower.CashOnlyBuyingPower
	if cashBP.IsZero() && bp.IsPositive() && !cashBalance.IsNegative() {
		// Older payloads may omit cashOnlyBuyingPower; assume same as bp so the
		// margin delta is zero rather than spuriously positive.
		// Only apply when cash >= 0: negative cash means an active margin loan,
		// so cashOnlyBuyingPower == 0 is genuine, not a missing-field sentinel.
		cashBP = bp
	}

	snap := &PortfolioSnapshot{
		TotalEquity: totalEquity,
		CashBalance: cashBalance,
		BuyingPower: bp,
		CashOnlyBP:  cashBP,
		EquityPos:   map[string]decimal.Decimal{},
		CryptoPos:   map[string]decimal.Decimal{},
		EquityQty:   map[string]decimal.Decimal{},
		CryptoQty:   map[string]decimal.Decimal{},
		Orders:      p.Orders,
	}
	for _, pos := range p.Positions {
		sym := pos.Instrument.Symbol
		switch pos.Instrument.Type {
		case "EQUITY":
			if pos.CurrentValue != nil {
				snap.EquityPos[sym] = *pos.CurrentValue
			}
			snap.EquityQty[sym] = pos.Quantity
		case "CRYPTO":
			if pos.CurrentValue != nil {
				snap.CryptoPos[sym] = *pos.CurrentValue
			}
			snap.CryptoQty[sym] = pos.Quantity
		}
	}
	return snap, nil
}

// MarginState matches Python's estimate_margin_state output.
type MarginState struct {
	PortfolioNAV      decimal.Decimal
	MarginLoanEst     decimal.Decimal
	AllowedMarginLoan decimal.Decimal
	InvestmentBase    decimal.Decimal
	EffectiveBP       decimal.Decimal
}

func EstimateMarginState(snap *PortfolioSnapshot, marginUsagePct decimal.Decimal) MarginState {
	portfolioNAV := decimal.Max(decimal.Zero, snap.TotalEquity.Add(snap.CashBalance))
	marginCapacity := decimal.Max(decimal.Zero, snap.BuyingPower.Sub(snap.CashOnlyBP))
	allowedMarginLoan := marginUsagePct.Mul(marginCapacity)
	investmentBase := portfolioNAV.Add(allowedMarginLoan)
	effectiveBP := decimal.Max(decimal.Zero, snap.CashOnlyBP.Add(allowedMarginLoan))
	currentMarginLoan := decimal.Zero
	if marginCapacity.IsPositive() {
		currentMarginLoan = decimal.Max(decimal.Zero, snap.CashBalance.Neg())
	}
	return MarginState{
		PortfolioNAV:      portfolioNAV,
		MarginLoanEst:     currentMarginLoan,
		AllowedMarginLoan: allowedMarginLoan,
		InvestmentBase:    investmentBase,
		EffectiveBP:       effectiveBP,
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Main rebalance orchestration
// ─────────────────────────────────────────────────────────────────────────────

// Run executes the full rebalance for the given account.
func Run(accountID string, dryRun bool) error {
	cfg := config.LoadRebalanceConfig(accountID)
	if !cfg.RebalanceEnabled && !dryRun {
		log.Printf("INFO     Rebalancing is disabled for account %s — skipping.", accountID)
		return nil
	}

	// Attach log file
	logPath := config.RebalanceLogPath(accountID)
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err == nil {
		log.SetOutput(logFile)
		defer func() {
			log.SetOutput(os.Stdout)
			logFile.Close()
		}()
	}

	log.Printf("INFO     %s", strings.Repeat("=", 64))
	mode := "PORTFOLIO REBALANCE"
	if dryRun {
		mode = "DRY-RUN PORTFOLIO REBALANCE"
	}
	log.Printf("INFO     %s  —  %s", mode, time.Now().Format("2006-01-02 15:04:05"))

	index := cfg.Index
	if index == "" {
		index = IndexSP500
	}
	topN := cfg.TopN
	if topN <= 0 {
		topN = 500
	}
	marginUsagePct := decimal.NewFromFloat(cfg.MarginUsagePct)
	excludedSet := map[string]bool{}
	for _, t := range cfg.ExcludedTickers {
		excludedSet[t] = true
	}

	alloc := cfg.Allocations
	if alloc == nil {
		alloc = config.DefaultAllocations
	}
	allocStocks := decimal.NewFromFloat(alloc["stocks"])
	allocBTC := decimal.NewFromFloat(alloc["btc"])
	allocETH := decimal.NewFromFloat(alloc["eth"])
	allocSOL := decimal.NewFromFloat(alloc["sol"])
	allocGold := decimal.NewFromFloat(alloc["gold"])

	log.Printf("INFO     Allocation: stocks %.0f%%  btc %.0f%%  eth %.0f%%  sol %.0f%%  gold %.0f%%  cash %.0f%%",
		alloc["stocks"]*100, alloc["btc"]*100, alloc["eth"]*100, alloc["sol"]*100, alloc["gold"]*100, alloc["cash"]*100)
	log.Printf("INFO     Index: %s (%s) — top %d", index, SupportedIndexes[index], topN)
	log.Printf("INFO     Margin usage: %.0f%% of margin capacity", cfg.MarginUsagePct*100)

	// Skip file
	skipPath := config.SkipFilePath(accountID)
	if _, err := os.Stat(skipPath); err == nil {
		if dryRun {
			log.Printf("INFO     DRY RUN — skip sentinel is present but continuing to show plan.")
		} else {
			_ = os.Remove(skipPath)
			log.Printf("INFO     SKIPPED — skip sentinel removed; next run will proceed normally.")
			return nil
		}
	}

	// Create API client
	client, err := api.NewClient(accountID)
	if err != nil {
		return fmt.Errorf("creating API client: %w", err)
	}

	log.Printf("INFO     --- Selecting Public-tradable stock basket ---")

	// Fetch constituents
	cachePath := config.IndexCachePath(accountID, index)
	constituents, fundWeights, err := FetchConstituents(index, accountID, cachePath)
	if err != nil {
		return fmt.Errorf("fetching constituents: %w", err)
	}

	// Filter to Public-tradable
	tradable, err := client.ListTradableInstruments("EQUITY", "BUY_AND_SELL")
	if err != nil {
		return fmt.Errorf("listing tradable instruments: %w", err)
	}
	buyable := map[string]bool{}
	for _, d := range tradable {
		if d.IsBuyable() {
			buyable[d.Instrument.Symbol] = true
		}
	}
	log.Printf("INFO     Loaded %d Public-buyable equity symbols.", len(buyable))

	tradableConstituents := make([]string, 0, len(constituents))
	for _, t := range constituents {
		if buyable[t] {
			tradableConstituents = append(tradableConstituents, t)
		}
	}
	log.Printf("INFO     Filtered constituents to %d / %d Public-tradable tickers.", len(tradableConstituents), len(constituents))

	// Market caps
	var marketCaps map[string]float64
	capCachePath := config.MarketCapCachePath(accountID)
	if fundWeights != nil {
		tradableWeights := map[string]float64{}
		for _, t := range tradableConstituents {
			if w, ok := fundWeights[t]; ok {
				tradableWeights[t] = w
			}
		}
		totalW := 0.0
		for _, w := range tradableWeights {
			totalW += w
		}
		if totalW > 0 && ValidateMarketCapCoverage(tradableConstituents, floatMap(tradableWeights), topN) {
			marketCaps = make(map[string]float64, len(tradableWeights))
			for t, w := range tradableWeights {
				marketCaps[t] = w / totalW
			}
			log.Printf("INFO     Using fund-provided weights for %d tradable constituents.", len(marketCaps))
		} else {
			log.Printf("WARNING  Fund weights insufficient — falling back to Yahoo Finance market caps.")
			marketCaps, err = FetchMarketCaps(tradableConstituents, index, capCachePath)
			if err != nil {
				return fmt.Errorf("fetching market caps: %w", err)
			}
		}
	} else {
		marketCaps, err = FetchMarketCaps(tradableConstituents, index, capCachePath)
		if err != nil {
			return fmt.Errorf("fetching market caps: %w", err)
		}
	}

	if !ValidateMarketCapCoverage(tradableConstituents, marketCaps, topN) {
		return fmt.Errorf("REBALANCE ABORTED — market-cap data incomplete; retry later")
	}

	// Select top stocks (already filtered to buyable via tradableConstituents)
	topStocks := TopNByMarketCap(tradableConstituents, marketCaps, topN)
	// Apply excluded filter
	finalStocks := make([]string, 0, len(topStocks))
	for _, s := range topStocks {
		if !excludedSet[s] {
			finalStocks = append(finalStocks, s)
		}
	}
	topStocks = finalStocks
	if len(topStocks) == 0 {
		return fmt.Errorf("REBALANCE ABORTED — no Public-buyable top stocks")
	}
	stockWeights, err := ComputeStockWeights(topStocks, marketCaps)
	if err != nil {
		return err
	}

	// Fetch initial portfolio
	log.Printf("INFO     Fetching portfolio from Public.com…")
	initial, err := GetPortfolioSnapshot(client)
	if err != nil {
		return fmt.Errorf("fetching portfolio: %w", err)
	}

	// Cancel open orders
	CancelOpenOrders(client, initial.Orders, dryRun)

	// Re-fetch after cancellations
	snap, err := GetPortfolioSnapshot(client)
	if err != nil {
		return fmt.Errorf("re-fetching portfolio: %w", err)
	}
	margin := EstimateMarginState(snap, marginUsagePct)
	log.Printf("INFO     Portfolio NAV: $%.2f  |  margin loan est.: $%.2f  |  allowed margin: $%.2f  |  investment base: $%.2f  |  effective BP: $%.2f  |  equity positions: %d",
		must2(margin.PortfolioNAV.Float64()), must2(margin.MarginLoanEst.Float64()),
		must2(margin.AllowedMarginLoan.Float64()), must2(margin.InvestmentBase.Float64()),
		must2(margin.EffectiveBP.Float64()), len(snap.EquityPos))

	// Load today-buys ledger
	todayBuysPath := config.TodayBuysPath(accountID)
	todayBuys := LoadTodayBuys(todayBuysPath)
	if len(todayBuys) > 0 {
		log.Printf("INFO     Day-trade prevention: %d symbol(s) bought earlier today are protected.", len(todayBuys))
	}

	var sells, buys []OrderSpec

	queue := func(spec *OrderSpec) {
		if spec == nil {
			return
		}
		if spec.Side == "BUY" && excludedSet[spec.Symbol] {
			log.Printf("INFO       Skipping BUY %s — excluded by config.", spec.Symbol)
			return
		}
		if spec.Side == "SELL" && spec.InstrumentType == "EQUITY" && todayBuys[spec.Symbol] {
			log.Printf("WARNING    Day-trade prevention: skipping SELL %s — purchased today.", spec.Symbol)
			return
		}
		if spec.Side == "SELL" {
			sells = append(sells, *spec)
		} else {
			buys = append(buys, *spec)
		}
	}

	// Stock deltas
	log.Printf("INFO     --- Computing stock deltas (%s top-%d) ---", index, topN)
	allStockSymbols := map[string]bool{}
	for _, s := range topStocks {
		allStockSymbols[s] = true
	}
	for s := range snap.EquityPos {
		if !nonStockETFs[s] {
			allStockSymbols[s] = true
		}
	}
	for symbol := range allStockSymbols {
		if nonStockETFs[symbol] {
			continue
		}
		weight := stockWeights[symbol]
		if excludedSet[symbol] {
			weight = decimal.Zero
		}
		target := weight.Mul(allocStocks).Mul(margin.InvestmentBase).RoundBank(2)
		current := snap.EquityPos[symbol]
		queue(ComputeDelta(symbol, "EQUITY", target, current, decimal.NewFromFloat(1.00)))
	}

	// Crypto prices
	cryptoPrices := map[string]decimal.Decimal{}
	cryptoAllocs := map[string]decimal.Decimal{
		BTCSymbol: allocBTC, ETHSymbol: allocETH, SOLSymbol: allocSOL,
	}
	for symbol, targetAlloc := range cryptoAllocs {
		if excludedSet[symbol] {
			continue
		}
		currentVal := snap.CryptoPos[symbol]
		if !targetAlloc.IsPositive() && !currentVal.IsPositive() {
			continue
		}
		price, err := client.GetCryptoQuote(symbol)
		if err != nil {
			return fmt.Errorf("could not fetch %s price: %w", symbol, err)
		}
		cryptoPrices[symbol] = price
	}

	// Crypto deltas
	for symbol, targetAlloc := range cryptoAllocs {
		if excludedSet[symbol] {
			continue
		}
		currentVal := snap.CryptoPos[symbol]
		if !targetAlloc.IsPositive() && !currentVal.IsPositive() {
			continue
		}
		log.Printf("INFO     --- Computing %s delta ---", symbol)
		price := cryptoPrices[symbol]
		targetVal := targetAlloc.Mul(margin.InvestmentBase).RoundBank(2)
		log.Printf("INFO       %-4s price=$%.2f  target=$%.2f  current=$%.2f  delta=$%.2f",
			symbol, must2(price.Float64()), must2(targetVal.Float64()), must2(currentVal.Float64()), must2(targetVal.Sub(currentVal).Float64()))
		spec := ComputeDelta(symbol, "CRYPTO", targetVal, currentVal, decimal.NewFromFloat(1.00))
		if spec != nil {
			cryptoQty := snap.CryptoQty[symbol]
			spec.CryptoQty = &cryptoQty
			queue(spec)
		}
	}

	// Gold ETF delta
	log.Printf("INFO     --- Computing GLDM delta ---")
	goldTarget := decimal.Zero
	if !excludedSet[GoldSymbol] {
		goldTarget = allocGold.Mul(margin.InvestmentBase).RoundBank(2)
	}
	goldCurrent := snap.EquityPos[GoldSymbol]
	log.Printf("INFO       GLDM  target=$%.2f  current=$%.2f  delta=$%.2f",
		must2(goldTarget.Float64()), must2(goldCurrent.Float64()), must2(goldTarget.Sub(goldCurrent).Float64()))
	queue(ComputeDelta(GoldSymbol, "EQUITY", goldTarget, goldCurrent, decimal.NewFromFloat(1.00)))

	log.Printf("INFO     Rebalance plan: %d sells  |  %d buys", len(sells), len(buys))
	if len(sells) == 0 && len(buys) == 0 {
		log.Printf("INFO     Portfolio is within threshold on all buckets — nothing to do.")
		return nil
	}

	// Sort sells descending by amount
	sort.Slice(sells, func(i, j int) bool {
		if sells[i].DollarAmount.Equal(sells[j].DollarAmount) {
			return sells[i].Symbol < sells[j].Symbol
		}
		return sells[i].DollarAmount.GreaterThan(sells[j].DollarAmount)
	})

	// Priority-sort buys
	buys = SortBuysByPriority(buys, stockWeights, allocStocks, margin.InvestmentBase)

	// Liquidation quantities (full equity exits must use share-qty orders)
	liquidationQtys := map[string]*decimal.Decimal{}
	for _, s := range sells {
		if s.InstrumentType == "EQUITY" && s.Side == "SELL" && !nonStockETFs[s.Symbol] {
			if stockWeights[s.Symbol].IsZero() {
				if qty, ok := snap.EquityQty[s.Symbol]; ok {
					q := qty
					liquidationQtys[s.Symbol] = &q
				}
			}
		}
	}
	// Attach equity quantities to sell specs
	for i, s := range sells {
		if q, ok := liquidationQtys[s.Symbol]; ok {
			sells[i].EquityQty = q
		}
	}
	if len(liquidationQtys) > 0 {
		syms := make([]string, 0, len(liquidationQtys))
		for s := range liquidationQtys {
			syms = append(syms, s)
		}
		log.Printf("INFO     %d positions will be liquidated by share quantity: %s", len(liquidationQtys), strings.Join(syms[:min10(len(syms))], ", "))
	}

	if dryRun {
		sells = FilterByTradability(client, sells)
		buys = FilterByTradability(client, buys)
		buys = FillBuyOrders(buys, margin.EffectiveBP)
		log.Printf("INFO     --- DRY RUN ORDER PLAN ---")
		LogDryRunOrders(sells)
		LogDryRunOrders(buys)
		log.Printf("INFO     DRY RUN complete — no orders placed.")
		return nil
	}

	// Live execution
	var sellIDs []string
	if len(sells) > 0 {
		sells = FilterByTradability(client, sells)
	}
	if len(sells) > 0 {
		log.Printf("INFO     --- Placing SELL orders (%d) ---", len(sells))
		var err error
		sellIDs, _, err = PlaceBatch(client, sells, cryptoPrices, false)
		if err != nil {
			if _, ok := err.(PatternDayTradingError); ok {
				log.Printf("ERROR    REBALANCE ABORTED — PDT restriction.")
				return nil
			}
			return err
		}
		if !WaitForOrdersToClear(client, sellIDs, "sell", SellWaitTimeoutSecs) {
			log.Printf("ERROR    Sell orders did not clear — aborting buy phase.")
			return nil
		}
	}

	if len(buys) > 0 {
		buys = FilterByTradability(client, buys)
	}
	if len(buys) > 0 {
		// Re-fetch post-sell portfolio
		postSnap, err := GetPortfolioSnapshot(client)
		postEffectiveBP := margin.EffectiveBP
		if err == nil {
			postMargin := EstimateMarginState(postSnap, marginUsagePct)
			postEffectiveBP = postMargin.EffectiveBP
			log.Printf("INFO       Post-sell effective BP: $%.2f", must2(postEffectiveBP.Float64()))

			// Supplemental sells for shortfall
			totalBuyNeed := decimal.Zero
			for _, b := range buys {
				totalBuyNeed = totalBuyNeed.Add(b.DollarAmount)
			}
			shortfall := decimal.Max(decimal.Zero, totalBuyNeed.Sub(postEffectiveBP))
			if shortfall.IsPositive() {
				log.Printf("INFO       Buy shortfall $%.2f — generating supplemental sells.", must2(shortfall.Float64()))
				alreadySelling := map[string]bool{}
				for _, s := range sells {
					alreadySelling[s.Symbol] = true
				}
				alreadyBuying := map[string]bool{}
				for _, b := range buys {
					alreadyBuying[b.Symbol] = true
				}
				supplemental := ComputeSupplementalSells(shortfall, postSnap.EquityPos, alreadySelling, alreadyBuying, todayBuys, stockWeights, allocStocks, margin.InvestmentBase)
				if len(supplemental) > 0 {
					supplemental = FilterByTradability(client, supplemental)
				}
				if len(supplemental) > 0 {
					log.Printf("INFO     --- Placing supplemental SELL orders (%d) ---", len(supplemental))
					suppIDs, _, _ := PlaceBatch(client, supplemental, cryptoPrices, false)
					WaitForOrdersToClear(client, suppIDs, "supplemental sell", SellWaitTimeoutSecs)
					// Refresh BP again
					if snap2, err2 := GetPortfolioSnapshot(client); err2 == nil {
						postEffectiveBP = EstimateMarginState(snap2, marginUsagePct).EffectiveBP
					}
				}
			}
		}

		buys = FillBuyOrders(buys, postEffectiveBP)
	}
	if len(buys) > 0 {
		log.Printf("INFO     --- Placing BUY orders (%d) ---", len(buys))
		_, submittedBuys, err := PlaceBatch(client, buys, cryptoPrices, false)
		if err != nil {
			if _, ok := err.(PatternDayTradingError); ok {
				log.Printf("ERROR    REBALANCE ABORTED — PDT restriction.")
				return nil
			}
			return err
		}
		boughtToday := map[string]bool{}
		for _, b := range submittedBuys {
			if b.InstrumentType == "EQUITY" {
				boughtToday[b.Symbol] = true
			}
		}
		RecordTodayBuys(todayBuysPath, boughtToday)
	}

	log.Printf("INFO     Rebalance complete.")
	return nil
}

// floatMap converts map[string]float64 values to the same type (for coverage check).
func floatMap(m map[string]float64) map[string]float64 { return m }

// ValidatePlan fetches live portfolio + market data and prints what the
// rebalancer would do, without placing any orders. Used by --validate flag.
func ValidatePlan(accountID string) error {
	return Run(accountID, true)
}
