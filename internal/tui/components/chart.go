package components

import (
	"fmt"
	"strings"
	"time"

	"github.com/guptarohit/asciigraph"
	"github.com/shopspring/decimal"

	"github.com/ks1686/public-terminal/internal/api"
	"github.com/ks1686/public-terminal/internal/tui/theme"
)

// ChartModel renders the ASCII portfolio value chart.
// Live points are appended only from the Bubble Tea Update loop — no mutex needed.
type ChartModel struct {
	Bars      []api.Bar
	PeriodIdx int
	Width     int
	Height    int
	Symbol    string

	Live       bool
	livePoints []livePoint
}

type livePoint struct {
	t time.Time
	v float64
}

func NewChartModel(width, height int) ChartModel {
	return ChartModel{Width: width, Height: height}
}

// AppendLivePoint adds a portfolio equity value. Must be called from the Update loop.
func (m *ChartModel) AppendLivePoint(equity decimal.Decimal) {
	f, _ := equity.Float64()
	cutoff := time.Now().Add(-24 * time.Hour)
	m.livePoints = append(m.livePoints, livePoint{t: time.Now(), v: f})
	filtered := m.livePoints[:0]
	for _, p := range m.livePoints {
		if p.t.After(cutoff) {
			filtered = append(filtered, p)
		}
	}
	m.livePoints = filtered
}

func (m *ChartModel) ClearLive() {
	m.livePoints = nil
}

func (m ChartModel) PeriodLabel() string {
	if m.PeriodIdx < len(api.ChartPeriods) {
		return api.ChartPeriods[m.PeriodIdx].Label
	}
	return ""
}

func (m ChartModel) View() string {
	if m.Live {
		return m.viewLive()
	}
	return m.viewHistoric()
}

func (m ChartModel) viewLive() string {
	if len(m.livePoints) < 2 {
		return theme.Muted.Render("  Live chart — waiting for data…")
	}
	data := make([]float64, len(m.livePoints))
	for i, p := range m.livePoints {
		data[i] = p.v
	}
	title := theme.Title.Render("Portfolio — LIVE")
	return title + "\n" + renderGraph(data, m.Width, m.Height)
}

func (m ChartModel) viewHistoric() string {
	if len(m.Bars) == 0 {
		return theme.Muted.Render("  No chart data. Press [ / ] to change period.")
	}

	tabs := make([]string, len(api.ChartPeriods))
	for i, p := range api.ChartPeriods {
		if i == m.PeriodIdx {
			tabs[i] = theme.Title.Render("[" + p.Label + "]")
		} else {
			tabs[i] = theme.Muted.Render(" " + p.Label + " ")
		}
	}
	tabLine := strings.Join(tabs, " ")

	data := make([]float64, len(m.Bars))
	for i, b := range m.Bars {
		data[i] = b.Close
	}

	sym := m.Symbol
	if sym == "" {
		sym = "Portfolio"
	}
	title := fmt.Sprintf("%s  %s", theme.Title.Render(sym), tabLine)
	return title + "\n" + renderGraph(data, m.Width, m.Height-2)
}

func renderGraph(data []float64, width, height int) string {
	if height < 3 {
		height = 3
	}
	graphWidth := width - 12
	if graphWidth < 10 {
		graphWidth = 10
	}
	return asciigraph.Plot(data,
		asciigraph.Height(height),
		asciigraph.Width(graphWidth),
		asciigraph.Precision(2),
	)
}
