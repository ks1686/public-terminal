package components

import (
	"fmt"
	"strings"

	"github.com/ks1686/public-terminal/internal/config"
	"github.com/ks1686/public-terminal/internal/tui/theme"
)

// RebalancerStatus holds the display state of the rebalancer strip.
type RebalancerStatus struct {
	Active      bool
	Enabled     bool
	SkipPending bool
	LastRun     string
	NextRun     string
	Cfg         config.RebalanceConfig
}

// RebalancerModel renders the bottom status bar showing rebalancer state.
type RebalancerModel struct {
	Status RebalancerStatus
	Width  int
}

func NewRebalancerModel() RebalancerModel { return RebalancerModel{} }

func (m RebalancerModel) View() string {
	s := m.Status
	w := m.Width
	if w < 1 {
		w = 1
	}

	var parts []string

	indexStr := s.Cfg.Index
	if indexStr == "" {
		indexStr = "—"
	}
	parts = append(parts, fmt.Sprintf("Index: %s  Top: %d", indexStr, s.Cfg.TopN))

	if s.Enabled {
		parts = append(parts, theme.Positive.Render("Enabled"))
	} else {
		parts = append(parts, theme.Negative.Render("Disabled"))
	}

	if s.SkipPending {
		parts = append(parts, theme.Warning.Render("SKIP PENDING"))
	}

	if s.Active {
		parts = append(parts, theme.Warning.Render("● Running"))
	}

	if s.LastRun != "" {
		parts = append(parts, "Last: "+theme.Muted.Render(s.LastRun))
	}
	if s.NextRun != "" {
		parts = append(parts, "Next: "+theme.Muted.Render(s.NextRun))
	}

	sep := theme.Muted.Render("  │  ")
	line := strings.Join(parts, sep)
	return theme.RebalancerBar.Width(w).Render(line)
}
