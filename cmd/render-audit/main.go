// Throwaway: renders every modal + component with realistic data so the views
// can be eyeballed without a TTY. Delete after use.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/shopspring/decimal"

	"github.com/ks1686/public-terminal/internal/api"
	"github.com/ks1686/public-terminal/internal/config"
	"github.com/ks1686/public-terminal/internal/tui/components"
	"github.com/ks1686/public-terminal/internal/tui/modals"
)

func sec(name string) {
	fmt.Printf("\n══════════════════════ %s ══════════════════════\n", name)
}

func loadPortfolio(path string) *api.Portfolio {
	b, err := os.ReadFile(path)
	if err != nil {
		fmt.Println("read", path, ":", err)
		return nil
	}
	var p api.Portfolio
	if err := json.Unmarshal(b, &p); err != nil {
		fmt.Println("parse", path, ":", err)
		return nil
	}
	return &p
}

func dec(s string) *decimal.Decimal {
	d, _ := decimal.NewFromString(s)
	return &d
}

func main() {
	cashAcct := loadPortfolio("/tmp/pt-portfolio.json")
	marginAcct := loadPortfolio("/tmp/pt-port2.json")

	width, height := 180, 50

	// ── Components ──
	sec("BalanceModel — cash account")
	if cashAcct != nil {
		bm := components.NewBalanceModel()
		bm.FromPortfolio(cashAcct, "5OD10524")
		bm.Width = width
		fmt.Println(bm.View())
	}

	sec("BalanceModel — margin account")
	if marginAcct != nil {
		bm := components.NewBalanceModel()
		bm.FromPortfolio(marginAcct, "5OP95222")
		bm.Width = width
		fmt.Println(bm.View())
	}

	sec("HoldingsModel — cash account (27 positions)")
	if cashAcct != nil {
		hm := components.NewHoldingsModel()
		hm.FromPortfolio(cashAcct)
		fmt.Println(hm.ViewWithHeight(15))
	}

	sec("HoldingsModel — empty")
	{
		hm := components.NewHoldingsModel()
		hm.FromPortfolio(&api.Portfolio{})
		fmt.Println(hm.ViewWithHeight(15))
	}

	sec("OptionsModel — empty (likely none in test data)")
	{
		om := components.NewOptionsModel()
		if cashAcct != nil {
			om.FromPortfolio(cashAcct)
		}
		fmt.Println(om.ViewWithHeight(10))
	}

	sec("OrdersModel — empty")
	{
		om := components.NewOrdersModel()
		if cashAcct != nil {
			om.FromPortfolio(cashAcct)
		}
		fmt.Println(om.ViewWithHeight(10))
	}

	sec("OrdersModel — synthetic open orders")
	{
		om := components.NewOrdersModel()
		p := &api.Portfolio{Orders: []api.Order{
			{OrderID: "ord_1", Side: "BUY", Type: "MARKET", Status: "NEW",
				Instrument: api.Instrument{Symbol: "AAPL"},
				Quantity:   dec("3.5"), NotionalValue: dec("550.25")},
			{OrderID: "ord_2", Side: "SELL", Type: "LIMIT", Status: "PARTIALLY_FILLED",
				Instrument: api.Instrument{Symbol: "TSLA"},
				Quantity:   dec("10"), LimitPrice: dec("245.50")},
		}}
		om.FromPortfolio(p)
		fmt.Println(om.ViewWithHeight(8))
	}

	sec("RebalancerModel — sample")
	{
		rm := components.NewRebalancerModel()
		rm.Width = width
		rm.Status.Cfg = config.RebalanceConfig{Index: "SP500", TopN: 500, RebalanceEnabled: true}
		rm.Status.SvcInstalled = true
		rm.Status.SvcEnabled = true
		rm.Status.SvcActive = true
		rm.Status.LastRun = "Mon 2026-05-12 12:00 EST"
		rm.Status.NextRun = "Tue 2026-05-13 12:00 EST"
		fmt.Println(rm.View())
	}

	sec("RebalancerModel — paused")
	{
		rm := components.NewRebalancerModel()
		rm.Width = width
		rm.Status.Cfg = config.RebalanceConfig{Index: "FTSE_GLOBAL_ALL_CAP", TopN: 25}
		rm.Status.SvcInstalled = true
		rm.Status.SvcEnabled = false
		rm.Status.SkipPending = true
		fmt.Println(rm.View())
	}

	// ── Modals ──
	sec("SetupModal")
	fmt.Println(modals.NewSetupModal().View())

	sec("OrderModal — BUY")
	{
		om := modals.NewOrderModal(nil, "BUY", "AAPL", "EQUITY")
		fmt.Println(om.View())
	}

	sec("OrderModal — SELL")
	{
		om := modals.NewOrderModal(nil, "SELL", "TSLA", "EQUITY")
		fmt.Println(om.View())
	}

	sec("CancelModal")
	{
		cm := modals.NewCancelModal(nil, "ord_abc123", "AAPL")
		fmt.Println(cm.View())
	}

	sec("AccountsModal — list mode")
	{
		am := modals.NewAccountsModal([]string{"5OD10524", "5OP95222"})
		fmt.Println(am.View())
	}

	sec("HistoryModal — empty")
	fmt.Println(modals.NewHistoryModal(nil, false, width, 30).View())

	sec("HistoryModal — sample data")
	{
		entries := []api.HistoryEntry{
			{Timestamp: "2026-05-15T14:30:00Z", Type: "ORDER_FILLED",
				Symbol: "AAPL", Side: "BUY", Quantity: dec("3.5"), NetAmount: dec("612.50")},
			{Timestamp: "2026-05-15T13:15:00Z", Type: "MONEY_MOVEMENT",
				Symbol: "", Side: "", NetAmount: dec("267.49"), Description: "Cash adjusted"},
			{Timestamp: "2026-05-14T09:00:00Z", Type: "ORDER_FILLED",
				Symbol: "TSLA", Side: "SELL", Quantity: dec("10"), NetAmount: dec("-2455.00")},
		}
		fmt.Println(modals.NewHistoryModal(entries, false, width, 30).View())
	}

	sec("RebalanceCfgModal — margin available")
	{
		cfg := config.LoadRebalanceConfig("5OP95222")
		fmt.Println(modals.NewRebalanceCfgModal("5OP95222", cfg, true, decimal.RequireFromString("4622.58")).View())
	}

	sec("RebalanceCfgModal — cash-only account (margin disabled)")
	{
		cfg := config.LoadRebalanceConfig("5OD10524")
		fmt.Println(modals.NewRebalanceCfgModal("5OD10524", cfg, false, decimal.Zero).View())
	}

	sec("OrderDetailsModal — STOP_LIMIT")
	{
		o := api.Order{
			OrderID:    "ord_sl_001",
			Side:       "BUY",
			Type:       "STOP_LIMIT",
			Status:     "NEW",
			Instrument: api.Instrument{Symbol: "NVDA"},
			Quantity:   dec("2"),
			LimitPrice: dec("142.00"),
			StopPrice:  dec("140.00"),
		}
		fmt.Println(modals.NewOrderDetailsModal(o).View())
	}

	_ = height
}
