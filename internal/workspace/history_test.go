package workspace

import (
	"path/filepath"
	"testing"
	"time"
)

func TestAppendHistoryAndLoadHistoryNewestFirst(t *testing.T) {
	ws := New(filepath.Join(t.TempDir(), "workspace"))
	older := time.Date(2026, 3, 11, 10, 0, 0, 0, time.UTC)
	newer := older.Add(5 * time.Minute)
	if err := ws.AppendHistory(HistoryEntry{ExecutedAt: older, Connection: "dev", Mode: "BLOCK", SQL: "select 1"}); err != nil {
		t.Fatalf("AppendHistory(older) error = %v", err)
	}
	if err := ws.AppendHistory(HistoryEntry{ExecutedAt: newer, Connection: "prod", Mode: "BUFFER", SQL: "select 2"}); err != nil {
		t.Fatalf("AppendHistory(newer) error = %v", err)
	}
	entries, err := ws.LoadHistory(10)
	if err != nil {
		t.Fatalf("LoadHistory() error = %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}
	if entries[0].SQL != "select 2" || entries[0].Connection != "prod" || entries[0].Mode != "BUFFER" {
		t.Fatalf("entries[0] = %#v, want newest prod/buffer entry", entries[0])
	}
	if entries[1].SQL != "select 1" || entries[1].Connection != "dev" || entries[1].Mode != "BLOCK" {
		t.Fatalf("entries[1] = %#v, want older dev/block entry", entries[1])
	}
	if !entries[0].ExecutedAt.Equal(newer) {
		t.Fatalf("entries[0].ExecutedAt = %v, want %v", entries[0].ExecutedAt, newer)
	}
	if !entries[1].ExecutedAt.Equal(older) {
		t.Fatalf("entries[1].ExecutedAt = %v, want %v", entries[1].ExecutedAt, older)
	}
}
