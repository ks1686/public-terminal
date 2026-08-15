package config

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const LaunchdLabel = "com.public-terminal.rebalance"

func launchdUID() string {
	return strconv.Itoa(os.Getuid())
}

func launchdDomain() string {
	return "gui/" + launchdUID()
}

func launchdServiceTarget() string {
	return launchdDomain() + "/" + LaunchdLabel
}

// launchctlRun executes launchctl with a short timeout.
func launchctlRun(args ...string) (int, string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "launchctl", args...)
	out, err := cmd.CombinedOutput()
	rc := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			rc = ee.ExitCode()
		} else {
			rc = -1
		}
	}
	return rc, strings.TrimSpace(string(out))
}

// LaunchdHourForNoonET returns the local wall-clock hour that corresponds to
// 12:00 America/New_York on the next weekday. launchd StartCalendarInterval is
// always interpreted in the machine's local timezone.
func LaunchdHourForNoonET(now time.Time) int {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		return 12
	}
	et := now.In(loc)
	// Next weekday noon ET (today if weekday and before noon, else next weekday).
	candidate := time.Date(et.Year(), et.Month(), et.Day(), 12, 0, 0, 0, loc)
	if !candidate.After(et) {
		candidate = candidate.Add(24 * time.Hour)
	}
	for candidate.Weekday() == time.Saturday || candidate.Weekday() == time.Sunday {
		candidate = candidate.Add(24 * time.Hour)
	}
	return candidate.In(now.Location()).Hour()
}

func launchdPlistBody(binaryPath string, hour int) string {
	logPath := filepathJoinAppRebalanceLog()
	var intervals strings.Builder
	for _, weekday := range []int{1, 2, 3, 4, 5} {
		fmt.Fprintf(&intervals, "        <dict><key>Weekday</key><integer>%d</integer><key>Hour</key><integer>%d</integer><key>Minute</key><integer>0</integer></dict>\n", weekday, hour)
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>%s</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
        <string>--rebalance</string>
    </array>
    <key>StartCalendarInterval</key>
    <array>
%s    </array>
    <key>StandardOutPath</key>
    <string>%s</string>
    <key>StandardErrorPath</key>
    <string>%s</string>
    <key>RunAtLoad</key>
    <false/>
</dict>
</plist>
`, LaunchdLabel, binaryPath, intervals.String(), logPath, logPath)
}

func filepathJoinAppRebalanceLog() string {
	return AppDir() + "/rebalance.log"
}

func LaunchdInstalled() bool {
	_, err := os.Stat(launchdPlistPath())
	return err == nil
}

func LaunchdLoaded() bool {
	rc, _ := launchctlRun("print", launchdServiceTarget())
	return rc == 0
}

func bootstrapLaunchd() error {
	// bootout first so a rewrite replaces a stale ProgramArguments path.
	_, _ = launchctlRun("bootout", launchdServiceTarget())
	rc, out := launchctlRun("bootstrap", launchdDomain(), launchdPlistPath())
	if rc != 0 {
		return fmt.Errorf("launchctl bootstrap: %s", out)
	}
	return nil
}

func bootoutLaunchd() {
	_, _ = launchctlRun("bootout", launchdServiceTarget())
}

// LaunchdEnable bootstraps the agent if needed so weekday noon-ET runs fire.
func LaunchdEnable() error {
	if !LaunchdInstalled() {
		return fmt.Errorf("launchd plist not installed")
	}
	if LaunchdLoaded() {
		return nil
	}
	return bootstrapLaunchd()
}

// LaunchdDisable unloads the agent but leaves the plist on disk so it can be resumed.
func LaunchdDisable() {
	bootoutLaunchd()
}
