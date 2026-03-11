package mssql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/sqltui/sql/internal/db"
)

func TestIntrospectIncludesForeignKeys(t *testing.T) {
	conn, scenario := openFakeMSSQLDB(t, []fakeQueryResponse{
		{wantContains: "SELECT DB_NAME()", columns: []string{"db"}, rows: [][]driver.Value{{"ActivePLC"}}},
		{wantContains: "FROM INFORMATION_SCHEMA.TABLES", columns: []string{"TABLE_SCHEMA", "TABLE_NAME", "TABLE_TYPE"}, rows: [][]driver.Value{{"dbo", "tblLogger", "BASE TABLE"}, {"dbo", "tblOutpost", "BASE TABLE"}}},
		{wantContains: "FROM INFORMATION_SCHEMA.COLUMNS", columns: []string{"TABLE_SCHEMA", "TABLE_NAME", "COLUMN_NAME", "DATA_TYPE", "IS_NULLABLE"}, rows: [][]driver.Value{{"dbo", "tblLogger", "lngLoggerID", "bigint", "NO"}, {"dbo", "tblLogger", "strName", "nvarchar", "YES"}, {"dbo", "tblOutpost", "lngOutpostID", "bigint", "NO"}, {"dbo", "tblOutpost", "lngLoggerID", "bigint", "NO"}}},
		{wantContains: "CONSTRAINT_TYPE = 'PRIMARY KEY'", columns: []string{"TABLE_SCHEMA", "TABLE_NAME", "COLUMN_NAME"}, rows: [][]driver.Value{{"dbo", "tblLogger", "lngLoggerID"}, {"dbo", "tblOutpost", "lngOutpostID"}}},
		{wantContains: "FROM sys.foreign_key_columns", columns: []string{"parent_schema", "parent_table", "parent_column", "referenced_schema", "referenced_table", "referenced_column"}, rows: [][]driver.Value{{"dbo", "tblOutpost", "lngLoggerID", "dbo", "tblLogger", "lngLoggerID"}}},
	})

	schema, err := (&Driver{}).Introspect(context.Background(), conn)
	if err != nil {
		t.Fatalf("Introspect() error = %v", err)
	}
	if remaining := scenario.remaining(); remaining != 0 {
		t.Fatalf("fake scenario has %d unconsumed responses", remaining)
	}

	logger := mustFindTable(t, schema, "dbo", "tblLogger")
	if col := mustFindColumn(t, logger, "lngLoggerID"); !col.PrimaryKey {
		t.Fatalf("tblLogger.lngLoggerID should be primary key")
	}

	outpost := mustFindTable(t, schema, "dbo", "tblOutpost")
	if col := mustFindColumn(t, outpost, "lngOutpostID"); !col.PrimaryKey {
		t.Fatalf("tblOutpost.lngOutpostID should be primary key")
	}
	loggerID := mustFindColumn(t, outpost, "lngLoggerID")
	if loggerID.ForeignKey == nil {
		t.Fatalf("tblOutpost.lngLoggerID foreign key = nil, want populated foreign key metadata")
	}
	if loggerID.ForeignKey.RefTable != "dbo.tblLogger" || loggerID.ForeignKey.RefColumn != "lngLoggerID" {
		t.Fatalf("tblOutpost.lngLoggerID foreign key = %#v, want dbo.tblLogger.lngLoggerID", loggerID.ForeignKey)
	}
}

func mustFindTable(t *testing.T, schema *db.Schema, schemaName, tableName string) db.Table {
	t.Helper()
	for _, database := range schema.Databases {
		for _, schemaNode := range database.Schemas {
			if !strings.EqualFold(schemaNode.Name, schemaName) {
				continue
			}
			for _, table := range schemaNode.Tables {
				if strings.EqualFold(table.Name, tableName) {
					return table
				}
			}
		}
	}
	t.Fatalf("table %s.%s not found in schema %#v", schemaName, tableName, schema)
	return db.Table{}
}

func mustFindColumn(t *testing.T, table db.Table, columnName string) db.ColumnDef {
	t.Helper()
	for _, col := range table.Columns {
		if strings.EqualFold(col.Name, columnName) {
			return col
		}
	}
	t.Fatalf("column %s not found in table %#v", columnName, table)
	return db.ColumnDef{}
}

type fakeQueryResponse struct {
	wantContains string
	columns      []string
	rows         [][]driver.Value
}

type fakeScenario struct {
	mu        sync.Mutex
	responses []fakeQueryResponse
	index     int
}

func (s *fakeScenario) next(query string) (fakeQueryResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.index >= len(s.responses) {
		return fakeQueryResponse{}, fmt.Errorf("unexpected query after scenario exhausted: %s", query)
	}
	resp := s.responses[s.index]
	if !strings.Contains(query, resp.wantContains) {
		return fakeQueryResponse{}, fmt.Errorf("query %d = %q, want contains %q", s.index, query, resp.wantContains)
	}
	s.index++
	return resp, nil
}

func (s *fakeScenario) remaining() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.responses) - s.index
}

var (
	fakeMSSQLDriverOnce sync.Once
	fakeMSSQLScenarios  sync.Map
	fakeMSSQLSeq        uint64
)

func openFakeMSSQLDB(t *testing.T, responses []fakeQueryResponse) (*sql.DB, *fakeScenario) {
	t.Helper()
	fakeMSSQLDriverOnce.Do(func() {
		sql.Register("augment-fake-mssql", fakeMSSQLDriver{})
	})
	id := fmt.Sprintf("scenario-%d", atomic.AddUint64(&fakeMSSQLSeq, 1))
	scenario := &fakeScenario{responses: append([]fakeQueryResponse(nil), responses...)}
	fakeMSSQLScenarios.Store(id, scenario)
	conn, err := sql.Open("augment-fake-mssql", id)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	t.Cleanup(func() {
		_ = conn.Close()
		fakeMSSQLScenarios.Delete(id)
	})
	return conn, scenario
}

type fakeMSSQLDriver struct{}

func (fakeMSSQLDriver) Open(name string) (driver.Conn, error) {
	scenario, ok := fakeMSSQLScenarios.Load(name)
	if !ok {
		return nil, fmt.Errorf("unknown fake mssql scenario %q", name)
	}
	return &fakeMSSQLConn{scenario: scenario.(*fakeScenario)}, nil
}

type fakeMSSQLConn struct {
	scenario *fakeScenario
}

func (c *fakeMSSQLConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("not implemented")
}
func (c *fakeMSSQLConn) Close() error              { return nil }
func (c *fakeMSSQLConn) Begin() (driver.Tx, error) { return nil, errors.New("not implemented") }

func (c *fakeMSSQLConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	resp, err := c.scenario.next(query)
	if err != nil {
		return nil, err
	}
	return &fakeRows{columns: resp.columns, rows: resp.rows}, nil
}

var _ driver.QueryerContext = (*fakeMSSQLConn)(nil)

type fakeRows struct {
	columns []string
	rows    [][]driver.Value
	index   int
}

func (r *fakeRows) Columns() []string { return r.columns }
func (r *fakeRows) Close() error      { return nil }

func (r *fakeRows) Next(dest []driver.Value) error {
	if r.index >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.index])
	r.index++
	return nil
}
