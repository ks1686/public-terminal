package modals

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/ks1686/public-terminal/internal/config"
	"github.com/ks1686/public-terminal/internal/rebalance"
	"github.com/ks1686/public-terminal/internal/tui/theme"
)

// RebalanceCfgModal lets the user edit the rebalancer configuration.
type RebalanceCfgModal struct {
	accountID string
	cfg       config.RebalanceConfig

	indexIdx      int
	topNInput     textinput.Model
	marginInput   textinput.Model
	excludeInput  textinput.Model
	allocInputs   map[string]textinput.Model
	enabledToggle bool

	focus      int
	focusNames []string
	err        string
}

type RebalanceCfgSavedMsg struct{ Cfg config.RebalanceConfig }
type RebalanceCfgClosedMsg struct{}

var allocKeys = []string{"stocks", "btc", "eth", "sol", "gold", "cash"}

func NewRebalanceCfgModal(accountID string, cfg config.RebalanceConfig) RebalanceCfgModal {
	indexIdx := 0
	for i, idx := range rebalance.SupportedIndexList {
		if idx == cfg.Index {
			indexIdx = i
			break
		}
	}

	topN := textinput.New()
	topN.Placeholder = "e.g. 500"
	topN.SetValue(strconv.Itoa(cfg.TopN))

	margin := textinput.New()
	margin.Placeholder = "0.0–1.0"
	margin.SetValue(fmt.Sprintf("%.2f", cfg.MarginUsagePct))

	exclude := textinput.New()
	exclude.Placeholder = "comma-separated tickers to exclude"
	exclude.SetValue(strings.Join(cfg.ExcludedTickers, ", "))

	if cfg.Allocations == nil {
		cfg.Allocations = config.DefaultAllocations
	}

	allocInputs := make(map[string]textinput.Model, len(allocKeys))
	for _, k := range allocKeys {
		ti := textinput.New()
		ti.Placeholder = "0.00"
		ti.SetValue(fmt.Sprintf("%.2f", cfg.Allocations[k]))
		allocInputs[k] = ti
	}

	focusNames := []string{"topn", "margin", "exclude"}
	for _, k := range allocKeys {
		focusNames = append(focusNames, "alloc_"+k)
	}

	m := RebalanceCfgModal{
		accountID:     accountID,
		cfg:           cfg,
		indexIdx:      indexIdx,
		topNInput:     topN,
		marginInput:   margin,
		excludeInput:  exclude,
		allocInputs:   allocInputs,
		enabledToggle: cfg.RebalanceEnabled,
		focusNames:    focusNames,
	}
	m.refocus()
	return m
}

func (m RebalanceCfgModal) Init() tea.Cmd { return textinput.Blink }

func (m RebalanceCfgModal) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return m, func() tea.Msg { return RebalanceCfgClosedMsg{} }
		case "tab":
			m.focus = (m.focus + 1) % len(m.focusNames)
			m.refocus()
		case "shift+tab":
			m.focus = (m.focus + len(m.focusNames) - 1) % len(m.focusNames)
			m.refocus()
		case "[":
			m.indexIdx = (m.indexIdx + len(rebalance.SupportedIndexList) - 1) % len(rebalance.SupportedIndexList)
		case "]":
			m.indexIdx = (m.indexIdx + 1) % len(rebalance.SupportedIndexList)
		case " ":
			m.enabledToggle = !m.enabledToggle
		case "ctrl+s":
			return m, m.trySave()
		}
	}

	var cmd tea.Cmd
	name := m.focusNames[m.focus]
	switch name {
	case "topn":
		m.topNInput, cmd = m.topNInput.Update(msg)
	case "margin":
		m.marginInput, cmd = m.marginInput.Update(msg)
	case "exclude":
		m.excludeInput, cmd = m.excludeInput.Update(msg)
	default:
		key := strings.TrimPrefix(name, "alloc_")
		if ti, ok := m.allocInputs[key]; ok {
			ti, cmd = ti.Update(msg)
			m.allocInputs[key] = ti
		}
	}
	return m, cmd
}

func (m *RebalanceCfgModal) refocus() {
	m.topNInput.Blur()
	m.marginInput.Blur()
	m.excludeInput.Blur()
	for k, ti := range m.allocInputs {
		ti.Blur()
		m.allocInputs[k] = ti
	}
	name := m.focusNames[m.focus]
	switch name {
	case "topn":
		m.topNInput.Focus()
	case "margin":
		m.marginInput.Focus()
	case "exclude":
		m.excludeInput.Focus()
	default:
		key := strings.TrimPrefix(name, "alloc_")
		if ti, ok := m.allocInputs[key]; ok {
			ti.Focus()
			m.allocInputs[key] = ti
		}
	}
}

func (m RebalanceCfgModal) trySave() tea.Cmd {
	return func() tea.Msg {
		topN, err := strconv.Atoi(strings.TrimSpace(m.topNInput.Value()))
		if err != nil || topN < 1 {
			return errMsg{fmt.Errorf("invalid top-N")}
		}
		margin, err := strconv.ParseFloat(strings.TrimSpace(m.marginInput.Value()), 64)
		if err != nil || margin < 0 || margin > 1 {
			return errMsg{fmt.Errorf("margin must be 0.0–1.0")}
		}

		var excluded []string
		for _, t := range strings.Split(m.excludeInput.Value(), ",") {
			t = strings.TrimSpace(strings.ToUpper(t))
			if t != "" {
				excluded = append(excluded, t)
			}
		}

		allocs := make(map[string]float64)
		var sum float64
		for _, k := range allocKeys {
			v, err := strconv.ParseFloat(strings.TrimSpace(m.allocInputs[k].Value()), 64)
			if err != nil {
				return errMsg{fmt.Errorf("invalid allocation for %s", k)}
			}
			allocs[k] = v
			sum += v
		}
		if sum < 0.99 || sum > 1.01 {
			return errMsg{fmt.Errorf("allocations must sum to 1.0 (got %.2f)", sum)}
		}

		cfg := config.RebalanceConfig{
			Index:            rebalance.SupportedIndexList[m.indexIdx],
			TopN:             topN,
			MarginUsagePct:   margin,
			ExcludedTickers:  excluded,
			Allocations:      allocs,
			RebalanceEnabled: m.enabledToggle,
		}
		if err := config.SaveRebalanceConfig(m.accountID, cfg); err != nil {
			return errMsg{err}
		}
		return RebalanceCfgSavedMsg{Cfg: cfg}
	}
}

func (m RebalanceCfgModal) View() string {
	lines := []string{
		theme.Title.Render("Rebalancer Settings"),
		"",
	}

	indexTabs := make([]string, len(rebalance.SupportedIndexList))
	for i, idx := range rebalance.SupportedIndexList {
		if i == m.indexIdx {
			indexTabs[i] = theme.Title.Render("[" + idx + "]")
		} else {
			indexTabs[i] = theme.Muted.Render(" " + idx + " ")
		}
	}
	lines = append(lines, "Index:    "+strings.Join(indexTabs, " ")+"  ([ / ])")

	lines = append(lines, "Top-N:    "+m.topNInput.View())
	lines = append(lines, "Margin:   "+m.marginInput.View())
	lines = append(lines, "Exclude:  "+m.excludeInput.View())

	enabledStr := theme.Negative.Render("disabled")
	if m.enabledToggle {
		enabledStr = theme.Positive.Render("enabled")
	}
	lines = append(lines, "Status:   "+enabledStr+"  (space to toggle)")
	lines = append(lines, "")
	lines = append(lines, "Allocations (must sum to 1.0):")
	for _, k := range allocKeys {
		lines = append(lines, fmt.Sprintf("  %-6s %s", k+":", m.allocInputs[k].View()))
	}
	lines = append(lines, "")
	lines = append(lines, theme.Muted.Render("tab/shift+tab: navigate  ctrl+s: save  esc: cancel"))
	if m.err != "" {
		lines = append(lines, theme.StatusErr.Render(m.err))
	}
	return theme.ModalBox.Render(strings.Join(lines, "\n"))
}
