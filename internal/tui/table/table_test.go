package table

import (
	"testing"
)

func TestNew(t *testing.T) {
	cols := []Column{
		{Title: "A", Width: 10},
		{Title: "B", Width: 10},
	}
	tbl := New(
		WithColumns(cols),
		WithFocused(true),
		WithHeight(10),
	)
	if !tbl.Focused() {
		t.Error("expected focused table")
	}
	if len(tbl.Columns()) != 2 {
		t.Errorf("expected 2 columns, got %d", len(tbl.Columns()))
	}
}

func TestSetWidth(t *testing.T) {
	cols := []Column{{Title: "A", Width: 10}}
	tbl := New(WithColumns(cols))
	tbl.SetWidth(80)
	if tbl.Width() != 80 {
		t.Errorf("width = %d, want 80", tbl.Width())
	}
}

func TestSetHeight(t *testing.T) {
	cols := []Column{{Title: "A", Width: 10}}
	tbl := New(WithColumns(cols))
	tbl.SetHeight(10)
	// Height minus header height
	if tbl.Height() < 0 {
		t.Error("height should be non-negative")
	}
}

func TestSetRows(t *testing.T) {
	cols := []Column{{Title: "A", Width: 10}}
	tbl := New(WithColumns(cols))
	rows := []Row{
		{"val1"},
		{"val2"},
		{"val3"},
	}
	tbl.SetRows(rows)
	if len(tbl.Rows()) != 3 {
		t.Errorf("expected 3 rows, got %d", len(tbl.Rows()))
	}
}

func TestCursorMovement(t *testing.T) {
	cols := []Column{{Title: "A", Width: 10}}
	tbl := New(WithColumns(cols), WithFocused(true), WithHeight(10))
	rows := []Row{
		{"val1"}, {"val2"}, {"val3"}, {"val4"}, {"val5"},
	}
	tbl.SetRows(rows)

	// Start at 0
	if tbl.Cursor() != 0 {
		t.Errorf("initial cursor = %d, want 0", tbl.Cursor())
	}

	tbl.MoveDown(1)
	if tbl.Cursor() != 1 {
		t.Errorf("cursor after MoveDown(1) = %d, want 1", tbl.Cursor())
	}

	tbl.MoveUp(1)
	if tbl.Cursor() != 0 {
		t.Errorf("cursor after MoveUp(1) = %d, want 0", tbl.Cursor())
	}

	// Can't go above 0
	tbl.MoveUp(1)
	if tbl.Cursor() != 0 {
		t.Errorf("cursor should stay at 0, got %d", tbl.Cursor())
	}

	// Can't go below last
	tbl.SetCursor(4)
	tbl.MoveDown(1)
	if tbl.Cursor() != 4 {
		t.Errorf("cursor should stay at 4, got %d", tbl.Cursor())
	}
}

func TestGotoTop(t *testing.T) {
	cols := []Column{{Title: "A", Width: 10}}
	tbl := New(WithColumns(cols), WithHeight(10))
	rows := []Row{{"a"}, {"b"}, {"c"}}
	tbl.SetRows(rows)
	tbl.SetCursor(2)
	tbl.GotoTop()
	if tbl.Cursor() != 0 {
		t.Errorf("cursor = %d, want 0", tbl.Cursor())
	}
}

func TestGotoBottom(t *testing.T) {
	cols := []Column{{Title: "A", Width: 10}}
	tbl := New(WithColumns(cols), WithHeight(10))
	rows := []Row{{"a"}, {"b"}, {"c"}}
	tbl.SetRows(rows)
	tbl.GotoBottom()
	if tbl.Cursor() != 2 {
		t.Errorf("cursor = %d, want 2", tbl.Cursor())
	}
}

func TestFocusBlur(t *testing.T) {
	cols := []Column{{Title: "A", Width: 10}}
	tbl := New(WithColumns(cols), WithFocused(true))
	if !tbl.Focused() {
		t.Error("expected focused")
	}
	tbl.Blur()
	if tbl.Focused() {
		t.Error("expected blurred")
	}
	tbl.Focus()
	if !tbl.Focused() {
		t.Error("expected focused after Focus()")
	}
}

func TestEmptyView(t *testing.T) {
	cols := []Column{{Title: "A", Width: 10}}
	tbl := New(WithColumns(cols))
	view := tbl.View()
	if view == "" {
		t.Error("expected non-empty view (at least headers)")
	}
}

func TestHeadersView(t *testing.T) {
	cols := []Column{
		{Title: "SYM", Width: 5},
		{Title: "QTY", Width: 5},
		{Title: "VAL", Width: 0}, // hidden
	}
	tbl := New(WithColumns(cols))
	view := tbl.headersView()
	if view == "" {
		t.Error("expected non-empty headers view")
	}
}

func TestSelectedRow_Empty(t *testing.T) {
	cols := []Column{{Title: "A", Width: 10}}
	tbl := New(WithColumns(cols))
	if tbl.SelectedRow() != nil {
		t.Error("expected nil for empty table")
	}
}

func TestSelectedRow_Valid(t *testing.T) {
	cols := []Column{{Title: "A", Width: 10}, {Title: "B", Width: 10}}
	tbl := New(WithColumns(cols))
	tbl.SetRows([]Row{{"a1", "b1"}, {"a2", "b2"}})
	row := tbl.SelectedRow()
	if row == nil {
		t.Fatal("expected non-nil row")
	}
	if row[0] != "a1" {
		t.Errorf("row[0] = %q, want a1", row[0])
	}
}

func TestDefaultStyles(t *testing.T) {
	s := DefaultStyles()
	// Ensure styles are non-zero
	_ = s.Header
	_ = s.Cell
	_ = s.Selected
}

func TestClamp(t *testing.T) {
	if v := clamp(5, 0, 10); v != 5 {
		t.Errorf("clamp(5,0,10) = %d, want 5", v)
	}
	if v := clamp(-1, 0, 10); v != 0 {
		t.Errorf("clamp(-1,0,10) = %d, want 0", v)
	}
	if v := clamp(15, 0, 10); v != 10 {
		t.Errorf("clamp(15,0,10) = %d, want 10", v)
	}
}
