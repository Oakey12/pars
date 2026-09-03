package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "printers.json")
	if err := os.WriteFile(path, []byte(`{"printers":[{"name":"Office","address":"192.0.2.10"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ParsedTimeout != 3*time.Second || *cfg.Retries != 1 || cfg.Concurrency != 8 {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if got := cfg.Printers[0]; got.Port != 161 || got.Community != "public" || got.Version != "2c" || !got.IsEnabled() {
		t.Fatalf("unexpected printer defaults: %+v", got)
	}
}

func TestLoadRejectsDuplicateAddress(t *testing.T) {
	path := filepath.Join(t.TempDir(), "printers.json")
	data := `{"printers":[{"name":"A","address":"192.0.2.10"},{"name":"B","address":"192.0.2.10"}]}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected duplicate-address error")
	}
}

func TestLoadAcceptsAutomaticVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "printers.json")
	data := `{"printers":[{"name":"A","address":"192.0.2.10","version":"auto"}]}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err != nil {
		t.Fatalf("auto version rejected: %v", err)
	}
}
