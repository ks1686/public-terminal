package tui

import "github.com/charmbracelet/bubbles/key"

// KeyMap holds all application-level key bindings.
type KeyMap struct {
	Quit          key.Binding
	Refresh       key.Binding
	PaneNext      key.Binding
	PanePrev      key.Binding
	PaneLeft      key.Binding
	PaneRight     key.Binding
	PaneUp        key.Binding
	PaneDown      key.Binding
	Buy           key.Binding
	Sell          key.Binding
	ViewOrder     key.Binding
	Cancel        key.Binding
	History       key.Binding
	ToggleTimer   key.Binding
	InstallSvc    key.Binding
	SkipRebalance key.Binding
	RebalanceNow  key.Binding
	RebalanceCfg  key.Binding
	PrevAccount   key.Binding
	NextAccount   key.Binding
	ManageAccts   key.Binding
}

var DefaultKeyMap = KeyMap{
	Quit:          key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
	Refresh:       key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
	PaneNext:      key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next pane")),
	PanePrev:      key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "prev pane")),
	PaneLeft:      key.NewBinding(key.WithKeys("alt+left"), key.WithHelp("alt+←", "pane left")),
	PaneRight:     key.NewBinding(key.WithKeys("alt+right"), key.WithHelp("alt+→", "pane right")),
	PaneUp:        key.NewBinding(key.WithKeys("alt+up"), key.WithHelp("alt+↑", "pane up")),
	PaneDown:      key.NewBinding(key.WithKeys("alt+down"), key.WithHelp("alt+↓", "pane down")),
	Buy:           key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "buy")),
	Sell:          key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "sell")),
	ViewOrder:     key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "view order")),
	Cancel:        key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "cancel order")),
	History:       key.NewBinding(key.WithKeys("h"), key.WithHelp("h", "history")),
	ToggleTimer:   key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "pause/resume rebalancer")),
	InstallSvc:    key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "install/remove service")),
	SkipRebalance: key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "skip/unskip rebalance")),
	RebalanceNow:  key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "rebalance now")),
	RebalanceCfg:  key.NewBinding(key.WithKeys("S"), key.WithHelp("S", "rebalance config")),
	PrevAccount:   key.NewBinding(key.WithKeys("ctrl+left"), key.WithHelp("ctrl+←", "prev account")),
	NextAccount:   key.NewBinding(key.WithKeys("ctrl+right"), key.WithHelp("ctrl+→", "next account")),
	ManageAccts:   key.NewBinding(key.WithKeys("ctrl+a"), key.WithHelp("ctrl+a", "manage accounts")),
}
