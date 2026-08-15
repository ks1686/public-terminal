package rebalance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseWeightPct_SubOnePercent(t *testing.T) {
	got := parseWeightPct("0.823")
	want := 0.00823
	if got < want-1e-9 || got > want+1e-9 {
		t.Fatalf("parseWeightPct(0.823) = %v, want %v", got, want)
	}
}

func TestParseWeightPct_PercentSuffix(t *testing.T) {
	got := parseWeightPct("4.797%")
	want := 0.04797
	if got < want-1e-9 || got > want+1e-9 {
		t.Fatalf("parseWeightPct(4.797%%) = %v, want %v", got, want)
	}
}

func TestFetchConstituents_RejectsStaleCache(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "constituents_SP500.json")
	cache := indexCache{
		UpdatedAt: time.Now().Add(-8 * 24 * time.Hour).Format(time.RFC3339),
		Tickers:   []string{"AAPL"},
		Weights:   map[string]float64{"AAPL": 1},
	}
	b, err := json.Marshal(cache)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}

	_, _, err = loadFreshOrStaleCache(path)
	if err == nil {
		t.Fatal("expected error for cache older than 7 days")
	}
}
