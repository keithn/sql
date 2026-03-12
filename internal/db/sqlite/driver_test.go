package sqlite

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestExplainQueryReturnsSQLitePlanText(t *testing.T) {
	conn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	defer conn.Close()

	if _, err := conn.Exec(`CREATE TABLE items (id INTEGER PRIMARY KEY, name TEXT); CREATE INDEX idx_items_name ON items(name);`); err != nil {
		t.Fatalf("setup exec error = %v", err)
	}

	plan, err := (&Driver{}).ExplainQuery(context.Background(), conn, `SELECT name FROM items WHERE name = 'a'`)
	if err != nil {
		t.Fatalf("ExplainQuery() error = %v", err)
	}
	if strings.TrimSpace(plan) == "" {
		t.Fatalf("ExplainQuery() returned empty plan")
	}
	if !strings.Contains(strings.ToLower(plan), "items") {
		t.Fatalf("ExplainQuery() = %q, want plan mentioning items table", plan)
	}
}
