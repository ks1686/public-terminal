// Package config mirrors the Python config.py: same file paths, same JSON
// formats. Existing ~/.config/public-terminal/ data is fully compatible.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ─────────────────────────────────────────────────────────────────────────────
// Paths
// ─────────────────────────────────────────────────────────────────────────────

// AppDir returns the base config/data directory.
func AppDir() string {
	xdg := os.Getenv("XDG_CONFIG_HOME")
	if xdg == "" {
		xdg = filepath.Join(homeDir(), ".config")
	}
	dir := filepath.Join(xdg, "public-terminal")
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

func homeDir() string {
	h, _ := os.UserHomeDir()
	return h
}

func accountDir(accountID string) string {
	dir := filepath.Join(AppDir(), "accounts", norm(accountID))
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

func cacheDir(accountID string) string {
	dir := filepath.Join(accountDir(accountID), "cache")
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

func norm(id string) string { return strings.ToUpper(strings.TrimSpace(id)) }

func AccountsFile() string          { return filepath.Join(AppDir(), "accounts.json") }
func RebalanceConfigPath(id string) string { return filepath.Join(accountDir(id), "rebalance_config.json") }
func PortfolioCachePath(id string) string  { return filepath.Join(cacheDir(id), "portfolio_cache.json") }
func IndexCachePath(id, index string) string {
	return filepath.Join(cacheDir(id), "constituents_"+strings.ToUpper(index)+".json")
}
func RebalanceLogPath(id string) string  { return filepath.Join(cacheDir(id), "rebalance.log") }
func TodayBuysPath(id string) string     { return filepath.Join(cacheDir(id), "today_buys.json") }
func SkipFilePath(id string) string      { return filepath.Join(cacheDir(id), "skip_next_rebalance") }
func MarketCapCachePath(id string) string { return filepath.Join(cacheDir(id), "market_caps.json") }

// ─────────────────────────────────────────────────────────────────────────────
// Accounts
// ─────────────────────────────────────────────────────────────────────────────

func GetAccounts() []string {
	b, err := os.ReadFile(AccountsFile())
	if err != nil {
		return nil
	}
	var raw []string
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, a := range raw {
		if n := norm(a); n != "" {
			out = append(out, n)
		}
	}
	return out
}

func AddAccount(id string) error {
	n := norm(id)
	if n == "" {
		return fmt.Errorf("empty account ID")
	}
	accounts := GetAccounts()
	for _, a := range accounts {
		if a == n {
			return nil // already present
		}
	}
	accounts = append(accounts, n)
	_ = accountDir(n) // ensure directory exists
	return writeJSON(AccountsFile(), accounts)
}

func RemoveAccount(id string) error {
	n := norm(id)
	accounts := GetAccounts()
	if len(accounts) <= 1 {
		return fmt.Errorf("cannot remove the last account")
	}
	filtered := accounts[:0]
	for _, a := range accounts {
		if a != n {
			filtered = append(filtered, a)
		}
	}
	if err := writeJSON(AccountsFile(), filtered); err != nil {
		return err
	}
	// Remove account directory
	dir := filepath.Join(AppDir(), "accounts", n)
	_ = os.RemoveAll(dir)
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Rebalancer config
// ─────────────────────────────────────────────────────────────────────────────

type RebalanceConfig struct {
	Index            string             `json:"index"`
	TopN             int                `json:"top_n"`
	MarginUsagePct   float64            `json:"margin_usage_pct"`
	ExcludedTickers  []string           `json:"excluded_tickers"`
	Allocations      map[string]float64 `json:"allocations"`
	RebalanceEnabled bool               `json:"rebalance_enabled"`
}

var DefaultAllocations = map[string]float64{
	"stocks": 0.65,
	"btc":    0.12,
	"eth":    0.04,
	"sol":    0.04,
	"gold":   0.10,
	"cash":   0.05,
}

func LoadRebalanceConfig(accountID string) RebalanceConfig {
	cfg := RebalanceConfig{
		Index:            "SP500",
		TopN:             500,
		MarginUsagePct:   0.5,
		RebalanceEnabled: false,
		Allocations:      DefaultAllocations,
	}
	b, err := os.ReadFile(RebalanceConfigPath(accountID))
	if err != nil {
		return cfg
	}
	_ = json.Unmarshal(b, &cfg)
	if cfg.Allocations == nil {
		cfg.Allocations = DefaultAllocations
	}
	return cfg
}

func SaveRebalanceConfig(accountID string, cfg RebalanceConfig) error {
	// Normalize excluded tickers
	seen := map[string]bool{}
	cleaned := cfg.ExcludedTickers[:0]
	for _, t := range cfg.ExcludedTickers {
		t = norm(t)
		if t != "" && !seen[t] {
			seen[t] = true
			cleaned = append(cleaned, t)
		}
	}
	cfg.ExcludedTickers = cleaned
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(RebalanceConfigPath(accountID), b, 0o644)
}

// ─────────────────────────────────────────────────────────────────────────────
// Systemd / launchd service management
// ─────────────────────────────────────────────────────────────────────────────

const (
	ServiceUnit = "public-terminal-rebalance.service"
	TimerUnit   = "public-terminal-rebalance.timer"
)

func SystemdUserDir() string {
	return filepath.Join(homeDir(), ".config", "systemd", "user")
}

func HasSystemctl() bool {
	_, err := os.Stat("/usr/bin/systemctl")
	if err == nil {
		return true
	}
	// Also check via PATH
	for _, p := range filepath.SplitList(os.Getenv("PATH")) {
		if _, err := os.Stat(filepath.Join(p, "systemctl")); err == nil {
			return true
		}
	}
	return false
}

func InstallServiceFiles(binaryPath string) error {
	if runtime.GOOS == "darwin" {
		return installLaunchdPlist(binaryPath)
	}
	return installSystemdUnits(binaryPath)
}

func RemoveServiceFiles() error {
	if runtime.GOOS == "darwin" {
		return removeLaunchdPlist()
	}
	return removeSystemdUnits()
}

func installSystemdUnits(binaryPath string) error {
	dir := SystemdUserDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	service := fmt.Sprintf(`[Unit]
Description=Public Terminal — daily portfolio rebalance
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
ExecStart=%s --rebalance
WorkingDirectory=%s
StandardOutput=journal
StandardError=journal
Restart=on-failure
RestartSec=60
StartLimitBurst=3
StartLimitIntervalSec=300
`, binaryPath, filepath.Dir(binaryPath))

	timer := `[Unit]
Description=Run portfolio rebalance daily at 12:00 ET

[Timer]
OnCalendar=Mon..Fri *-*-* 12:00:00
TimeZone=America/New_York
Persistent=true

[Install]
WantedBy=timers.target
`
	if err := os.WriteFile(filepath.Join(dir, ServiceUnit), []byte(service), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, TimerUnit), []byte(timer), 0o644)
}

func removeSystemdUnits() error {
	dir := SystemdUserDir()
	_ = os.Remove(filepath.Join(dir, ServiceUnit))
	_ = os.Remove(filepath.Join(dir, TimerUnit))
	return nil
}

func launchdPlistPath() string {
	return filepath.Join(homeDir(), "Library", "LaunchAgents", "com.public-terminal.rebalance.plist")
}

func installLaunchdPlist(binaryPath string) error {
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.public-terminal.rebalance</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
        <string>--rebalance</string>
    </array>
    <key>StartCalendarInterval</key>
    <array>
        <dict><key>Weekday</key><integer>1</integer><key>Hour</key><integer>12</integer><key>Minute</key><integer>0</integer></dict>
        <dict><key>Weekday</key><integer>2</integer><key>Hour</key><integer>12</integer><key>Minute</key><integer>0</integer></dict>
        <dict><key>Weekday</key><integer>3</integer><key>Hour</key><integer>12</integer><key>Minute</key><integer>0</integer></dict>
        <dict><key>Weekday</key><integer>4</integer><key>Hour</key><integer>12</integer><key>Minute</key><integer>0</integer></dict>
        <dict><key>Weekday</key><integer>5</integer><key>Hour</key><integer>12</integer><key>Minute</key><integer>0</integer></dict>
    </array>
    <key>StandardOutPath</key>
    <string>%s</string>
    <key>StandardErrorPath</key>
    <string>%s</string>
    <key>RunAtLoad</key>
    <false/>
</dict>
</plist>
`, binaryPath, filepath.Join(AppDir(), "rebalance.log"), filepath.Join(AppDir(), "rebalance.log"))

	dir := filepath.Join(homeDir(), "Library", "LaunchAgents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(launchdPlistPath(), []byte(plist), 0o644)
}

func removeLaunchdPlist() error {
	_ = os.Remove(launchdPlistPath())
	return nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

func writeJSON(path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
