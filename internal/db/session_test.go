package db

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

type explainStubDriver struct {
	calls []string
}

func (d *explainStubDriver) Open(context.Context, string) (*sql.DB, error) { return nil, nil }
func (d *explainStubDriver) BuildDSN(map[string]string) (string, error)    { return "", nil }
func (d *explainStubDriver) Introspect(context.Context, *sql.DB) (*Schema, error) {
	return &Schema{}, nil
}
func (d *explainStubDriver) ExpandStar(context.Context, *sql.DB, string, string) ([]string, error) {
	return nil, nil
}
func (d *explainStubDriver) ExplainQuery(_ context.Context, _ *sql.DB, query string) (string, error) {
	d.calls = append(d.calls, query)
	return "plan for: " + query, nil
}
func (d *explainStubDriver) Dialect() string { return "test" }

func TestSessionExplainSplitsBufferIntoExplainableStatements(t *testing.T) {
	driver := &explainStubDriver{}
	session := &Session{Driver: driver}

	results, err := session.Explain(context.Background(), "select 1;\n\nselect 2\nGO\nselect 3")
	if err != nil {
		t.Fatalf("Explain() error = %v", err)
	}

	wantCalls := []string{"select 1;", "select 2", "select 3"}
	if strings.Join(driver.calls, "|") != strings.Join(wantCalls, "|") {
		t.Fatalf("ExplainQuery calls = %#v, want %#v", driver.calls, wantCalls)
	}
	if len(results) != 3 {
		t.Fatalf("len(results) = %d, want 3", len(results))
	}
	if got := results[1].Rows[0][0]; got != "plan for: select 2" {
		t.Fatalf("results[1] first row = %v, want plan output for second statement", got)
	}
}
