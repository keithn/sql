package connections

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Entry is a saved connection in the managed store.
// Passwords are never stored here; they live in the OS keychain.
type Entry struct {
	Name   string            `json:"name"`
	Driver string            `json:"driver"` // "mssql", "postgres", "sqlite"
	Params map[string]string `json:"params"` // driver-specific key/value pairs
}

// Store manages the JSON-backed connection registry.
type Store struct {
	path    string
	entries []Entry
}

// Load reads the connection store from disk. Missing file is not an error.
func Load(path string) (*Store, error) {
	s := &Store{path: path}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &s.entries); err != nil {
		return nil, err
	}
	return s, nil
}

// All returns all stored entries.
func (s *Store) All() []Entry { return s.entries }

// Get returns the entry with the given name, or nil.
func (s *Store) Get(name string) *Entry {
	for i := range s.entries {
		if s.entries[i].Name == name {
			return &s.entries[i]
		}
	}
	return nil
}

// Add adds or replaces an entry and persists to disk.
func (s *Store) Add(e Entry) error {
	for i, existing := range s.entries {
		if existing.Name == e.Name {
			s.entries[i] = e
			return s.save()
		}
	}
	s.entries = append(s.entries, e)
	return s.save()
}

// Remove deletes the entry with the given name and persists.
func (s *Store) Remove(name string) error {
	filtered := s.entries[:0]
	for _, e := range s.entries {
		if e.Name != name {
			filtered = append(filtered, e)
		}
	}
	s.entries = filtered
	return s.save()
}

func (s *Store) save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0600)
}
