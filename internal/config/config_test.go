package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func tmpAppDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	appDir := filepath.Join(dir, ".config", "public-terminal")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	return dir
}

func TestGetAccounts_Empty(t *testing.T) {
	tmpAppDir(t)
	accounts := GetAccounts()
	if len(accounts) != 0 {
		t.Errorf("expected empty, got %v", accounts)
	}
}

func TestGetAccounts_ReturnsList(t *testing.T) {
	dir := tmpAppDir(t)
	appDir := filepath.Join(dir, ".config", "public-terminal")
	_ = os.WriteFile(filepath.Join(appDir, "accounts.json"), []byte(`["ACCT001","ACCT002"]`), 0o644)

	accounts := GetAccounts()
	if len(accounts) != 2 {
		t.Errorf("expected 2, got %d", len(accounts))
	}
	if accounts[0] != "ACCT001" || accounts[1] != "ACCT002" {
		t.Errorf("unexpected accounts: %v", accounts)
	}
}

func TestAddAccount_CreatesEntry(t *testing.T) {
	dir := tmpAppDir(t)
	appDir := filepath.Join(dir, ".config", "public-terminal")
	_ = os.WriteFile(filepath.Join(appDir, "accounts.json"), []byte("[]"), 0o644)

	if err := AddAccount("ACCT001"); err != nil {
		t.Fatalf("AddAccount: %v", err)
	}
	accounts := GetAccounts()
	if len(accounts) != 1 || accounts[0] != "ACCT001" {
		t.Errorf("unexpected accounts: %v", accounts)
	}
	acctDir := filepath.Join(appDir, "accounts", "ACCT001")
	if _, err := os.Stat(acctDir); os.IsNotExist(err) {
		t.Error("account dir not created")
	}
}

func TestAddAccount_Deduplicates(t *testing.T) {
	dir := tmpAppDir(t)
	appDir := filepath.Join(dir, ".config", "public-terminal")
	_ = os.WriteFile(filepath.Join(appDir, "accounts.json"), []byte("[]"), 0o644)

	_ = AddAccount("ACCT001")
	_ = AddAccount("ACCT001")
	accounts := GetAccounts()
	if len(accounts) != 1 {
		t.Errorf("expected 1, got %d", len(accounts))
	}
}

func TestAddAccount_NormalizesCase(t *testing.T) {
	dir := tmpAppDir(t)
	appDir := filepath.Join(dir, ".config", "public-terminal")
	_ = os.WriteFile(filepath.Join(appDir, "accounts.json"), []byte("[]"), 0o644)

	_ = AddAccount("acct001")
	accounts := GetAccounts()
	if len(accounts) != 1 || accounts[0] != "ACCT001" {
		t.Errorf("expected ACCT001, got %v", accounts)
	}
}

func TestAddAccount_EmptyString(t *testing.T) {
	dir := tmpAppDir(t)
	appDir := filepath.Join(dir, ".config", "public-terminal")
	_ = os.WriteFile(filepath.Join(appDir, "accounts.json"), []byte("[]"), 0o644)

	if err := AddAccount(""); err == nil {
		t.Error("expected error for empty account")
	}
	if err := AddAccount("  "); err == nil {
		t.Error("expected error for whitespace account")
	}
}

func TestRemoveAccount_DeletesDir(t *testing.T) {
	dir := tmpAppDir(t)
	appDir := filepath.Join(dir, ".config", "public-terminal")
	_ = os.WriteFile(filepath.Join(appDir, "accounts.json"), []byte(`["ACCT001","ACCT002"]`), 0o644)
	_ = os.MkdirAll(filepath.Join(appDir, "accounts", "ACCT001"), 0o755)
	_ = os.MkdirAll(filepath.Join(appDir, "accounts", "ACCT002"), 0o755)

	if err := RemoveAccount("ACCT001"); err != nil {
		t.Fatalf("RemoveAccount: %v", err)
	}
	accounts := GetAccounts()
	if len(accounts) != 1 || accounts[0] != "ACCT002" {
		t.Errorf("unexpected accounts: %v", accounts)
	}
	if _, err := os.Stat(filepath.Join(appDir, "accounts", "ACCT001")); !os.IsNotExist(err) {
		t.Error("account dir not deleted")
	}
}

func TestRemoveAccount_LastAccountFails(t *testing.T) {
	dir := tmpAppDir(t)
	appDir := filepath.Join(dir, ".config", "public-terminal")
	_ = os.WriteFile(filepath.Join(appDir, "accounts.json"), []byte(`["ACCT001"]`), 0o644)
	_ = os.MkdirAll(filepath.Join(appDir, "accounts", "ACCT001"), 0o755)

	if err := RemoveAccount("ACCT001"); err == nil {
		t.Error("expected error for removing last account")
	}
}

func TestSaveRebalanceConfig_FilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-style permission bits are not enforced on windows")
	}

	tmpAppDir(t)
	cfg := RebalanceConfig{
		Index:            "SP500",
		TopN:             100,
		MarginUsagePct:   0.5,
		ExcludedTickers:  []string{"aapl"},
		Allocations:      map[string]float64{"stocks": 1.0},
		RebalanceEnabled: true,
	}

	t.Run("creates owner-only file", func(t *testing.T) {
		accountID := "ACCT001"
		if err := SaveRebalanceConfig(accountID, cfg); err != nil {
			t.Fatalf("SaveRebalanceConfig: %v", err)
		}

		info, err := os.Stat(RebalanceConfigPath(accountID))
		if err != nil {
			t.Fatalf("stat rebalance config: %v", err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("permissions = %o, want 600", got)
		}
	})

	t.Run("tightens existing broad permissions", func(t *testing.T) {
		accountID := "ACCT002"
		path := RebalanceConfigPath(accountID)
		if err := os.WriteFile(path, []byte(`{"index":"SP500"}`), 0o644); err != nil {
			t.Fatalf("seed rebalance config: %v", err)
		}
		if err := os.Chmod(path, 0o644); err != nil {
			t.Fatalf("chmod seed rebalance config: %v", err)
		}

		if err := SaveRebalanceConfig(accountID, cfg); err != nil {
			t.Fatalf("SaveRebalanceConfig: %v", err)
		}

		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat rebalance config: %v", err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("permissions = %o, want 600", got)
		}
	})
}

func TestMigration_V0ToV1_MovesRebalanceConfig(t *testing.T) {
	dir := tmpAppDir(t)
	appDir := filepath.Join(dir, ".config", "public-terminal")
	envFile := filepath.Join(appDir, ".env")
	_ = os.WriteFile(envFile, []byte("PUBLIC_ACCOUNT_NUMBER=ACCT001\n"), 0o644)
	oldConfig := filepath.Join(appDir, "rebalance_config.json")
	_ = os.WriteFile(oldConfig, []byte(`{"index":"SP500","top_n":500}`), 0o644)
	_ = os.WriteFile(filepath.Join(appDir, "schema_version.json"), []byte(`{"version":0}`), 0o644)

	MigrateIfNeeded()

	newConfig := filepath.Join(appDir, "accounts", "ACCT001", "rebalance_config.json")
	if _, err := os.Stat(newConfig); os.IsNotExist(err) {
		t.Error("rebalance config not migrated")
	}
	if _, err := os.Stat(oldConfig); !os.IsNotExist(err) {
		t.Error("old config still exists")
	}
}

func TestMigration_SkipsWhenCurrent(t *testing.T) {
	dir := tmpAppDir(t)
	appDir := filepath.Join(dir, ".config", "public-terminal")
	_ = os.WriteFile(filepath.Join(appDir, "schema_version.json"),
		[]byte(`{"version":1}`), 0o644)

	MigrateIfNeeded()

	data, _ := os.ReadFile(filepath.Join(appDir, "schema_version.json"))
	var v struct{ Version float64 }
	json.Unmarshal(data, &v)
	if int(v.Version) != CurrentSchemaVersion {
		t.Errorf("version = %d, want %d", int(v.Version), CurrentSchemaVersion)
	}
}

func TestMigration_NoSchemaWithAccounts(t *testing.T) {
	dir := tmpAppDir(t)
	appDir := filepath.Join(dir, ".config", "public-terminal")
	_ = os.MkdirAll(filepath.Join(appDir, "accounts", "ACCT001"), 0o755)

	MigrateIfNeeded()

	data, _ := os.ReadFile(filepath.Join(appDir, "schema_version.json"))
	var v struct{ Version float64 }
	json.Unmarshal(data, &v)
	if int(v.Version) != CurrentSchemaVersion {
		t.Errorf("version = %d, want %d", int(v.Version), CurrentSchemaVersion)
	}
}
