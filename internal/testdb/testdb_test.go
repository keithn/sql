package testdb_test

import (
	"testing"

	"github.com/sqltui/sql/internal/testdb"
)

func TestSQLiteDBProvidesSampleData(t *testing.T) {
	conn, sess := testdb.SQLiteDB(t)
	if sess == nil {
		t.Fatal("SQLiteDB returned nil session")
	}

	var count int
	if err := conn.QueryRow("SELECT COUNT(*) FROM Products").Scan(&count); err != nil {
		t.Fatalf("SELECT COUNT(*) FROM Products: %v", err)
	}
	if count != 5 {
		t.Fatalf("Products count = %d, want 5", count)
	}

	if err := conn.QueryRow("SELECT COUNT(*) FROM Orders").Scan(&count); err != nil {
		t.Fatalf("SELECT COUNT(*) FROM Orders: %v", err)
	}
	if count != 3 {
		t.Fatalf("Orders count = %d, want 3", count)
	}

	if err := conn.QueryRow("SELECT COUNT(*) FROM OrderItems").Scan(&count); err != nil {
		t.Fatalf("SELECT COUNT(*) FROM OrderItems: %v", err)
	}
	if count != 4 {
		t.Fatalf("OrderItems count = %d, want 4", count)
	}
}

func TestSQLiteDBSessionDriverIsSet(t *testing.T) {
	_, sess := testdb.SQLiteDB(t)
	if sess.Driver == nil {
		t.Fatal("SQLiteDB session Driver is nil")
	}
	if sess.Driver.Dialect() != "sqlite" {
		t.Fatalf("session Driver.Dialect() = %q, want %q", sess.Driver.Dialect(), "sqlite")
	}
}

func TestMSSQLDBSkipsWhenEnvNotSet(t *testing.T) {
	// When TEST_MSSQL_PASSWORD is not set this test should be skipped,
	// not fail. Run as a sub-test so the outer test still passes.
	t.Run("mssql_skip_without_creds", func(t *testing.T) {
		_, _, _ = testdb.MSSQLDB(t)
		// If we get here without TEST_MSSQL_PASSWORD the call above
		// should have called t.Skip, so we'll never reach this line.
		t.Log("MSSQL credentials found, connected successfully")
	})
}
