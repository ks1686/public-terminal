package modals

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/ks1686/public-terminal/internal/config"
	"github.com/ks1686/public-terminal/internal/tui/theme"
)

// SetupModal collects the API token and at least one account ID on first run.
type SetupModal struct {
	tokenInput   textinput.Model
	accountInput textinput.Model
	accounts     []string
	focus        int // 0=token 1=account
	err          string
}

type SetupDoneMsg struct {
	Token    string
	Accounts []string
}

func NewSetupModal(existingToken string) SetupModal {
	ti := textinput.New()
	ti.Placeholder = "API token"
	ti.EchoMode = textinput.EchoPassword
	ti.EchoCharacter = '•'
	ti.SetValue(existingToken)
	ti.Focus()

	ai := textinput.New()
	ai.Placeholder = "Account ID (e.g. DW12345678)"

	return SetupModal{tokenInput: ti, accountInput: ai}
}

func (m SetupModal) Init() tea.Cmd { return textinput.Blink }

func (m SetupModal) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return m, func() tea.Msg { return SetupDoneMsg{} }

		case "tab", "shift+tab":
			m.focus = 1 - m.focus
			if m.focus == 0 {
				m.tokenInput.Focus()
				m.accountInput.Blur()
			} else {
				m.accountInput.Focus()
				m.tokenInput.Blur()
			}

		case "enter":
			if m.focus == 1 && m.accountInput.Value() != "" {
				m.accounts = append(m.accounts, strings.TrimSpace(m.accountInput.Value()))
				m.accountInput.Reset()
			}

		case "ctrl+s":
			token := strings.TrimSpace(m.tokenInput.Value())
			if token == "" {
				m.err = "Token is required."
				return m, nil
			}
			if len(m.accounts) == 0 {
				acct := strings.TrimSpace(m.accountInput.Value())
				if acct == "" {
					m.err = "At least one account ID is required."
					return m, nil
				}
				m.accounts = append(m.accounts, acct)
			}
			if err := config.WriteEnv(token); err != nil {
				m.err = err.Error()
				return m, nil
			}
			for _, a := range m.accounts {
				_ = config.AddAccount(a)
			}
			accounts := m.accounts
			return m, func() tea.Msg { return SetupDoneMsg{Token: token, Accounts: accounts} }
		}
	}

	var cmd tea.Cmd
	if m.focus == 0 {
		m.tokenInput, cmd = m.tokenInput.Update(msg)
	} else {
		m.accountInput, cmd = m.accountInput.Update(msg)
	}
	return m, cmd
}

func (m SetupModal) View() string {
	lines := []string{
		theme.Title.Render("Setup — Public Terminal"),
		"",
		"Token:   " + m.tokenInput.View(),
		"Account: " + m.accountInput.View(),
	}
	if len(m.accounts) > 0 {
		lines = append(lines, "Added accounts: "+theme.Positive.Render(strings.Join(m.accounts, ", ")))
	}
	lines = append(lines, "")
	lines = append(lines, theme.Muted.Render("enter: add account  ctrl+s: save  tab: switch field  esc: cancel"))
	if m.err != "" {
		lines = append(lines, theme.StatusErr.Render(m.err))
	}
	return theme.ModalBox.Render(strings.Join(lines, "\n"))
}
