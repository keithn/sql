package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/sqltui/sql/internal/db"
)

func init() {
	db.Register("postgres", &Driver{})
}

// Driver implements db.Driver for PostgreSQL via pgx/v5.
type Driver struct{}

func (d *Driver) Dialect() string { return "postgresql" }

func (d *Driver) Open(ctx context.Context, dsn string) (*sql.DB, error) {
	conn, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("postgres: open: %w", err)
	}
	if err := conn.PingContext(ctx); err != nil {
		conn.Close()
		return nil, fmt.Errorf("postgres: ping: %w", err)
	}
	return conn, nil
}

func (d *Driver) BuildDSN(params map[string]string) (string, error) {
	// If a raw connection string is provided, use it as-is.
	if raw, ok := params["raw"]; ok && raw != "" {
		return raw, nil
	}

	host := params["host"]
	if host == "" {
		host = "localhost"
	}
	port := params["port"]
	if port == "" {
		port = "5432"
	}
	database := params["database"]
	username := params["username"]
	password := params["password"]
	sslmode := params["sslmode"]
	if sslmode == "" {
		sslmode = "disable"
	}

	dsn := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s",
		username, password, host, port, database, sslmode)
	return dsn, nil
}

func (d *Driver) Introspect(ctx context.Context, conn *sql.DB) (*db.Schema, error) {
	// Get current database name.
	var dbName string
	row := conn.QueryRowContext(ctx, "SELECT current_database()")
	if err := row.Scan(&dbName); err != nil {
		return nil, fmt.Errorf("postgres: introspect db name: %w", err)
	}

	// Fetch tables and views (exclude system schemas).
	type tableKey struct {
		schema    string
		name      string
		tableType string
	}
	var tables []tableKey

	tRows, err := conn.QueryContext(ctx, `
SELECT table_schema, table_name, table_type
FROM information_schema.tables
WHERE table_schema NOT IN ('information_schema', 'pg_catalog', 'pg_toast')
  AND table_schema NOT LIKE 'pg_%'
ORDER BY table_schema, table_name`)
	if err != nil {
		return nil, fmt.Errorf("postgres: introspect tables: %w", err)
	}
	defer tRows.Close()
	for tRows.Next() {
		var tk tableKey
		if err := tRows.Scan(&tk.schema, &tk.name, &tk.tableType); err != nil {
			return nil, fmt.Errorf("postgres: introspect tables scan: %w", err)
		}
		tables = append(tables, tk)
	}
	if err := tRows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: introspect tables rows: %w", err)
	}

	// Fetch columns.
	type colKey struct {
		schema string
		table  string
	}
	colMap := make(map[colKey][]db.ColumnDef)

	cRows, err := conn.QueryContext(ctx, `
SELECT table_schema, table_name, column_name, data_type, is_nullable
FROM information_schema.columns
WHERE table_schema NOT IN ('information_schema', 'pg_catalog', 'pg_toast')
  AND table_schema NOT LIKE 'pg_%'
ORDER BY table_schema, table_name, ordinal_position`)
	if err != nil {
		return nil, fmt.Errorf("postgres: introspect columns: %w", err)
	}
	defer cRows.Close()
	for cRows.Next() {
		var tableSchema, tableName, colName, dataType, isNullable string
		if err := cRows.Scan(&tableSchema, &tableName, &colName, &dataType, &isNullable); err != nil {
			return nil, fmt.Errorf("postgres: introspect columns scan: %w", err)
		}
		ck := colKey{schema: tableSchema, table: tableName}
		colMap[ck] = append(colMap[ck], db.ColumnDef{
			Name:     colName,
			Type:     dataType,
			Nullable: strings.EqualFold(isNullable, "YES"),
		})
	}
	if err := cRows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: introspect columns rows: %w", err)
	}

	// Fetch primary keys.
	type pkKey struct {
		schema string
		table  string
		column string
	}
	pkSet := make(map[pkKey]bool)

	pkRows, err := conn.QueryContext(ctx, `
SELECT kcu.table_schema, kcu.table_name, kcu.column_name
FROM information_schema.table_constraints tc
JOIN information_schema.key_column_usage kcu
    ON tc.constraint_name = kcu.constraint_name
    AND tc.table_schema = kcu.table_schema
WHERE tc.constraint_type = 'PRIMARY KEY'`)
	if err != nil {
		return nil, fmt.Errorf("postgres: introspect pks: %w", err)
	}
	defer pkRows.Close()
	for pkRows.Next() {
		var tableSchema, tableName, colName string
		if err := pkRows.Scan(&tableSchema, &tableName, &colName); err != nil {
			return nil, fmt.Errorf("postgres: introspect pks scan: %w", err)
		}
		pkSet[pkKey{schema: tableSchema, table: tableName, column: colName}] = true
	}
	if err := pkRows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: introspect pks rows: %w", err)
	}

	// Apply primary key flags to columns.
	for ck, cols := range colMap {
		for i, col := range cols {
			if pkSet[pkKey{schema: ck.schema, table: ck.table, column: col.Name}] {
				cols[i].PrimaryKey = true
			}
		}
		colMap[ck] = cols
	}

	// Build schema nodes grouped by table_schema.
	schemaMap := make(map[string]*db.SchemaNode)
	schemaOrder := []string{}

	for _, tk := range tables {
		if _, exists := schemaMap[tk.schema]; !exists {
			schemaMap[tk.schema] = &db.SchemaNode{Name: tk.schema}
			schemaOrder = append(schemaOrder, tk.schema)
		}
		sn := schemaMap[tk.schema]
		ck := colKey{schema: tk.schema, table: tk.name}
		cols := colMap[ck]
		t := db.Table{Name: tk.name, Columns: cols}
		if strings.EqualFold(tk.tableType, "VIEW") {
			sn.Views = append(sn.Views, t)
		} else {
			sn.Tables = append(sn.Tables, t)
		}
	}

	schemaNodes := make([]db.SchemaNode, 0, len(schemaOrder))
	for _, name := range schemaOrder {
		schemaNodes = append(schemaNodes, *schemaMap[name])
	}

	return &db.Schema{
		Databases: []db.Database{
			{Name: dbName, Schemas: schemaNodes},
		},
	}, nil
}

func (d *Driver) ExpandStar(ctx context.Context, conn *sql.DB, schema, table string) ([]string, error) {
	// TODO: SELECT column_name FROM information_schema.columns WHERE ...
	return nil, fmt.Errorf("postgres: ExpandStar not yet implemented")
}

func (d *Driver) ExplainQuery(ctx context.Context, conn *sql.DB, query string) (string, error) {
	rows, err := conn.QueryContext(ctx, "EXPLAIN (FORMAT TEXT) "+strings.TrimSpace(query))
	if err != nil {
		return "", fmt.Errorf("postgres: explain: %w", err)
	}
	defer rows.Close()

	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return "", fmt.Errorf("postgres: explain scan: %w", err)
		}
		lines = append(lines, line)
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("postgres: explain rows: %w", err)
	}
	return strings.Join(lines, "\n"), nil
}
