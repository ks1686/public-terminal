package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/ks1686/public-terminal/internal/config"
	"github.com/ks1686/public-terminal/internal/rebalance"
	"github.com/ks1686/public-terminal/internal/tui"
)

const version = "0.5.3"

func main() {
	var (
		showVersion = flag.Bool("version", false, "print version and exit")
		doRebalance = flag.Bool("rebalance", false, "run the rebalancer and exit")
		dryRun      = flag.Bool("dry-run", false, "dry-run mode (no orders placed)")
		accountID   = flag.String("account", "", "account ID to operate on (default: first configured account)")
		installSvc  = flag.Bool("install-service", false, "install systemd/launchd service files and exit")
		removeSvc   = flag.Bool("remove-service", false, "remove systemd/launchd service files and exit")
		validate    = flag.Bool("validate", false, "compute rebalance plan without placing orders and print it")
	)
	flag.Parse()

	config.MigrateIfNeeded()

	if *showVersion {
		fmt.Println("public-terminal", version)
		return
	}

	if *installSvc {
		exe, err := os.Executable()
		if err != nil {
			fmt.Fprintln(os.Stderr, "could not determine binary path:", err)
			os.Exit(1)
		}
		if err := config.InstallServiceFiles(exe); err != nil {
			fmt.Fprintln(os.Stderr, "install-service failed:", err)
			os.Exit(1)
		}
		fmt.Println("Service files installed.")
		return
	}

	if *removeSvc {
		if err := config.RemoveServiceFiles(); err != nil {
			fmt.Fprintln(os.Stderr, "remove-service failed:", err)
			os.Exit(1)
		}
		fmt.Println("Service files removed.")
		return
	}

	// Resolve account
	accounts := config.GetAccounts()
	acct := *accountID
	if acct == "" {
		if len(accounts) > 0 {
			acct = accounts[0]
		}
	}

	if *validate {
		if acct == "" {
			fmt.Fprintln(os.Stderr, "no account configured; run public-terminal (TUI) to set up")
			os.Exit(1)
		}
		fmt.Println("VALIDATE MODE — no orders will be placed")
		fmt.Println("Account:", acct)
		if err := rebalance.Run(acct, true); err != nil {
			fmt.Fprintln(os.Stderr, "validate failed:", err)
			os.Exit(1)
		}
		return
	}

	if *doRebalance {
		if acct == "" {
			fmt.Fprintln(os.Stderr, "no account configured; run public-terminal (TUI) to set up")
			os.Exit(1)
		}
		// When --account is explicit, run only that account.
		// When defaulting to accounts[0], run all accounts that have rebalance enabled.
		targets := []string{acct}
		if *accountID == "" && len(accounts) > 1 {
			targets = accounts
		}
		failed := false
		for _, a := range targets {
			if err := rebalance.Run(a, *dryRun); err != nil {
				fmt.Fprintf(os.Stderr, "rebalance failed for account %s: %v\n", a, err)
				failed = true
			}
		}
		if failed {
			os.Exit(1)
		}
		return
	}

	// Default: launch TUI
	activeIdx := 0
	if *accountID != "" {
		for i, a := range accounts {
			if a == *accountID {
				activeIdx = i
				break
			}
		}
	}

	if err := tui.Run(accounts, activeIdx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
