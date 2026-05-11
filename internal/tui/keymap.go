package tui

import "github.com/charmbracelet/bubbles/key"

// KeyMap holds all application-level key bindings.
type KeyMap struct {
	Quit          key.Binding
	Refresh       key.Binding
	Buy           key.Binding
	Sell          key.Binding
	ViewOrder     key.Binding
	Cancel        key.Binding
	History       key.Binding
	ToggleLive    key.Binding
	ToggleTimer   key.Binding
	InstallSvc    key.Binding
	SkipRebalance key.Binding
	RebalanceNow  key.Binding
	RebalanceCfg  key.Binding
	ChartPrev     key.Binding
	ChartNext     key.Binding
	PrevAccount   key.Binding
	NextAccount   key.Binding
	ManageAccts   key.Binding
}

var DefaultKeyMap = KeyMap{
	Quit:          key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
	Refresh:       key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
	Buy:           key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "buy")),
	Sell:          key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "sell")),
	ViewOrder:     key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "view order")),
	Cancel:        key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "cancel order")),
	History:       key.NewBinding(key.WithKeys("h"), key.WithHelp("h", "history")),
	ToggleLive:    key.NewBinding(key.WithKeys("l"), key.WithHelp("l", "live chart")),
	ToggleTimer:   key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "pause/resume rebalancer")),
	InstallSvc:    key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "install/remove service")),
	SkipRebalance: key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "skip/unskip rebalance")),
	RebalanceNow:  key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "rebalance now")),
	RebalanceCfg:  key.NewBinding(key.WithKeys("S"), key.WithHelp("S", "rebalance config")),
	ChartPrev:     key.NewBinding(key.WithKeys("["), key.WithHelp("[", "chart period prev")),
	ChartNext:     key.NewBinding(key.WithKeys("]"), key.WithHelp("]", "chart period next")),
	PrevAccount:   key.NewBinding(key.WithKeys("ctrl+left"), key.WithHelp("ctrl+←", "prev account")),
	NextAccount:   key.NewBinding(key.WithKeys("ctrl+right"), key.WithHelp("ctrl+→", "next account")),
	ManageAccts:   key.NewBinding(key.WithKeys("ctrl+a"), key.WithHelp("ctrl+a", "manage accounts")),
}
