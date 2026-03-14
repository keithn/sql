package app

import (
	"github.com/sqltui/sql/internal/db"
)

// ExecuteMsg is sent when the user triggers query execution.
type ExecuteMsg struct {
	SQL  string
	Mode ExecMode
}

// ExecMode controls which part of the buffer to execute.
type ExecMode int

const (
	ExecBlock  ExecMode = iota // logical block under cursor (Ctrl-Enter)
	ExecBuffer                 // full buffer (F5)
	ExecSelect                 // visual selection (F5 with selection)
	ExecAll                    // all blocks sequentially (Ctrl-Shift-Enter)
)

// QueryStartedMsg signals that async execution has begun.
type QueryStartedMsg struct{}

// QueryDoneMsg carries results from a completed query.
type QueryDoneMsg struct {
	Results []db.QueryResult
}

// QueryErrorMsg carries an error from a failed query.
type QueryErrorMsg struct {
	Err    error
	LineNo int // server-reported line number, 0 if unknown
}

// CancelQueryMsg cancels the in-flight query.
type CancelQueryMsg struct{}

// BeginTransactionMsg requests entering manual transaction mode.
type BeginTransactionMsg struct{}

// CommitTransactionMsg requests committing the current transaction.
type CommitTransactionMsg struct{}

// RollbackTransactionMsg requests rolling back the current transaction.
type RollbackTransactionMsg struct{}

// ExplainBlockMsg requests an explain plan for the logical block under the cursor.
type ExplainBlockMsg struct{}

// ExplainBufferMsg requests explain plans for the current buffer.
type ExplainBufferMsg struct{}

// ExecuteBlockInTransactionMsg requests running the current block in a transaction.
type ExecuteBlockInTransactionMsg struct{}

// ExecuteBufferInTransactionMsg requests running the full buffer in a transaction.
type ExecuteBufferInTransactionMsg struct{}

// ConnectMsg requests opening a new connection.
type ConnectMsg struct {
	Name string // named connection or raw connection string
}

// ConnectedMsg signals a successful connection.
type ConnectedMsg struct {
	DisplayName  string
	WorkspaceKey string
	Session      *db.Session
	ConnectStr   string // the nameOrDSN used to connect (for auto-reconnect)
}

// ReconnectAndRetryMsg is sent when a dropped-connection error is detected.
// The app will reconnect and retry the original SQL.
type ReconnectAndRetryMsg struct {
	SQL string
}

// ReconnectDoneMsg is sent when the background reconnect succeeds.
type ReconnectDoneMsg struct {
	Session    *db.Session
	Results    []db.QueryResult
	ConnectStr string
}

// ReconnectErrMsg is sent when the background reconnect fails.
type ReconnectErrMsg struct {
	Err error
}

// ConnectErrMsg signals a failed connection attempt.
type ConnectErrMsg struct {
	Err error
}

// ToggleSchemaMsg toggles the schema browser overlay.
type ToggleSchemaMsg struct{}

// ToggleVimMsg toggles vim mode on/off.
type ToggleVimMsg struct{}

// SchemaLoadedMsg carries the introspected schema after connecting.
type SchemaLoadedMsg struct {
	Schema     *db.Schema
	ConnName   string
	DriverName string
}

// SchemaTableSelectedMsg is sent when the user selects a table from the schema browser.
type SchemaTableSelectedMsg struct {
	SQL string
}

// PollTickMsg is fired by the poller timer to trigger a re-execution.
type PollTickMsg struct{}


