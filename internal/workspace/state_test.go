package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveAndLoadLastConnection(t *testing.T) {
	w := New(t.TempDir())
	if err := w.SaveLastConnection("prod"); err != nil {
		t.Fatalf("SaveLastConnection() error = %v", err)
	}
	got, err := w.LoadLastConnection()
	if err != nil {
		t.Fatalf("LoadLastConnection() error = %v", err)
	}
	if got != "prod" {
		t.Fatalf("LoadLastConnection() = %q, want %q", got, "prod")
	}
}

func TestSaveLastConnectionEmptyClearsState(t *testing.T) {
	root := t.TempDir()
	w := New(root)
	if err := w.SaveLastConnection("prod"); err != nil {
		t.Fatalf("SaveLastConnection() error = %v", err)
	}
	if err := w.SaveLastConnection(""); err != nil {
		t.Fatalf("SaveLastConnection(clear) error = %v", err)
	}
	got, err := w.LoadLastConnection()
	if err != nil {
		t.Fatalf("LoadLastConnection() error = %v", err)
	}
	if got != "" {
		t.Fatalf("LoadLastConnection() = %q, want empty after clear", got)
	}
	if _, err := os.Stat(filepath.Join(root, "state.json")); !os.IsNotExist(err) {
		t.Fatalf("state.json should be removed after clearing, stat err = %v", err)
	}
	if _, err := w.LoadLastConnection(); err != nil {
		t.Fatalf("LoadLastConnection() unexpected error after clear = %v", err)
	}
}

func TestSaveLastConnectionEmptyPreservesOtherState(t *testing.T) {
	root := t.TempDir()
	w := New(root)
	if err := w.SaveLastConnection("prod"); err != nil {
		t.Fatalf("SaveLastConnection() error = %v", err)
	}
	if err := w.SaveVimMode(true); err != nil {
		t.Fatalf("SaveVimMode() error = %v", err)
	}
	if err := w.SaveLastConnection(""); err != nil {
		t.Fatalf("SaveLastConnection(clear) error = %v", err)
	}
	got, err := w.LoadLastConnection()
	if err != nil {
		t.Fatalf("LoadLastConnection() error = %v", err)
	}
	if got != "" {
		t.Fatalf("LoadLastConnection() = %q, want empty after clear", got)
	}
	vimMode, ok, err := w.LoadVimMode()
	if err != nil {
		t.Fatalf("LoadVimMode() error = %v", err)
	}
	if !ok || !vimMode {
		t.Fatalf("LoadVimMode() = (%v,%v), want (true,true)", vimMode, ok)
	}
	if _, err := os.Stat(filepath.Join(root, "state.json")); err != nil {
		t.Fatalf("state.json should remain when vim mode is still persisted, stat err = %v", err)
	}
}

func TestLoadLastConnectionIgnoresCorruptState(t *testing.T) {
	root := t.TempDir()
	w := New(root)
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "state.json"), []byte("{not json"), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	got, err := w.LoadLastConnection()
	if err != nil {
		t.Fatalf("LoadLastConnection() error = %v", err)
	}
	if got != "" {
		t.Fatalf("LoadLastConnection() = %q, want empty for corrupt state", got)
	}
}

func TestSaveAndLoadVimMode(t *testing.T) {
	w := New(t.TempDir())
	if err := w.SaveVimMode(true); err != nil {
		t.Fatalf("SaveVimMode() error = %v", err)
	}
	got, ok, err := w.LoadVimMode()
	if err != nil {
		t.Fatalf("LoadVimMode() error = %v", err)
	}
	if !ok || !got {
		t.Fatalf("LoadVimMode() = (%v,%v), want (true,true)", got, ok)
	}
	if err := w.SaveVimMode(false); err != nil {
		t.Fatalf("SaveVimMode(false) error = %v", err)
	}
	got, ok, err = w.LoadVimMode()
	if err != nil {
		t.Fatalf("LoadVimMode() error = %v", err)
	}
	if !ok || got {
		t.Fatalf("LoadVimMode() = (%v,%v), want (false,true)", got, ok)
	}
}
