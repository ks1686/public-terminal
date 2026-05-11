package modals

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/ks1686/public-terminal/internal/config"
	"github.com/ks1686/public-terminal/internal/tui/theme"
)

// AccountsModal lets the user add/remove accounts.
type AccountsModal struct {
	accounts []string
	cursor   int
	input    textinput.Model
	mode     int // 0=list 1=add
	err      string
}

type AccountsUpdatedMsg struct{ Accounts []string }
type AccountsClosedMsg struct{}

func NewAccountsModal(accounts []string) AccountsModal {
	ti := textinput.New()
	ti.Placeholder = "Account ID (e.g. DW12345678)"
	return AccountsModal{accounts: append([]string(nil), accounts...)}
}

func (m AccountsModal) Init() tea.Cmd { return nil }

func (m AccountsModal) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch m.mode {
		case 0:
			switch msg.String() {
			case "esc", "q":
				return m, func() tea.Msg { return AccountsClosedMsg{} }
			case "a":
				m.mode = 1
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
		case 1:
			switch msg.String() {
			case "esc":
				m.mode = 0
				m.input.Blur()
				m.input.Reset()
			case "enter":
				id := strings.TrimSpace(m.input.Value())
				if id == "" {
					m.err = "Account ID cannot be empty."
					return m, nil
				}
				if err := config.AddAccount(id); err != nil {
					m.err = err.Error()
					return m, nil
				}
				m.accounts = append(m.accounts, id)
				m.input.Reset()
				m.mode = 0
				accounts := m.accounts
				return m, func() tea.Msg { return AccountsUpdatedMsg{Accounts: accounts} }
			}
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}
	}
	return m, nil
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
	if m.mode == 1 {
		lines = append(lines, "New account: "+m.input.View())
		lines = append(lines, theme.Muted.Render("enter: add  esc: cancel"))
	} else {
		lines = append(lines, theme.Muted.Render("a: add  d/del: remove  ↑↓: navigate  esc: close"))
	}
	if m.err != "" {
		lines = append(lines, theme.StatusErr.Render(m.err))
	}
	return theme.ModalBox.Render(strings.Join(lines, "\n"))
}
