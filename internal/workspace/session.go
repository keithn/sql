package workspace

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Session records the open tabs and cursor positions for a connection.
type Session struct {
	Tabs      []TabRecord `json:"tabs"`
	ActiveTab int         `json:"active_tab"`
}

// TabRecord holds the persisted state of one editor tab.
type TabRecord struct {
	File   string `json:"file"`
	Cursor struct {
		Line int `json:"line"`
		Col  int `json:"col"`
	} `json:"cursor"`
}

// LoadSession reads session.json from the connection's workspace dir.
// Returns an empty session if the file does not exist.
func LoadSession(connDir string) (*Session, error) {
	path := filepath.Join(connDir, "session.json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Session{}, nil
	}
	if err != nil {
		return nil, err
	}
	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return &Session{}, nil // corrupt session; start fresh
	}
	return &s, nil
}

// SaveSession writes session.json to the connection's workspace dir.
func SaveSession(connDir string, s *Session) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(connDir, "session.json"), data, 0600)
}
