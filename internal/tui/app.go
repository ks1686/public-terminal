package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ks1686/public-terminal/internal/api"
	"github.com/ks1686/public-terminal/internal/config"
	"github.com/ks1686/public-terminal/internal/rebalance"
	"github.com/ks1686/public-terminal/internal/tui/components"
	"github.com/ks1686/public-terminal/internal/tui/modals"
	"github.com/ks1686/public-terminal/internal/tui/theme"
)

// ─────────────────────────────────────────────────────────────────────────────
// Messages
// ─────────────────────────────────────────────────────────────────────────────

type portfolioLoadedMsg struct{ p *api.Portfolio }
type historyLoadedMsg struct{ entries []api.HistoryEntry }
type chartLoadedMsg struct {
	bars   []api.Bar
	symbol string
}
type rebalancerRunningMsg struct{ running bool }
type liveTickMsg struct{}
type appErrMsg struct {
	err error
	ctx string
}
type rebalancerStatusMsg struct {
	cfg         config.RebalanceConfig
	skipPending bool
}

// ─────────────────────────────────────────────────────────────────────────────
// Root model
// ─────────────────────────────────────────────────────────────────────────────

type Model struct {
	keys    KeyMap
	clients map[string]*api.Client

	accounts   []string
	activeIdx  int
	portfolio  *api.Portfolio
	loading    bool
	liveActive bool

	width  int
	height int

	balance    components.BalanceModel
	holdings   components.HoldingsModel
	opts       components.OptionsModel
	orders     components.OrdersModel
	chart      components.ChartModel
	rebalancer components.RebalancerModel
	spin       spinner.Model

	modal tea.Model

	rebalancerRunning bool
	rebalanceCfg      config.RebalanceConfig
	skipPending       bool

	status      string
	statusIsErr bool
}

func NewModel(accounts []string, activeIdx int) *Model {
	if activeIdx < 0 || activeIdx >= len(accounts) {
		activeIdx = 0
	}
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(theme.ColorCyan)

	m := &Model{
		keys:       DefaultKeyMap,
		accounts:   accounts,
		activeIdx:  activeIdx,
		loading:    true,
		spin:       sp,
		holdings:   components.NewHoldingsModel(),
		opts:       components.NewOptionsModel(),
		orders:     components.NewOrdersModel(),
		chart:      components.NewChartModel(),
		balance:    components.NewBalanceModel(),
		rebalancer: components.NewRebalancerModel(),
	}
	m.initClients()
	return m
}

func (m *Model) activeAccount() string {
	if len(m.accounts) == 0 {
		return ""
	}
	return m.accounts[m.activeIdx]
}

func (m *Model) activeClient() *api.Client {
	if m.clients == nil {
		return nil
	}
	return m.clients[m.activeAccount()]
}

func (m *Model) initClients() {
	m.clients = make(map[string]*api.Client)
	for _, acct := range m.accounts {
		c, err := api.NewClient(acct, config.EnvFile())
		if err == nil {
			m.clients[acct] = c
		}
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.spin.Tick,
		m.loadPortfolio(),
		m.loadRebalancerStatus(),
	)
}

// ─────────────────────────────────────────────────────────────────────────────
// Commands
// ─────────────────────────────────────────────────────────────────────────────

func (m *Model) loadPortfolio() tea.Cmd {
	client := m.activeClient()
	if client == nil {
		return func() tea.Msg {
			return appErrMsg{
				err: fmt.Errorf("public CLI not found — install with: uv tool install publicdotcom-cli"),
				ctx: "client",
			}
		}
	}
	return func() tea.Msg {
		p, err := client.GetPortfolio()
		if err != nil {
			return appErrMsg{err: err, ctx: "portfolio"}
		}
		return portfolioLoadedMsg{p: p}
	}
}

func (m *Model) loadHistory() tea.Cmd {
	client := m.activeClient()
	if client == nil {
		return nil
	}
	return func() tea.Msg {
		entries, err := client.ListHistory(1000)
		if err != nil {
			return appErrMsg{err: err, ctx: "history"}
		}
		return historyLoadedMsg{entries: entries}
	}
}

func (m *Model) loadRebalancerStatus() tea.Cmd {
	acct := m.activeAccount()
	return func() tea.Msg {
		cfg := config.LoadRebalanceConfig(acct)
		_, skipErr := os.Stat(config.SkipFilePath(acct))
		return rebalancerStatusMsg{cfg: cfg, skipPending: skipErr == nil}
	}
}

func (m *Model) loadChartData() tea.Cmd {
	client := m.activeClient()
	if client == nil {
		return nil
	}
	sym := m.holdings.SelectedSymbol()
	if sym == "" {
		return func() tea.Msg {
			return appErrMsg{err: fmt.Errorf("select a holding first"), ctx: "chart"}
		}
	}
	p := api.ChartPeriods[m.chart.PeriodIdx]
	return func() tea.Msg {
		bars, err := client.GetHistoricBars(sym, p.Period, p.Aggregation)
		if err != nil {
			return appErrMsg{err: err, ctx: "chart"}
		}
		return chartLoadedMsg{bars: bars, symbol: sym}
	}
}

func (m *Model) runRebalanceAsync(dryRun bool) tea.Cmd {
	acct := m.activeAccount()
	return func() tea.Msg {
		_ = rebalance.Run(acct, dryRun)
		return rebalancerRunningMsg{running: false}
	}
}

func liveTick() tea.Cmd {
	return tea.Tick(30*time.Second, func(time.Time) tea.Msg { return liveTickMsg{} })
}

// ─────────────────────────────────────────────────────────────────────────────
// Update
// ─────────────────────────────────────────────────────────────────────────────

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.modal != nil {
		return m.updateModal(msg)
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.balance.Width = m.width
		m.rebalancer.Width = m.width
		m.chart.Width = m.width
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case portfolioLoadedMsg:
		m.loading = false
		m.portfolio = msg.p
		m.applyPortfolio()
		return m, nil

	case historyLoadedMsg:
		m.modal = modals.NewHistoryModal(msg.entries, m.width, m.height)
		return m, nil

	case chartLoadedMsg:
		m.chart.Bars = msg.bars
		m.chart.Symbol = msg.symbol
		return m, nil

	case rebalancerRunningMsg:
		m.rebalancerRunning = false
		m.rebalancer.Status.Active = false
		m.rebalancer.Status.LastRun = time.Now().Format("15:04:05")
		return m, m.loadPortfolio()

	case liveTickMsg:
		if m.liveActive {
			return m, tea.Batch(liveTick(), m.loadPortfolio())
		}
		return m, nil

	case appErrMsg:
		m.loading = false
		m.status = fmt.Sprintf("%s: %v", msg.ctx, msg.err)
		m.statusIsErr = true
		return m, nil

	case rebalancerStatusMsg:
		m.rebalanceCfg = msg.cfg
		m.skipPending = msg.skipPending
		m.rebalancer.Status.Cfg = msg.cfg
		m.rebalancer.Status.Enabled = msg.cfg.RebalanceEnabled
		m.rebalancer.Status.SkipPending = msg.skipPending
		return m, nil

	case spinner.TickMsg:
		if m.loading {
			var cmd tea.Cmd
			m.spin, cmd = m.spin.Update(msg)
			return m, cmd
		}
		return m, nil
	}

	// Forward unrecognized messages (mostly nav keys) to the holdings table,
	// which is the primary selection (matches Python's HoldingsTable focus).
	var cmd tea.Cmd
	m.holdings, cmd = m.holdings.Update(msg)
	return m, cmd
}

// ─────────────────────────────────────────────────────────────────────────────
// Modal routing
// ─────────────────────────────────────────────────────────────────────────────

func (m Model) updateModal(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.modal.Update(msg)
	m.modal = next

	switch msg := msg.(type) {
	case modals.SetupDoneMsg:
		m.modal = nil
		m.initClients()
		m.loading = true
		return m, tea.Batch(m.spin.Tick, m.loadPortfolio())

	case modals.OrderPlacedMsg:
		m.modal = nil
		m.status = fmt.Sprintf("Order placed: %s", msg.Symbol)
		m.statusIsErr = false
		return m, m.loadPortfolio()

	case modals.OrderCancelledMsg:
		m.modal = nil
		return m, nil

	case modals.OrderCancelledConfirmMsg:
		m.modal = nil
		m.status = "Order cancelled."
		m.statusIsErr = false
		return m, m.loadPortfolio()

	case modals.HistoryClosedMsg:
		m.modal = nil
		return m, nil

	case modals.AccountsClosedMsg:
		m.modal = nil
		return m, nil

	case modals.AccountsUpdatedMsg:
		m.modal = nil
		m.accounts = msg.Accounts
		if m.activeIdx >= len(m.accounts) {
			m.activeIdx = len(m.accounts) - 1
		}
		m.initClients()
		m.loading = true
		return m, tea.Batch(m.spin.Tick, m.loadPortfolio())

	case modals.RebalanceCfgSavedMsg:
		m.modal = nil
		m.rebalanceCfg = msg.Cfg
		m.rebalancer.Status.Cfg = msg.Cfg
		m.rebalancer.Status.Enabled = msg.Cfg.RebalanceEnabled
		m.status = "Rebalancer config saved."
		m.statusIsErr = false
		return m, nil

	case modals.RebalanceCfgClosedMsg:
		m.modal = nil
		return m, nil
	}

	return m, cmd
}

// ─────────────────────────────────────────────────────────────────────────────
// Key handling
// ─────────────────────────────────────────────────────────────────────────────

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	km := m.keys

	switch {
	case key.Matches(msg, km.Quit):
		return m, tea.Quit

	case key.Matches(msg, km.Refresh):
		m.loading = true
		m.status = ""
		return m, tea.Batch(m.spin.Tick, m.loadPortfolio(), m.loadRebalancerStatus())

	case key.Matches(msg, km.Buy):
		if client := m.activeClient(); client != nil {
			m.modal = modals.NewOrderModal(client, "BUY", m.holdings.SelectedSymbol(), "EQUITY")
		}
		return m, nil

	case key.Matches(msg, km.Sell):
		if client := m.activeClient(); client != nil {
			m.modal = modals.NewOrderModal(client, "SELL", m.holdings.SelectedSymbol(), "EQUITY")
		}
		return m, nil

	case key.Matches(msg, km.Cancel):
		orderID := m.orders.SelectedOrderID()
		if orderID == "" {
			m.status = "No open order selected."
			m.statusIsErr = false
			return m, nil
		}
		row := m.orders.SelectedRow()
		sym := ""
		if len(row) > 0 {
			sym = row[0]
		}
		if client := m.activeClient(); client != nil {
			m.modal = modals.NewCancelModal(client, orderID, sym)
		}
		return m, nil

	case key.Matches(msg, km.History):
		return m, m.loadHistory()

	case key.Matches(msg, km.ToggleLive):
		m.liveActive = !m.liveActive
		m.chart.Live = m.liveActive
		if m.liveActive {
			m.status = "Live chart on."
			return m, liveTick()
		}
		m.status = "Live chart off."
		return m, nil

	case key.Matches(msg, km.ChartPrev):
		if m.chart.PeriodIdx > 0 {
			m.chart.PeriodIdx--
		}
		return m, m.loadChartData()

	case key.Matches(msg, km.ChartNext):
		if m.chart.PeriodIdx < len(api.ChartPeriods)-1 {
			m.chart.PeriodIdx++
		}
		return m, m.loadChartData()

	case key.Matches(msg, km.SkipRebalance):
		acct := m.activeAccount()
		skipPath := config.SkipFilePath(acct)
		if m.skipPending {
			_ = os.Remove(skipPath)
			m.skipPending = false
			m.status = "Skip cancelled."
		} else {
			_ = os.WriteFile(skipPath, []byte("skip"), 0o644)
			m.skipPending = true
			m.status = "Next rebalance will be skipped."
		}
		m.rebalancer.Status.SkipPending = m.skipPending
		return m, nil

	case key.Matches(msg, km.RebalanceNow):
		if m.rebalancerRunning {
			m.status = "Rebalancer already running."
			return m, nil
		}
		m.rebalancerRunning = true
		m.rebalancer.Status.Active = true
		m.status = "Rebalancer started (this may take a few minutes)."
		return m, m.runRebalanceAsync(false)

	case key.Matches(msg, km.RebalanceCfg):
		m.modal = modals.NewRebalanceCfgModal(m.activeAccount(), m.rebalanceCfg)
		return m, nil

	case key.Matches(msg, km.InstallSvc):
		exe, _ := os.Executable()
		if err := config.InstallServiceFiles(exe); err != nil {
			m.status = "Install failed: " + err.Error()
			m.statusIsErr = true
		} else {
			m.status = "Service files installed."
			m.statusIsErr = false
		}
		return m, nil

	case key.Matches(msg, km.PrevAccount):
		if m.activeIdx > 0 {
			m.activeIdx--
			m.loading = true
			return m, tea.Batch(m.spin.Tick, m.loadPortfolio(), m.loadRebalancerStatus())
		}
		return m, nil

	case key.Matches(msg, km.NextAccount):
		if m.activeIdx < len(m.accounts)-1 {
			m.activeIdx++
			m.loading = true
			return m, tea.Batch(m.spin.Tick, m.loadPortfolio(), m.loadRebalancerStatus())
		}
		return m, nil

	case key.Matches(msg, km.ManageAccts):
		m.modal = modals.NewAccountsModal(m.accounts)
		return m, nil
	}

	// Pass remaining keys (arrow navigation, etc.) to holdings table.
	var cmd tea.Cmd
	m.holdings, cmd = m.holdings.Update(msg)
	return m, cmd
}

// ─────────────────────────────────────────────────────────────────────────────
// View — matches Python TUI compose() layout exactly.
// ─────────────────────────────────────────────────────────────────────────────

func (m Model) View() string {
	if m.modal != nil {
		return m.renderOverlay()
	}
	return m.renderMain()
}

func (m Model) renderMain() string {
	if m.width == 0 || m.height == 0 {
		return "Initializing…"
	}

	accountBar := m.renderAccountTabs()
	balance := m.balance.View()
	rebal := m.rebalancer.View()
	status := m.renderStatus()
	footer := m.renderKeyHints()

	if m.loading {
		spinLine := m.spin.View() + " Loading…"
		return lipgloss.JoinVertical(lipgloss.Left,
			accountBar, balance, rebal, spinLine, status, footer,
		)
	}

	// Vertical layout budget: account(1) + balance(1) + rebal(1) + status(1) + footer(1) = 5
	used := 5
	remaining := m.height - used
	if remaining < 10 {
		remaining = 10
	}

	chartH := remaining / 3
	if chartH < 6 {
		chartH = 6
	}
	if chartH > 14 {
		chartH = 14
	}
	mainH := remaining - chartH
	if mainH < 6 {
		mainH = 6
	}

	chart := m.chart.ViewWithHeight(chartH)

	// Horizontal split — left 2fr, right 1fr.
	leftW := (m.width * 2) / 3
	rightW := m.width - leftW

	left := m.renderLeftPane(leftW, mainH)
	right := m.renderRightPane(rightW, mainH)
	mainRow := lipgloss.JoinHorizontal(lipgloss.Top, left, right)

	return lipgloss.JoinVertical(lipgloss.Left,
		accountBar, balance, rebal, chart, mainRow, status, footer,
	)
}

func (m Model) renderAccountTabs() string {
	if len(m.accounts) == 0 {
		return theme.Muted.Render(" (no accounts)")
	}
	parts := make([]string, 0, len(m.accounts))
	for i, acct := range m.accounts {
		if i == m.activeIdx {
			parts = append(parts, theme.AccountTabActive.Render(acct))
		} else {
			parts = append(parts, theme.AccountTabInactive.Render(acct))
		}
	}
	bar := strings.Join(parts, " ")
	hint := theme.Muted.Render(" ctrl+←/→ switch  ctrl+a manage ")
	pad := m.width - lipgloss.Width(bar) - lipgloss.Width(hint)
	if pad < 1 {
		pad = 1
	}
	return bar + strings.Repeat(" ", pad) + hint
}

func (m Model) renderLeftPane(w, h int) string {
	// Border eats 2 columns + 2 rows.
	innerW := w - 2
	innerH := h - 2
	if innerW < 10 {
		innerW = 10
	}
	if innerH < 4 {
		innerH = 4
	}
	// Split inner height: 60% holdings, 40% options.
	holdH := (innerH * 6) / 10
	if holdH < 3 {
		holdH = 3
	}
	optH := innerH - holdH
	if optH < 3 {
		optH = 3
	}

	hView := m.holdings.ViewWithHeight(holdH)
	oView := m.opts.ViewWithHeight(optH)
	content := lipgloss.JoinVertical(lipgloss.Left, hView, oView)

	return theme.PaneLeft.Width(innerW).Height(innerH).Render(content)
}

func (m Model) renderRightPane(w, h int) string {
	innerW := w - 2
	innerH := h - 2
	if innerW < 10 {
		innerW = 10
	}
	if innerH < 4 {
		innerH = 4
	}
	content := m.orders.ViewWithHeight(innerH)
	return theme.PaneRight.Width(innerW).Height(innerH).Render(content)
}

func (m Model) renderStatus() string {
	if m.status == "" {
		return strings.Repeat(" ", m.width)
	}
	if m.statusIsErr {
		return theme.StatusErr.Render(m.status)
	}
	return theme.StatusOK.Render(m.status)
}

func (m Model) renderKeyHints() string {
	return theme.KeyHint.Render(
		"q quit  r refresh  b buy  s sell  c cancel  h history  l live  [/] chart  R rebalance  S settings  ctrl+a accounts",
	)
}

func (m Model) renderOverlay() string {
	content := m.modal.View()
	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		content,
	)
}

// ─────────────────────────────────────────────────────────────────────────────
// Portfolio helpers
// ─────────────────────────────────────────────────────────────────────────────

func (m *Model) applyPortfolio() {
	m.balance.FromPortfolio(m.portfolio, m.activeAccount())
	m.holdings.FromPortfolio(m.portfolio)
	m.opts.FromPortfolio(m.portfolio)
	m.orders.FromPortfolio(m.portfolio)
	if m.liveActive {
		m.chart.AppendLivePoint(m.balance.TotalEquity)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Entry point
// ─────────────────────────────────────────────────────────────────────────────

func Run(accounts []string, activeIdx int) error {
	if len(accounts) == 0 {
		p := tea.NewProgram(modals.NewSetupModal(config.ReadEnvToken()), tea.WithAltScreen())
		_, err := p.Run()
		return err
	}
	m := NewModel(accounts, activeIdx)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err := p.Run()
	return err
}
