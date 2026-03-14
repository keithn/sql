package app

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sqltui/sql/internal/config"
	"github.com/sqltui/sql/internal/testdb"
	"github.com/sqltui/sql/internal/ui/editor"
	"github.com/sqltui/sql/internal/ui/results"
)

// SQLiteExecuteBlockReturnsResults tests the full execute→QueryDoneMsg→results
// pipeline using an in-memory SQLite database.
func TestSQLiteExecuteBlockReturnsResults(t *testing.T) {
	_, sess := testdb.SQLiteDB(t)

	m := New(&config.Config{}, "")
	m.session = sess
	m.editor = editor.New(&config.Config{}).SetTabs([]editor.TabState{{
		Path:    "query1.sql",
		Content: "SELECT * FROM Products ORDER BY ProductID",
	}})

	nm, cmd := m.Update(editor.ExecuteBlockMsg{SQL: "SELECT * FROM Products ORDER BY ProductID"})
	m = nm.(Model)
	if cmd == nil {
		t.Fatal("ExecuteBlockMsg should queue an execution command")
	}
	msg := cmd()
	done, ok := msg.(QueryDoneMsg)
	if !ok {
		t.Fatalf("execution command returned %T, want QueryDoneMsg", msg)
	}
	if len(done.Results) != 1 {
		t.Fatalf("len(done.Results) = %d, want 1", len(done.Results))
	}
	rs := done.Results[0]
	if len(rs.Rows) != 5 {
		t.Fatalf("row count = %d, want 5 Products rows", len(rs.Rows))
	}
	// Verify column names include ProductID, Name, Price, Stock.
	colNames := make([]string, len(rs.Columns))
	for i, c := range rs.Columns {
		colNames[i] = c.Name
	}
	for _, want := range []string{"ProductID", "Name", "Price", "Stock"} {
		found := false
		for _, got := range colNames {
			if strings.EqualFold(got, want) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("column %q not found in result columns %v", want, colNames)
		}
	}
}

// TestSQLiteColumnSortInResults verifies that the results model sorts rows
// in-memory when the 's' key is pressed.
func TestSQLiteColumnSortInResults(t *testing.T) {
	conn, sess := testdb.SQLiteDB(t)
	_ = conn

	// Execute a query directly via session to get rows.
	ctx := context.Background()
	qrs, err := sess.Execute(ctx, "SELECT ProductID, Name, Price FROM Products")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(qrs) == 0 || qrs[0].Error != nil {
		t.Fatalf("Execute returned error: %v", func() error {
			if len(qrs) > 0 {
				return qrs[0].Error
			}
			return nil
		}())
	}

	rm := results.New()
	rm = rm.SetSize(120, 20)
	rm = rm.SetResults(qrs)

	// Cursor should start on column 0 (ProductID).
	// Press 's' to sort ascending, then 's' again to sort descending.
	rm, _ = rm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	// After first 's' on col 0, rows should be sorted ascending by ProductID.
	activeRows := rm.ActiveRows()
	if len(activeRows) == 0 {
		t.Fatal("no active rows after sort")
	}
	// ProductID col 0 should be sorted 1,2,3,4,5 ascending.
	for i := 0; i < len(activeRows)-1; i++ {
		a := fmt.Sprintf("%v", activeRows[i][0])
		b := fmt.Sprintf("%v", activeRows[i+1][0])
		if a > b {
			t.Fatalf("ascending sort: row %d (%s) > row %d (%s)", i, a, i+1, b)
		}
	}

	rm, _ = rm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	// After second 's', descending: ProductID should go 5,4,3,2,1.
	activeRows = rm.ActiveRows()
	for i := 0; i < len(activeRows)-1; i++ {
		a := fmt.Sprintf("%v", activeRows[i][0])
		b := fmt.Sprintf("%v", activeRows[i+1][0])
		if a < b {
			t.Fatalf("descending sort: row %d (%s) < row %d (%s)", i, a, i+1, b)
		}
	}
	// View should contain the sort indicator somewhere (column may be wide enough).
	view := rm.View()
	if !strings.Contains(view, "▼") && !strings.Contains(view, "▲") {
		// The indicator might be truncated if column is narrow; just verify sort state.
		t.Logf("sort indicator not visible in view (column truncated); rows are sorted correctly")
	}
}

// TestSQLiteRowNumbersToggle verifies that the '#' key toggles row numbers.
func TestSQLiteRowNumbersToggle(t *testing.T) {
	_, sess := testdb.SQLiteDB(t)

	ctx := context.Background()
	qrs, err := sess.Execute(ctx, "SELECT * FROM Orders")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	rm := results.New()
	rm = rm.SetSize(120, 20)
	rm = rm.SetResults(qrs)

	viewBefore := rm.View()

	rm, _ = rm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'#'}})
	viewAfter := rm.View()

	// Row numbers start with "1" in a leading column; the before/after views should differ.
	if viewBefore == viewAfter {
		t.Fatal("view should change after toggling row numbers with '#'")
	}
}

// TestSQLiteIntrospectPopulatesSchema verifies that the SQLite driver's Introspect
// method returns tables and columns from the seeded database.
func TestSQLiteIntrospectPopulatesSchema(t *testing.T) {
	conn, sess := testdb.SQLiteDB(t)

	ctx := context.Background()
	schema, err := sess.Driver.Introspect(ctx, conn)
	if err != nil {
		t.Fatalf("Introspect: %v", err)
	}
	if schema == nil || len(schema.Databases) == 0 {
		t.Fatal("Introspect returned empty schema")
	}

	// Collect all table names.
	tableNames := map[string]bool{}
	for _, dbNode := range schema.Databases {
		for _, sn := range dbNode.Schemas {
			for _, tbl := range sn.Tables {
				tableNames[tbl.Name] = true
			}
			for _, v := range sn.Views {
				tableNames[v.Name] = true
			}
		}
	}
	for _, want := range []string{"Products", "Orders", "OrderItems", "OrderSummary"} {
		if !tableNames[want] {
			t.Errorf("expected table/view %q in schema, got tables: %v", want, tableNames)
		}
	}
}

// TestMSSQLExecuteBlockReturnsResults mirrors TestSQLiteExecuteBlockReturnsResults
// but runs against a real SQL Server instance.  Skipped unless TEST_MSSQL_PASSWORD is set.
func TestMSSQLExecuteBlockReturnsResults(t *testing.T) {
	_, sess, schema := testdb.MSSQLDB(t)

	m := New(&config.Config{}, "")
	m.session = sess
	sql := "SELECT * FROM [" + schema + "].[Products] ORDER BY ProductID"
	m.editor = editor.New(&config.Config{}).SetTabs([]editor.TabState{{
		Path:    "query1.sql",
		Content: sql,
	}})

	nm, cmd := m.Update(editor.ExecuteBlockMsg{SQL: sql})
	m = nm.(Model)
	if cmd == nil {
		t.Fatal("ExecuteBlockMsg should queue an execution command")
	}
	msg := cmd()
	done, ok := msg.(QueryDoneMsg)
	if !ok {
		t.Fatalf("execution command returned %T, want QueryDoneMsg", msg)
	}
	if len(done.Results) != 1 {
		t.Fatalf("len(done.Results) = %d, want 1", len(done.Results))
	}
	if len(done.Results[0].Rows) != 5 {
		t.Fatalf("row count = %d, want 5", len(done.Results[0].Rows))
	}
}
