package api

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveBin_FoundOnPath(t *testing.T) {
	dir := t.TempDir()
	fakeBin := filepath.Join(dir, "public")
	if err := os.WriteFile(fakeBin, []byte("#!/bin/sh\necho test"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	bin, err := resolveBin()
	if err != nil {
		t.Fatalf("resolveBin failed: %v", err)
	}
	if bin != fakeBin {
		t.Errorf("bin = %q, want %q", bin, fakeBin)
	}
}

func TestResolveBin_NotFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	_, err := resolveBin()
	if err == nil {
		t.Error("expected error when public not found")
	}
}

func TestNewClient_Success(t *testing.T) {
	dir := t.TempDir()
	fakeBin := filepath.Join(dir, "public")
	os.WriteFile(fakeBin, []byte("#!/bin/sh\necho '{}'"), 0o755)
	t.Setenv("PATH", dir)

	client, err := NewClient("ACCT001")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if client.accountID != "ACCT001" {
		t.Errorf("accountID = %q, want ACCT001", client.accountID)
	}
	if client.bin != fakeBin {
		t.Errorf("bin = %q, want %q", client.bin, fakeBin)
	}
}

func TestNewClient_NotFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("HOME", t.TempDir())

	_, err := NewClient("ACCT001")
	if err == nil {
		t.Error("expected error when public not found")
	}
}

func TestResolveBin_HomeDirFallback(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	home := t.TempDir()
	t.Setenv("HOME", home)

	localBin := filepath.Join(home, ".local", "bin")
	os.MkdirAll(localBin, 0o755)
	os.WriteFile(filepath.Join(localBin, "public"), []byte("#!/bin/sh\necho test"), 0o755)

	bin, err := resolveBin()
	if err != nil {
		t.Fatalf("resolveBin with fallback: %v", err)
	}
	if bin == "" {
		t.Error("expected non-empty bin path")
	}
}
