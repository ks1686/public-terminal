// Package theme holds Lipgloss style variables shared across tui and tui/components.
package theme

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

var (
	ColorBlack  = lipgloss.Color("0")
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

	// Panes — mirror Python CSS: left-pane primary (cyan), right-pane accent (yellow).
	PaneLeft = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(ColorCyan)
	PaneRight = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(ColorYellow)

	// Section title labels inside panes — colored backgrounds, bold, height 1.
	PaneTitle = lipgloss.NewStyle().
			Background(ColorCyan).
			Foreground(ColorBlack).
			Bold(true)
	PaneTitleAccent = lipgloss.NewStyle().
			Background(ColorYellow).
			Foreground(ColorBlack).
			Bold(true)

	// Account tab strip — active vs. inactive.
	AccountTabActive = lipgloss.NewStyle().
				Background(ColorCyan).
				Foreground(ColorBlack).
				Bold(true).
				Padding(0, 1)
	AccountTabInactive = lipgloss.NewStyle().
				Foreground(ColorGray).
				Padding(0, 1)
)

// FormatGain returns a styled string for a positive or negative percentage.
func FormatGain(pct float64) string {
	if pct >= 0 {
		return Positive.Render(fmt.Sprintf("+%.2f%%", pct))
	}
	return Negative.Render(fmt.Sprintf("%.2f%%", pct))
}
