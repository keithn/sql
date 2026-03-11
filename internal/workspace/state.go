package workspace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type state struct {
	LastConnection string `json:"last_connection"`
	VimMode        *bool  `json:"vim_mode,omitempty"`
}

func (w *Workspace) LoadVimMode() (bool, bool, error) {
	s, err := w.loadState()
	if err != nil {
		return false, false, err
	}
	if s.VimMode == nil {
		return false, false, nil
	}
	return *s.VimMode, true, nil
}

func (w *Workspace) SaveVimMode(enabled bool) error {
	s, err := w.loadState()
	if err != nil {
		return err
	}
	s.VimMode = boolPtr(enabled)
	return w.saveState(s)
}

func (w *Workspace) LoadLastConnection() (string, error) {
	s, err := w.loadState()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(s.LastConnection), nil
}

func (w *Workspace) SaveLastConnection(name string) error {
	s, err := w.loadState()
	if err != nil {
		return err
	}
	s.LastConnection = strings.TrimSpace(name)
	return w.saveState(s)
}

func (w *Workspace) loadState() (state, error) {
	data, err := os.ReadFile(filepath.Join(w.root, "state.json"))
	if os.IsNotExist(err) {
		return state{}, nil
	}
	if err != nil {
		return state{}, err
	}
	var s state
	if err := json.Unmarshal(data, &s); err != nil {
		return state{}, nil
	}
	return s, nil
}

func (w *Workspace) saveState(s state) error {
	path := filepath.Join(w.root, "state.json")
	if strings.TrimSpace(s.LastConnection) == "" && s.VimMode == nil {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(w.root, 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

func boolPtr(v bool) *bool { return &v }
