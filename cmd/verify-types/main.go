// Throwaway: verifies api types parse against captured CLI JSON. Delete after use.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/shopspring/decimal"

	"github.com/ks1686/public-terminal/internal/api"
)

func dispDec(d *decimal.Decimal) string {
	if d == nil {
		return "—"
	}
	return d.StringFixed(4)
}

func main() {
	for _, path := range []string{"/tmp/pt-portfolio.json", "/tmp/pt-port2.json"} {
		b, err := os.ReadFile(path)
		if err != nil {
			fmt.Println(path, "read:", err)
			continue
		}
		var p api.Portfolio
		if err := json.Unmarshal(b, &p); err != nil {
			fmt.Println(path, "parse:", err)
			continue
		}
		fmt.Printf("\n== %s ==\n", path)
		fmt.Printf("Account=%s Type=%s\n", p.AccountID, p.AccountType)
		fmt.Printf("BP=%s CashOnly=%s Opt=%s\n",
			p.BuyingPower.BuyingPower.StringFixed(2),
			p.BuyingPower.CashOnlyBuyingPower.StringFixed(2),
			p.BuyingPower.OptionsBuyingPower.StringFixed(2))
		fmt.Printf("Equity rows: %d\n", len(p.Equity))
		for _, e := range p.Equity {
			fmt.Printf("  %s = %s (%s%%)\n", e.Type, e.Value.StringFixed(2), e.PercentageOfPortfolio.StringFixed(2))
		}
		fmt.Printf("Positions: %d\n", len(p.Positions))
		if len(p.Positions) > 0 {
			pos := p.Positions[0]
			fmt.Printf("  first: %s qty=%s value=%s\n",
				pos.Instrument.Symbol, pos.Quantity.StringFixed(4), dispDec(pos.CurrentValue))
			if pos.PositionDailyGain != nil {
				fmt.Printf("    daily gain pct=%s value=%s\n",
					dispDec(pos.PositionDailyGain.GainPercentage),
					dispDec(pos.PositionDailyGain.GainValue))
			}
		}
	}
}
