package workspace

import (
	"database/sql"
	"os"
	"path/filepath"
	"time"
)

// SnippetEntry represents a saved named SQL snippet.
type SnippetEntry struct {
	ID        int64
	Name      string
	SQL       string
	CreatedAt time.Time
}

// AddSnippet saves a new snippet with the given name and SQL body.
func (w *Workspace) AddSnippet(name, sqlText string) error {
	if w == nil {
		return nil
	}
	db, err := openSnippetsDB(w.snippetsDBPath())
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec(
		`INSERT INTO snippets (name, sql_text, created_at) VALUES (?, ?, ?)`,
		name, sqlText, time.Now().UTC().Format(time.RFC3339Nano),
	)
	return err
}

// ListSnippets returns all snippets ordered by name.
func (w *Workspace) ListSnippets() ([]SnippetEntry, error) {
	if w == nil {
		return nil, nil
	}
	db, err := openSnippetsDB(w.snippetsDBPath())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query(`SELECT id, name, sql_text, created_at FROM snippets ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []SnippetEntry
	for rows.Next() {
		var e SnippetEntry
		var createdAt string
		if err := rows.Scan(&e.ID, &e.Name, &e.SQL, &createdAt); err != nil {
			return nil, err
		}
		e.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// DeleteSnippet removes the snippet with the given ID.
func (w *Workspace) DeleteSnippet(id int64) error {
	if w == nil {
		return nil
	}
	db, err := openSnippetsDB(w.snippetsDBPath())
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec(`DELETE FROM snippets WHERE id = ?`, id)
	return err
}

func (w *Workspace) snippetsDBPath() string {
	return filepath.Join(w.root, "snippets.db")
}

func openSnippetsDB(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if err := ensureSnippetsSchema(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func ensureSnippetsSchema(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS snippets (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		name       TEXT NOT NULL,
		sql_text   TEXT NOT NULL,
		created_at TEXT NOT NULL
	)`)
	return err
}
