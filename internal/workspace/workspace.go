package workspace

import (
	"os"
	"path/filepath"
	"strings"
)

// Workspace manages the per-connection query file directories.
type Workspace struct {
	root string // e.g. ~/.local/share/sql/workspace
}

// New creates a Workspace rooted at the given directory.
func New(root string) *Workspace {
	return &Workspace{root: root}
}

// ConnDir returns the directory for the given connection name,
// creating it if it doesn't exist.
func (w *Workspace) ConnDir(connName string) (string, error) {
	dir := filepath.Join(w.root, sanitizeName(connName))
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	return dir, nil
}

// NewQueryFile creates a new uniquely-named query file in the connection dir.
func (w *Workspace) NewQueryFile(connName string) (string, error) {
	dir, err := w.ConnDir(connName)
	if err != nil {
		return "", err
	}
	// Find the next available name.
	for i := 1; ; i++ {
		name := filepath.Join(dir, queryName(connName, i))
		if _, err := os.Stat(name); os.IsNotExist(err) {
			f, err := os.Create(name)
			if err != nil {
				return "", err
			}
			f.Close()
			return name, nil
		}
	}
}

func queryName(connName string, n int) string {
	if n == 1 && connName != "" && connName != "_adhoc" {
		return sanitizeName(connName) + ".sql"
	}
	if n == 1 {
		return "query1.sql"
	}
	return "query" + itoa(n) + ".sql"
}

// sanitizeName makes a connection name safe for use as a directory name.
func sanitizeName(name string) string {
	if name == "" {
		return "_adhoc"
	}
	r := strings.NewReplacer(
		"/", "_", "\\", "_", ":", "_", "*", "_",
		"?", "_", "\"", "_", "<", "_", ">", "_", "|", "_",
	)
	return r.Replace(name)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := make([]byte, 0, 10)
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	return string(buf)
}
