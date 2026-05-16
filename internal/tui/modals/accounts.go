package modals

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/ks1686/public-terminal/internal/api"
	"github.com/ks1686/public-terminal/internal/config"
	"github.com/ks1686/public-terminal/internal/tui/theme"
)

// AccountsModal lets the user add/remove accounts. Adds are validated against
// the live Public API before being persisted (matches Python's
// AccountManagementModal._validate_and_add).
type AccountsModal struct {
	accounts   []string
	cursor     int
	input      textinput.Model
	mode       int // 0=list 1=add 2=validating
	err        string
	status     string
	pendingAdd string
}

type AccountsUpdatedMsg struct{ Accounts []string }
type AccountsClosedMsg struct{}

// accountValidationMsg is internal: result of the async API check.
type accountValidationMsg struct {
	account     string
	apiError    bool // 404/unauthorized/forbidden — reject
	networkErr  bool // other failure — accept anyway with warning
	credMissing bool // no credentials at all — accept anyway
}

var acctIDRe = regexp.MustCompile(`^[A-Z0-9]{4,12}$`)

func NewAccountsModal(accounts []string) AccountsModal {
	ti := textinput.New()
	ti.Placeholder = "Account ID (e.g. ACCT0002)"
	ti.CharLimit = 12
	return AccountsModal{
		accounts: append([]string(nil), accounts...),
		input:    ti,
	}
}

func (m AccountsModal) Init() tea.Cmd { return nil }

func (m AccountsModal) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case accountValidationMsg:
		return m.handleValidation(msg)

	case tea.KeyMsg:
		switch m.mode {
		case 0:
			return m.updateList(msg)
		case 1:
			return m.updateAdd(msg)
		case 2:
			// Block input while validating; allow esc to abort.
			if msg.String() == "esc" {
				m.mode = 1
				m.status = ""
				return m, nil
			}
			return m, nil
		}
	}
	return m, nil
}

func (m AccountsModal) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		return m, func() tea.Msg { return AccountsClosedMsg{} }
	case "a":
		m.mode = 1
		m.err = ""
		m.input.Focus()
		return m, textinput.Blink
	case "d", "delete":
		if len(m.accounts) <= 1 {
			m.err = "Must keep at least one account."
			return m, nil
		}
		removed := m.accounts[m.cursor]
		m.accounts = append(m.accounts[:m.cursor], m.accounts[m.cursor+1:]...)
		if m.cursor >= len(m.accounts) {
			m.cursor = len(m.accounts) - 1
		}
		_ = config.RemoveAccount(removed)
		accounts := m.accounts
		return m, func() tea.Msg { return AccountsUpdatedMsg{Accounts: accounts} }
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.accounts)-1 {
			m.cursor++
		}
	}
	return m, nil
}

func (m AccountsModal) updateAdd(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = 0
		m.err = ""
		m.status = ""
		m.input.Blur()
		m.input.Reset()
		return m, nil

	case "enter":
		id := strings.ToUpper(strings.TrimSpace(m.input.Value()))
		if id == "" {
			m.err = "Account ID is required."
			return m, nil
		}
		if !acctIDRe.MatchString(id) {
			m.err = "Account ID must be 4–12 alphanumeric characters."
			return m, nil
		}
		for _, a := range m.accounts {
			if a == id {
				m.err = id + " is already registered."
				return m, nil
			}
		}
		m.err = ""
		m.status = "Validating with Public.com…"
		m.pendingAdd = id
		m.mode = 2
		return m, validateAccountCmd(id)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m AccountsModal) handleValidation(msg accountValidationMsg) (tea.Model, tea.Cmd) {
	m.status = ""
	if msg.apiError {
		m.mode = 1
		m.err = "Account not found or not accessible with the current token."
		return m, nil
	}
	if err := config.AddAccount(msg.account); err != nil {
		m.mode = 1
		m.err = "Save failed: " + err.Error()
		return m, nil
	}
	m.accounts = append(m.accounts, msg.account)
	m.input.Reset()
	m.mode = 0
	m.pendingAdd = ""
	if msg.networkErr || msg.credMissing {
		// Still emit the update; the warning surfaces via app status bar
		// could be wired later if desired.
	}
	accounts := m.accounts
	return m, func() tea.Msg { return AccountsUpdatedMsg{Accounts: accounts} }
}

// validateAccountCmd attempts to fetch the portfolio for the given account.
// Mirrors Python's _validate_and_add: API errors (404/unauthorized/forbidden)
// reject; network errors accept with a warning; missing credentials accept.
func validateAccountCmd(account string) tea.Cmd {
	return func() tea.Msg {
		client, err := api.NewClient(account)
		if err != nil {
			// No CLI — accept anyway (user may authenticate later).
			return accountValidationMsg{account: account, credMissing: true}
		}
		if _, err := client.GetPortfolio(); err != nil {
			low := strings.ToLower(err.Error())
			for _, kw := range []string{"404", "not found", "unauthorized", "forbidden", "invalid"} {
				if strings.Contains(low, kw) {
					return accountValidationMsg{account: account, apiError: true}
				}
			}
			return accountValidationMsg{account: account, networkErr: true}
		}
		return accountValidationMsg{account: account}
	}
}

func (m AccountsModal) View() string {
	lines := []string{
		theme.Title.Render("Manage Accounts"),
		"",
	}
	for i, a := range m.accounts {
		prefix := "  "
		if i == m.cursor {
			prefix = theme.Title.Render("> ")
		}
		lines = append(lines, prefix+a)
	}
	lines = append(lines, "")
	switch m.mode {
	case 1:
		lines = append(lines, "New account: "+m.input.View())
		lines = append(lines, theme.Muted.Render("enter: add (validates)  esc: cancel"))
	case 2:
		lines = append(lines, "New account: "+m.pendingAdd)
		lines = append(lines, theme.Muted.Render(m.status+"  esc: abort"))
	default:
		lines = append(lines, theme.Muted.Render("a: add  d/del: remove  ↑↓: navigate  esc: close"))
	}
	if m.err != "" {
		lines = append(lines, theme.StatusErr.Render(m.err))
	}
	return theme.ModalBox.Render(strings.Join(lines, "\n"))
}
