// Package rebalance is a faithful Go port of rebalance.py.
// index.go: index constituent fetching (official sources → Wikipedia → stale cache).
package rebalance

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode"

	"golang.org/x/net/html"
)

const (
	userAgent     = "Mozilla/5.0 (compatible; public-terminal/2.0)"
	fetchTimeout  = 30 * time.Second
	bugReportURL  = "https://github.com/ks1686/public-terminal/issues"
)

// Index identifiers (canonical, stored in rebalance_config.json).
const (
	IndexSP500    = "SP500"
	IndexNAS100   = "NASDAQ100"
	IndexDJIA     = "DJIA"
	IndexVT       = "FTSE_GLOBAL_ALL_CAP"
	IndexSPUS     = "SPUS"
)

// SupportedIndexes maps index ID → display name.
var SupportedIndexes = map[string]string{
	IndexSP500:  "S&P 500",
	IndexNAS100: "NASDAQ-100",
	IndexDJIA:   "Dow Jones (DJIA)",
	IndexVT:     "Global equities (ACWI proxy)",
	IndexSPUS:   "SP Funds SPUS (Shariah)",
}

// SupportedIndexList is an ordered slice of index IDs for UI iteration.
var SupportedIndexList = []string{IndexSP500, IndexNAS100, IndexDJIA, IndexVT, IndexSPUS}

// ETFToIndex maps legacy ETF tickers to canonical index IDs.
var ETFToIndex = map[string]string{
	"SPY": IndexSP500, "VOO": IndexSP500, "IVV": IndexSP500, "SPLG": IndexSP500,
	"QQQ": IndexNAS100, "QQQM": IndexNAS100, "ONEQ": IndexNAS100,
	"DIA": IndexDJIA,
	"VT":  IndexVT,
	"SPUS": IndexSPUS,
}

var iSharesToBroker = map[string]string{"BRKB": "BRK.B"}

// indexCache is the on-disk JSON structure for constituent caches.
type indexCache struct {
	UpdatedAt string             `json:"updated_at"`
	Tickers   []string           `json:"tickers"`
	Weights   map[string]float64 `json:"weights"`
}

// FetchConstituents implements the 3-tier strategy: Official → Wikipedia → Stale cache.
func FetchConstituents(index, accountID string, cachePath string) (tickers []string, weights map[string]float64, err error) {
	// 1. Official
	tickers, weights, err = fetchOfficial(index)
	if err == nil {
		saveIndexCache(cachePath, index, tickers, weights)
		return
	}
	log.Printf("WARNING  %s official fetch failed: %v — trying Wikipedia", index, err)

	// 2. Wikipedia fallback
	tickers, weights, err = fetchWikipedia(index)
	if err == nil {
		saveIndexCache(cachePath, index, tickers, weights)
		return
	}
	log.Printf("WARNING  %s Wikipedia fallback failed: %v — trying stale cache", index, err)

	// 3. Stale cache
	tickers, weights, _, err = loadIndexCache(cachePath)
	if err == nil {
		log.Printf("WARNING  Using stale %s constituent cache. Please report at %s", index, bugReportURL)
		return
	}
	return nil, nil, fmt.Errorf("all sources for %s constituents failed and no cache available", index)
}

// ─────────────────────────────────────────────────────────────────────────────
// Official sources
// ─────────────────────────────────────────────────────────────────────────────

func fetchOfficial(index string) ([]string, map[string]float64, error) {
	switch index {
	case IndexSP500:
		return fetchSP500Official()
	case IndexNAS100:
		return fetchNASDAQ100Official()
	case IndexDJIA:
		return fetchDJIAOfficial()
	case IndexVT:
		return fetchVTOfficial()
	case IndexSPUS:
		return fetchSPUSOfficial()
	default:
		return nil, nil, fmt.Errorf("unknown index: %s", index)
	}
}

func fetchSP500Official() ([]string, map[string]float64, error) {
	url := "https://www.ishares.com/us/products/239726/ISHARES-CORE-SP-500-ETF/1467271812596.ajax?fileType=csv&fileName=IVV_holdings&dataType=fund"
	body, err := fetchBytes(url, nil)
	if err != nil {
		return nil, nil, err
	}
	return parseISharesCSV(body, 9, "Weight (%)")
}

func fetchNASDAQ100Official() ([]string, map[string]float64, error) {
	url := "https://dng-api.invesco.com/cache/v1/accounts/en_US/shareclasses/QQQ/holdings/fund?idType=ticker&productType=ETF"
	body, err := fetchBytes(url, map[string]string{"Referer": "https://www.invesco.com/qqq-etf/en/about.html"})
	if err != nil {
		return nil, nil, err
	}
	return parseInvescoJSON(body)
}

func fetchDJIAOfficial() ([]string, map[string]float64, error) {
	// SSGA XLSX is complex — try the direct CSV if available, else error to trigger Wikipedia fallback
	url := "https://www.ssga.com/us/en/intermediary/etfs/library-content/products/fund-data/etfs/us/holdings-daily-us-en-dia.xlsx"
	body, err := fetchBytes(url, map[string]string{"Referer": "https://www.ssga.com/"})
	if err != nil {
		return nil, nil, fmt.Errorf("SSGA XLSX fetch failed: %w", err)
	}
	return parseSSGAXLSX(body)
}

func fetchVTOfficial() ([]string, map[string]float64, error) {
	url := "https://www.ishares.com/us/products/239600/ISHARES-MSCI-ACWI-ETF/1467271812596.ajax?fileType=csv&fileName=ACWI_holdings&dataType=fund"
	body, err := fetchBytes(url, nil)
	if err != nil {
		return nil, nil, err
	}
	return parseACWICSV(body)
}

func fetchSPUSOfficial() ([]string, map[string]float64, error) {
	url := "https://www.sp-funds.com/wp-content/uploads/data/TidalFG_Holdings_SPUS.csv"
	body, err := fetchBytes(url, nil)
	if err != nil {
		return nil, nil, err
	}
	return parseSPUSCSV(body)
}

// ─────────────────────────────────────────────────────────────────────────────
// CSV / JSON / XLSX parsers
// ─────────────────────────────────────────────────────────────────────────────

func parseISharesCSV(body []byte, skipRows int, weightCol string) ([]string, map[string]float64, error) {
	r := csv.NewReader(strings.NewReader(string(body)))
	for i := 0; i < skipRows; i++ {
		if _, err := r.Read(); err != nil {
			return nil, nil, fmt.Errorf("iShares CSV: not enough rows to skip: %w", err)
		}
	}
	header, err := r.Read()
	if err != nil {
		return nil, nil, fmt.Errorf("iShares CSV: missing header: %w", err)
	}
	colIdx := make(map[string]int)
	for i, h := range header {
		colIdx[strings.TrimSpace(h)] = i
	}
	tickerCol, ok := colIdx["Ticker"]
	if !ok {
		return nil, nil, fmt.Errorf("iShares CSV: no Ticker column")
	}
	assetClassCol := colIdx["Asset Class"]
	weightColIdx, hasWeight := colIdx[weightCol]

	var tickers []string
	rawWeights := map[string]float64{}
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue
		}
		if assetClassCol > 0 && assetClassCol < len(row) {
			if !strings.Contains(row[assetClassCol], "Equity") {
				continue
			}
		}
		if tickerCol >= len(row) {
			continue
		}
		ticker := cleanTicker(row[tickerCol])
		ticker = normIShares(ticker)
		if ticker == "" {
			continue
		}
		tickers = append(tickers, ticker)
		if hasWeight && weightColIdx < len(row) {
			if w := parseWeightPct(row[weightColIdx]); w > 0 {
				rawWeights[ticker] = w
			}
		}
	}
	var weights map[string]float64
	if len(rawWeights) > 0 {
		weights = normalizeWeights(rawWeights)
	}
	return dedupe(tickers), weights, nil
}

func parseInvescoJSON(body []byte) ([]string, map[string]float64, error) {
	var data struct {
		Holdings []struct {
			Ticker                  string  `json:"ticker"`
			SecurityTypeCode        string  `json:"securityTypeCode"`
			PercentageOfTotalNetAssets float64 `json:"percentageOfTotalNetAssets"`
		} `json:"holdings"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, nil, fmt.Errorf("Invesco JSON: %w", err)
	}
	skipTypes := map[string]bool{"IFUT": true, "CASH": true, "FXFWD": true}
	var tickers []string
	rawWeights := map[string]float64{}
	for _, h := range data.Holdings {
		if skipTypes[h.SecurityTypeCode] {
			continue
		}
		ticker := cleanTicker(h.Ticker)
		if !isStockTicker(ticker) {
			continue
		}
		tickers = append(tickers, ticker)
		if h.PercentageOfTotalNetAssets > 0 {
			rawWeights[ticker] = h.PercentageOfTotalNetAssets / 100.0
		}
	}
	var weights map[string]float64
	if len(rawWeights) > 0 {
		weights = normalizeWeights(rawWeights)
	}
	return dedupe(tickers), weights, nil
}

func parseSSGAXLSX(body []byte) ([]string, map[string]float64, error) {
	// Use tealeg/xlsx to parse
	// Write to temp file first (xlsx library needs a file)
	f, err := os.CreateTemp("", "pt-ssga-*.xlsx")
	if err != nil {
		return nil, nil, err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(body); err != nil {
		f.Close()
		return nil, nil, err
	}
	f.Close()

	// Use a simpler approach: read XLSX as ZIP and parse the shared strings + sheet XML
	// Since xlsx lib imports the whole file, just use the library
	// NOTE: If tealeg/xlsx fails, we return an error to trigger Wikipedia fallback
	// Import is handled at the package level
	return parseSSGAXLSXFile(f.Name())
}

func parseACWICSV(body []byte) ([]string, map[string]float64, error) {
	r := csv.NewReader(strings.NewReader(string(body)))
	for i := 0; i < 9; i++ {
		if _, err := r.Read(); err != nil {
			return nil, nil, fmt.Errorf("ACWI CSV: not enough rows to skip")
		}
	}
	header, err := r.Read()
	if err != nil {
		return nil, nil, fmt.Errorf("ACWI CSV: missing header")
	}
	colIdx := make(map[string]int)
	for i, h := range header {
		colIdx[strings.TrimSpace(h)] = i
	}
	tickerCol := colIdx["Ticker"]
	assetClassCol := colIdx["Asset Class"]
	marketValueCol, hasMarketValue := colIdx["Market Value"]

	caps := map[string]float64{}
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue
		}
		if assetClassCol > 0 && assetClassCol < len(row) {
			if !strings.Contains(row[assetClassCol], "Equity") {
				continue
			}
		}
		if tickerCol >= len(row) {
			continue
		}
		rawTicker := strings.TrimSpace(row[tickerCol])
		ticker := normIShares(rawTicker)
		// Only accept pure-alpha tickers (≤5 chars) for US-listed holdings
		if !((isAlpha(ticker) && len(ticker) <= 5) || iSharesToBroker[ticker] != "") {
			continue
		}
		if hasMarketValue && marketValueCol < len(row) {
			mv := parseFloat(strings.ReplaceAll(row[marketValueCol], ",", ""))
			if mv > 0 {
				if existing, ok := caps[ticker]; !ok || mv > existing {
					caps[ticker] = mv
				}
			}
		}
	}
	tickers := make([]string, 0, len(caps))
	for t := range caps {
		tickers = append(tickers, t)
	}
	if len(tickers) == 0 {
		return nil, nil, fmt.Errorf("ACWI CSV: no usable tickers found")
	}
	weights := normalizeWeights(caps)
	return tickers, weights, nil
}

func parseSPUSCSV(body []byte) ([]string, map[string]float64, error) {
	r := csv.NewReader(strings.NewReader(string(body)))
	header, err := r.Read()
	if err != nil {
		return nil, nil, fmt.Errorf("SPUS CSV: missing header")
	}
	colIdx := make(map[string]int)
	for i, h := range header {
		colIdx[strings.TrimSpace(h)] = i
	}
	tickerCol, ok := colIdx["StockTicker"]
	if !ok {
		return nil, nil, fmt.Errorf("SPUS CSV: missing StockTicker column (got: %v)", header)
	}
	weightCol, hasWeight := colIdx["Weightings"]

	rawWeights := map[string]float64{}
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil || tickerCol >= len(row) {
			continue
		}
		ticker := cleanTicker(row[tickerCol])
		if !isStockTicker(ticker) {
			continue
		}
		if hasWeight && weightCol < len(row) {
			if w := parseWeightPct(row[weightCol]); w > 0 {
				rawWeights[ticker] = w
			}
		}
	}
	if len(rawWeights) == 0 {
		return nil, nil, fmt.Errorf("SPUS CSV: no usable weight data")
	}
	tickers := make([]string, 0, len(rawWeights))
	for t := range rawWeights {
		tickers = append(tickers, t)
	}
	return tickers, normalizeWeights(rawWeights), nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Wikipedia fallbacks
// ─────────────────────────────────────────────────────────────────────────────

func fetchWikipedia(index string) ([]string, map[string]float64, error) {
	switch index {
	case IndexSP500:
		return fetchSP500Wikipedia()
	case IndexNAS100:
		return fetchNASDAQ100Wikipedia()
	case IndexDJIA:
		return fetchDJIAWikipedia()
	default:
		return nil, nil, fmt.Errorf("no Wikipedia fallback for index %s", index)
	}
}

func fetchSP500Wikipedia() ([]string, map[string]float64, error) {
	log.Printf("INFO     Falling back to Wikipedia for S&P 500 constituents…")
	body, err := fetchBytes("https://en.wikipedia.org/wiki/List_of_S%26P_500_companies", nil)
	if err != nil {
		return nil, nil, err
	}
	tickers, err := parseWikipediaTable(body, "constituents", []string{"Symbol"})
	if err != nil {
		return nil, nil, err
	}
	return tickers, nil, nil
}

func fetchNASDAQ100Wikipedia() ([]string, map[string]float64, error) {
	log.Printf("INFO     Falling back to Wikipedia for NASDAQ-100 constituents…")
	body, err := fetchBytes("https://en.wikipedia.org/wiki/Nasdaq-100", nil)
	if err != nil {
		return nil, nil, err
	}
	tickers, err := parseWikipediaTable(body, "constituents", []string{"Ticker", "Symbol"})
	if err != nil {
		return nil, nil, err
	}
	return tickers, nil, nil
}

func fetchDJIAWikipedia() ([]string, map[string]float64, error) {
	log.Printf("INFO     Falling back to Wikipedia for DJIA constituents…")
	body, err := fetchBytes("https://en.wikipedia.org/wiki/Dow_Jones_Industrial_Average", nil)
	if err != nil {
		return nil, nil, err
	}
	// DJIA Wikipedia page doesn't always have a well-defined table ID.
	// Find any table with a Symbol/Ticker column containing ≥20 rows.
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil, nil, err
	}
	var tickers []string
	var walkTable func(*html.Node, int) []string
	walkTable = func(n *html.Node, depth int) []string {
		if n.Type == html.ElementNode && n.Data == "table" {
			t := extractTableColumn(n, []string{"Symbol", "Ticker"})
			if len(t) >= 20 {
				return t
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if r := walkTable(c, depth+1); len(r) > 0 {
				return r
			}
		}
		return nil
	}
	tickers = walkTable(doc, 0)
	if len(tickers) == 0 {
		return nil, nil, fmt.Errorf("could not find DJIA constituent table on Wikipedia")
	}
	return tickers, nil, nil
}

// parseWikipediaTable finds a <table id="tableID"> and extracts the first
// matching column from the header row.
func parseWikipediaTable(body []byte, tableID string, colNames []string) ([]string, error) {
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	var table *html.Node
	var findTable func(*html.Node)
	findTable = func(n *html.Node) {
		if table != nil {
			return
		}
		if n.Type == html.ElementNode && n.Data == "table" {
			for _, attr := range n.Attr {
				if attr.Key == "id" && attr.Val == tableID {
					table = n
					return
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			findTable(c)
		}
	}
	findTable(doc)
	if table == nil {
		return nil, fmt.Errorf("could not find table id=%q on Wikipedia", tableID)
	}
	tickers := extractTableColumn(table, colNames)
	if len(tickers) == 0 {
		return nil, fmt.Errorf("table id=%q: no matching column %v found", tableID, colNames)
	}
	return tickers, nil
}

// extractTableColumn finds the first matching header column and returns all
// data cell values from that column.
func extractTableColumn(table *html.Node, colNames []string) []string {
	// Collect all rows
	var rows []*html.Node
	var gatherRows func(*html.Node)
	gatherRows = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "tr" {
			rows = append(rows, n)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			gatherRows(c)
		}
	}
	gatherRows(table)
	if len(rows) == 0 {
		return nil
	}

	// Find header col index
	colIdx := -1
	headerRow := rows[0]
	ci := 0
	for td := headerRow.FirstChild; td != nil; td = td.NextSibling {
		if td.Type != html.ElementNode {
			continue
		}
		if td.Data == "th" || td.Data == "td" {
			text := textContent(td)
			for _, name := range colNames {
				if strings.EqualFold(strings.TrimSpace(text), name) {
					colIdx = ci
					break
				}
			}
			ci++
		}
		if colIdx >= 0 {
			break
		}
	}
	if colIdx < 0 {
		return nil
	}

	var tickers []string
	for _, row := range rows[1:] {
		ci = 0
		for td := row.FirstChild; td != nil; td = td.NextSibling {
			if td.Type != html.ElementNode || (td.Data != "td" && td.Data != "th") {
				continue
			}
			if ci == colIdx {
				text := strings.TrimSpace(textContent(td))
				if t := cleanTicker(text); isStockTicker(t) {
					tickers = append(tickers, t)
				}
				break
			}
			ci++
		}
	}
	return tickers
}

func textContent(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		b.WriteString(textContent(c))
	}
	return b.String()
}

// ─────────────────────────────────────────────────────────────────────────────
// Cache I/O
// ─────────────────────────────────────────────────────────────────────────────

func saveIndexCache(cachePath, index string, tickers []string, weights map[string]float64) {
	c := indexCache{
		UpdatedAt: time.Now().Format(time.RFC3339),
		Tickers:   tickers,
		Weights:   weights,
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return
	}
	if err := os.WriteFile(cachePath, b, 0o644); err != nil {
		log.Printf("WARNING  Could not save index cache to %s: %v", cachePath, err)
		return
	}
	log.Printf("INFO     %s constituents cache saved (%d entries).", index, len(tickers))
}

func loadIndexCache(cachePath string) (tickers []string, weights map[string]float64, updatedAt time.Time, err error) {
	b, err := os.ReadFile(cachePath)
	if err != nil {
		return nil, nil, time.Time{}, err
	}
	var c indexCache
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, nil, time.Time{}, err
	}
	t, _ := time.Parse(time.RFC3339, c.UpdatedAt)
	return c.Tickers, c.Weights, t, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// HTTP helpers
// ─────────────────────────────────────────────────────────────────────────────

var httpClient = &http.Client{Timeout: fetchTimeout}

func fetchBytes(url string, extraHeaders map[string]string) ([]byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	return io.ReadAll(resp.Body)
}

// ─────────────────────────────────────────────────────────────────────────────
// Ticker / weight helpers
// ─────────────────────────────────────────────────────────────────────────────

func cleanTicker(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || s == "-" || s == "N/A" {
		return ""
	}
	return strings.ToUpper(s)
}

func normIShares(ticker string) string {
	if b, ok := iSharesToBroker[ticker]; ok {
		return b
	}
	return ticker
}

func isStockTicker(s string) bool {
	if s == "" {
		return false
	}
	for _, ch := range s {
		if !unicode.IsLetter(ch) && ch != '.' {
			return false
		}
	}
	return true
}

func isAlpha(s string) bool {
	for _, ch := range s {
		if !unicode.IsLetter(ch) {
			return false
		}
	}
	return true
}

func parseWeightPct(s string) float64 {
	s = strings.TrimSpace(s)
	isPct := strings.HasSuffix(s, "%")
	s = strings.TrimSuffix(s, "%")
	f := parseFloat(s)
	if f < 0 {
		return 0
	}
	if isPct || f >= 1.0 {
		return f / 100.0
	}
	return f
}

func parseFloat(s string) float64 {
	s = strings.TrimSpace(s)
	var f float64
	_, err := fmt.Sscanf(s, "%f", &f)
	if err != nil {
		return 0
	}
	return f
}

func normalizeWeights(raw map[string]float64) map[string]float64 {
	total := 0.0
	for _, v := range raw {
		total += v
	}
	if total <= 0 {
		return nil
	}
	out := make(map[string]float64, len(raw))
	for k, v := range raw {
		out[k] = v / total
	}
	return out
}

func dedupe(tickers []string) []string {
	seen := map[string]bool{}
	out := tickers[:0]
	for _, t := range tickers {
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}
