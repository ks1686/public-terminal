package api

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// Client wraps the public CLI binary with --json output.
type Client struct {
	bin       string
	accountID string
	token     string
}

// NewClient creates a Client. It resolves the public binary from PATH or
// ~/.local/bin/public, and reads the token from the env file path provided.
func NewClient(accountID, envFilePath string) (*Client, error) {
	bin, err := resolveBin()
	if err != nil {
		return nil, err
	}
	token := readTokenFromEnv(envFilePath)
	return &Client{bin: bin, accountID: accountID, token: token}, nil
}

func resolveBin() (string, error) {
	if p, err := exec.LookPath("public"); err == nil {
		return p, nil
	}
	candidates := []string{
		filepath.Join(homeDir(), ".local", "bin", "public"),
		"/usr/local/bin/public",
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}
	return "", fmt.Errorf("public CLI binary not found; run: uv tool install publicdotcom-cli")
}

func homeDir() string {
	h, _ := os.UserHomeDir()
	return h
}

func readTokenFromEnv(envFile string) string {
	b, err := os.ReadFile(envFile)
	if err != nil {
		return os.Getenv("PUBLIC_ACCESS_TOKEN")
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "PUBLIC_ACCESS_TOKEN=") {
			return strings.TrimPrefix(line, "PUBLIC_ACCESS_TOKEN=")
		}
	}
	return os.Getenv("PUBLIC_ACCESS_TOKEN")
}

// run executes: public [globalFlags...] subcommand [args...]
// Returns stdout bytes; returns error with stderr message on failure.
func (c *Client) run(args ...string) ([]byte, error) {
	global := []string{"--json"}
	if c.token != "" {
		global = append(global, "--token", c.token)
	}
	cmdArgs := append(global, args...)
	cmd := exec.Command(c.bin, cmdArgs...)
	cmd.Env = append(os.Environ(), "PUBLIC_ACCESS_TOKEN="+c.token)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("public %s: %s", strings.Join(args[:min(2, len(args))], " "), strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, err
	}
	return out, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ─────────────────────────────────────────────────────────────────────────────
// Portfolio
// ─────────────────────────────────────────────────────────────────────────────

func (c *Client) GetPortfolio() (*Portfolio, error) {
	out, err := c.run("portfolio", "show", "-a", c.accountID)
	if err != nil {
		return nil, err
	}
	var p Portfolio
	if err := json.Unmarshal(out, &p); err != nil {
		return nil, fmt.Errorf("parsing portfolio: %w", err)
	}
	return &p, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Orders
// ─────────────────────────────────────────────────────────────────────────────

func (c *Client) PlaceOrder(req OrderRequest) error {
	f, err := os.CreateTemp("", "pt-order-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := json.NewEncoder(f).Encode(req); err != nil {
		f.Close()
		return err
	}
	f.Close()
	_, err = c.run("order", "place", "-f", f.Name(), "-y", "-a", c.accountID)
	return err
}

func (c *Client) CancelOrder(orderID string) error {
	_, err := c.run("order", "cancel", orderID, "-y", "-a", c.accountID)
	return err
}

func (c *Client) GetOrder(orderID string) (*Order, error) {
	out, err := c.run("order", "get", orderID, "-a", c.accountID)
	if err != nil {
		return nil, err
	}
	var o Order
	if err := json.Unmarshal(out, &o); err != nil {
		return nil, fmt.Errorf("parsing order: %w", err)
	}
	return &o, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// History
// ─────────────────────────────────────────────────────────────────────────────

func (c *Client) ListHistory(pageSize int) ([]HistoryEntry, error) {
	// Fetch up to 90 days
	end := time.Now()
	start := end.AddDate(0, 0, -90)
	args := []string{
		"history", "list",
		"-a", c.accountID,
		"--start", start.Format(time.RFC3339),
		"--end", end.Format(time.RFC3339),
		"--page-size", fmt.Sprintf("%d", pageSize),
	}
	out, err := c.run(args...)
	if err != nil {
		return nil, err
	}
	var resp HistoryResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		// Might be a bare array
		var items []HistoryEntry
		if err2 := json.Unmarshal(out, &items); err2 != nil {
			return nil, fmt.Errorf("parsing history: %w", err)
		}
		return items, nil
	}
	return resp.Items, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Instruments
// ─────────────────────────────────────────────────────────────────────────────

func (c *Client) GetInstrument(symbol, instrType string) (*InstrumentDetail, error) {
	out, err := c.run("instruments", "get", strings.ToUpper(symbol), strings.ToUpper(instrType))
	if err != nil {
		return nil, err
	}
	var d InstrumentDetail
	if err := json.Unmarshal(out, &d); err != nil {
		return nil, fmt.Errorf("parsing instrument: %w", err)
	}
	return &d, nil
}

func (c *Client) ListTradableInstruments(instrType, tradingFilter string) ([]InstrumentDetail, error) {
	out, err := c.run("instruments", "list",
		"--type-filter", strings.ToUpper(instrType),
		"--trading-filter", tradingFilter,
	)
	if err != nil {
		return nil, err
	}
	var resp InstrumentsListResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, fmt.Errorf("parsing instruments: %w", err)
	}
	return resp.Instruments, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Market quotes
// ─────────────────────────────────────────────────────────────────────────────

func (c *Client) GetQuotes(symbols []string, instrType string) ([]Quote, error) {
	if len(symbols) == 0 {
		return nil, nil
	}
	args := append([]string{"market", "quotes", "--type", instrType}, symbols...)
	out, err := c.run(args...)
	if err != nil {
		return nil, err
	}
	var quotes []Quote
	if err := json.Unmarshal(out, &quotes); err != nil {
		return nil, fmt.Errorf("parsing quotes: %w", err)
	}
	return quotes, nil
}

func (c *Client) GetCryptoQuote(symbol string) (decimal.Decimal, error) {
	quotes, err := c.GetQuotes([]string{symbol}, "CRYPTO")
	if err != nil || len(quotes) == 0 {
		return decimal.Zero, fmt.Errorf("no quote for %s: %w", symbol, err)
	}
	q := quotes[0]
	if q.Last != nil && q.Last.IsPositive() {
		return *q.Last, nil
	}
	if q.Bid != nil && q.Ask != nil {
		return q.Bid.Add(*q.Ask).Div(decimal.NewFromInt(2)), nil
	}
	return decimal.Zero, fmt.Errorf("no price for %s", symbol)
}

// ─────────────────────────────────────────────────────────────────────────────
// Historic bars (for chart)
// ─────────────────────────────────────────────────────────────────────────────

// ChartPeriods maps (label, CLI period, CLI aggregation) for the 5 chart tabs.
var ChartPeriods = []ChartPeriod{
	{Label: "24H", Period: "DAY", Aggregation: "FIVE_MINUTES"},
	{Label: "1W", Period: "WEEK", Aggregation: "ONE_HOUR"},
	{Label: "1M", Period: "MONTH", Aggregation: "ONE_DAY"},
	{Label: "3M", Period: "QUARTER", Aggregation: "ONE_DAY"},
	{Label: "1Y", Period: "YEAR", Aggregation: "ONE_DAY"},
}

type ChartPeriod struct {
	Label       string
	Period      string
	Aggregation string
}

func (c *Client) GetHistoricBars(symbol, period, aggregation string) ([]Bar, error) {
	args := []string{"historicdata", "bars", symbol, period}
	if aggregation != "" {
		args = append(args, "--aggregation", aggregation)
	}
	out, err := c.run(args...)
	if err != nil {
		return nil, err
	}
	var resp BarsResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		// Try bare array
		var bars []Bar
		if err2 := json.Unmarshal(out, &bars); err2 != nil {
			return nil, fmt.Errorf("parsing bars: %w", err)
		}
		return bars, nil
	}
	return resp.Bars, nil
}
