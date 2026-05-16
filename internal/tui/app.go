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
	"github.com/charmbracelet/x/ansi"
	"github.com/shopspring/decimal"

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
type historyLoadedMsg struct {
	entries   []api.HistoryEntry
	truncated bool
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
	active      bool
	enabled     bool
	lastRun     string
	nextRun     string
}

type paneID int

const (
	paneStocks paneID = iota
	paneCrypto
	paneOptions
	paneOrders
)

// ─────────────────────────────────────────────────────────────────────────────
// Root model
// ─────────────────────────────────────────────────────────────────────────────

type Model struct {
	keys    KeyMap
	clients map[string]*api.Client

	accounts  []string
	activeIdx int
	portfolio *api.Portfolio
	loading   bool

	width  int
	height int

	balance    components.BalanceModel
	holdings   components.HoldingsModel
	crypto     components.CryptoModel
	opts       components.OptionsModel
	orders     components.OrdersModel
	rebalancer components.RebalancerModel
	spin       spinner.Model

	modal tea.Model

	rebalancerRunning bool
	rebalanceCfg      config.RebalanceConfig
	skipPending       bool
	activePane        paneID

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
		crypto:     components.NewCryptoModel(),
		opts:       components.NewOptionsModel(),
		orders:     components.NewOrdersModel(),
		balance:    components.NewBalanceModel(),
		rebalancer: components.NewRebalancerModel(),
		activePane: paneStocks,
	}
	m.initClients()
	m.applyCachedPortfolio()
	return m
}

// applyCachedPortfolio paints whatever's on disk so the UI has something
// usable before the first live fetch lands. Best-effort: errors are silent.
func (m *Model) applyCachedPortfolio() {
	acct := m.activeAccount()
	if acct == "" {
		return
	}
	p, err := api.LoadPortfolio(config.PortfolioCachePath(acct))
	if err != nil || p == nil {
		return
	}
	m.portfolio = p
	m.applyPortfolio()
	m.status = acct + " (cached)"
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
		c, err := api.NewClient(acct)
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
		liveTick(),
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
				err: fmt.Errorf("public CLI not found — install: pipx install publicdotcom-cli  then run: public auth login"),
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
		entries, truncated, err := client.ListHistory()
		if err != nil {
			return appErrMsg{err: err, ctx: "history"}
		}
		return historyLoadedMsg{entries: entries, truncated: truncated}
	}
}

func (m *Model) loadRebalancerStatus() tea.Cmd {
	acct := m.activeAccount()
	return func() tea.Msg {
		cfg := config.LoadRebalanceConfig(acct)
		_, skipErr := os.Stat(config.SkipFilePath(acct))
		msg := rebalancerStatusMsg{cfg: cfg, skipPending: skipErr == nil}
		if config.HasSystemctl() {
			msg.active = config.SystemctlIsActive(config.TimerUnit)
			msg.enabled = config.SystemctlIsEnabled(config.TimerUnit)
			props := config.SystemctlShow(config.TimerUnit,
				"LastTriggerUSec", "NextElapseUSecRealtime")
			msg.lastRun = props["LastTriggerUSec"]
			msg.nextRun = props["NextElapseUSecRealtime"]
		}
		return msg
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
	case tea.MouseMsg:
		return m.handleMouse(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.balance.Width = m.width
		m.rebalancer.Width = m.width
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case portfolioLoadedMsg:
		m.loading = false
		m.portfolio = msg.p
		m.applyPortfolio()
		// Persist for next launch so the UI can paint immediately.
		if acct := m.activeAccount(); acct != "" {
			_ = api.SavePortfolio(config.PortfolioCachePath(acct), msg.p)
			streamSuffix := "  |  STREAMING"
			m.status = fmt.Sprintf("  %s%s", acct, streamSuffix)
			m.statusIsErr = false
		}
		var cmd tea.Cmd
		return m, cmd

	case historyLoadedMsg:
		m.modal = modals.NewHistoryModal(msg.entries, msg.truncated, m.width, m.height)
		return m, nil

	case rebalancerRunningMsg:
		m.rebalancerRunning = false
		m.rebalancer.Status.SvcActive = false
		m.rebalancer.Status.LastRun = time.Now().Format("15:04:05")
		return m, m.loadPortfolio()

	case liveTickMsg:
		return m, tea.Batch(liveTick(), m.loadPortfolio())

	case appErrMsg:
		m.loading = false
		m.status = fmt.Sprintf("%s: %v", msg.ctx, msg.err)
		m.statusIsErr = true
		return m, nil

	case rebalancerStatusMsg:
		m.rebalanceCfg = msg.cfg
		m.skipPending = msg.skipPending
		m.rebalancer.Status.Cfg = msg.cfg
		m.rebalancer.Status.SvcInstalled = msg.enabled || msg.active // installed if either is true
		m.rebalancer.Status.SvcEnabled = msg.enabled
		m.rebalancer.Status.SvcActive = msg.active || m.rebalancerRunning
		m.rebalancer.Status.SkipPending = msg.skipPending
		m.rebalancer.Status.LastRun = msg.lastRun
		m.rebalancer.Status.NextRun = msg.nextRun
		return m, nil

	case spinner.TickMsg:
		if m.loading {
			var cmd tea.Cmd
			m.spin, cmd = m.spin.Update(msg)
			return m, cmd
		}
		return m, nil
	}

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

	case modals.CancelRequestedMsg:
		// User asked to cancel from inside OrderDetailsModal — swap to
		// CancelModal for the y/n confirmation.
		if client := m.activeClient(); client != nil {
			m.modal = modals.NewCancelModal(client, msg.OrderID, msg.Symbol)
		} else {
			m.modal = nil
		}
		return m, nil

	case modals.ModifyRequestedMsg:
		// The Public CLI has no modify endpoint; mirror Python by directing
		// the user to cancel and re-place.
		m.modal = nil
		m.status = fmt.Sprintf("Modify %s: cancel (c) the order then place a new one (b/s).", msg.OrderID)
		m.statusIsErr = false
		return m, nil

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
		m.status = "Rebalancer config saved."
		m.statusIsErr = false
		return m, m.loadRebalancerStatus()

	case modals.RebalanceCfgClosedMsg:
		m.modal = nil
		return m, nil
	}
	return m, cmd
}

func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	// Mouse support is intentionally limited to account tab switching.
	// Pane/table interaction is keyboard-only.
	if msg.Y == 0 && msg.Action == tea.MouseActionRelease && msg.Button == tea.MouseButtonLeft {
		currX := 0
		for i, acct := range m.accounts {
			tabW := len(acct) + 2
			if i > 0 {
				currX += 1
			}
			if msg.X >= currX && msg.X < currX+tabW {
				if m.activeIdx != i {
					m.activeIdx = i
					m.applyCachedPortfolio()
					m.loading = true
					return m, tea.Batch(m.spin.Tick, m.loadPortfolio(), m.loadRebalancerStatus())
				}
				break
			}
			currX += tabW
		}
		return m, nil
	}
	return m, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// View
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

	case key.Matches(msg, km.PaneNext):
		m.activePane = nextPane(m.activePane)
		return m, nil

	case key.Matches(msg, km.PanePrev):
		m.activePane = prevPane(m.activePane)
		return m, nil

	case key.Matches(msg, km.PaneLeft):
		m.activePane = movePane(m.activePane, "left")
		return m, nil

	case key.Matches(msg, km.PaneRight):
		m.activePane = movePane(m.activePane, "right")
		return m, nil

	case key.Matches(msg, km.PaneUp):
		m.activePane = movePane(m.activePane, "up")
		return m, nil

	case key.Matches(msg, km.PaneDown):
		m.activePane = movePane(m.activePane, "down")
		return m, nil

	case key.Matches(msg, km.Buy):
		m.openOrderModal("BUY")
		return m, nil

	case key.Matches(msg, km.Sell):
		m.openOrderModal("SELL")
		return m, nil

	case key.Matches(msg, km.ViewOrder):
		o := m.orders.SelectedOrder()
		if o == nil {
			m.status = "No open order selected."
			m.statusIsErr = false
			return m, nil
		}
		m.modal = modals.NewOrderDetailsModal(*o)
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
		m.rebalancer.Status.SvcActive = true
		m.status = "Rebalancer started (this may take a few minutes)."
		return m, m.runRebalanceAsync(false)

	case key.Matches(msg, km.RebalanceCfg):
		var marginEnabled bool
		var marginCapacity = decimal.Zero
		if m.portfolio != nil {
			marginEnabled, marginCapacity = m.portfolio.MarginStatus()
		}
		m.modal = modals.NewRebalanceCfgModal(m.activeAccount(), m.rebalanceCfg, marginEnabled, marginCapacity)
		return m, nil

	case key.Matches(msg, km.InstallSvc):
		return m.toggleScheduleInstall()

	case key.Matches(msg, km.ToggleTimer):
		return m.toggleTimerActive()

	case key.Matches(msg, km.PrevAccount):
		if m.activeIdx > 0 {
			m.activeIdx--
			m.applyCachedPortfolio()
			m.loading = true
			return m, tea.Batch(m.spin.Tick, m.loadPortfolio(), m.loadRebalancerStatus())
		}
		return m, nil

	case key.Matches(msg, km.NextAccount):
		if m.activeIdx < len(m.accounts)-1 {
			m.activeIdx++
			m.applyCachedPortfolio()
			m.loading = true
			return m, tea.Batch(m.spin.Tick, m.loadPortfolio(), m.loadRebalancerStatus())
		}
		return m, nil

	case key.Matches(msg, km.ManageAccts):
		m.modal = modals.NewAccountsModal(m.accounts)
		return m, nil
	}
	return m.updateActivePane(msg)
}

func (m Model) updateActivePane(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.activePane {
	case paneStocks:
		m.holdings, cmd = m.holdings.Update(msg)
	case paneCrypto:
		m.crypto, cmd = m.crypto.Update(msg)
	case paneOptions:
		m.opts, cmd = m.opts.Update(msg)
	case paneOrders:
		m.orders, cmd = m.orders.Update(msg)
	}
	return m, cmd
}

func nextPane(p paneID) paneID {
	return (p + 1) % 4
}

func prevPane(p paneID) paneID {
	return (p + 3) % 4
}

func movePane(p paneID, dir string) paneID {
	switch dir {
	case "left":
		switch p {
		case paneCrypto:
			return paneStocks
		case paneOrders:
			return paneOptions
		}
	case "right":
		switch p {
		case paneStocks:
			return paneCrypto
		case paneOptions:
			return paneOrders
		}
	case "up":
		switch p {
		case paneOptions:
			return paneStocks
		case paneOrders:
			return paneCrypto
		}
	case "down":
		switch p {
		case paneStocks:
			return paneOptions
		case paneCrypto:
			return paneOrders
		}
	}
	return p
}

// ─────────────────────────────────────────────────────────────────────────────
// View
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

	_, mainH, leftW, rightW := m.layoutDims(accountBar, balance, rebal, status, footer)
	if mainH < 16 || leftW < 20 || rightW < 20 {
		return "TOP"
	}

	topH, bottomH := splitMainHeights(mainH)

	topLeft := m.renderHoldingsPane(leftW, topH)
	topRight := m.renderCryptoPane(rightW, topH)
	bottomLeft := m.renderOptionsPane(leftW, bottomH)
	bottomRight := m.renderOrdersPane(rightW, bottomH)

	if m.loading {
		topLeft = m.renderLoadingPane(leftW, topH, theme.PaneStocks, theme.PaneTitleStocks.Render(" STOCKS"), "Loading stocks…")
		topRight = m.renderLoadingPane(rightW, topH, theme.PaneCrypto, theme.PaneTitleCrypto.Render(" CRYPTO"), "Loading crypto…")
		bottomLeft = m.renderLoadingPane(leftW, bottomH, theme.PaneOptions, theme.PaneTitleOptions.Render(" OPTIONS"), "Loading options…")
		bottomRight = m.renderLoadingPane(rightW, bottomH, theme.PaneOrders, theme.PaneTitleOrders.Render(" OPEN ORDERS"), "Loading open orders…")
	}

	_ = lipgloss.JoinHorizontal(lipgloss.Top, topLeft, topRight)
	_ = lipgloss.JoinHorizontal(lipgloss.Top, bottomLeft, bottomRight)

	return "TOP"
}

func (m Model) renderAccountTabs() string {
	if len(m.accounts) == 0 {
		return truncateForWidth(theme.Muted.Render(" (no accounts)"), m.width)
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
	if m.width <= lipgloss.Width(bar)+1 {
		return truncateForWidth(bar, m.width)
	}
	if lipgloss.Width(bar)+lipgloss.Width(hint)+1 > m.width {
		return truncateForWidth(bar, m.width)
	}
	pad := m.width - lipgloss.Width(bar) - lipgloss.Width(hint)
	if pad < 1 {
		pad = 1
	}
	return truncateForWidth(bar+strings.Repeat(" ", pad)+hint, m.width)
}

func (m Model) renderHoldingsPane(w, h int) string {
	contentW := max(w-2, 10)
	contentH := max(h-2, 4)
	holdings := m.holdings
	holdings.SetWidth(contentW)
	content := holdings.ViewWithHeight(contentH)
	style := theme.PaneStocks
	if m.activePane == paneStocks {
		style = style.BorderForeground(theme.ColorWhite)
	}
	return style.Width(w).Height(h).Render(content)
}

func (m Model) renderCryptoPane(w, h int) string {
	contentW := max(w-2, 10)
	contentH := max(h-2, 4)
	crypto := m.crypto
	crypto.SetWidth(contentW)
	content := crypto.ViewWithHeight(contentH)
	style := theme.PaneCrypto
	if m.activePane == paneCrypto {
		style = style.BorderForeground(theme.ColorWhite)
	}
	return style.Width(w).Height(h).Render(content)
}

func (m Model) renderOptionsPane(w, h int) string {
	contentW := max(w-2, 10)
	contentH := max(h-2, 4)
	opts := m.opts
	opts.SetWidth(contentW)
	content := opts.ViewWithHeight(contentH)
	style := theme.PaneOptions
	if m.activePane == paneOptions {
		style = style.BorderForeground(theme.ColorWhite)
	}
	return style.Width(w).Height(h).Render(content)
}

func (m Model) renderOrdersPane(w, h int) string {
	contentW := max(w-2, 10)
	contentH := max(h-2, 4)
	orders := m.orders
	orders.SetWidth(contentW)
	content := orders.ViewWithHeight(contentH)
	style := theme.PaneOrders
	if m.activePane == paneOrders {
		style = style.BorderForeground(theme.ColorWhite)
	}
	return style.Width(w).Height(h).Render(content)
}

func (m Model) renderLoadingPane(w, h int, paneStyle lipgloss.Style, title, msg string) string {
	contentW := max(w-2, 10)
	contentH := max(h-2, 4)
	content := loadingSection(contentW, contentH, title, msg)
	return paneStyle.Width(w).Height(h).Render(content)
}

func loadingSection(w, h int, title, msg string) string {
	if h < 2 {
		h = 2
	}
	bodyH := h - 1
	body := lipgloss.Place(
		w,
		bodyH,
		lipgloss.Center,
		lipgloss.Center,
		theme.Muted.Render(msg),
	)
	return title + "\n" + body
}

func (m Model) renderStatus() string {
	if m.loading {
		line := strings.TrimSpace(m.spin.View() + " Loading live portfolio…")
		if m.status != "" {
			line += "  |  " + m.status
		}
		line = truncateForWidth(line, m.width)
		if m.statusIsErr {
			return theme.StatusErr.Width(max(1, m.width)).Render(line)
		}
		return theme.StatusOK.Width(max(1, m.width)).Render(line)
	}
	if m.status == "" {
		return strings.Repeat(" ", max(1, m.width))
	}
	line := truncateForWidth(m.status, m.width)
	if m.statusIsErr {
		return theme.StatusErr.Width(max(1, m.width)).Render(line)
	}
	return theme.StatusOK.Width(max(1, m.width)).Render(line)
}

func (m Model) renderKeyHints() string {
	hint := "tab/shift+tab pane  alt+arrows move pane  ↑↓/j/k row  q quit  r refresh  b/s order  v/c order  h history"
	if m.width < 100 {
		hint = "tab pane  alt+arrows pane  ↑↓/j/k row  q/r  b/s  v/c  h  R/S"
	}
	if m.width < 72 {
		hint = "tab pane  alt+arrows  ↑↓ row  q/r  b/s  v/c  h  R/S"
	}
	return theme.KeyHint.Width(max(1, m.width)).Render(truncateForWidth(hint, m.width))
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

func (m *Model) openOrderModal(side string) {
	client := m.activeClient()
	if client == nil {
		return
	}
	symbol := m.holdings.SelectedSymbol()
	instrumentType := "EQUITY"
	if m.activePane == paneCrypto {
		if s := m.crypto.SelectedSymbol(); s != "" {
			symbol = s
		}
		instrumentType = "CRYPTO"
	}
	m.modal = modals.NewOrderModal(client, side, symbol, instrumentType)
}

// toggleScheduleInstall: install + enable + start, or stop + disable + remove,
// depending on current systemd state. Mirrors Python action_toggle_enable_rebalancer.
func (m Model) toggleScheduleInstall() (tea.Model, tea.Cmd) {
	if !config.HasSystemctl() {
		m.status = "Install/remove schedule requires systemctl on this platform."
		m.statusIsErr = true
		return m, nil
	}
	if config.SystemctlIsEnabled(config.TimerUnit) {
		if ok, out := config.SystemctlDisableNow(config.TimerUnit); !ok {
			m.status = "Disable failed: " + out
			m.statusIsErr = true
			return m, nil
		}
		if err := config.RemoveServiceFiles(); err != nil {
			m.status = "Remove failed: " + err.Error()
			m.statusIsErr = true
			return m, nil
		}
		_, _ = config.SystemctlDaemonReload()
		m.status = "Rebalancer schedule removed."
		m.statusIsErr = false
		return m, m.loadRebalancerStatus()
	}
	exe, err := os.Executable()
	if err != nil {
		m.status = "Could not resolve binary path: " + err.Error()
		m.statusIsErr = true
		return m, nil
	}
	if err := config.InstallServiceFiles(exe); err != nil {
		m.status = "Install failed: " + err.Error()
		m.statusIsErr = true
		return m, nil
	}
	if _, out := config.SystemctlDaemonReload(); out != "" {
		// daemon-reload prints diagnostics; surface only on hard failures.
	}
	if ok, out := config.SystemctlEnableNow(config.TimerUnit); !ok {
		m.status = "Schedule activation failed: " + out
		m.statusIsErr = true
		return m, nil
	}
	m.status = "Rebalancer timer enabled — scheduled Mon-Fri at 12:00 ET."
	m.statusIsErr = false
	return m, m.loadRebalancerStatus()
}

// toggleTimerActive: pauses the timer if running, resumes it if stopped.
// Refuses if the schedule isn't installed. Mirrors Python action_toggle_rebalancer.
func (m Model) toggleTimerActive() (tea.Model, tea.Cmd) {
	if !config.HasSystemctl() {
		m.status = "Pause/resume requires systemctl on this platform."
		m.statusIsErr = true
		return m, nil
	}
	if !config.SystemctlIsEnabled(config.TimerUnit) {
		m.status = "No rebalancer schedule installed — press [e] to install it."
		m.statusIsErr = true
		return m, nil
	}
	if config.SystemctlIsActive(config.TimerUnit) {
		if ok, out := config.SystemctlStop(config.TimerUnit); !ok {
			m.status = "Pause failed: " + out
			m.statusIsErr = true
			return m, nil
		}
		m.status = "Rebalancer schedule paused — press [t] to resume."
		m.statusIsErr = false
		return m, m.loadRebalancerStatus()
	}
	if _, _ = config.SystemctlDaemonReload(); false {
	}
	if ok, out := config.SystemctlStart(config.TimerUnit); !ok {
		m.status = "Resume failed: " + out
		m.statusIsErr = true
		return m, nil
	}
	m.status = "Rebalancer schedule resumed — next run follows the 12:00 ET schedule."
	m.statusIsErr = false
	return m, m.loadRebalancerStatus()
}

func (m *Model) applyPortfolio() {
	m.balance.FromPortfolio(m.portfolio, m.activeAccount())
	m.holdings.FromPortfolio(m.portfolio)
	m.crypto.FromPortfolio(m.portfolio)
	m.opts.FromPortfolio(m.portfolio)
	m.orders.FromPortfolio(m.portfolio)
}

func (m Model) layoutDims(accountBar, balance, rebal, status, footer string) (mainY, mainH, leftW, rightW int) {
	topH := lipgloss.Height(accountBar) + lipgloss.Height(balance) + lipgloss.Height(rebal)
	bottomH := lipgloss.Height(status) + lipgloss.Height(footer)
	mainY = topH
	mainH = m.height - topH - bottomH
	if mainH < 0 {
		mainH = 0
	}
	if m.width <= 1 {
		return mainY, mainH, m.width, 0
	}
	// Adaptive split: left pane (stocks/options) needs more width than
	// right pane (crypto/orders), especially on smaller terminals.
	if m.width >= 120 {
		leftW = m.width / 2
	} else if m.width >= 100 {
		leftW = m.width * 11 / 20
	} else {
		leftW = m.width * 3 / 5
	}
	if leftW < 20 {
		leftW = 20
	}
	if leftW > m.width-20 {
		leftW = m.width - 20
	}
	rightW = m.width - leftW
	return mainY, mainH, leftW, rightW
}

func truncateForWidth(s string, w int) string {
	if w < 1 {
		return ""
	}
	return ansi.Truncate(s, w, "…")
}

func splitMainHeights(mainH int) (topH, bottomH int) {
	topH = mainH / 2
	bottomH = mainH - topH
	minH := 8 // each half needs at least 8 rows (2 border + 1 title + 5 data)
	if mainH < minH*2 {
		return topH, bottomH // not enough for both halves; caller handles collapse
	}
	if topH < minH {
		topH = minH
		bottomH = mainH - topH
	}
	if bottomH < minH {
		bottomH = minH
		topH = mainH - bottomH
	}
	return topH, bottomH
}

// ─────────────────────────────────────────────────────────────────────────────
// Entry point
// ─────────────────────────────────────────────────────────────────────────────

func Run(accounts []string, activeIdx int) error {
	if len(accounts) == 0 {
		// First run: collect accounts via setup modal.
		p := tea.NewProgram(modals.NewSetupModal(), tea.WithAltScreen())
		if _, err := p.Run(); err != nil {
			return err
		}
		// Re-read accounts after setup so we can launch the main app immediately.
		accounts = config.GetAccounts()
		if len(accounts) == 0 {
			return nil // user quit without saving
		}
	}
	m := NewModel(accounts, activeIdx)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err := p.Run()
	return err
}
