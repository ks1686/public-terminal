package api

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// Client wraps the public CLI binary with --json output.
// Authentication is handled entirely by the public CLI (via keychain after
// `public auth login`). This project does not read, store, or pass any tokens.
type Client struct {
	bin       string
	accountID string
}

// NewClient creates a Client. It resolves the public binary from PATH or
// common install locations. The public CLI must already be authenticated
// via `public auth login`.
func NewClient(accountID string) (*Client, error) {
	bin, err := resolveBin()
	if err != nil {
		return nil, err
	}
	return &Client{bin: bin, accountID: accountID}, nil
}

func resolveBin() (string, error) {
	if p, err := exec.LookPath("public"); err == nil {
		return p, nil
	}
	candidates := []string{
		filepath.Join(homeDir(), ".local", "bin", "public"),
		filepath.Join(homeDir(), ".local", "bin", "public.exe"),
		"/usr/local/bin/public",
		"/opt/homebrew/bin/public",
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}
	return "", fmt.Errorf("public CLI not found; install from https://github.com/anomalyco/publicdotcom-cli")
}

func homeDir() string {
	h, _ := os.UserHomeDir()
	return h
}

// run executes: public --json <args...>
// --json is a root-level flag in the publicdotcom-cli, so it must come BEFORE
// the subcommand. Authentication is handled by the CLI via OS keychain
// (set up with `public auth login`).
func (c *Client) run(timeout time.Duration, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	fullArgs := append([]string{"--json"}, args...)
	cmd := exec.CommandContext(ctx, c.bin, fullArgs...)

	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("public %s: timed out after %v", cmdLabel(args), timeout)
		}
		if ee, ok := err.(*exec.ExitError); ok {
			stderr := strings.TrimSpace(string(ee.Stderr))
			if stderr != "" {
				return nil, fmt.Errorf("public %s: %s", cmdLabel(args), stderr)
			}
			return nil, fmt.Errorf("public %s: exit %d", cmdLabel(args), ee.ExitCode())
		}
		return nil, fmt.Errorf("public %s: %w", cmdLabel(args), err)
	}
	return out, nil
}

func cmdLabel(args []string) string {
	n := 2
	if len(args) < n {
		n = len(args)
	}
	return strings.Join(args[:n], " ")
}

// ─────────────────────────────────────────────────────────────────────────────
// Portfolio
// ─────────────────────────────────────────────────────────────────────────────

func (c *Client) GetPortfolio() (*Portfolio, error) {
	out, err := c.run(30*time.Second, "portfolio", "show", "-a", c.accountID)
	if err != nil {
		return nil, err
	}
	var p Portfolio
	if err := json.Unmarshal(out, &p); err != nil {
		return nil, fmt.Errorf("parsing portfolio JSON: %w", err)
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
	_, err = c.run(30*time.Second, "order", "place", "-f", f.Name(), "-y", "-a", c.accountID)
	return err
}

func (c *Client) CancelOrder(orderID string) error {
	_, err := c.run(30*time.Second, "order", "cancel", orderID, "-y", "-a", c.accountID)
	return err
}

func (c *Client) GetOrder(orderID string) (*Order, error) {
	out, err := c.run(30*time.Second, "order", "get", orderID, "-a", c.accountID)
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

const (
	HistoryPageSize        = 100
	HistoryMaxPages        = 20
	HistoryMaxTransactions = 1000
)

// ListHistory pulls the last 90 days of transactions, walking nextToken until
// the cap is hit. Returns newest-first. Mirrors Python HistoryModal._load_history.
func (c *Client) ListHistory() ([]HistoryEntry, bool, error) {
	end := time.Now().UTC()
	start := end.AddDate(0, 0, -90)
	startStr := start.Format("2006-01-02T15:04:05Z")
	endStr := end.Format("2006-01-02T15:04:05Z")

	var (
		all       []HistoryEntry
		nextToken string
		truncated bool
	)
	for page := 0; page < HistoryMaxPages; page++ {
		args := []string{
			"history", "list",
			"-a", c.accountID,
			"--start", startStr,
			"--end", endStr,
			"--page-size", fmt.Sprintf("%d", HistoryPageSize),
		}
		if nextToken != "" {
			args = append(args, "--next-token", nextToken)
		}
		out, err := c.run(30*time.Second, args...)
		if err != nil {
			return nil, false, err
		}
		var resp HistoryResponse
		if err := json.Unmarshal(out, &resp); err != nil {
			return nil, false, fmt.Errorf("parsing history: %w", err)
		}
		all = append(all, resp.Transactions...)
		nextToken = resp.NextToken
		if nextToken == "" {
			break
		}
		if len(all) >= HistoryMaxTransactions {
			truncated = true
			break
		}
	}
	if nextToken != "" && !truncated {
		truncated = true
	}

	// Newest first, then cap.
	sort.SliceStable(all, func(i, j int) bool { return all[i].Timestamp > all[j].Timestamp })
	if len(all) > HistoryMaxTransactions {
		all = all[:HistoryMaxTransactions]
	}
	return all, truncated, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Instruments
// ─────────────────────────────────────────────────────────────────────────────

func (c *Client) GetInstrument(symbol, instrType string) (*InstrumentDetail, error) {
	out, err := c.run(15*time.Second, "instruments", "get", strings.ToUpper(symbol), strings.ToUpper(instrType))
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
	out, err := c.run(60*time.Second, "instruments", "list",
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
	args := []string{"market", "quotes", "--type", instrType, "--account-id", c.accountID}
	args = append(args, symbols...)
	out, err := c.run(15*time.Second, args...)
	if err != nil {
		return nil, err
	}
	var resp QuotesResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		// Older payload was a bare array — try that as a fallback.
		var bare []Quote
		if err2 := json.Unmarshal(out, &bare); err2 == nil {
			return bare, nil
		}
		return nil, fmt.Errorf("parsing quotes: %w", err)
	}
	return resp.Quotes, nil
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

func (c *Client) GetHistoricBars(symbol, period, aggregation string) ([]Bar, error) {
	args := []string{"historicdata", "bars", strings.ToUpper(symbol), strings.ToUpper(period)}
	if aggregation != "" {
		args = append(args, "--aggregation", strings.ToUpper(aggregation))
	}
	out, err := c.run(30*time.Second, args...)
	if err != nil {
		return nil, err
	}
	// Try the session-partitioned shape first.
	var resp BarsResponse
	respErr := json.Unmarshal(out, &resp)
	if respErr == nil {
		if flat := resp.Flatten(); len(flat) > 0 {
			return flat, nil
		}
	}
	// Fallback: some endpoints may return a flat list.
	var bars []Bar
	if listErr := json.Unmarshal(out, &bars); listErr == nil {
		return bars, nil
	}
	// Neither shape parsed — surface whichever error we have, with raw context.
	if respErr != nil {
		return nil, fmt.Errorf("parsing bars (session shape): %w", respErr)
	}
	return nil, fmt.Errorf("bars response had no points")
}
