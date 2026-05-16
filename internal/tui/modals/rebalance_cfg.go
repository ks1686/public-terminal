package modals

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/shopspring/decimal"

	"github.com/ks1686/public-terminal/internal/config"
	"github.com/ks1686/public-terminal/internal/rebalance"
	"github.com/ks1686/public-terminal/internal/tui/theme"
)

// RebalanceCfgModal lets the user edit the rebalancer configuration. Allocation
// percentages are entered as integers 0–100 (mirrors Python). They're stored on
// disk as 0.0–1.0 decimals (also matches Python).
type RebalanceCfgModal struct {
	accountID string
	cfg       config.RebalanceConfig

	marginAvailable bool
	marginCapacity  decimal.Decimal

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

var allocLabels = map[string]string{
	"stocks": "Stocks",
	"btc":    "Bitcoin (BTC)",
	"eth":    "Ethereum (ETH)",
	"sol":    "Solana (SOL)",
	"gold":   "Gold (GLDM ETF)",
	"cash":   "Cash (uninvested)",
}

func NewRebalanceCfgModal(
	accountID string,
	cfg config.RebalanceConfig,
	marginAvailable bool,
	marginCapacity decimal.Decimal,
) RebalanceCfgModal {
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
		ti.Placeholder = "0–100"
		ti.SetValue(strconv.Itoa(int(math.Round(cfg.Allocations[k] * 100))))
		allocInputs[k] = ti
	}

	focusNames := []string{"topn", "margin", "exclude"}
	for _, k := range allocKeys {
		focusNames = append(focusNames, "alloc_"+k)
	}

	m := RebalanceCfgModal{
		accountID:       accountID,
		cfg:             cfg,
		marginAvailable: marginAvailable,
		marginCapacity:  marginCapacity,
		indexIdx:        indexIdx,
		topNInput:       topN,
		marginInput:     margin,
		excludeInput:    exclude,
		allocInputs:     allocInputs,
		enabledToggle:   cfg.RebalanceEnabled,
		focusNames:      focusNames,
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
			m.skipDisabled(1)
			m.refocus()
		case "shift+tab":
			m.focus = (m.focus + len(m.focusNames) - 1) % len(m.focusNames)
			m.skipDisabled(-1)
			m.refocus()
		case "[":
			m.indexIdx = (m.indexIdx + len(rebalance.SupportedIndexList) - 1) % len(rebalance.SupportedIndexList)
		case "]":
			m.indexIdx = (m.indexIdx + 1) % len(rebalance.SupportedIndexList)
		case " ":
			// Only toggle enable when not focused on an input; otherwise space
			// goes into the input.
			if m.focusNames[m.focus] == "" {
				m.enabledToggle = !m.enabledToggle
			}
		case "ctrl+e":
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
		if m.marginAvailable {
			m.marginInput, cmd = m.marginInput.Update(msg)
		}
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

// skipDisabled advances past the margin field while it's not editable.
func (m *RebalanceCfgModal) skipDisabled(dir int) {
	if m.marginAvailable {
		return
	}
	for m.focusNames[m.focus] == "margin" {
		m.focus = (m.focus + dir + len(m.focusNames)) % len(m.focusNames)
	}
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
		if m.marginAvailable {
			m.marginInput.Focus()
		}
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

func (m RebalanceCfgModal) parseAllocPcts() (map[string]int, int, string) {
	out := make(map[string]int, len(allocKeys))
	total := 0
	for _, k := range allocKeys {
		raw := strings.TrimSpace(m.allocInputs[k].Value())
		v, err := strconv.Atoi(raw)
		if err != nil {
			return out, total, fmt.Sprintf("%s %% must be a whole number 0–100.", allocLabels[k])
		}
		if v < 0 || v > 100 {
			return out, total, fmt.Sprintf("%s %% must be 0–100.", allocLabels[k])
		}
		out[k] = v
		total += v
	}
	return out, total, ""
}

func (m RebalanceCfgModal) trySave() tea.Cmd {
	return func() tea.Msg {
		topN, err := strconv.Atoi(strings.TrimSpace(m.topNInput.Value()))
		if err != nil || topN < 1 {
			return errMsg{fmt.Errorf("Top-N must be a whole number ≥ 1")}
		}
		var marginPct float64
		if m.marginAvailable {
			marginPct, err = strconv.ParseFloat(strings.TrimSpace(m.marginInput.Value()), 64)
			if err != nil || marginPct < 0 || marginPct > 1 {
				return errMsg{fmt.Errorf("Margin must be a number between 0.0 and 1.0")}
			}
		}

		var excluded []string
		for _, t := range strings.Split(m.excludeInput.Value(), ",") {
			t = strings.TrimSpace(strings.ToUpper(t))
			if t != "" {
				excluded = append(excluded, t)
			}
		}

		pcts, total, allocErr := m.parseAllocPcts()
		if allocErr != "" {
			return errMsg{fmt.Errorf("%s", allocErr)}
		}
		if total != 100 {
			return errMsg{fmt.Errorf("Allocations sum to %d%% — must equal 100%%", total)}
		}

		allocs := make(map[string]float64, len(pcts))
		for k, v := range pcts {
			// 4 decimal places matches Python's round(v/100, 4).
			allocs[k] = math.Round(float64(v)/100*10000) / 10000
		}

		cfg := config.RebalanceConfig{
			Index:            rebalance.SupportedIndexList[m.indexIdx],
			TopN:             topN,
			MarginUsagePct:   marginPct,
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
	lines := []string{theme.Title.Render("Rebalancer Settings"), ""}

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

	if m.marginAvailable {
		capacity := fmt.Sprintf("$%s", m.marginCapacity.StringFixed(2))
		lines = append(lines,
			"Margin:   "+m.marginInput.View()+"  "+
				theme.Muted.Render(fmt.Sprintf("(0.0=cash, 1.0=full | capacity %s)", capacity)),
		)
	} else {
		lines = append(lines,
			"Margin:   "+theme.Muted.Render("(disabled — account is cash-only)"),
		)
	}

	lines = append(lines, "Exclude:  "+m.excludeInput.View())

	enabledStr := theme.Negative.Render("disabled")
	if m.enabledToggle {
		enabledStr = theme.Positive.Render("enabled")
	}
	lines = append(lines, "Status:   "+enabledStr+"  "+theme.Muted.Render("(ctrl+e to toggle)"))

	pcts, total, allocErr := m.parseAllocPcts()
	_ = pcts
	totalLabel := fmt.Sprintf("  Total: %d%%", total)
	if allocErr != "" {
		totalLabel = "  " + allocErr
	} else if total == 100 {
		totalLabel = theme.Positive.Render(totalLabel + "  ✓")
	} else {
		totalLabel = theme.Negative.Render(totalLabel + "  — must equal 100%")
	}

	lines = append(lines, "")
	lines = append(lines, "Target Allocation (whole-number percentages):")
	for _, k := range allocKeys {
		lines = append(lines, fmt.Sprintf("  %-18s %s", allocLabels[k]+" %:", m.allocInputs[k].View()))
	}
	lines = append(lines, totalLabel)
	lines = append(lines, "")
	lines = append(lines, theme.Muted.Render("tab/shift+tab: navigate  ctrl+s: save  esc: cancel"))
	if m.err != "" {
		lines = append(lines, theme.StatusErr.Render(m.err))
	}
	return theme.ModalBox.Render(strings.Join(lines, "\n"))
}
