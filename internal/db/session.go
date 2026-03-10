package db

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"
)

// Session wraps a live database connection with execution state.
type Session struct {
	Name   string
	Driver Driver
	DB     *sql.DB

	mu        sync.Mutex
	cancelFn  context.CancelFunc // cancel the active query, if any
	inTx      bool
	tx        *sql.Tx
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
