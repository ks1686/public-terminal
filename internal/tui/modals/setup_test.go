package modals

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestNewSetupModal(t *testing.T) {
	m := NewSetupModal()
	if m.accountInput.Placeholder == "" {
		t.Error("account input should have placeholder")
	}
}

func TestSetupModal_Init(t *testing.T) {
	m := NewSetupModal()
	cmd := m.Init()
	if cmd == nil {
		t.Error("Init should return a command")
	}
}

func TestSetupModal_View(t *testing.T) {
	m := NewSetupModal()
	v := m.View()
	if v == "" {
		t.Error("View returned empty string")
	}
}

func TestSetupModal_AddAccount(t *testing.T) {
	m := NewSetupModal()
	m.accountInput.SetValue("ACCT001")
	// Simulate enter key
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m2 := updated.(SetupModal)
	if len(m2.accounts) != 1 || m2.accounts[0] != "ACCT001" {
		t.Errorf("accounts = %v, want [ACCT001]", m2.accounts)
	}
}

func TestSetupModal_Escape(t *testing.T) {
	m := NewSetupModal()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	if cmd == nil {
		t.Error("esc should return a quit command")
	}
}

func TestSetupDoneMsg(t *testing.T) {
	msg := SetupDoneMsg{Accounts: []string{"ACCT001"}}
	if len(msg.Accounts) != 1 {
		t.Errorf("expected 1 account, got %d", len(msg.Accounts))
	}
}
