package mssql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	_ "github.com/microsoft/go-mssqldb"
	"github.com/sqltui/sql/internal/db"
)

func init() {
	db.Register("mssql", &Driver{})
}

// Driver implements db.Driver for Microsoft SQL Server.
type Driver struct{}

func (d *Driver) Dialect() string { return "tsql" }

func (d *Driver) Open(ctx context.Context, dsn string) (*sql.DB, error) {
	conn, err := sql.Open("sqlserver", dsn)
	if err != nil {
		return nil, fmt.Errorf("mssql: open: %w", err)
	}
	if err := conn.PingContext(ctx); err != nil {
		conn.Close()
		return nil, fmt.Errorf("mssql: ping: %w", err)
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
		port = "1433"
	}
	database := params["database"]
	username := params["username"]
	password := params["password"]

	var dsn string
	if params["windows_auth"] == "true" {
		dsn = fmt.Sprintf("sqlserver://%s:%s/%s?trusted_connection=true", host, port, database)
	} else {
		dsn = fmt.Sprintf("sqlserver://%s:%s@%s:%s/%s", username, password, host, port, database)
	}

	if encrypt, ok := params["encrypt"]; ok && encrypt != "" {
		if strings.Contains(dsn, "?") {
			dsn += "&encrypt=" + encrypt
		} else {
			dsn += "?encrypt=" + encrypt
		}
	}

	return dsn, nil
}

func (d *Driver) Introspect(ctx context.Context, conn *sql.DB) (*db.Schema, error) {
	// Get current database name.
	var dbName string
	row := conn.QueryRowContext(ctx, "SELECT DB_NAME() AS db")
	if err := row.Scan(&dbName); err != nil {
		return nil, fmt.Errorf("mssql: introspect db name: %w", err)
	}

	// Fetch tables and views.
	type tableKey struct {
		schema    string
		name      string
		tableType string
	}
	var tables []tableKey

	tRows, err := conn.QueryContext(ctx, `
SELECT TABLE_SCHEMA, TABLE_NAME, TABLE_TYPE
FROM INFORMATION_SCHEMA.TABLES
ORDER BY TABLE_SCHEMA, TABLE_NAME`)
	if err != nil {
		return nil, fmt.Errorf("mssql: introspect tables: %w", err)
	}
	defer tRows.Close()
	for tRows.Next() {
		var tk tableKey
		if err := tRows.Scan(&tk.schema, &tk.name, &tk.tableType); err != nil {
			return nil, fmt.Errorf("mssql: introspect tables scan: %w", err)
		}
		tables = append(tables, tk)
	}
	if err := tRows.Err(); err != nil {
		return nil, fmt.Errorf("mssql: introspect tables rows: %w", err)
	}

	// Fetch columns: map[schema][table][]ColumnDef
	type colKey struct {
		schema string
		table  string
	}
	colMap := make(map[colKey][]db.ColumnDef)

	cRows, err := conn.QueryContext(ctx, `
SELECT TABLE_SCHEMA, TABLE_NAME, COLUMN_NAME, DATA_TYPE, IS_NULLABLE
FROM INFORMATION_SCHEMA.COLUMNS
ORDER BY TABLE_SCHEMA, TABLE_NAME, ORDINAL_POSITION`)
	if err != nil {
		return nil, fmt.Errorf("mssql: introspect columns: %w", err)
	}
	defer cRows.Close()
	for cRows.Next() {
		var tableSchema, tableName, colName, dataType, isNullable string
		if err := cRows.Scan(&tableSchema, &tableName, &colName, &dataType, &isNullable); err != nil {
			return nil, fmt.Errorf("mssql: introspect columns scan: %w", err)
		}
		ck := colKey{schema: tableSchema, table: tableName}
		colMap[ck] = append(colMap[ck], db.ColumnDef{
			Name:     colName,
			Type:     dataType,
			Nullable: strings.EqualFold(isNullable, "YES"),
		})
	}
	if err := cRows.Err(); err != nil {
		return nil, fmt.Errorf("mssql: introspect columns rows: %w", err)
	}

	// Fetch primary keys: set[schema][table][column]
	type pkKey struct {
		schema string
		table  string
		column string
	}
	pkSet := make(map[pkKey]bool)

	pkRows, err := conn.QueryContext(ctx, `
SELECT kcu.TABLE_SCHEMA, kcu.TABLE_NAME, kcu.COLUMN_NAME
FROM INFORMATION_SCHEMA.TABLE_CONSTRAINTS tc
JOIN INFORMATION_SCHEMA.KEY_COLUMN_USAGE kcu
    ON tc.CONSTRAINT_NAME = kcu.CONSTRAINT_NAME
    AND tc.TABLE_SCHEMA = kcu.TABLE_SCHEMA
WHERE tc.CONSTRAINT_TYPE = 'PRIMARY KEY'`)
	if err != nil {
		return nil, fmt.Errorf("mssql: introspect pks: %w", err)
	}
	defer pkRows.Close()
	for pkRows.Next() {
		var tableSchema, tableName, colName string
		if err := pkRows.Scan(&tableSchema, &tableName, &colName); err != nil {
			return nil, fmt.Errorf("mssql: introspect pks scan: %w", err)
		}
		pkSet[pkKey{schema: tableSchema, table: tableName, column: colName}] = true
	}
	if err := pkRows.Err(); err != nil {
		return nil, fmt.Errorf("mssql: introspect pks rows: %w", err)
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

	// Fetch foreign keys keyed by source schema/table/column.
	type fkKey struct {
		schema string
		table  string
		column string
	}
	fkMap := make(map[fkKey]db.ForeignKey)

	fkRows, err := conn.QueryContext(ctx, `
SELECT
	ps.name AS parent_schema,
	pt.name AS parent_table,
	pc.name AS parent_column,
	rs.name AS referenced_schema,
	rt.name AS referenced_table,
	rc.name AS referenced_column
FROM sys.foreign_key_columns fkc
JOIN sys.tables pt
	ON fkc.parent_object_id = pt.object_id
JOIN sys.schemas ps
	ON pt.schema_id = ps.schema_id
JOIN sys.columns pc
	ON fkc.parent_object_id = pc.object_id
	AND fkc.parent_column_id = pc.column_id
JOIN sys.tables rt
	ON fkc.referenced_object_id = rt.object_id
JOIN sys.schemas rs
	ON rt.schema_id = rs.schema_id
JOIN sys.columns rc
	ON fkc.referenced_object_id = rc.object_id
	AND fkc.referenced_column_id = rc.column_id
ORDER BY ps.name, pt.name, fkc.constraint_column_id`)
	if err != nil {
		return nil, fmt.Errorf("mssql: introspect fks: %w", err)
	}
	defer fkRows.Close()
	for fkRows.Next() {
		var parentSchema, parentTable, parentColumn, refSchema, refTable, refColumn string
		if err := fkRows.Scan(&parentSchema, &parentTable, &parentColumn, &refSchema, &refTable, &refColumn); err != nil {
			return nil, fmt.Errorf("mssql: introspect fks scan: %w", err)
		}
		refName := refTable
		if refSchema != "" {
			refName = refSchema + "." + refTable
		}
		fkMap[fkKey{schema: parentSchema, table: parentTable, column: parentColumn}] = db.ForeignKey{
			RefTable:  refName,
			RefColumn: refColumn,
		}
	}
	if err := fkRows.Err(); err != nil {
		return nil, fmt.Errorf("mssql: introspect fks rows: %w", err)
	}

	// Apply foreign key metadata to columns.
	for ck, cols := range colMap {
		for i, col := range cols {
			if fk, ok := fkMap[fkKey{schema: ck.schema, table: ck.table, column: col.Name}]; ok {
				fkCopy := fk
				cols[i].ForeignKey = &fkCopy
			}
		}
		colMap[ck] = cols
	}

	// Build schema nodes grouped by TABLE_SCHEMA.
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
	// TODO: query sys.columns WHERE object_id = OBJECT_ID(schema+'.'+table)
	return nil, fmt.Errorf("mssql: ExpandStar not yet implemented")
}

func (d *Driver) ExplainQuery(ctx context.Context, conn *sql.DB, query string) (plan string, err error) {
	sqlConn, err := conn.Conn(ctx)
	if err != nil {
		return "", fmt.Errorf("mssql: explain acquire conn: %w", err)
	}
	defer sqlConn.Close()

	if _, err = sqlConn.ExecContext(ctx, "SET SHOWPLAN_TEXT ON"); err != nil {
		return "", fmt.Errorf("mssql: enable showplan: %w", err)
	}
	defer func() {
		if _, offErr := sqlConn.ExecContext(context.Background(), "SET SHOWPLAN_TEXT OFF"); offErr != nil {
			if err == nil {
				err = fmt.Errorf("mssql: disable showplan: %w", offErr)
			} else {
				err = fmt.Errorf("%v; additionally failed to disable showplan: %w", err, offErr)
			}
		}
	}()

	rows, err := sqlConn.QueryContext(ctx, strings.TrimSpace(query))
	if err != nil {
		return "", fmt.Errorf("mssql: explain query: %w", err)
	}
	defer rows.Close()

	var lines []string
	for {
		cols, colsErr := rows.Columns()
		if colsErr != nil {
			return "", fmt.Errorf("mssql: explain columns: %w", colsErr)
		}
		for rows.Next() {
			values := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range values {
				ptrs[i] = &values[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				return "", fmt.Errorf("mssql: explain scan: %w", err)
			}
			parts := make([]string, 0, len(values))
			for _, value := range values {
				parts = append(parts, fmt.Sprint(value))
			}
			lines = append(lines, strings.Join(parts, " | "))
		}
		if !rows.NextResultSet() {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("mssql: explain rows: %w", err)
	}
	return strings.Join(lines, "\n"), nil
}
