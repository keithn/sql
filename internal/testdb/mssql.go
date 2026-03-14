package testdb

import (
	"database/sql"
	"fmt"
	"os"
	"testing"

	"github.com/sqltui/sql/internal/db"
	dbmssql "github.com/sqltui/sql/internal/db/mssql"
	_ "github.com/microsoft/go-mssqldb"
)

// MSSQLEnvVars lists the environment variables consulted by MSSQLDB.
// The test is skipped if TEST_MSSQL_PASSWORD is not set.
const (
	envMSSQLHost     = "TEST_MSSQL_HOST"
	envMSSQLPort     = "TEST_MSSQL_PORT"
	envMSSQLUser     = "TEST_MSSQL_USER"
	envMSSQLPassword = "TEST_MSSQL_PASSWORD"
	envMSSQLDB       = "TEST_MSSQL_DB"
)

// MSSQLDB connects to a SQL Server instance described by TEST_MSSQL_* env vars,
// creates a unique schema for the test, seeds it with sample data, and registers
// a t.Cleanup to drop the schema afterwards.  The test is skipped automatically
// if TEST_MSSQL_PASSWORD is unset.
func MSSQLDB(t *testing.T) (*sql.DB, *db.Session, string) {
	t.Helper()

	password := os.Getenv(envMSSQLPassword)
	if password == "" {
		t.Skipf("skipping MSSQL test: %s not set", envMSSQLPassword)
	}

	host := envOrDefault(envMSSQLHost, "localhost")
	port := envOrDefault(envMSSQLPort, "1433")
	user := envOrDefault(envMSSQLUser, "sa")
	database := envOrDefault(envMSSQLDB, "master")

	dsn := fmt.Sprintf("sqlserver://%s:%s@%s:%s?database=%s",
		user, password, host, port, database)

	conn, err := sql.Open("sqlserver", dsn)
	if err != nil {
		t.Fatalf("testdb.MSSQLDB: sql.Open: %v", err)
	}
	if err := conn.Ping(); err != nil {
		conn.Close()
		t.Skipf("testdb.MSSQLDB: cannot reach SQL Server (%v); skipping", err)
	}
	t.Cleanup(func() { conn.Close() })

	// Create a per-test schema to isolate test data.
	schemaName := fmt.Sprintf("testdb_%d", testSchemaSeq())
	if _, err := conn.Exec(fmt.Sprintf("CREATE SCHEMA [%s]", schemaName)); err != nil {
		t.Fatalf("testdb.MSSQLDB: CREATE SCHEMA: %v", err)
	}
	t.Cleanup(func() {
		// Drop all tables in the schema first, then drop the schema.
		dropSchema(conn, schemaName)
	})

	if err := mssqlSeed(conn, schemaName); err != nil {
		t.Fatalf("testdb.MSSQLDB: seed: %v", err)
	}

	sess := &db.Session{DB: conn, Driver: &dbmssql.Driver{}}
	return conn, sess, schemaName
}

var schemaSeq int64

func testSchemaSeq() int64 {
	schemaSeq++
	return schemaSeq
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func dropSchema(conn *sql.DB, schema string) {
	// Drop tables (order matters for FK constraints).
	tables := []string{"OrderItems", "Orders", "Products"}
	for _, tbl := range tables {
		conn.Exec(fmt.Sprintf("IF OBJECT_ID('[%s].[%s]') IS NOT NULL DROP TABLE [%s].[%s]", schema, tbl, schema, tbl)) //nolint:errcheck
	}
	conn.Exec(fmt.Sprintf("DROP SCHEMA IF EXISTS [%s]", schema)) //nolint:errcheck
}

func mssqlSeed(conn *sql.DB, schema string) error {
	ddl := fmt.Sprintf(`
CREATE TABLE [%[1]s].[Products] (
    ProductID   INT          IDENTITY(1,1) PRIMARY KEY,
    Name        NVARCHAR(100) NOT NULL,
    Price       DECIMAL(10,2) NOT NULL,
    Stock       INT           NOT NULL DEFAULT 0
);
CREATE TABLE [%[1]s].[Orders] (
    OrderID     INT           IDENTITY(1,1) PRIMARY KEY,
    CustomerID  INT           NOT NULL,
    OrderDate   DATE          NOT NULL,
    Total       DECIMAL(10,2) NOT NULL DEFAULT 0
);
CREATE TABLE [%[1]s].[OrderItems] (
    ItemID      INT           IDENTITY(1,1) PRIMARY KEY,
    OrderID     INT           NOT NULL REFERENCES [%[1]s].[Orders](OrderID),
    ProductID   INT           NOT NULL REFERENCES [%[1]s].[Products](ProductID),
    Qty         INT           NOT NULL,
    UnitPrice   DECIMAL(10,2) NOT NULL
);
`, schema)
	if _, err := conn.Exec(ddl); err != nil {
		return fmt.Errorf("DDL: %w", err)
	}

	data := fmt.Sprintf(`
SET IDENTITY_INSERT [%[1]s].[Products] ON;
INSERT INTO [%[1]s].[Products] (ProductID, Name, Price, Stock) VALUES
    (1, 'Widget',       9.99,  100),
    (2, 'Gadget',      24.99,   50),
    (3, 'Doohickey',    4.99,  200),
    (4, 'Thingamajig', 14.99,   75),
    (5, 'Whatsit',      2.99,  500);
SET IDENTITY_INSERT [%[1]s].[Products] OFF;

SET IDENTITY_INSERT [%[1]s].[Orders] ON;
INSERT INTO [%[1]s].[Orders] (OrderID, CustomerID, OrderDate, Total) VALUES
    (1, 101, '2024-01-15', 34.97),
    (2, 102, '2024-01-16', 49.98),
    (3, 101, '2024-02-01',  9.99);
SET IDENTITY_INSERT [%[1]s].[Orders] OFF;

SET IDENTITY_INSERT [%[1]s].[OrderItems] ON;
INSERT INTO [%[1]s].[OrderItems] (ItemID, OrderID, ProductID, Qty, UnitPrice) VALUES
    (1, 1, 1, 2,  9.99),
    (2, 1, 3, 3,  4.99),
    (3, 2, 2, 2, 24.99),
    (4, 3, 1, 1,  9.99);
SET IDENTITY_INSERT [%[1]s].[OrderItems] OFF;
`, schema)
	if _, err := conn.Exec(data); err != nil {
		return fmt.Errorf("seed data: %w", err)
	}
	return nil
}
