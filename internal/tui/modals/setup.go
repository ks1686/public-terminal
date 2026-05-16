package modals

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/ks1686/public-terminal/internal/config"
	"github.com/ks1686/public-terminal/internal/tui/theme"
)

// SetupModal collects account IDs on first run. Authentication is handled
// externally via the public CLI (`public auth login`).
type SetupModal struct {
	accountInput textinput.Model
	accounts     []string
	err          string
}

type SetupDoneMsg struct {
	Accounts []string
}

func NewSetupModal() SetupModal {
	ai := textinput.New()
	ai.Placeholder = "Account ID (e.g. DW12345678)"
	ai.Focus()

	return SetupModal{accountInput: ai}
}

func (m SetupModal) Init() tea.Cmd { return textinput.Blink }

func (m SetupModal) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return m, tea.Quit

		case "enter":
			if m.accountInput.Value() != "" {
				m.accounts = append(m.accounts, strings.TrimSpace(m.accountInput.Value()))
				m.accountInput.Reset()
			}

		case "ctrl+s":
			if len(m.accounts) == 0 {
				acct := strings.TrimSpace(m.accountInput.Value())
				if acct == "" {
					m.err = "At least one account ID is required."
					return m, nil
				}
				m.accounts = append(m.accounts, acct)
			}
			for _, a := range m.accounts {
				_ = config.AddAccount(a)
			}
			accounts := m.accounts
			return m, tea.Sequence(
				func() tea.Msg { return SetupDoneMsg{Accounts: accounts} },
				tea.Quit,
			)
		}
	}

	var cmd tea.Cmd
	m.accountInput, cmd = m.accountInput.Update(msg)
	return m, cmd
}

func (m SetupModal) View() string {
	lines := []string{
		theme.Title.Render("Setup — Public Terminal"),
		"",
		theme.Muted.Render("Authentication is handled by the public CLI."),
		theme.Muted.Render("Run:  public auth login"),
		"",
		"Account: " + m.accountInput.View(),
	}
	if len(m.accounts) > 0 {
		lines = append(lines, "Added accounts: "+theme.Positive.Render(strings.Join(m.accounts, ", ")))
	}
	lines = append(lines, "")
	lines = append(lines, theme.Muted.Render("enter: add account  ctrl+s: save  esc: quit"))
	if m.err != "" {
		lines = append(lines, theme.StatusErr.Render(m.err))
	}
	return theme.ModalBox.Render(strings.Join(lines, "\n"))
}
