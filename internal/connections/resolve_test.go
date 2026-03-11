package connections

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/sqltui/sql/internal/config"
	_ "github.com/sqltui/sql/internal/db/postgres"
	_ "github.com/sqltui/sql/internal/db/sqlite"
	keyring "github.com/zalando/go-keyring"
)

func TestResolveNamedConnectionPrefersConfigProfile(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("LOCALAPPDATA", dataDir)
	t.Setenv("XDG_DATA_HOME", dataDir)

	store, err := LoadManagedStore()
	if err != nil {
		t.Fatalf("LoadManagedStore() error = %v", err)
	}
	if err := store.Add(Entry{Name: "dev", Driver: "sqlite", Params: map[string]string{"raw": "store.db"}}); err != nil {
		t.Fatalf("store.Add() error = %v", err)
	}

	cfg := &config.Config{Connections: []config.ConnectionProfile{
		{Name: "alpha", Driver: "sqlite", FilePath: "alpha.db"},
		{Name: "dev", Driver: "sqlite", FilePath: "cfg.db"},
	}}

	names, err := Names(cfg)
	if err != nil {
		t.Fatalf("Names() error = %v", err)
	}
	if want := []string{"alpha", "dev"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("Names() = %v, want %v", names, want)
	}

	target, err := Resolve(cfg, "dev")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if target.DisplayName != "dev" || target.WorkspaceKey != "dev" || target.Driver != "sqlite" || target.DSN != "cfg.db" || !target.Named {
		t.Fatalf("Resolve() = %+v, want named sqlite target backed by cfg.db", target)
	}
}

func TestResolveRawConnectionUsesAdhocWorkspace(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("LOCALAPPDATA", dataDir)
	t.Setenv("XDG_DATA_HOME", dataDir)

	raw := filepath.Join("tmp", "demo.db")
	target, err := Resolve(&config.Config{}, raw)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if target.WorkspaceKey != "_adhoc" || target.Driver != "sqlite" || target.DSN != raw || target.Named {
		t.Fatalf("Resolve() = %+v, want adhoc sqlite target", target)
	}
	if target.DisplayName != "demo.db" {
		t.Fatalf("Resolve() display name = %q, want %q", target.DisplayName, "demo.db")
	}
}

func TestParseConnStringFlagsPasswordsForSafeStorage(t *testing.T) {
	driver, params, err := ParseConnString("postgres://app:secret@localhost/mydb")
	if err != nil {
		t.Fatalf("ParseConnString() error = %v", err)
	}
	if driver != "postgres" {
		t.Fatalf("ParseConnString() driver = %q, want postgres", driver)
	}
	if params["password"] != "secret" {
		t.Fatalf("ParseConnString() password = %q, want secret", params["password"])
	}
	clean := SanitizedParamsForStorage(params)
	if _, ok := clean["password"]; ok {
		t.Fatalf("SanitizedParamsForStorage() should remove password")
	}
	if clean["raw"] != "postgres://app@localhost/mydb" {
		t.Fatalf("SanitizedParamsForStorage() raw = %q, want password-free raw connection string", clean["raw"])
	}
}

func TestResolveNamedConnectionInjectsKeychainPasswordForStoredRawDSN(t *testing.T) {
	keyring.MockInit()
	dataDir := t.TempDir()
	t.Setenv("LOCALAPPDATA", dataDir)
	t.Setenv("XDG_DATA_HOME", dataDir)

	store, err := LoadManagedStore()
	if err != nil {
		t.Fatalf("LoadManagedStore() error = %v", err)
	}
	if err := store.Add(Entry{
		Name:   "prod",
		Driver: "postgres",
		Params: map[string]string{"raw": "postgres://app@localhost/mydb"},
	}); err != nil {
		t.Fatalf("store.Add() error = %v", err)
	}
	if err := SavePassword("prod", "secret"); err != nil {
		t.Fatalf("SavePassword() error = %v", err)
	}

	target, err := Resolve(&config.Config{}, "prod")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if target.DSN != "postgres://app:secret@localhost/mydb" {
		t.Fatalf("Resolve() DSN = %q, want injected password DSN", target.DSN)
	}
}

func TestResolveNamedConnectionInjectsKeychainPasswordForConfigProfile(t *testing.T) {
	keyring.MockInit()
	dataDir := t.TempDir()
	t.Setenv("LOCALAPPDATA", dataDir)
	t.Setenv("XDG_DATA_HOME", dataDir)

	if err := SavePassword("prod", "secret"); err != nil {
		t.Fatalf("SavePassword() error = %v", err)
	}
	cfg := &config.Config{Connections: []config.ConnectionProfile{{
		Name:     "prod",
		Driver:   "postgres",
		Host:     "localhost",
		Database: "mydb",
		Username: "app",
	}}}

	target, err := Resolve(cfg, "prod")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if target.DSN != "postgres://app:secret@localhost:5432/mydb?sslmode=disable" {
		t.Fatalf("Resolve() DSN = %q, want structured DSN with injected password", target.DSN)
	}
}

func TestListIncludesSummaryForSwitcher(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("LOCALAPPDATA", dataDir)
	t.Setenv("XDG_DATA_HOME", dataDir)

	store, err := LoadManagedStore()
	if err != nil {
		t.Fatalf("LoadManagedStore() error = %v", err)
	}
	if err := store.Add(Entry{Name: "local", Driver: "sqlite", Params: map[string]string{"raw": "file:local.db"}}); err != nil {
		t.Fatalf("store.Add() error = %v", err)
	}
	cfg := &config.Config{Connections: []config.ConnectionProfile{{Name: "prod", Driver: "postgres", Host: "db.example.com", Database: "app"}}}

	infos, err := List(cfg)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(infos) != 2 {
		t.Fatalf("len(List()) = %d, want 2", len(infos))
	}
	if infos[0].Name != "local" || infos[0].Summary != "local.db" {
		t.Fatalf("infos[0] = %+v, want local sqlite summary", infos[0])
	}
	if infos[1].Name != "prod" || infos[1].Summary != "db.example.com/app" {
		t.Fatalf("infos[1] = %+v, want prod postgres summary", infos[1])
	}
}
