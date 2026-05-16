package components

import (
	"testing"

	"github.com/ks1686/public-terminal/internal/tui/table"
)

func TestHoldingsColumnsForWidth_Full(t *testing.T) {
	cols := holdingsColumnsForWidth(80)
	if len(cols) != 5 {
		t.Fatalf("expected 5 columns, got %d", len(cols))
	}
	total := 0
	for _, c := range cols {
		total += c.Width
	}
	if total > 80 {
		t.Errorf("total width %d exceeds available 80", total)
	}
	// At 80, all columns should be visible
	if cols[3].Width == 0 {
		t.Error("Last column should be visible at width 80")
	}
	if cols[4].Width == 0 {
		t.Error("Day% column should be visible at width 80")
	}
}

func TestHoldingsColumnsForWidth_Tight(t *testing.T) {
	for _, w := range []int{30, 40, 52, 62} {
		cols := holdingsColumnsForWidth(w)
		total := 0
		for _, c := range cols {
			total += c.Width
		}
		if total > w {
			t.Errorf("at w=%d, total column width %d exceeds available", w, total)
		}
		if cols[0].Width == 0 {
			t.Errorf("at w=%d, Symbol column should always be visible", w)
		}
	}
}

func TestCryptoColumnsForWidth_Full(t *testing.T) {
	cols := cryptoColumnsForWidth(80)
	total := 0
	for _, c := range cols {
		total += c.Width
	}
	if total > 80 {
		t.Errorf("total width %d exceeds available 80", total)
	}
	// At 80, all columns should be visible
	if cols[3].Width == 0 {
		t.Error("Last column should be visible at width 80")
	}
}

func TestCryptoColumnsForWidth_Tight(t *testing.T) {
	for _, w := range []int{28, 34, 42} {
		cols := cryptoColumnsForWidth(w)
		total := 0
		for _, c := range cols {
			total += c.Width
		}
		if total > w {
			t.Errorf("at w=%d, total column width %d exceeds available", w, total)
		}
	}
}

func TestOrdersColumnsForWidth_Full(t *testing.T) {
	cols := ordersColumnsForWidth(80)
	total := 0
	for _, c := range cols {
		total += c.Width
	}
	if total > 80 {
		t.Errorf("total width %d exceeds available 80", total)
	}
}

func TestOrdersColumnsForWidth_Tight(t *testing.T) {
	for _, w := range []int{28, 38, 48} {
		cols := ordersColumnsForWidth(w)
		total := 0
		for _, c := range cols {
			total += c.Width
		}
		if total > w {
			t.Errorf("at w=%d, total column width %d exceeds available", w, total)
		}
	}
}

func TestOptionsColumnsForWidth_Full(t *testing.T) {
	cols := optionsColumnsForWidth(100)
	total := 0
	for _, c := range cols {
		total += c.Width
	}
	if total > 100 {
		t.Errorf("total width %d exceeds available 100", total)
	}
}

func TestOptionsColumnsForWidth_Tight(t *testing.T) {
	for _, w := range []int{28, 38, 44, 54, 64} {
		cols := optionsColumnsForWidth(w)
		total := 0
		for _, c := range cols {
			total += c.Width
		}
		if total > w {
			t.Errorf("at w=%d, total column width %d exceeds available", w, total)
		}
		// Symbol (first) should always be visible
		if cols[0].Width == 0 {
			t.Errorf("at w=%d, Symbol column should always be visible", w)
		}
	}
}

func TestRenderTablePane_Empty(t *testing.T) {
	cols := holdingsColumnsForWidth(80)
	tbl := table.New(
		table.WithColumns(cols),
		table.WithHeight(10),
	)
	result := renderTablePane(&tbl, 10, "TITLE", "empty message", true)
	if result == "" {
		t.Error("expected non-empty result")
	}
}

func TestRenderTablePane_NonEmpty(t *testing.T) {
	cols := holdingsColumnsForWidth(80)
	tbl := table.New(
		table.WithColumns(cols),
		table.WithHeight(10),
	)
	tbl.SetRows([]table.Row{
		{"SYM1", "10", "$100", "$10.00", "+5.00%"},
	})
	result := renderTablePane(&tbl, 10, "TITLE", "empty", false)
	if result == "" {
		t.Error("expected non-empty result")
	}
}
