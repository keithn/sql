package main

import (
	"testing"

	"github.com/sqltui/sql/internal/connections"
	keyring "github.com/zalando/go-keyring"
)

func TestAddConnectionStoresPasswordInKeychainOnly(t *testing.T) {
	keyring.MockInit()
	dataDir := t.TempDir()
	t.Setenv("LOCALAPPDATA", dataDir)
	t.Setenv("XDG_DATA_HOME", dataDir)

	err := addConnection([]string{"--add", "postgres://app:secret@localhost/mydb", "--name", "prod"})
	if err != nil {
		t.Fatalf("addConnection() error = %v", err)
	}

	store, err := connections.LoadManagedStore()
	if err != nil {
		t.Fatalf("LoadManagedStore() error = %v", err)
	}
	entry := store.Get("prod")
	if entry == nil {
		t.Fatalf("expected saved entry for prod")
	}
	if entry.Params["raw"] != "postgres://app@localhost/mydb" {
		t.Fatalf("stored raw DSN = %q, want password-free DSN", entry.Params["raw"])
	}
	if _, ok := entry.Params["password"]; ok {
		t.Fatalf("stored entry should not include password")
	}

	password, ok, err := connections.LoadPassword("prod")
	if err != nil {
		t.Fatalf("LoadPassword() error = %v", err)
	}
	if !ok || password != "secret" {
		t.Fatalf("LoadPassword() = (%q, %v), want (%q, true)", password, ok, "secret")
	}
}

func TestAddConnectionWithoutPasswordClearsExistingKeychainSecret(t *testing.T) {
	keyring.MockInit()
	dataDir := t.TempDir()
	t.Setenv("LOCALAPPDATA", dataDir)
	t.Setenv("XDG_DATA_HOME", dataDir)

	if err := connections.SavePassword("prod", "secret"); err != nil {
		t.Fatalf("SavePassword() error = %v", err)
	}
	err := addConnection([]string{"--add", "postgres://app@localhost/mydb", "--name", "prod"})
	if err != nil {
		t.Fatalf("addConnection() error = %v", err)
	}

	_, ok, err := connections.LoadPassword("prod")
	if err != nil {
		t.Fatalf("LoadPassword() error = %v", err)
	}
	if ok {
		t.Fatalf("expected saved password to be cleared for password-free overwrite")
	}
}
