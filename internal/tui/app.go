package tui

import (
	"fmt"
	"os"
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
)

// ─────────────────────────────────────────────────────────────────────────────
// Messages
// ─────────────────────────────────────────────────────────────────────────────

type portfolioLoadedMsg struct{ p *api.Portfolio }
type historyLoadedMsg struct{ entries []api.HistoryEntry }
type rebalancerRunningMsg struct{ running bool }
type liveTickMsg struct{}
type appErrMsg struct{ err error; ctx string }

// ─────────────────────────────────────────────────────────────────────────────
// Root model
// ─────────────────────────────────────────────────────────────────────────────

// Model is the root Bubble Tea model for the application.
type Model struct {
	keys    KeyMap
	clients map[string]*api.Client // accountID → client

	accounts      []string
	activeIdx     int
	portfolio     *api.Portfolio
	loading       bool
	liveChart     bool
	liveActive    bool

	width  int
	height int

	// sub-models
	balance    components.BalanceModel
	holdings   components.HoldingsModel
	opts       components.OptionsModel
	orders     components.OrdersModel
	chart      components.ChartModel
	rebalancer components.RebalancerModel
	spin       spinner.Model

	// modal overlay (nil = no overlay)
	modal tea.Model

	// rebalancer state
	rebalancerRunning bool
	rebalanceCfg      config.RebalanceConfig
	skipPending       bool
	lastRebalanceRun  string

	status      string
	statusIsErr bool
}

func NewModel(accounts []string, activeIdx int) *Model {
	if activeIdx < 0 || activeIdx >= len(accounts) {
		activeIdx = 0
	}

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))

	m := &Model{
		keys:      DefaultKeyMap,
		accounts:  accounts,
		activeIdx: activeIdx,
		loading:   true,
		spin:      sp,
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
		return nil
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
		return struct {
			cfg         config.RebalanceConfig
			skipPending bool
		}{cfg: cfg, skipPending: skipErr == nil}
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
	return tea.Tick(30*time.Second, func(t time.Time) tea.Msg { return liveTickMsg{} })
}

// ─────────────────────────────────────────────────────────────────────────────
// Update
// ─────────────────────────────────────────────────────────────────────────────

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Route to modal first if one is open
	if m.modal != nil {
		return m.updateModal(msg)
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resizeComponents()

	case tea.KeyMsg:
		return m.handleKey(msg)

	case portfolioLoadedMsg:
		m.loading = false
		m.portfolio = msg.p
		m.applyPortfolio()

	case historyLoadedMsg:
		m.modal = modals.NewHistoryModal(msg.entries, m.width, m.height)

	case rebalancerRunningMsg:
		m.rebalancerRunning = msg.running
		m.lastRebalanceRun = time.Now().Format("15:04:05")
		m.rebalancer.Status.Active = false
		m.rebalancer.Status.LastRun = m.lastRebalanceRun
		return m, m.loadPortfolio()

	case liveTickMsg:
		if m.liveActive {
			return m, tea.Batch(liveTick(), m.loadPortfolio())
		}

	case appErrMsg:
		m.loading = false
		m.status = fmt.Sprintf("[%s] %v", msg.ctx, msg.err)
		m.statusIsErr = true

	case struct {
		cfg         config.RebalanceConfig
		skipPending bool
	}:
		m.rebalanceCfg = msg.cfg
		m.skipPending = msg.skipPending
		m.rebalancer.Status.Cfg = msg.cfg
		m.rebalancer.Status.Enabled = msg.cfg.RebalanceEnabled
		m.rebalancer.Status.SkipPending = msg.skipPending

	case struct {
		bars   []api.Bar
		symbol string
	}:
		m.chart.Bars = msg.bars
		m.chart.Symbol = msg.symbol

	case spinner.TickMsg:
		if m.loading {
			var cmd tea.Cmd
			m.spin, cmd = m.spin.Update(msg)
			return m, cmd
		}
	}

	// Pass events to focusable sub-models
	var cmds []tea.Cmd
	var cmd tea.Cmd
	m.holdings, cmd = m.holdings.Update(msg)
	cmds = append(cmds, cmd)
	m.orders, cmd = m.orders.Update(msg)
	cmds = append(cmds, cmd)
	return m, tea.Batch(cmds...)
}

func (m Model) updateModal(msg tea.Msg) (tea.Model, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok && km.String() == "ctrl+c" {
		m.modal = nil
		return m, nil
	}

	next, cmd := m.modal.Update(msg)

	// Detect close/result messages
	switch msg.(type) {
	case modals.SetupDoneMsg:
		m.modal = nil
		// Re-init clients with new credentials
		m.initClients()
		return m, m.loadPortfolio()

	case modals.OrderPlacedMsg:
		m.modal = nil
		m.status = "Order placed."
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
		newAccts := msg.(modals.AccountsUpdatedMsg).Accounts
		m.accounts = newAccts
		if m.activeIdx >= len(m.accounts) {
			m.activeIdx = len(m.accounts) - 1
		}
		m.initClients()
		return m, m.loadPortfolio()

	case modals.RebalanceCfgSavedMsg:
		m.modal = nil
		m.rebalanceCfg = msg.(modals.RebalanceCfgSavedMsg).Cfg
		m.rebalancer.Status.Cfg = m.rebalanceCfg
		m.rebalancer.Status.Enabled = m.rebalanceCfg.RebalanceEnabled
		m.status = "Rebalancer config saved."
		m.statusIsErr = false
		return m, nil

	case modals.RebalanceCfgClosedMsg:
		m.modal = nil
		return m, nil
	}

	m.modal = next
	return m, cmd
}

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
		client := m.activeClient()
		if client != nil {
			m.modal = modals.NewOrderModal(client, "BUY", m.holdings.SelectedSymbol(), "EQUITY")
		}
		return m, nil

	case key.Matches(msg, km.Sell):
		client := m.activeClient()
		if client != nil {
			m.modal = modals.NewOrderModal(client, "SELL", m.holdings.SelectedSymbol(), "EQUITY")
		}
		return m, nil

	case key.Matches(msg, km.Cancel):
		orderID := m.orders.SelectedOrderID()
		if orderID == "" {
			m.status = "No order selected."
			return m, nil
		}
		row := m.orders.SelectedRow()
		sym := ""
		if len(row) > 0 {
			sym = row[0]
		}
		client := m.activeClient()
		if client != nil {
			m.modal = modals.NewCancelModal(client, orderID, sym)
		}
		return m, nil

	case key.Matches(msg, km.History):
		return m, m.loadHistory()

	case key.Matches(msg, km.ToggleLive):
		m.liveActive = !m.liveActive
		if m.liveActive {
			m.chart.Live = true
			m.status = "Live chart on."
			return m, liveTick()
		}
		m.chart.Live = false
		m.status = "Live chart off."
		return m, nil

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
		m.status = "Rebalancer started."
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

	case key.Matches(msg, km.PrevAccount):
		if m.activeIdx > 0 {
			m.activeIdx--
			m.loading = true
			return m, tea.Batch(m.spin.Tick, m.loadPortfolio(), m.loadRebalancerStatus())
		}

	case key.Matches(msg, km.NextAccount):
		if m.activeIdx < len(m.accounts)-1 {
			m.activeIdx++
			m.loading = true
			return m, tea.Batch(m.spin.Tick, m.loadPortfolio(), m.loadRebalancerStatus())
		}

	case key.Matches(msg, km.ManageAccts):
		m.modal = modals.NewAccountsModal(m.accounts)
		return m, nil
	}
	return m, nil
}

func (m *Model) loadChartData() tea.Cmd {
	client := m.activeClient()
	if client == nil {
		return nil
	}
	sym := m.holdings.SelectedSymbol()
	if sym == "" {
		return nil
	}
	p := api.ChartPeriods[m.chart.PeriodIdx]
	return func() tea.Msg {
		bars, err := client.GetHistoricBars(sym, p.Period, p.Aggregation)
		if err != nil {
			return appErrMsg{err: err, ctx: "chart"}
		}
		return struct {
			bars   []api.Bar
			symbol string
		}{bars: bars, symbol: sym}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// View
// ─────────────────────────────────────────────────────────────────────────────

func (m Model) View() string {
	if m.modal != nil {
		// Render modal centered over blurred background
		bg := m.renderMain()
		overlay := m.modal.View()
		return overlayCenter(bg, overlay, m.width, m.height)
	}
	return m.renderMain()
}

func (m Model) renderMain() string {
	if m.loading {
		return m.balance.View() + "\n" +
			"\n" + m.spin.View() + " Loading…\n"
	}

	mainHeight := m.height - 3 // balance + rebalancer + status bars
	holdH := mainHeight / 3
	optH := mainHeight / 5
	ordH := mainHeight / 5
	chartH := mainHeight - holdH - optH - ordH

	hold := m.holdings.View()
	opts := m.opts.View()
	ords := m.orders.View()
	ch := m.chart.View()

	// Stack sections vertically, constrained to window
	_ = holdH
	_ = optH
	_ = ordH
	_ = chartH

	statusLine := m.renderStatus()

	return m.balance.View() + "\n" +
		hold + "\n" +
		opts + "\n" +
		ords + "\n" +
		ch + "\n" +
		m.rebalancer.View() + "\n" +
		statusLine + "\n" +
		m.keys.ShortHelp()
}

func (m Model) renderStatus() string {
	if m.status == "" {
		return ""
	}
	if m.statusIsErr {
		return StyleStatusErr.Render(m.status)
	}
	return StyleStatusOK.Render(m.status)
}

func (m *Model) applyPortfolio() {
	m.balance.FromPortfolio(m.portfolio, m.activeAccount())
	m.holdings.FromPortfolio(m.portfolio)
	m.opts.FromPortfolio(m.portfolio)
	m.orders.FromPortfolio(m.portfolio)
	if m.liveActive {
		m.chart.AppendLivePoint(m.balance.TotalEquity)
	}
}

func (m *Model) resizeComponents() {
	m.balance.Width = m.width
	m.rebalancer.Width = m.width

	mainH := m.height - 3
	holdH := mainH / 3
	optH := mainH / 5
	ordH := mainH / 5
	chartH := mainH - holdH - optH - ordH

	m.holdings = components.NewHoldingsModel(m.width, holdH)
	m.opts = components.NewOptionsModel(m.width, optH)
	m.orders = components.NewOrdersModel(m.width, ordH)
	m.chart = components.NewChartModel(m.width, chartH)

	if m.portfolio != nil {
		m.applyPortfolio()
	}
}

// overlayCenter places overlay string centered on bg.
func overlayCenter(bg, overlay string, width, height int) string {
	ovW := lipgloss.Width(overlay)
	ovH := lipgloss.Height(overlay)
	x := (width - ovW) / 2
	y := (height - ovH) / 2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	_ = x
	_ = y
	// Simple approach: just return overlay on top
	return bg[:0] + overlay
}

// Run starts the Bubble Tea program.
func Run(accounts []string, activeIdx int) error {
	if len(accounts) == 0 {
		// Show setup modal
		p := tea.NewProgram(modals.NewSetupModal(config.ReadEnvToken()), tea.WithAltScreen())
		_, err := p.Run()
		return err
	}

	m := NewModel(accounts, activeIdx)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}
