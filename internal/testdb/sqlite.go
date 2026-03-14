// Package testdb provides in-memory database helpers for tests.
package testdb

import (
	"database/sql"
	"testing"

	"github.com/sqltui/sql/internal/db"
	dbsqlite "github.com/sqltui/sql/internal/db/sqlite"
	_ "modernc.org/sqlite"
)

// SQLiteDB opens an in-memory SQLite database pre-populated with a sample
// schema (Products, Orders, OrderItems) and test data.  The database and its
// *db.Session are closed automatically when t finishes.
func SQLiteDB(t *testing.T) (*sql.DB, *db.Session) {
	t.Helper()
	conn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("testdb.SQLiteDB: sql.Open: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	if err := sqliteSeed(conn); err != nil {
		t.Fatalf("testdb.SQLiteDB: seed: %v", err)
	}
	sess := &db.Session{DB: conn, Driver: &dbsqlite.Driver{}}
	return conn, sess
}

func sqliteSeed(conn *sql.DB) error {
	ddl := `
CREATE TABLE Products (
    ProductID   INTEGER PRIMARY KEY,
    Name        TEXT    NOT NULL,
    Price       REAL    NOT NULL,
    Stock       INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE Orders (
    OrderID     INTEGER PRIMARY KEY,
    CustomerID  INTEGER NOT NULL,
    OrderDate   TEXT    NOT NULL,
    Total       REAL    NOT NULL DEFAULT 0
);
CREATE TABLE OrderItems (
    ItemID      INTEGER PRIMARY KEY,
    OrderID     INTEGER NOT NULL REFERENCES Orders(OrderID),
    ProductID   INTEGER NOT NULL REFERENCES Products(ProductID),
    Qty         INTEGER NOT NULL,
    UnitPrice   REAL    NOT NULL
);
CREATE VIEW OrderSummary AS
    SELECT o.OrderID, o.CustomerID, COUNT(i.ItemID) AS LineCount, SUM(i.Qty * i.UnitPrice) AS Total
    FROM Orders o
    LEFT JOIN OrderItems i ON i.OrderID = o.OrderID
    GROUP BY o.OrderID;
`
	if _, err := conn.Exec(ddl); err != nil {
		return err
	}

	data := `
INSERT INTO Products VALUES (1, 'Widget',  9.99,  100);
INSERT INTO Products VALUES (2, 'Gadget',  24.99,  50);
INSERT INTO Products VALUES (3, 'Doohickey', 4.99, 200);
INSERT INTO Products VALUES (4, 'Thingamajig', 14.99, 75);
INSERT INTO Products VALUES (5, 'Whatsit', 2.99, 500);

INSERT INTO Orders VALUES (1, 101, '2024-01-15', 34.97);
INSERT INTO Orders VALUES (2, 102, '2024-01-16', 49.98);
INSERT INTO Orders VALUES (3, 101, '2024-02-01',  9.99);

INSERT INTO OrderItems VALUES (1, 1, 1, 2,  9.99);
INSERT INTO OrderItems VALUES (2, 1, 3, 3,  4.99);
INSERT INTO OrderItems VALUES (3, 2, 2, 2, 24.99);
INSERT INTO OrderItems VALUES (4, 3, 1, 1,  9.99);
`
	_, err := conn.Exec(data)
	return err
}
