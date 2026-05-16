package tui

import (
	"testing"
)

func TestKeyMapKeys(t *testing.T) {
	km := DefaultKeyMap
	tests := []struct {
		name string
		key  string
	}{
		{"Quit", km.Quit.Help().Key},
		{"Refresh", km.Refresh.Help().Key},
		{"PaneNext", km.PaneNext.Help().Key},
		{"PanePrev", km.PanePrev.Help().Key},
		{"Buy", km.Buy.Help().Key},
		{"Sell", km.Sell.Help().Key},
		{"ViewOrder", km.ViewOrder.Help().Key},
		{"Cancel", km.Cancel.Help().Key},
		{"History", km.History.Help().Key},
	}
	for _, tc := range tests {
		if tc.key == "" {
			t.Errorf("%s key binding has empty key", tc.name)
		}
	}
}

func TestKeyMapAllBindings(t *testing.T) {
	km := DefaultKeyMap
	// Ensure all bindings have keys
	bindings := []struct{ name, key string }{
		{"Quit", km.Quit.Help().Key},
		{"Refresh", km.Refresh.Help().Key},
		{"ToggleTimer", km.ToggleTimer.Help().Key},
		{"InstallSvc", km.InstallSvc.Help().Key},
		{"SkipRebalance", km.SkipRebalance.Help().Key},
		{"RebalanceNow", km.RebalanceNow.Help().Key},
		{"RebalanceCfg", km.RebalanceCfg.Help().Key},
		{"PrevAccount", km.PrevAccount.Help().Key},
		{"NextAccount", km.NextAccount.Help().Key},
		{"ManageAccts", km.ManageAccts.Help().Key},
	}
	for _, b := range bindings {
		if b.key == "" {
			t.Errorf("%s binding has empty key", b.name)
		}
	}
}

func TestPaneNavigationFunctions(t *testing.T) {
	// Test nextPane wraps around
	if n := nextPane(paneStocks); n != paneCrypto {
		t.Errorf("nextPane(stocks) = %d", n)
	}
	if n := nextPane(paneOrders); n != paneStocks {
		t.Errorf("nextPane(orders) = %d, want stocks (wrap)", n)
	}
	// Test prevPane wraps around
	if n := prevPane(paneStocks); n != paneOrders {
		t.Errorf("prevPane(stocks) = %d, want orders (wrap)", n)
	}
}
