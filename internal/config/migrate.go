package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CurrentSchemaVersion is the latest target version for migrations.
const CurrentSchemaVersion = 1

func schemaVersionFile() string {
	return filepath.Join(AppDir(), "schema_version.json")
}

func readSchemaVersion() int {
	b, err := os.ReadFile(schemaVersionFile())
	if err != nil {
		return 0
	}
	var data struct {
		Version float64 `json:"version"` // Decode as float64 to safely handle arbitrary JSON numbers
	}
	if err := json.Unmarshal(b, &data); err != nil {
		return 0
	}
	return int(data.Version)
}

func writeSchemaVersion(version int) error {
	b, _ := json.Marshal(map[string]int{"version": version})
	return os.WriteFile(schemaVersionFile(), b, 0o644)
}

// MigrateIfNeeded runs any outstanding schema migrations at startup.
// Call this once before any other config function. Safe to call multiple
// times — already-applied migrations are skipped.
//
// Edge case: if schema_version.json is absent but accounts/ exists,
// treat as v1 to avoid overwriting existing account data.
func MigrateIfNeeded() {
	accountsDir := filepath.Join(AppDir(), "accounts")
	_, err := os.Stat(schemaVersionFile())
	hasSchema := err == nil
	_, err = os.Stat(accountsDir)
	hasAccounts := err == nil

	if !hasSchema && hasAccounts {
		_ = writeSchemaVersion(CurrentSchemaVersion)
		return
	}

	current := readSchemaVersion()
	if current >= CurrentSchemaVersion {
		return
	}

	migrations := []struct {
		from int
		fn   func() error
	}{
		{0, migrateV0ToV1},
	}

	for _, m := range migrations {
		if m.from >= current {
			if err := m.fn(); err != nil {
				fmt.Fprintf(os.Stderr, "[migration v%d→v%d] failed: %v\n", m.from, m.from+1, err)
				return
			}
			current = m.from + 1
		}
	}
}

func migrateV0ToV1() error {
	// Read legacy .env for PUBLIC_ACCOUNT_NUMBER
	envPath := filepath.Join(AppDir(), ".env")
	b, err := os.ReadFile(envPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	var accountID string
	lines := strings.Split(string(b), "\n")

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		parts := strings.SplitN(trimmed, "=", 2)
		key := strings.TrimSpace(parts[0])
		val := ""
		if len(parts) > 1 {
			val = strings.TrimSpace(parts[1])
		}

		if key == "PUBLIC_ACCOUNT_NUMBER" {
			accountID = strings.ToUpper(val)
			break
		}
	}

	if accountID == "" {
		return writeSchemaVersion(1)
	}

	acctDir := accountDir(accountID)
	_ = os.MkdirAll(acctDir, 0o755)

	// Move old shared config/cache to per-account directories.
	oldConfig := filepath.Join(AppDir(), "rebalance_config.json")
	newConfig := filepath.Join(acctDir, "rebalance_config.json")
	if _, err := os.Stat(oldConfig); err == nil {
		if _, err := os.Stat(newConfig); os.IsNotExist(err) {
			_ = os.Rename(oldConfig, newConfig)
		}
	}

	oldCache := filepath.Join(AppDir(), "cache")
	newCache := filepath.Join(acctDir, "cache")
	if _, err := os.Stat(oldCache); err == nil {
		if _, err := os.Stat(newCache); os.IsNotExist(err) {
			_ = os.Rename(oldCache, newCache)
		}
	}

	// Remove legacy .env file — credentials are now handled by the public CLI.
	_ = os.Remove(envPath)

	if _, err := os.Stat(AccountsFile()); os.IsNotExist(err) {
		b, _ := json.Marshal([]string{accountID})
		_ = os.WriteFile(AccountsFile(), b, 0o644)
	}

	return writeSchemaVersion(1)
}
