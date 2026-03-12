package workspace

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const defaultHistoryLimit = 200

type HistoryEntry struct {
	ID         int64
	ExecutedAt time.Time
	Connection string
	Mode       string
	SQL        string
}

func (w *Workspace) AppendHistory(entry HistoryEntry) error {
	if w == nil || strings.TrimSpace(entry.SQL) == "" {
		return nil
	}
	if entry.ExecutedAt.IsZero() {
		entry.ExecutedAt = time.Now().UTC()
	}
	if entry.Connection == "" {
		entry.Connection = "_adhoc"
	}
	if entry.Mode == "" {
		entry.Mode = "BLOCK"
	}
	db, err := openHistoryDB(w.historyDBPath())
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec(`INSERT INTO history (executed_at, connection_name, mode, sql_text) VALUES (?, ?, ?, ?)`, entry.ExecutedAt.UTC().Format(time.RFC3339Nano), entry.Connection, entry.Mode, entry.SQL)
	return err
}

func (w *Workspace) LoadHistory(limit int) ([]HistoryEntry, error) {
	if w == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = defaultHistoryLimit
	}
	db, err := openHistoryDB(w.historyDBPath())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query(`SELECT id, executed_at, connection_name, mode, sql_text FROM history ORDER BY executed_at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := make([]HistoryEntry, 0, limit)
	for rows.Next() {
		var entry HistoryEntry
		var executedAt string
		if err := rows.Scan(&entry.ID, &executedAt, &entry.Connection, &entry.Mode, &entry.SQL); err != nil {
			return nil, err
		}
		entry.ExecutedAt, _ = time.Parse(time.RFC3339Nano, executedAt)
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func (w *Workspace) historyDBPath() string {
	return filepath.Join(w.root, "history.db")
}

func openHistoryDB(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if err := ensureHistorySchema(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func ensureHistorySchema(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS history (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		executed_at TEXT NOT NULL,
		connection_name TEXT NOT NULL,
		mode TEXT NOT NULL,
		sql_text TEXT NOT NULL
	);
		CREATE INDEX IF NOT EXISTS idx_history_executed_at ON history (executed_at DESC, id DESC);`)
	return err
}
