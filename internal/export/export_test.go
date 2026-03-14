package export_test

import (
	"strings"
	"testing"

	"github.com/sqltui/sql/internal/db"
	"github.com/sqltui/sql/internal/export"
)

var sampleResult = db.QueryResult{
	Columns: []db.Column{
		{Name: "id"},
		{Name: "name"},
		{Name: "value"},
	},
	Rows: [][]any{
		{1, "Alice", 3.14},
		{2, "Bob, Jr.", nil},
		{3, `O'Brien`, "hello"},
	},
}

func TestCSV(t *testing.T) {
	out := export.CSV(sampleResult)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if lines[0] != "id,name,value" {
		t.Errorf("header: got %q", lines[0])
	}
	// Row with comma in value must be quoted.
	if !strings.Contains(lines[2], `"Bob, Jr."`) {
		t.Errorf("expected quoted field, got %q", lines[2])
	}
	// nil → empty field
	if !strings.HasSuffix(lines[2], ",") {
		t.Errorf("expected trailing comma for nil, got %q", lines[2])
	}
	if len(lines) != 4 { // header + 3 data rows
		t.Errorf("expected 4 lines, got %d", len(lines))
	}
}

func TestMarkdown(t *testing.T) {
	out := export.Markdown(sampleResult)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	// header + separator + 3 data rows = 5 lines
	if len(lines) != 5 {
		t.Errorf("expected 5 lines, got %d: %v", len(lines), lines)
	}
	if !strings.HasPrefix(lines[0], "|") || !strings.HasSuffix(lines[0], "|") {
		t.Errorf("header line not pipe-delimited: %q", lines[0])
	}
	if !strings.Contains(lines[1], "---") {
		t.Errorf("separator line missing dashes: %q", lines[1])
	}
}

func TestJSON(t *testing.T) {
	out, err := export.JSON(sampleResult)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "[") {
		t.Errorf("expected JSON array, got %q", out[:20])
	}
	if !strings.Contains(out, `"Alice"`) {
		t.Errorf("expected Alice in output")
	}
	if !strings.Contains(out, `"name"`) {
		t.Errorf("expected column name key")
	}
}

func TestSQLInsert(t *testing.T) {
	out := export.SQLInsert(sampleResult, "users")
	if !strings.HasPrefix(out, "INSERT INTO users (id, name, value)") {
		t.Errorf("unexpected header: %q", strings.SplitN(out, "\n", 2)[0])
	}
	// nil → NULL
	if !strings.Contains(out, "NULL") {
		t.Errorf("expected NULL for nil value")
	}
	// string with single quote → escaped
	if !strings.Contains(out, `'O''Brien'`) {
		t.Errorf("expected escaped single quote, got:\n%s", out)
	}
	// last row ends with semicolon
	trimmed := strings.TrimRight(out, "\n")
	if !strings.HasSuffix(trimmed, ";") {
		t.Errorf("expected semicolon at end, got: %q", trimmed[len(trimmed)-5:])
	}
}

func TestSQLInsertEmpty(t *testing.T) {
	empty := db.QueryResult{Columns: []db.Column{{Name: "x"}}, Rows: nil}
	if export.SQLInsert(empty, "t") != "" {
		t.Error("expected empty output for no rows")
	}
}

func TestMarkdownEmpty(t *testing.T) {
	empty := db.QueryResult{}
	if export.Markdown(empty) != "" {
		t.Error("expected empty output for no columns")
	}
}

func TestExtractTableName(t *testing.T) {
	cases := []struct {
		sql  string
		want string
	}{
		{"SELECT * FROM users", "users"},
		{"SELECT * FROM dbo.Orders WHERE id = 1", "dbo.Orders"},
		{"select * from [Order Details] where 1=1", "Order Details"},
		{"SELECT a FROM (SELECT 1 AS a) sub", "table_name"}, // subquery → fallback
		{"SELECT * FROM users u JOIN roles r ON u.id = r.user_id", "users"},
		{"WITH cte AS (SELECT 1) SELECT * FROM cte", "cte"},
		{"UPDATE users SET x = 1", "users"},
		{"UPDATE table_name\nSET [col] = 3\nWHERE [id] = 1", "table_name"},
		{"UPDATE [dbo].[Orders]\nSET x = 1\nWHERE id = 1", "dbo.Orders"},
		{"", "table_name"},
	}
	for _, c := range cases {
		got := export.ExtractTableName(c.sql)
		if got != c.want {
			t.Errorf("ExtractTableName(%q) = %q, want %q", c.sql, got, c.want)
		}
	}
}
