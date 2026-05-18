package rebalance

import (
	"fmt"
	"strings"

	"github.com/tealeg/xlsx"
)

// parseSSGAXLSXFile parses the SSGA DIA holdings XLSX file.
// SSGA xlsx structure: 4 metadata rows, then a header row (Name, Ticker, ...),
// then data. We skip 4 rows so row 4 becomes the column header.
func parseSSGAXLSXFile(path string) ([]string, map[string]float64, error) {
	f, err := xlsx.OpenFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("SSGA XLSX open: %w", err)
	}
	if len(f.Sheets) == 0 {
		return nil, nil, fmt.Errorf("SSGA XLSX: no sheets")
	}
	sheet := f.Sheets[0]

	// Skip 4 metadata rows
	if len(sheet.Rows) <= 5 {
		return nil, nil, fmt.Errorf("SSGA XLSX: not enough rows (%d)", len(sheet.Rows))
	}
	// Row index 4 (0-based) = 5th row = header
	headerRow := sheet.Rows[4]
	colIdx := make(map[string]int)
	for i, cell := range headerRow.Cells {
		colIdx[strings.TrimSpace(cell.String())] = i
	}

	tickerCol, ok := colIdx["Ticker"]
	if !ok {
		return nil, nil, fmt.Errorf("SSGA XLSX: no Ticker column (got: %v)", colIdx)
	}
	// Find weight column (case-insensitive search)
	weightCol := -1
	for h, idx := range colIdx {
		lower := strings.ToLower(h)
		if lower == "weight" || lower == "% weight" || lower == "weight (%)" {
			weightCol = idx
			break
		}
	}

	var tickers []string
	rawWeights := map[string]float64{}
	for _, row := range sheet.Rows[5:] {
		if tickerCol >= len(row.Cells) {
			continue
		}
		ticker := cleanTicker(row.Cells[tickerCol].String())
		if ticker == "" {
			continue
		}
		tickers = append(tickers, ticker)
		if weightCol >= 0 && weightCol < len(row.Cells) {
			if w := parseWeightPct(row.Cells[weightCol].String()); w > 0 {
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

// parseSPGMXLSXFile parses the SSGA SPGM (Portfolio MSCI Global Stock Market ETF) holdings XLSX.
// Structure: 5 metadata rows, two-row header at rows 5-6 (0-indexed), data from row 6.
// For most rows: Ticker is at column B (index 1), Weight at column E (index 4).
// For "overflow" rows: Ticker is at column A (index 0) with a CUSIP in B.
// For the first anomalous row: Ticker is at column D (index 3); len≠3 excludes ISO 4217
// currency codes (always 3 chars) that also appear in column D.
func parseSPGMXLSXFile(path string) ([]string, map[string]float64, error) {
	f, err := xlsx.OpenFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("SSGA SPGM XLSX open: %w", err)
	}
	if len(f.Sheets) == 0 {
		return nil, nil, fmt.Errorf("SSGA SPGM XLSX: no sheets")
	}
	sheet := f.Sheets[0]
	if len(sheet.Rows) <= 7 {
		return nil, nil, fmt.Errorf("SSGA SPGM XLSX: not enough rows (%d)", len(sheet.Rows))
	}

	// Scan for the header row: find the row whose column B (index 1) is "Ticker".
	// We expect it within the first 10 rows (SSGA has ~5 metadata rows).
	headerIdx := -1
	for i, row := range sheet.Rows {
		if i >= 10 {
			break
		}
		if len(row.Cells) >= 2 && strings.TrimSpace(row.Cells[1].String()) == "Ticker" {
			headerIdx = i
			break
		}
	}
	if headerIdx < 0 {
		return nil, nil, fmt.Errorf("SSGA SPGM XLSX: could not find header row with 'Ticker' in column B")
	}

	const (
		colB      = 1 // Ticker for most rows
		colWeight = 4 // E: weight percentage
		colA      = 0 // Ticker for overflow rows (A=ticker, B=CUSIP)
		colD      = 3 // Ticker for first anomalous row (A/B hold sub-header labels)
	)

	rawWeights := map[string]float64{}
	var tickers []string

	for _, row := range sheet.Rows[headerIdx+1:] {
		if len(row.Cells) <= colWeight {
			continue
		}
		w := parseWeightPct(row.Cells[colWeight].String())
		if w <= 0 {
			continue
		}

		var ticker string
		if len(row.Cells) > colB {
			if t := cleanTicker(row.Cells[colB].String()); isStockTicker(t) {
				ticker = t
			}
		}
		if ticker == "" && len(row.Cells) > colA {
			if t := cleanTicker(row.Cells[colA].String()); isStockTicker(t) {
				ticker = t
			}
		}
		if ticker == "" && len(row.Cells) > colD {
			// len != 3 excludes ISO 4217 currency codes (USD, EUR, KRW, etc.)
			if t := cleanTicker(row.Cells[colD].String()); isStockTicker(t) && len(t) != 3 {
				ticker = t
			}
		}
		if ticker == "" {
			continue
		}

		if _, seen := rawWeights[ticker]; !seen {
			tickers = append(tickers, ticker)
		}
		rawWeights[ticker] += w
	}

	if len(tickers) == 0 {
		return nil, nil, fmt.Errorf("SSGA SPGM XLSX: no usable tickers found")
	}
	return dedupe(tickers), normalizeWeights(rawWeights), nil
}
