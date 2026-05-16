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
