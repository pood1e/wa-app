package config

import (
	"os"
	"testing"
)

func TestRuntimeRequiresExplicitBackend(t *testing.T) {
	for _, name := range []string{"WA_APP_STORE", "WA_APP_PG_DSN", "WA_APP_SQLITE_FILE"} {
		t.Setenv(name, "")
	}
	t.Setenv("WA_SERVICE_TOKEN", "fixture-token")
	t.Setenv("WA_APP_EPHEMERAL_STORE", "memory")
	t.Setenv("WA_APP_VALKEY_URL", "")
	t.Setenv("WA_ACCOUNT_ADD_TARGET", "127.0.0.1:50092")
	if _, err := LoadRuntime(); err == nil {
		t.Fatal("missing backend was accepted")
	}
	t.Setenv("WA_APP_STORE", "sqlite")
	if _, err := LoadRuntime(); err == nil {
		t.Fatal("SQLite without an explicit file was accepted")
	}
	t.Setenv("WA_APP_SQLITE_FILE", t.TempDir()+"/accounts.sqlite3")
	cfg, err := LoadRuntime()
	if err != nil || cfg.Store != "sqlite" || cfg.PGDSN != "" {
		t.Fatal("explicit SQLite requires PostgreSQL")
	}
	t.Setenv("WA_APP_STORE", "postgres")
	if _, err := LoadRuntime(); err == nil {
		t.Fatal("missing PostgreSQL configuration silently selected SQLite")
	}
	t.Setenv("WA_APP_PG_DSN", "postgres://localhost/fixture")
	cfg, err = LoadRuntime()
	if err != nil || cfg.Store != "postgres" {
		t.Fatal("explicit PostgreSQL configuration was rejected")
	}
}

func TestRuntimeEphemeralBackend(t *testing.T) {
	t.Setenv("WA_SERVICE_TOKEN", "fixture-token")
	t.Setenv("WA_APP_STORE", "sqlite")
	t.Setenv("WA_APP_SQLITE_FILE", t.TempDir()+"/accounts.sqlite3")
	t.Setenv("WA_ACCOUNT_ADD_TARGET", "127.0.0.1:50092")
	t.Setenv("WA_APP_VALKEY_URL", "")
	t.Setenv("WA_APP_EPHEMERAL_STORE", "memory")
	if err := os.Unsetenv("WA_APP_EPHEMERAL_STORE"); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadRuntime()
	if err != nil || cfg.EphemeralStore != "memory" {
		t.Fatalf("default memory mode: backend=%q, err=%v", cfg.EphemeralStore, err)
	}
	t.Setenv("WA_APP_EPHEMERAL_STORE", "valkey")
	if _, err := LoadRuntime(); err == nil {
		t.Fatal("Valkey mode without a URL was accepted")
	}
	t.Setenv("WA_APP_VALKEY_URL", "valkey://127.0.0.1:6379")
	cfg, err = LoadRuntime()
	if err != nil || cfg.EphemeralStore != "valkey" {
		t.Fatalf("explicit Valkey mode: backend=%q, err=%v", cfg.EphemeralStore, err)
	}
	t.Setenv("WA_APP_EPHEMERAL_STORE", "invalid")
	if _, err := LoadRuntime(); err == nil {
		t.Fatal("unknown backend silently selected memory")
	}
}

func TestAccountAddNeedsNoPersistence(t *testing.T) {
	t.Setenv("WA_SERVICE_TOKEN", "fixture-token")
	t.Setenv("WA_ACCOUNT_RUNTIME_TARGET", "127.0.0.1:50091")
	for _, name := range []string{"WA_APP_STORE", "WA_APP_PG_DSN", "WA_APP_SQLITE_FILE", "WA_APP_EPHEMERAL_STORE", "WA_APP_VALKEY_URL"} {
		t.Setenv(name, "")
	}
	if _, err := LoadAccountAdd(); err != nil {
		t.Fatal("adding service requires persistence configuration")
	}
}
