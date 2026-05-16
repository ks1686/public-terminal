package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestNewModel(t *testing.T) {
	accounts := []string{"ACCT001"}
	m := NewModel(accounts, 0)
	if m == nil {
		t.Fatal("NewModel returned nil")
	}
	if m.activeIdx != 0 {
		t.Errorf("activeIdx = %d, want 0", m.activeIdx)
	}
	if len(m.accounts) != 1 || m.accounts[0] != "ACCT001" {
		t.Errorf("accounts = %v, want [ACCT001]", m.accounts)
	}
	if !m.loading {
		t.Error("expected loading to be true initially")
	}
}

func TestModelInit(t *testing.T) {
	m := NewModel([]string{"ACCT001"}, 0)
	cmd := m.Init()
	if cmd == nil {
		t.Error("Init() returned nil command, expected batch")
	}
}

func TestPaneNavigation(t *testing.T) {
	// nextPane
	if n := nextPane(paneStocks); n != paneCrypto {
		t.Errorf("nextPane(stocks) = %d, want crypto", n)
	}
	if n := nextPane(paneCrypto); n != paneOptions {
		t.Errorf("nextPane(crypto) = %d, want options", n)
	}
	if n := nextPane(paneOptions); n != paneOrders {
		t.Errorf("nextPane(options) = %d, want orders", n)
	}
	if n := nextPane(paneOrders); n != paneStocks {
		t.Errorf("nextPane(orders) = %d, want stocks", n)
	}

	// prevPane
	if n := prevPane(paneStocks); n != paneOrders {
		t.Errorf("prevPane(stocks) = %d, want orders", n)
	}
	if n := prevPane(paneOrders); n != paneOptions {
		t.Errorf("prevPane(orders) = %d, want options", n)
	}

	// movePane
	if n := movePane(paneCrypto, "left"); n != paneStocks {
		t.Errorf("movePane(crypto, left) = %d, want stocks", n)
	}
	if n := movePane(paneStocks, "right"); n != paneCrypto {
		t.Errorf("movePane(stocks, right) = %d, want crypto", n)
	}
	if n := movePane(paneOptions, "up"); n != paneStocks {
		t.Errorf("movePane(options, up) = %d, want stocks", n)
	}
	if n := movePane(paneStocks, "down"); n != paneOptions {
		t.Errorf("movePane(stocks, down) = %d, want options", n)
	}
}

func TestSplitMainHeights(t *testing.T) {
	// Normal case
	topH, bottomH := splitMainHeights(40)
	if topH != 20 || bottomH != 20 {
		t.Errorf("splitMainHeights(40) = %d, %d; want 20, 20", topH, bottomH)
	}

	// Odd height
	topH, bottomH = splitMainHeights(41)
	if topH != 20 || bottomH != 21 {
		t.Errorf("splitMainHeights(41) = %d, %d; want 20, 21", topH, bottomH)
	}

	// Below minimum — split evenly, caller handles collapse
	topH, bottomH = splitMainHeights(10)
	if topH != 5 || bottomH != 5 {
		t.Errorf("splitMainHeights(10) = %d, %d; want 5, 5 (below min, caller collapses)", topH, bottomH)
	}
}

func TestTruncateForWidth(t *testing.T) {
	if s := truncateForWidth("hello", 3); s != "he…" {
		t.Errorf("truncateForWidth(hello, 3) = %q, want 'he…'", s)
	}
	if s := truncateForWidth("hi", 10); s != "hi" {
		t.Errorf("truncateForWidth(hi, 10) = %q, want 'hi'", s)
	}
	if s := truncateForWidth("", 5); s != "" {
		t.Errorf("truncateForWidth(empty, 5) = %q, want ''", s)
	}
	if s := truncateForWidth("test", 0); s != "" {
		t.Errorf("truncateForWidth(test, 0) = %q, want ''", s)
	}
}

func TestWindowSizeMsg(t *testing.T) {
	m := NewModel([]string{"ACCT001"}, 0)
	msg := tea.WindowSizeMsg{Width: 120, Height: 40}
	newM, cmd := m.Update(msg)
	if cmd != nil {
		t.Error("WindowSizeMsg should return nil command")
	}
	m2 := newM.(Model)
	if m2.width != 120 {
		t.Errorf("width = %d, want 120", m2.width)
	}
	if m2.height != 40 {
		t.Errorf("height = %d, want 40", m2.height)
	}
}
