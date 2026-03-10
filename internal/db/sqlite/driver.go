package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
	"github.com/sqltui/sql/internal/db"
)

func init() {
	db.Register("sqlite", &Driver{})
}

// Driver implements db.Driver for SQLite via modernc.org/sqlite (pure Go).
type Driver struct{}

func (d *Driver) Dialect() string { return "sqlite" }

func (d *Driver) Open(ctx context.Context, dsn string) (*sql.DB, error) {
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("sqlite: open: %w", err)
	}
	if err := conn.PingContext(ctx); err != nil {
		conn.Close()
		return nil, fmt.Errorf("sqlite: ping: %w", err)
	}
	return conn, nil
}

func (d *Driver) BuildDSN(params map[string]string) (string, error) {
	if path, ok := params["file"]; ok {
		return path, nil
	}
	return "", fmt.Errorf("sqlite: missing 'file' parameter")
}

func (d *Driver) Introspect(ctx context.Context, conn *sql.DB) (*db.Schema, error) {
	// Fetch all tables and views from sqlite_master.
	type objectEntry struct {
		name    string
		objType string // "table" or "view"
	}
	var objects []objectEntry

	rows, err := conn.QueryContext(ctx, `
SELECT name, type FROM sqlite_master
WHERE type IN ('table', 'view') AND name NOT LIKE 'sqlite_%'
ORDER BY type, name`)
	if err != nil {
		return nil, fmt.Errorf("sqlite: introspect objects: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var e objectEntry
		if err := rows.Scan(&e.name, &e.objType); err != nil {
			return nil, fmt.Errorf("sqlite: introspect objects scan: %w", err)
		}
		objects = append(objects, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: introspect objects rows: %w", err)
	}

	var tables []db.Table
	var views []db.Table

	for _, obj := range objects {
		// Use PRAGMA table_info to get column details.
		// Columns: cid, name, type, notnull, dflt_value, pk
		pragmaRows, err := conn.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info('%s')", obj.name))
		if err != nil {
			return nil, fmt.Errorf("sqlite: introspect pragma table_info(%s): %w", obj.name, err)
		}

		var cols []db.ColumnDef
		for pragmaRows.Next() {
			var cid, notnull, pk int
			var colName, colType string
			var dfltValue sql.NullString
			if err := pragmaRows.Scan(&cid, &colName, &colType, &notnull, &dfltValue, &pk); err != nil {
				pragmaRows.Close()
				return nil, fmt.Errorf("sqlite: introspect pragma scan(%s): %w", obj.name, err)
			}
			cols = append(cols, db.ColumnDef{
				Name:       colName,
				Type:       colType,
				Nullable:   notnull == 0,
				PrimaryKey: pk > 0,
			})
		}
		pragmaRows.Close()
		if err := pragmaRows.Err(); err != nil {
			return nil, fmt.Errorf("sqlite: introspect pragma rows(%s): %w", obj.name, err)
		}

		t := db.Table{Name: obj.name, Columns: cols}
		if obj.objType == "view" {
			views = append(views, t)
		} else {
			tables = append(tables, t)
		}
	}

	schemaNode := db.SchemaNode{
		Name:   "main",
		Tables: tables,
		Views:  views,
	}

	return &db.Schema{
		Databases: []db.Database{
			{Name: "main", Schemas: []db.SchemaNode{schemaNode}},
		},
	}, nil
}

func (d *Driver) ExpandStar(ctx context.Context, conn *sql.DB, schema, table string) ([]string, error) {
	// TODO: PRAGMA table_info(<table>)
	return nil, fmt.Errorf("sqlite: ExpandStar not yet implemented")
}

func (d *Driver) ExplainQuery(ctx context.Context, conn *sql.DB, query string) (string, error) {
	// TODO: EXPLAIN QUERY PLAN ...
	return "", fmt.Errorf("sqlite: ExplainQuery not yet implemented")
}
