package config

import (
	"strings"
	"testing"
	"time"
)

func TestLaunchdHourForNoonET_EasternStandard(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	// Monday 09:00 EST (UTC-5) — next noon ET is today, local hour 12.
	now := time.Date(2026, 1, 5, 9, 0, 0, 0, loc)
	if got := LaunchdHourForNoonET(now); got != 12 {
		t.Fatalf("LaunchdHourForNoonET(%v) = %d, want 12", now, got)
	}
}

func TestLaunchdHourForNoonET_PacificStandard(t *testing.T) {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatal(err)
	}
	// Monday morning PT: noon ET is 09:00 local.
	now := time.Date(2026, 1, 5, 8, 0, 0, 0, loc)
	if got := LaunchdHourForNoonET(now); got != 9 {
		t.Fatalf("LaunchdHourForNoonET(%v) = %d, want 9", now, got)
	}
}

func TestLaunchdHourForNoonET_SkipsWeekend(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	// Saturday afternoon — next weekday noon is Monday.
	now := time.Date(2026, 1, 3, 15, 0, 0, 0, loc)
	if got := LaunchdHourForNoonET(now); got != 12 {
		t.Fatalf("LaunchdHourForNoonET(%v) = %d, want 12", now, got)
	}
}

func TestLaunchdPlistBody_WeekdaysAndHour(t *testing.T) {
	body := launchdPlistBody("/usr/local/bin/public-terminal", 9)
	if !strings.Contains(body, LaunchdLabel) {
		t.Fatal("plist missing label")
	}
	if !strings.Contains(body, "<string>--rebalance</string>") {
		t.Fatal("plist missing --rebalance argument")
	}
	if strings.Count(body, "<key>Weekday</key>") != 5 {
		t.Fatalf("expected 5 weekday intervals, got %d", strings.Count(body, "<key>Weekday</key>"))
	}
	if !strings.Contains(body, "<key>Hour</key><integer>9</integer>") {
		t.Fatal("plist missing converted local hour")
	}
}
