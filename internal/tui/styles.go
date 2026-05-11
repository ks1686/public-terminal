package tui

import "github.com/ks1686/public-terminal/internal/tui/theme"

// Re-export theme styles for use within the tui package.
var (
	StylePositive      = theme.Positive
	StyleNegative      = theme.Negative
	StyleWarning       = theme.Warning
	StyleMuted         = theme.Muted
	StyleTitle         = theme.Title
	StyleBalanceBar    = theme.BalanceBar
	StyleRebalancerBar = theme.RebalancerBar
	StyleTableHeader   = theme.TableHeader
	StyleSelectedRow   = theme.SelectedRow
	StyleModalBox      = theme.ModalBox
	StyleStatusOK      = theme.StatusOK
	StyleStatusErr     = theme.StatusErr
	StyleKeyHint       = theme.KeyHint
)

// FormatGain delegates to theme.
func FormatGain(pct float64) string { return theme.FormatGain(pct) }
