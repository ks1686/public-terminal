// Package theme holds Lipgloss style variables shared across tui and tui/components.
package theme

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

var (
	ColorGreen  = lipgloss.Color("2")
	ColorRed    = lipgloss.Color("1")
	ColorYellow = lipgloss.Color("3")
	ColorCyan   = lipgloss.Color("6")
	ColorGray   = lipgloss.Color("8")
	ColorWhite  = lipgloss.Color("15")

	Positive = lipgloss.NewStyle().Foreground(ColorGreen)
	Negative = lipgloss.NewStyle().Foreground(ColorRed)
	Warning  = lipgloss.NewStyle().Foreground(ColorYellow)
	Muted    = lipgloss.NewStyle().Foreground(ColorGray)
	Title    = lipgloss.NewStyle().Bold(true).Foreground(ColorCyan)

	BalanceBar = lipgloss.NewStyle().
			Background(lipgloss.Color("235")).
			Foreground(ColorWhite).
			Padding(0, 1)

	RebalancerBar = lipgloss.NewStyle().
			Background(lipgloss.Color("234")).
			Foreground(ColorGray).
			Padding(0, 1)

	TableHeader = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorCyan)

	SelectedRow = lipgloss.NewStyle().
			Background(lipgloss.Color("237")).
			Foreground(ColorWhite)

	ModalBox = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorCyan).
			Padding(1, 2)

	StatusOK  = lipgloss.NewStyle().Foreground(ColorGreen)
	StatusErr = lipgloss.NewStyle().Foreground(ColorRed)

	KeyHint = lipgloss.NewStyle().Foreground(ColorGray)
)

// FormatGain returns a styled string for a positive or negative percentage.
func FormatGain(pct float64) string {
	if pct >= 0 {
		return Positive.Render(fmt.Sprintf("+%.2f%%", pct))
	}
	return Negative.Render(fmt.Sprintf("%.2f%%", pct))
}
