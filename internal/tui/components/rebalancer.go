package components

import (
	"fmt"
	"strings"

	"github.com/ks1686/public-terminal/internal/config"
	"github.com/ks1686/public-terminal/internal/tui/theme"
)

// RebalancerStatus holds the display state of the rebalancer strip.
type RebalancerStatus struct {
	SvcInstalled bool // systemd timer unit is installed
	SvcActive    bool // systemd timer unit is currently active
	SvcEnabled   bool // systemd timer unit is enabled
	SkipPending  bool
	LastRun      string
	NextRun      string
	Cfg          config.RebalanceConfig
}

// RebalancerModel renders the bottom status bar showing rebalancer state.
type RebalancerModel struct {
	Status RebalancerStatus
	Width  int
}

func NewRebalancerModel() RebalancerModel { return RebalancerModel{} }

func (m RebalancerModel) View() string {
	s := m.Status

	var parts []string

	indexStr := s.Cfg.Index
	if indexStr == "" {
		indexStr = "—"
	}
	parts = append(parts, fmt.Sprintf("Index: %s  Top: %d", indexStr, s.Cfg.TopN))

	// Config toggle: shows whether rebalancing is enabled in settings (independent of systemd).
	if s.Cfg.RebalanceEnabled {
		parts = append(parts, theme.Positive.Render("Enabled"))
	} else {
		parts = append(parts, theme.Negative.Render("Disabled"))
	}

	// Systemd schedule state: separate from the config toggle.
	if s.SvcInstalled {
		if s.SvcEnabled {
			if s.SvcActive {
				parts = append(parts, theme.Positive.Render("● Running"))
			} else {
				parts = append(parts, theme.Warning.Render("Ⅱ Paused"))
			}
		} else {
			parts = append(parts, theme.Muted.Render("Not scheduled"))
		}
	} else {
		parts = append(parts, theme.Muted.Render("No schedule"))
	}

	if s.SkipPending {
		parts = append(parts, theme.Warning.Render("SKIP PENDING"))
	}

	if s.LastRun != "" {
		parts = append(parts, "Last: "+theme.Muted.Render(s.LastRun))
	}
	if s.NextRun != "" {
		parts = append(parts, "Next: "+theme.Muted.Render(s.NextRun))
	}

	sep := theme.Muted.Render("  │  ")
	line := strings.Join(parts, sep)
	return line
}
