package rebalance

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	MarketCapCacheMaxAgeHours  = 20
	MarketCapMinCoveragePct    = 0.95
	MarketCapFetchWorkers      = 20
	MarketCapFetchTimeoutSecs  = 300
	YahooQuoteBatchSize        = 50 // Yahoo Finance v7 quote endpoint batch size
)

// ─────────────────────────────────────────────────────────────────────────────
// Market cap cache I/O
// ─────────────────────────────────────────────────────────────────────────────

type marketCapCache struct {
	UpdatedAt        string             `json:"updated_at"`
	Index            string             `json:"index"`
	SourceTickerCount int               `json:"source_ticker_count"`
	Caps             map[string]float64 `json:"caps"`
}

var brokerToYF = map[string]string{
	"BF.B": "BF-B", "BRK.B": "BRK-B",
	"BTC": "BTC-USD", "ETH": "ETH-USD", "SOL": "SOL-USD",
}
var yfToBroker = map[string]string{
	"BF-B": "BF.B", "BRK-B": "BRK.B",
	"BTC-USD": "BTC", "ETH-USD": "ETH", "SOL-USD": "SOL",
}

func loadMarketCapCache(cachePath, index string) (map[string]float64, bool) {
	b, err := os.ReadFile(cachePath)
	if err != nil {
		return nil, false
	}
	var c marketCapCache
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, false
	}
	if len(c.Caps) == 0 {
		return nil, false
	}
	if c.SourceTickerCount < 1 {
		return nil, false
	}
	minRequired := marketCapMinRequired(c.SourceTickerCount)
	if len(c.Caps) < minRequired {
		log.Printf("INFO     Market cap cache coverage too low (%d/%d, threshold %d) — discarding.", len(c.Caps), c.SourceTickerCount, minRequired)
		return nil, false
	}
	// Check index match (support legacy "etf_ticker" key via ETFToIndex)
	cachedIndex := c.Index
	if cachedIndex == "" {
		cachedIndex = IndexSP500
	}
	if cachedIndex != index {
		log.Printf("INFO     Index changed (%s → %s) — discarding market cap cache.", cachedIndex, index)
		return nil, false
	}
	// Check age
	t, err := time.Parse(time.RFC3339, c.UpdatedAt)
	if err == nil && time.Since(t) > time.Duration(MarketCapCacheMaxAgeHours)*time.Hour {
		log.Printf("INFO     Market cap cache is %.1f hours old — will refresh.", time.Since(t).Hours())
		return nil, false
	}
	// Normalize symbols back to broker style
	out := make(map[string]float64, len(c.Caps))
	for sym, cap := range c.Caps {
		broker := yfToBroker[sym]
		if broker == "" {
			broker = sym
		}
		out[broker] = cap
	}
	log.Printf("INFO     Using cached market caps (%d entries, %.0f min old).", len(out), time.Since(t).Minutes())
	return out, true
}

func saveMarketCapCache(cachePath, index string, caps map[string]float64, sourceTickerCount int) {
	c := marketCapCache{
		UpdatedAt:         time.Now().Format(time.RFC3339),
		Index:             index,
		SourceTickerCount: sourceTickerCount,
		Caps:              caps,
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return
	}
	if err := os.WriteFile(cachePath, b, 0o644); err != nil {
		log.Printf("WARNING  Could not save market cap cache: %v", err)
		return
	}
	log.Printf("INFO     Market cap cache saved (%d entries).", len(caps))
}

func marketCapMinRequired(sourceCount int) int {
	n := int(float64(sourceCount) * MarketCapMinCoveragePct)
	if n < 1 {
		return 1
	}
	return n
}

// ─────────────────────────────────────────────────────────────────────────────
// Market cap fetching via Yahoo Finance v7 quote API
// ─────────────────────────────────────────────────────────────────────────────

// FetchMarketCaps returns {ticker: marketCap} for the given tickers.
// Uses the same-day cache; fetches in parallel batches otherwise.
func FetchMarketCaps(tickers []string, index, cachePath string) (map[string]float64, error) {
	if caps, ok := loadMarketCapCache(cachePath, index); ok {
		return caps, nil
	}

	log.Printf("INFO     Fetching market caps for %d tickers (%d workers)…", len(tickers), MarketCapFetchWorkers)

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(MarketCapFetchTimeoutSecs)*time.Second)
	defer cancel()

	caps := make(map[string]float64)
	var mu sync.Mutex

	// Build batches
	batches := make([][]string, 0)
	for i := 0; i < len(tickers); i += YahooQuoteBatchSize {
		end := i + YahooQuoteBatchSize
		if end > len(tickers) {
			end = len(tickers)
		}
		// Convert to yfinance symbols
		batch := make([]string, end-i)
		for j, t := range tickers[i:end] {
			yf := brokerToYF[t]
			if yf == "" {
				yf = t
			}
			batch[j] = yf
		}
		batches = append(batches, batch)
	}

	sem := make(chan struct{}, MarketCapFetchWorkers)
	var wg sync.WaitGroup
	done := 0

	for _, batch := range batches {
		batch := batch
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			select {
			case <-ctx.Done():
				return
			default:
			}

			batchCaps, err := fetchYahooBatchMarketCaps(ctx, batch)
			if err != nil {
				log.Printf("WARNING  batch market cap fetch error: %v", err)
				return
			}
			mu.Lock()
			for yfSym, cap := range batchCaps {
				broker := yfToBroker[yfSym]
				if broker == "" {
					broker = yfSym
				}
				caps[broker] = cap
			}
			done += len(batch)
			if done%100 == 0 {
				log.Printf("INFO       … %d / %d done", done, len(tickers))
			}
			mu.Unlock()
		}()
	}
	wg.Wait()

	log.Printf("INFO     → market caps for %d / %d tickers", len(caps), len(tickers))
	minRequired := marketCapMinRequired(len(tickers))
	if len(caps) >= minRequired {
		saveMarketCapCache(cachePath, index, caps, len(tickers))
	} else {
		log.Printf("WARNING  Market cap fetch returned only %d / %d results (threshold: %d) — skipping cache write.", len(caps), len(tickers), minRequired)
	}
	return caps, nil
}

// fetchYahooBatchMarketCaps calls the Yahoo Finance v7 quote API for a batch of symbols.
func fetchYahooBatchMarketCaps(ctx context.Context, yfSymbols []string) (map[string]float64, error) {
	params := url.Values{}
	params.Set("symbols", strings.Join(yfSymbols, ","))
	params.Set("fields", "marketCap")
	reqURL := "https://query1.finance.yahoo.com/v7/finance/quote?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var data struct {
		QuoteResponse struct {
			Result []struct {
				Symbol    string  `json:"symbol"`
				MarketCap float64 `json:"marketCap"`
			} `json:"result"`
			Error *struct {
				Code        string `json:"code"`
				Description string `json:"description"`
			} `json:"error"`
		} `json:"quoteResponse"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("Yahoo Finance JSON parse: %w", err)
	}
	if data.QuoteResponse.Error != nil {
		return nil, fmt.Errorf("Yahoo Finance error: %s", data.QuoteResponse.Error.Description)
	}

	out := make(map[string]float64)
	for _, r := range data.QuoteResponse.Result {
		if r.MarketCap > 0 {
			out[r.Symbol] = r.MarketCap
		}
	}
	return out, nil
}

// ValidateMarketCapCoverage checks that the market-cap data is complete enough
// for a reliable top-N basket. Mirrors Python's validate_market_cap_coverage.
func ValidateMarketCapCoverage(tickers []string, marketCaps map[string]float64, topN int) bool {
	sourceCount := len(tickers)
	available := 0
	for _, t := range tickers {
		if _, ok := marketCaps[t]; ok {
			available++
		}
	}
	minRequired := marketCapMinRequired(sourceCount)
	desired := topN
	if desired > sourceCount {
		desired = sourceCount
	}
	if available < minRequired || available < desired {
		log.Printf("ERROR    Market cap coverage too low for top-%d: %d / %d available (threshold: %d, desired: %d).",
			topN, available, sourceCount, minRequired, desired)
		return false
	}
	return true
}
