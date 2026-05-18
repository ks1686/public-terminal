package rebalance

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestComputeUnallocatedBuyDelta(t *testing.T) {
	tests := []struct {
		name         string
		targetValue  decimal.Decimal
		currentValue decimal.Decimal
		threshold    decimal.Decimal
		want         decimal.Decimal
	}{
		{
			name:         "negative delta (target < current)",
			targetValue:  decimal.NewFromFloat(100.0),
			currentValue: decimal.NewFromFloat(120.0),
			threshold:    decimal.NewFromFloat(0.0),
			want:         decimal.Zero,
		},
		{
			name:         "zero delta (target == current)",
			targetValue:  decimal.NewFromFloat(100.0),
			currentValue: decimal.NewFromFloat(100.0),
			threshold:    decimal.NewFromFloat(0.0),
			want:         decimal.Zero,
		},
		{
			name:         "delta greater than driftThreshold (delta > 5.0 MinOrderDollars)",
			targetValue:  decimal.NewFromFloat(100.0), // RebalanceThresholdPct=0.005 -> 0.5
			currentValue: decimal.NewFromFloat(94.0),  // Delta = 6.0
			threshold:    decimal.NewFromFloat(0.0),   // Max(0.5, 5.0, 0.0) = 5.0
			want:         decimal.Zero,                // 6.0 > 5.0 -> Zero
		},
		{
			name:         "delta equals driftThreshold (delta == 5.0 MinOrderDollars)",
			targetValue:  decimal.NewFromFloat(100.0), // RebalanceThresholdPct=0.005 -> 0.5
			currentValue: decimal.NewFromFloat(95.0),  // Delta = 5.0
			threshold:    decimal.NewFromFloat(0.0),   // Max(0.5, 5.0, 0.0) = 5.0
			want:         decimal.NewFromFloat(5.0),   // 5.0 <= 5.0 -> 5.0
		},
		{
			name:         "delta less than driftThreshold (delta < 5.0 MinOrderDollars)",
			targetValue:  decimal.NewFromFloat(100.0), // RebalanceThresholdPct=0.005 -> 0.5
			currentValue: decimal.NewFromFloat(96.0),  // Delta = 4.0
			threshold:    decimal.NewFromFloat(0.0),   // Max(0.5, 5.0, 0.0) = 5.0
			want:         decimal.NewFromFloat(4.0),   // 4.0 <= 5.0 -> 4.0
		},
		{
			name:         "targetValue * RebalanceThresholdPct dominates",
			targetValue:  decimal.NewFromFloat(2000.0), // 2000 * 0.005 = 10.0
			currentValue: decimal.NewFromFloat(1991.0), // Delta = 9.0
			threshold:    decimal.NewFromFloat(0.0),    // Max(10.0, 5.0, 0.0) = 10.0
			want:         decimal.NewFromFloat(9.0),    // 9.0 <= 10.0 -> 9.0
		},
		{
			name:         "threshold parameter dominates",
			targetValue:  decimal.NewFromFloat(100.0), // 100 * 0.005 = 0.5
			currentValue: decimal.NewFromFloat(90.0),  // Delta = 10.0
			threshold:    decimal.NewFromFloat(15.0),  // Max(0.5, 5.0, 15.0) = 15.0
			want:         decimal.NewFromFloat(10.0),  // 10.0 <= 15.0 -> 10.0
		},
		{
			name:         "rounding check (RoundBank 2)",
			targetValue:  decimal.NewFromFloat(100.0),  // 100 * 0.005 = 0.5
			currentValue: decimal.NewFromFloat(96.125), // Delta = 3.875
			threshold:    decimal.NewFromFloat(0.0),    // Max(0.5, 5.0, 0.0) = 5.0
			want:         decimal.NewFromFloat(3.88),   // RoundBank(3.875, 2) -> 3.88
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeUnallocatedBuyDelta(tt.targetValue, tt.currentValue, tt.threshold)
			if !got.Equal(tt.want) {
				t.Errorf("ComputeUnallocatedBuyDelta() = %v, want %v", got, tt.want)
			}
		})
	}
}
