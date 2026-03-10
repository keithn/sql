package db

import (
	"context"
	"database/sql"
)

// Driver is the factory interface each database backend implements.
// Drivers register themselves via Register() in their package init().
type Driver interface {
	// Open creates a new database connection from the given DSN.
	Open(ctx context.Context, dsn string) (*sql.DB, error)

	// BuildDSN converts a ConnectionProfile into a driver-native DSN string.
	BuildDSN(params map[string]string) (string, error)

	// Introspect returns the schema tree for the connected database.
	Introspect(ctx context.Context, db *sql.DB) (*Schema, error)

	// ExpandStar returns the column list for a given table/view.
	ExpandStar(ctx context.Context, db *sql.DB, schema, table string) ([]string, error)

	// ExplainQuery returns the EXPLAIN output for the given SQL.
	ExplainQuery(ctx context.Context, db *sql.DB, sql string) (string, error)

	// Dialect returns the SQL dialect name for syntax highlighting.
	Dialect() string
}

// Schema is the full introspected schema tree for a connection.
type Schema struct {
	Databases []Database
}

type Database struct {
	Name    string
	Schemas []SchemaNode
}

type SchemaNode struct {
	Name       string
	Tables     []Table
	Views      []Table
	Procedures []Routine
	Functions  []Routine
}

type Table struct {
	Name    string
	Columns []ColumnDef
	Indexes []Index
}

type ColumnDef struct {
	Name       string
	Type       string
	Nullable   bool
	PrimaryKey bool
	ForeignKey *ForeignKey
}

type ForeignKey struct {
	RefTable  string
	RefColumn string
}

type Index struct {
	Name    string
	Columns []string
	Unique  bool
}

type Routine struct {
	Name       string
	Definition string
}
