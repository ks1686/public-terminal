package config

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// systemctl runs `systemctl --user <args...>` with a short timeout. Returns
// the trimmed combined stdout+stderr and the exit code (0 == success).
func systemctl(args ...string) (int, string) {
	if !HasSystemctl() {
		return 1, "systemctl not available on this platform"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "systemctl", append([]string{"--user"}, args...)...)
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

// SystemctlIsActive returns true when the unit is currently running.
func SystemctlIsActive(unit string) bool {
	rc, _ := systemctl("is-active", unit)
	return rc == 0
}

// SystemctlIsEnabled returns true when the unit is enabled (will start at boot).
func SystemctlIsEnabled(unit string) bool {
	rc, _ := systemctl("is-enabled", unit)
	return rc == 0
}

// SystemctlStart starts the unit. Returns trimmed stderr on failure.
func SystemctlStart(unit string) (bool, string) {
	rc, out := systemctl("start", unit)
	return rc == 0, out
}

// SystemctlStop stops the unit.
func SystemctlStop(unit string) (bool, string) {
	rc, out := systemctl("stop", unit)
	return rc == 0, out
}

// SystemctlEnableNow enables and immediately starts the unit.
func SystemctlEnableNow(unit string) (bool, string) {
	rc, out := systemctl("enable", "--now", unit)
	return rc == 0, out
}

// SystemctlDisableNow disables and stops the unit.
func SystemctlDisableNow(unit string) (bool, string) {
	rc, out := systemctl("disable", "--now", unit)
	return rc == 0, out
}

// SystemctlDaemonReload picks up newly-written unit files.
func SystemctlDaemonReload() (bool, string) {
	rc, out := systemctl("daemon-reload")
	return rc == 0, out
}

// SystemctlShow returns the requested unit properties as a map. Properties
// systemd reports as "n/a" or "0" are normalized to "" for easier display.
func SystemctlShow(unit string, props ...string) map[string]string {
	args := []string{"show", unit}
	for _, p := range props {
		args = append(args, "--property="+p)
	}
	_, out := systemctl(args...)
	res := make(map[string]string, len(props))
	for _, line := range strings.Split(out, "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		switch v {
		case "n/a", "0":
			v = ""
		}
		res[k] = v
	}
	return res
}

// TimerInstalled reports whether both the timer and service unit files exist
// in the user's systemd directory.
func TimerInstalled() bool {
	dir := SystemdUserDir()
	_, errT := os.Stat(filepath.Join(dir, TimerUnit))
	_, errS := os.Stat(filepath.Join(dir, ServiceUnit))
	return errT == nil && errS == nil
}
