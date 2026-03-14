package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Session wraps a live database connection with execution state.
type Session struct {
	Name       string
	DriverName string // "mssql", "postgres", "sqlite"
	Driver     Driver
	DB         *sql.DB

	mu         sync.Mutex
	cancelFn   context.CancelFunc // cancel the active query, if any
	inTx       bool
	tx         *sql.Tx
	autoCommit bool
}

// QueryResult holds the outcome of a single statement execution.
type QueryResult struct {
	Columns      []Column
	Rows         [][]any
	RowsAffected int64
	Duration     time.Duration
	Error        error
}

// Column describes a result column.
type Column struct {
	Name     string
	Type     string
	Nullable bool
}

// Execute runs sql on the session and returns all result sets.
// It is safe to call from a goroutine; cancel via CancelActive().
func (s *Session) Execute(ctx context.Context, query string) ([]QueryResult, error) {
	ctx, cancel := context.WithCancel(ctx)

	s.mu.Lock()
	s.cancelFn = cancel
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.cancelFn = nil
		s.mu.Unlock()
		cancel()
	}()

	start := time.Now()

	var rows *sql.Rows
	var err error

	if s.inTx && s.tx != nil {
		rows, err = s.tx.QueryContext(ctx, query)
	} else {
		rows, err = s.DB.QueryContext(ctx, query)
	}
	if err != nil {
		return nil, fmt.Errorf("execute: %w", err)
	}
	defer rows.Close()

	var results []QueryResult

	for {
		cols, err := rows.Columns()
		if err != nil {
			return results, err
		}

		colTypes, _ := rows.ColumnTypes()
		columns := make([]Column, len(cols))
		for i, c := range cols {
			columns[i] = Column{Name: c}
			if i < len(colTypes) {
				columns[i].Type = colTypes[i].DatabaseTypeName()
				nullable, ok := colTypes[i].Nullable()
				if ok {
					columns[i].Nullable = nullable
				}
			}
		}

		var rowData [][]any
		for rows.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				return results, err
			}
			rowData = append(rowData, vals)
		}

		results = append(results, QueryResult{
			Columns:  columns,
			Rows:     rowData,
			Duration: time.Since(start),
		})

		if !rows.NextResultSet() {
			break
		}
	}

	return results, rows.Err()
}

// Exec runs a DML statement and returns rows affected. Use this instead of
// Execute when you need RowsAffected (UPDATE / DELETE / INSERT).
func (s *Session) Exec(ctx context.Context, query string) (int64, error) {
	ctx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.cancelFn = cancel
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.cancelFn = nil
		s.mu.Unlock()
		cancel()
	}()

	var result sql.Result
	var err error
	if s.inTx && s.tx != nil {
		result, err = s.tx.ExecContext(ctx, query)
	} else {
		result, err = s.DB.ExecContext(ctx, query)
	}
	if err != nil {
		return 0, err
	}
	n, _ := result.RowsAffected()
	return n, nil
}

// Explain returns estimated/non-executing plan output for each explainable SQL unit.
func (s *Session) Explain(ctx context.Context, query string) ([]QueryResult, error) {
	ctx, cancel := context.WithCancel(ctx)

	s.mu.Lock()
	s.cancelFn = cancel
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.cancelFn = nil
		s.mu.Unlock()
		cancel()
	}()

	if s.Driver == nil {
		return nil, fmt.Errorf("explain: no active driver")
	}

	statements := splitExplainStatements(query)
	if len(statements) == 0 {
		return nil, fmt.Errorf("explain: empty query")
	}

	results := make([]QueryResult, 0, len(statements))
	for _, statement := range statements {
		start := time.Now()
		plan, err := s.Driver.ExplainQuery(ctx, s.DB, statement)
		if err != nil {
			return nil, fmt.Errorf("explain: %w", err)
		}
		results = append(results, explainPlanResult(plan, time.Since(start)))
	}

	return results, nil
}

// CancelActive cancels the currently running query, if any.
func (s *Session) CancelActive() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancelFn != nil {
		s.cancelFn()
	}
}

// BeginTx starts a manual transaction.
func (s *Session) BeginTx(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inTx {
		return fmt.Errorf("already in a transaction")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	s.tx = tx
	s.inTx = true
	return nil
}

// Commit commits the current transaction.
func (s *Session) Commit() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.inTx || s.tx == nil {
		return fmt.Errorf("no active transaction")
	}
	err := s.tx.Commit()
	s.tx = nil
	s.inTx = false
	return err
}

// Rollback rolls back the current transaction.
func (s *Session) Rollback() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.inTx || s.tx == nil {
		return fmt.Errorf("no active transaction")
	}
	err := s.tx.Rollback()
	s.tx = nil
	s.inTx = false
	return err
}

// InTransaction reports whether a manual transaction is active.
func (s *Session) InTransaction() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inTx
}

// Close closes the underlying database connection pool.
func (s *Session) Close() error {
	return s.DB.Close()
}

// Introspect returns the schema tree for the connected database.
func (s *Session) Introspect(ctx context.Context) (*Schema, error) {
	return s.Driver.Introspect(ctx, s.DB)
}

func splitExplainStatements(query string) []string {
	lines := strings.Split(query, "\n")
	parts := make([]string, 0, 4)
	current := make([]string, 0, len(lines))
	flush := func() {
		statement := strings.TrimSpace(strings.Join(current, "\n"))
		if statement != "" {
			parts = append(parts, statement)
		}
		current = current[:0]
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.EqualFold(trimmed, "GO"):
			flush()
		case trimmed == "":
			flush()
		default:
			current = append(current, line)
			if strings.HasSuffix(trimmed, ";") {
				flush()
			}
		}
	}
	flush()
	return parts
}

func explainPlanResult(plan string, duration time.Duration) QueryResult {
	plan = strings.ReplaceAll(plan, "\r\n", "\n")
	plan = strings.TrimSpace(plan)
	rows := [][]any{{"(no plan output)"}}
	if plan != "" {
		lines := strings.Split(plan, "\n")
		rows = make([][]any, 0, len(lines))
		for _, line := range lines {
			rows = append(rows, []any{line})
		}
	}
	return QueryResult{
		Columns:  []Column{{Name: "Plan", Type: "TEXT", Nullable: true}},
		Rows:     rows,
		Duration: duration,
	}
}
