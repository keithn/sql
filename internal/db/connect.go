package db

import (
	"context"
	"fmt"
	"strings"
)

// Connect opens a database connection using the named driver and DSN.
func Connect(ctx context.Context, driverName, dsn string) (*Session, error) {
	driver, err := Get(driverName)
	if err != nil {
		return nil, err
	}
	sqlDB, err := driver.Open(ctx, dsn)
	if err != nil {
		return nil, err
	}
	return &Session{Driver: driver, DB: sqlDB}, nil
}

// DetectAndConnect detects the driver from the connection string and opens a
// database connection. It returns the session, the detected driver name, and
// any error.
func DetectAndConnect(ctx context.Context, connString string) (*Session, string, error) {
	driverName, err := detectDriver(connString)
	if err != nil {
		return nil, "", err
	}
	session, err := Connect(ctx, driverName, connString)
	if err != nil {
		return nil, driverName, err
	}
	return session, driverName, nil
}

// detectDriver infers the driver name from a connection string.
func detectDriver(connString string) (string, error) {
	lower := strings.ToLower(connString)

	// MSSQL / SQL Server
	if strings.HasPrefix(lower, "sqlserver://") ||
		strings.HasPrefix(lower, "mssql://") ||
		strings.Contains(lower, "server=") ||
		strings.Contains(lower, "data source=") ||
		strings.Contains(lower, "initial catalog=") ||
		strings.Contains(lower, "trusted_connection=") {
		return "mssql", nil
	}

	// PostgreSQL
	if strings.HasPrefix(lower, "postgres://") ||
		strings.HasPrefix(lower, "postgresql://") ||
		strings.Contains(lower, "host=") ||
		strings.Contains(lower, "dbname=") {
		return "postgres", nil
	}

	// SQLite
	if strings.HasSuffix(lower, ".db") ||
		strings.HasSuffix(lower, ".sqlite") ||
		strings.HasSuffix(lower, ".sqlite3") ||
		strings.HasPrefix(lower, "file:") {
		return "sqlite", nil
	}

	return "", fmt.Errorf("cannot detect database driver from connection string")
}
