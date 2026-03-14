package app

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sqltui/sql/internal/config"
	"github.com/sqltui/sql/internal/connections"
	"github.com/sqltui/sql/internal/db"
	dbsqlite "github.com/sqltui/sql/internal/db/sqlite"
	"github.com/sqltui/sql/internal/ui/editor"
	uihelp "github.com/sqltui/sql/internal/ui/help"
	"github.com/sqltui/sql/internal/ui/modal"
	"github.com/sqltui/sql/internal/ui/palette"
	"github.com/sqltui/sql/internal/workspace"
	keyring "github.com/zalando/go-keyring"
	_ "modernc.org/sqlite"
)

func TestNewRestoresLastConnectionWhenNoArgumentProvided(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("LOCALAPPDATA", dataHome)
	t.Setenv("XDG_DATA_HOME", dataHome)

	dataDir, err := config.DataDir()
	if err != nil {
		t.Fatalf("DataDir() error = %v", err)
	}
	ws := workspace.New(filepath.Join(dataDir, "workspace"))
	if err := ws.SaveLastConnection("prod"); err != nil {
		t.Fatalf("SaveLastConnection() error = %v", err)
	}

	m := New(&config.Config{}, "")
	if m.pendingConnect != "prod" {
		t.Fatalf("pendingConnect = %q, want %q", m.pendingConnect, "prod")
	}
}

func TestNewExplicitArgumentOverridesRememberedConnection(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("LOCALAPPDATA", dataHome)
	t.Setenv("XDG_DATA_HOME", dataHome)

	dataDir, err := config.DataDir()
	if err != nil {
		t.Fatalf("DataDir() error = %v", err)
	}
	ws := workspace.New(filepath.Join(dataDir, "workspace"))
	if err := ws.SaveLastConnection("prod"); err != nil {
		t.Fatalf("SaveLastConnection() error = %v", err)
	}

	m := New(&config.Config{}, "staging")
	if m.pendingConnect != "staging" {
		t.Fatalf("pendingConnect = %q, want %q", m.pendingConnect, "staging")
	}
}

func TestConnectedMsgPersistsOnlyNamedConnections(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("LOCALAPPDATA", dataHome)
	t.Setenv("XDG_DATA_HOME", dataHome)

	dataDir, err := config.DataDir()
	if err != nil {
		t.Fatalf("DataDir() error = %v", err)
	}
	ws := workspace.New(filepath.Join(dataDir, "workspace"))

	m := New(&config.Config{}, "")
	updated, _ := m.Update(ConnectedMsg{DisplayName: "prod", WorkspaceKey: "prod", Session: &db.Session{}})
	m = updated.(Model)
	last, err := ws.LoadLastConnection()
	if err != nil {
		t.Fatalf("LoadLastConnection() error = %v", err)
	}
	if last != "prod" {
		t.Fatalf("LoadLastConnection() = %q, want %q after named connect", last, "prod")
	}

	updated, _ = m.Update(ConnectedMsg{DisplayName: "demo.db", WorkspaceKey: "_adhoc", Session: &db.Session{}})
	_ = updated.(Model)
	last, err = ws.LoadLastConnection()
	if err != nil {
		t.Fatalf("LoadLastConnection() error = %v", err)
	}
	if last != "" {
		t.Fatalf("LoadLastConnection() = %q, want empty after adhoc connect", last)
	}
}

func TestSaveAndRestoreSessionPreservesCursorPositions(t *testing.T) {
	cfg := &config.Config{}
	wsRoot := t.TempDir()
	ws := workspace.New(wsRoot)
	connDir, err := ws.ConnDir("prod")
	if err != nil {
		t.Fatalf("ConnDir() error = %v", err)
	}

	file1 := filepath.Join(connDir, "query1.sql")
	file2 := filepath.Join(connDir, "query2.sql")
	if err := os.WriteFile(file1, []byte("select 1;\nselect 2;\nselect 3;"), 0600); err != nil {
		t.Fatalf("WriteFile(query1) error = %v", err)
	}
	if err := os.WriteFile(file2, []byte("from dual\nwhere x = 1"), 0600); err != nil {
		t.Fatalf("WriteFile(query2) error = %v", err)
	}

	m := New(cfg, "")
	m.ws = ws
	m.wsDir = connDir
	m.editor = m.editor.SetSize(80, 20).SetTabs([]editor.TabState{
		{Path: file1, Content: "select 1;\nselect 2;\nselect 3;", CursorLine: 2, CursorCol: 5},
		{Path: file2, Content: "from dual\nwhere x = 1", CursorLine: 1, CursorCol: 3},
	}).SetActiveTab(1)

	m.saveSession()
	sess, err := workspace.LoadSession(connDir)
	if err != nil {
		t.Fatalf("LoadSession() error = %v", err)
	}
	if len(sess.Tabs) != 2 {
		t.Fatalf("len(sess.Tabs) = %d, want 2", len(sess.Tabs))
	}
	if sess.Tabs[0].Cursor.Line != 2 || sess.Tabs[0].Cursor.Col != 5 {
		t.Fatalf("tab 0 cursor = (%d,%d), want (2,5)", sess.Tabs[0].Cursor.Line, sess.Tabs[0].Cursor.Col)
	}
	if sess.Tabs[1].Cursor.Line != 1 || sess.Tabs[1].Cursor.Col != 3 {
		t.Fatalf("tab 1 cursor = (%d,%d), want (1,3)", sess.Tabs[1].Cursor.Line, sess.Tabs[1].Cursor.Col)
	}

	restored := restoreEditorTabs(editor.New(cfg).SetSize(80, 20), ws, "prod", connDir)
	restored = restored.SetActiveTab(sess.ActiveTab)
	info := restored.TabsInfo()
	if len(info) != 2 {
		t.Fatalf("len(restored.TabsInfo()) = %d, want 2", len(info))
	}
	if info[0].CursorLine != 2 || info[0].CursorCol != 5 {
		t.Fatalf("restored tab 0 cursor = (%d,%d), want (2,5)", info[0].CursorLine, info[0].CursorCol)
	}
	if info[1].CursorLine != 1 || info[1].CursorCol != 3 {
		t.Fatalf("restored tab 1 cursor = (%d,%d), want (1,3)", info[1].CursorLine, info[1].CursorCol)
	}
}

func TestCtrlKOpensConnectionSwitcherAndQueuesConnectMsg(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("LOCALAPPDATA", dataHome)
	t.Setenv("XDG_DATA_HOME", dataHome)

	m := New(&config.Config{Connections: []config.ConnectionProfile{
		{Name: "alpha", Driver: "sqlite", FilePath: "alpha.db"},
		{Name: "prod", Driver: "sqlite", FilePath: "prod.db"},
	}}, "")
	m.width = 100
	m.height = 30
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlK})
	m = nm.(Model)
	if !m.palette.Active() {
		t.Fatalf("palette should be active after Ctrl+K")
	}
	if got := m.statusbar.View(); got == "" {
		t.Fatalf("statusbar view should remain renderable while palette is active")
	}

	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = nm.(Model)
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nm.(Model)
	msg := cmd()
	accepted, ok := msg.(palette.AcceptedMsg)
	if !ok {
		t.Fatalf("enter should yield palette.AcceptedMsg, got %T", msg)
	}
	if accepted.Key != "prod" {
		t.Fatalf("AcceptedMsg.Key = %q, want %q", accepted.Key, "prod")
	}
	if accepted.Kind != palette.KindConnections {
		t.Fatalf("AcceptedMsg.Kind = %v, want %v", accepted.Kind, palette.KindConnections)
	}

	nm, cmd = m.Update(accepted)
	m = nm.(Model)
	if m.palette.Active() {
		t.Fatalf("palette should close after accepting a selection")
	}
	msg = cmd()
	connect, ok := msg.(ConnectMsg)
	if !ok {
		t.Fatalf("accepted selection should queue ConnectMsg, got %T", msg)
	}
	if connect.Name != "prod" {
		t.Fatalf("ConnectMsg.Name = %q, want %q", connect.Name, "prod")
	}
}

func TestCtrlNOpensAddConnectionModalOutsideEditor(t *testing.T) {
	m := New(&config.Config{}, "")
	m.focused = PaneResults
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	m = nm.(Model)
	if !m.modal.Active() {
		t.Fatalf("modal should be active after Ctrl+N outside editor")
	}
}

func TestPaletteCtrlNOpensAddConnectionModal(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("LOCALAPPDATA", dataHome)
	t.Setenv("XDG_DATA_HOME", dataHome)

	m := New(&config.Config{Connections: []config.ConnectionProfile{{Name: "prod", Driver: "sqlite", FilePath: "prod.db"}}}, "")
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlK})
	m = nm.(Model)
	if !m.palette.Active() {
		t.Fatalf("palette should be active after Ctrl+K")
	}
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	m = nm.(Model)
	if m.palette.Active() {
		t.Fatalf("palette should close when opening add-connection modal")
	}
	if !m.modal.Active() {
		t.Fatalf("modal should be active after Ctrl+N from palette")
	}
}

func TestCtrlPOpensCommandPaletteAndRunsHelp(t *testing.T) {
	m := New(&config.Config{}, "")
	m.width = 100
	m.height = 30

	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	m = nm.(Model)
	if !m.palette.Active() {
		t.Fatalf("palette should be active after Ctrl+P")
	}
	if m.palette.Kind() != palette.KindCommands {
		t.Fatalf("palette kind = %v, want %v", m.palette.Kind(), palette.KindCommands)
	}
	if got := m.View(); !strings.Contains(got, "Commands") || !strings.Contains(got, "Connection switcher") {
		t.Fatalf("command palette view missing expected content: %q", got)
	}

	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h', 'e', 'l', 'p'}})
	m = nm.(Model)
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nm.(Model)
	msg := cmd()
	accepted, ok := msg.(palette.AcceptedMsg)
	if !ok {
		t.Fatalf("enter should yield palette.AcceptedMsg, got %T", msg)
	}
	if accepted.Kind != palette.KindCommands || accepted.Key != commandPaletteHelp {
		t.Fatalf("AcceptedMsg = %#v, want command help selection", accepted)
	}

	nm, _ = m.Update(accepted)
	m = nm.(Model)
	if !m.help.Active() {
		t.Fatalf("help should be active after selecting help command")
	}
}

func TestCommandPaletteCtrlNMovesSelectionInsteadOfOpeningModal(t *testing.T) {
	m := New(&config.Config{}, "")
	m.width = 100
	m.height = 30

	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	m = nm.(Model)
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	m = nm.(Model)
	if !m.palette.Active() {
		t.Fatalf("command palette should stay active after Ctrl+N navigation")
	}
	if m.modal.Active() {
		t.Fatalf("command palette Ctrl+N should not immediately open add-connection modal")
	}

	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nm.(Model)
	msg := cmd()
	accepted, ok := msg.(palette.AcceptedMsg)
	if !ok {
		t.Fatalf("enter should yield palette.AcceptedMsg, got %T", msg)
	}
	if accepted.Key != commandPaletteAddConnection {
		t.Fatalf("AcceptedMsg.Key = %q, want %q", accepted.Key, commandPaletteAddConnection)
	}

	nm, _ = m.Update(accepted)
	m = nm.(Model)
	if !m.modal.Active() {
		t.Fatalf("selecting Add connection from command palette should open modal")
	}
}

func TestCtrlHOpensHistoryPaletteAndPastesIntoEditor(t *testing.T) {
	m := New(&config.Config{}, "")
	m.width = 100
	m.height = 30
	m.ws = workspace.New(t.TempDir())
	m.editor = editor.New(&config.Config{}).SetSize(100, 12).SetTabs([]editor.TabState{{Path: "query1.sql", Content: ""}})
	if err := m.ws.AppendHistory(workspace.HistoryEntry{Connection: "dev", Mode: "BLOCK", SQL: "select * from tblUser where Name like '%keith%'"}); err != nil {
		t.Fatalf("AppendHistory() error = %v", err)
	}

	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlH})
	m = nm.(Model)
	if !m.palette.Active() {
		t.Fatalf("history palette should be active after Ctrl+H")
	}
	if m.palette.Kind() != palette.KindHistory {
		t.Fatalf("palette kind = %v, want %v", m.palette.Kind(), palette.KindHistory)
	}

	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nm.(Model)
	msg := cmd()
	accepted, ok := msg.(palette.AcceptedMsg)
	if !ok {
		t.Fatalf("enter should yield palette.AcceptedMsg, got %T", msg)
	}
	if accepted.Kind != palette.KindHistory {
		t.Fatalf("AcceptedMsg.Kind = %v, want %v", accepted.Kind, palette.KindHistory)
	}

	nm, _ = m.Update(accepted)
	m = nm.(Model)
	if got := m.editor.Value(); got != "select * from tblUser where Name like '%keith%'" {
		t.Fatalf("editor value after history insert = %q", got)
	}
	if m.focused != PaneEditor {
		t.Fatalf("focused pane = %v, want editor after history insert", m.focused)
	}
}

func TestExecuteBlockMsgRecordsHistory(t *testing.T) {
	dbConn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	defer dbConn.Close()

	m := New(&config.Config{}, "")
	m.ws = workspace.New(t.TempDir())
	m.activeConn = "prod"
	m.session = &db.Session{DB: dbConn}

	nm, _ := m.Update(editor.ExecuteBlockMsg{SQL: "select 1"})
	m = nm.(Model)
	entries, err := m.ws.LoadHistory(10)
	if err != nil {
		t.Fatalf("LoadHistory() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}
	if entries[0].SQL != "select 1" || entries[0].Connection != "prod" || entries[0].Mode != "BLOCK" {
		t.Fatalf("entries[0] = %#v, want recorded block history entry", entries[0])
	}
}

func TestExecuteBufferMsgOpensConfirmationModalBeforeRunning(t *testing.T) {
	dbConn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	defer dbConn.Close()

	m := New(&config.Config{}, "")
	m.ws = workspace.New(t.TempDir())
	m.session = &db.Session{DB: dbConn}

	nm, cmd := m.Update(editor.ExecuteBufferMsg{SQL: "select 1; select 2"})
	m = nm.(Model)
	if cmd != nil {
		t.Fatalf("ExecuteBufferMsg should not immediately start execution when confirmation is required")
	}
	if !m.modal.Active() {
		t.Fatalf("confirmation modal should be active after full-buffer execute request")
	}
	if got := m.View(); !strings.Contains(got, "Run full buffer?") {
		t.Fatalf("confirmation modal view missing title: %q", got)
	}
	entries, err := m.ws.LoadHistory(10)
	if err != nil {
		t.Fatalf("LoadHistory() error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("history should remain empty until confirmation, got %d entries", len(entries))
	}
}

func TestExecuteBufferConfirmationRunsAndRecordsHistory(t *testing.T) {
	dbConn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	defer dbConn.Close()

	m := New(&config.Config{}, "")
	m.ws = workspace.New(t.TempDir())
	m.activeConn = "prod"
	m.session = &db.Session{DB: dbConn}

	nm, _ := m.Update(editor.ExecuteBufferMsg{SQL: "select 1; select 2"})
	m = nm.(Model)
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nm.(Model)
	msg := cmd()
	confirmed, ok := msg.(modal.ConfirmedMsg)
	if !ok {
		t.Fatalf("confirm enter returned %T, want modal.ConfirmedMsg", msg)
	}
	nm, execCmd := m.Update(confirmed)
	m = nm.(Model)
	if execCmd == nil {
		t.Fatalf("confirmed full-buffer run should queue execution command")
	}
	if m.modal.Active() {
		t.Fatalf("confirmation modal should close after confirm")
	}
	entries, err := m.ws.LoadHistory(10)
	if err != nil {
		t.Fatalf("LoadHistory() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}
	if entries[0].SQL != "select 1; select 2" || entries[0].Mode != "BUFFER" || entries[0].Connection != "prod" {
		t.Fatalf("entries[0] = %#v, want recorded confirmed buffer history entry", entries[0])
	}
}

func TestExecuteBufferConfirmationCancelDoesNotRun(t *testing.T) {
	dbConn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	defer dbConn.Close()

	m := New(&config.Config{}, "")
	m.ws = workspace.New(t.TempDir())
	m.session = &db.Session{DB: dbConn}

	nm, _ := m.Update(editor.ExecuteBufferMsg{SQL: "select 1; select 2"})
	m = nm.(Model)
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = nm.(Model)
	msg := cmd()
	if _, ok := msg.(modal.CancelledMsg); !ok {
		t.Fatalf("esc returned %T, want modal.CancelledMsg", msg)
	}
	nm, execCmd := m.Update(msg)
	m = nm.(Model)
	if execCmd != nil {
		t.Fatalf("cancelled full-buffer run should not queue execution command")
	}
	if m.modal.Active() {
		t.Fatalf("confirmation modal should close after cancel")
	}
	entries, err := m.ws.LoadHistory(10)
	if err != nil {
		t.Fatalf("LoadHistory() error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("history should stay empty after cancellation, got %d entries", len(entries))
	}
}

func TestCommandPaletteShowsRunInTransactionCommandsWhenConnected(t *testing.T) {
	dbConn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	defer dbConn.Close()

	m := New(&config.Config{}, "")
	m.session = &db.Session{DB: dbConn}
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = nm.(Model)

	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	m = nm.(Model)
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("transaction")})
	m = nm.(Model)
	if got := m.View(); !strings.Contains(got, "Run current block in transaction") || !strings.Contains(got, "Run full buffer in transaction") {
		t.Fatalf("command palette view missing run-in-transaction actions: %q", got)
	}
}

func TestExecuteBlockInTransactionBeginsTransactionAndRecordsHistory(t *testing.T) {
	dbConn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	defer dbConn.Close()

	m := New(&config.Config{}, "")
	m.ws = workspace.New(t.TempDir())
	m.activeConn = "prod"
	m.session = &db.Session{DB: dbConn}
	m.editor = editor.New(&config.Config{}).SetTabs([]editor.TabState{{Path: "query1.sql", Content: "select 1"}})

	nm, cmd := m.Update(ExecuteBlockInTransactionMsg{})
	m = nm.(Model)
	if cmd == nil {
		t.Fatalf("ExecuteBlockInTransactionMsg should queue execution command")
	}
	if !m.session.InTransaction() {
		t.Fatalf("transaction should be active before executing block")
	}
	msg := cmd()
	if _, ok := msg.(QueryDoneMsg); !ok {
		t.Fatalf("transaction block command returned %T, want QueryDoneMsg", msg)
	}
	entries, err := m.ws.LoadHistory(10)
	if err != nil {
		t.Fatalf("LoadHistory() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}
	if entries[0].Mode != "BLOCK_TX" || entries[0].SQL != "select 1" || entries[0].Connection != "prod" {
		t.Fatalf("entries[0] = %#v, want recorded transaction block history entry", entries[0])
	}
	if got := m.statusbar.View(); !strings.Contains(got, "TXN") {
		t.Fatalf("statusbar should show TXN after transaction execution start: %q", got)
	}
}

func TestExecuteBufferInTransactionOpensConfirmationModal(t *testing.T) {
	dbConn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	defer dbConn.Close()

	m := New(&config.Config{}, "")
	m.session = &db.Session{DB: dbConn}
	m.editor = editor.New(&config.Config{}).SetTabs([]editor.TabState{{Path: "query1.sql", Content: "select 1; select 2"}})

	nm, cmd := m.Update(ExecuteBufferInTransactionMsg{})
	m = nm.(Model)
	if cmd != nil {
		t.Fatalf("ExecuteBufferInTransactionMsg should wait for confirmation before executing")
	}
	if !m.modal.Active() {
		t.Fatalf("confirmation modal should be active for full-buffer transaction run")
	}
	if got := m.View(); !strings.Contains(got, "Run full buffer in transaction?") {
		t.Fatalf("confirmation modal view missing transaction title: %q", got)
	}
	if m.session.InTransaction() {
		t.Fatalf("transaction should not begin before confirmation")
	}
}

func TestExecuteBufferInTransactionConfirmationRunsAndLeavesTransactionOpen(t *testing.T) {
	dbConn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	defer dbConn.Close()

	m := New(&config.Config{}, "")
	m.ws = workspace.New(t.TempDir())
	m.activeConn = "prod"
	m.session = &db.Session{DB: dbConn}
	m.editor = editor.New(&config.Config{}).SetTabs([]editor.TabState{{Path: "query1.sql", Content: "select 1; select 2"}})

	nm, _ := m.Update(ExecuteBufferInTransactionMsg{})
	m = nm.(Model)
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nm.(Model)
	msg := cmd()
	confirmed, ok := msg.(modal.ConfirmedMsg)
	if !ok {
		t.Fatalf("confirm enter returned %T, want modal.ConfirmedMsg", msg)
	}
	nm, execCmd := m.Update(confirmed)
	m = nm.(Model)
	if execCmd == nil {
		t.Fatalf("confirmed transaction buffer run should queue execution command")
	}
	if !m.session.InTransaction() {
		t.Fatalf("transaction should be active after confirming full-buffer transaction run")
	}
	resultMsg := execCmd()
	if _, ok := resultMsg.(QueryDoneMsg); !ok {
		t.Fatalf("transaction buffer command returned %T, want QueryDoneMsg", resultMsg)
	}
	entries, err := m.ws.LoadHistory(10)
	if err != nil {
		t.Fatalf("LoadHistory() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}
	if entries[0].Mode != "BUFFER_TX" || entries[0].SQL != "select 1; select 2" || entries[0].Connection != "prod" {
		t.Fatalf("entries[0] = %#v, want recorded transaction buffer history entry", entries[0])
	}
}

func TestExecuteBufferInTransactionConfirmationCancelDoesNotBeginTransaction(t *testing.T) {
	dbConn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	defer dbConn.Close()

	m := New(&config.Config{}, "")
	m.ws = workspace.New(t.TempDir())
	m.session = &db.Session{DB: dbConn}
	m.editor = editor.New(&config.Config{}).SetTabs([]editor.TabState{{Path: "query1.sql", Content: "select 1; select 2"}})

	nm, _ := m.Update(ExecuteBufferInTransactionMsg{})
	m = nm.(Model)
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = nm.(Model)
	msg := cmd()
	if _, ok := msg.(modal.CancelledMsg); !ok {
		t.Fatalf("esc returned %T, want modal.CancelledMsg", msg)
	}
	nm, execCmd := m.Update(msg)
	m = nm.(Model)
	if execCmd != nil {
		t.Fatalf("cancelled transaction buffer run should not queue execution command")
	}
	if m.session.InTransaction() {
		t.Fatalf("transaction should not begin after cancelling full-buffer transaction run")
	}
	entries, err := m.ws.LoadHistory(10)
	if err != nil {
		t.Fatalf("LoadHistory() error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("history should stay empty after cancellation, got %d entries", len(entries))
	}
}

func TestCommandPaletteShowsExplainCommandsWhenConnected(t *testing.T) {
	dbConn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	defer dbConn.Close()

	m := New(&config.Config{}, "")
	m.session = &db.Session{DB: dbConn, Driver: &dbsqlite.Driver{}}
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = nm.(Model)

	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	m = nm.(Model)
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("explain")})
	m = nm.(Model)
	if got := m.View(); !strings.Contains(got, "Explain current block") || !strings.Contains(got, "Explain full buffer") {
		t.Fatalf("command palette view missing explain actions: %q", got)
	}
}

func TestExplainBlockMsgReturnsPlanResults(t *testing.T) {
	dbConn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	defer dbConn.Close()

	m := New(&config.Config{}, "")
	m.session = &db.Session{DB: dbConn, Driver: &dbsqlite.Driver{}}
	m.editor = editor.New(&config.Config{}).SetTabs([]editor.TabState{{
		Path:       "query1.sql",
		Content:    "select 1;\n\nselect 2;",
		CursorLine: 2,
		CursorCol:  3,
	}})

	nm, cmd := m.Update(ExplainBlockMsg{})
	m = nm.(Model)
	if cmd == nil {
		t.Fatalf("ExplainBlockMsg should queue an explain command")
	}
	msg := cmd()
	done, ok := msg.(QueryDoneMsg)
	if !ok {
		t.Fatalf("explain command returned %T, want QueryDoneMsg", msg)
	}
	if len(done.Results) != 1 {
		t.Fatalf("len(done.Results) = %d, want 1", len(done.Results))
	}
	if len(done.Results[0].Columns) != 1 || done.Results[0].Columns[0].Name != "Plan" {
		t.Fatalf("explain columns = %#v, want single Plan column", done.Results[0].Columns)
	}
}

func TestExplainBufferMsgSplitsFullBufferIntoMultipleResultSets(t *testing.T) {
	dbConn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	defer dbConn.Close()

	m := New(&config.Config{}, "")
	m.session = &db.Session{DB: dbConn, Driver: &dbsqlite.Driver{}}
	m.editor = editor.New(&config.Config{}).SetTabs([]editor.TabState{{
		Path:    "query1.sql",
		Content: "select 1;\n\nselect 2;",
	}})

	nm, cmd := m.Update(ExplainBufferMsg{})
	m = nm.(Model)
	if cmd == nil {
		t.Fatalf("ExplainBufferMsg should queue an explain command")
	}
	msg := cmd()
	done, ok := msg.(QueryDoneMsg)
	if !ok {
		t.Fatalf("explain command returned %T, want QueryDoneMsg", msg)
	}
	if len(done.Results) != 2 {
		t.Fatalf("len(done.Results) = %d, want 2 result sets for split full-buffer explain", len(done.Results))
	}
}

func TestCommandPaletteShowsBeginTransactionWhenConnected(t *testing.T) {
	dbConn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	defer dbConn.Close()

	m := New(&config.Config{}, "")
	m.session = &db.Session{DB: dbConn}
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = nm.(Model)

	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	m = nm.(Model)
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("transaction")})
	m = nm.(Model)
	if got := m.View(); !strings.Contains(got, "Begin transaction") {
		t.Fatalf("command palette view missing transaction action: %q", got)
	}
}

func TestBeginAndCommitTransactionUpdateStatusbar(t *testing.T) {
	dbConn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	defer dbConn.Close()

	m := New(&config.Config{}, "")
	m.statusbar = m.statusbar.SetWidth(120)
	m.session = &db.Session{DB: dbConn}

	nm, _ := m.Update(BeginTransactionMsg{})
	m = nm.(Model)
	if got := m.statusbar.View(); !strings.Contains(got, "TXN") {
		t.Fatalf("statusbar after begin tx missing TXN indicator: %q", got)
	}

	nm, _ = m.Update(CommitTransactionMsg{})
	m = nm.(Model)
	if got := m.statusbar.View(); strings.Contains(got, "TXN") {
		t.Fatalf("statusbar after commit should clear TXN indicator: %q", got)
	}
}

func TestQueryDoneUpdatesStatusbarRowsAndDuration(t *testing.T) {
	m := New(&config.Config{}, "")
	m.statusbar = m.statusbar.SetWidth(120)

	nm, _ := m.Update(QueryDoneMsg{Results: []db.QueryResult{{Rows: [][]any{{1}, {2}}, Duration: 1500 * time.Millisecond}}})
	m = nm.(Model)
	if got := m.statusbar.View(); !strings.Contains(got, "2 rows  1500ms") {
		t.Fatalf("statusbar after query done missing rows/duration: %q", got)
	}
}

func TestAddConnectionModalSaveOnlyPersistsConnection(t *testing.T) {
	keyring.MockInit()
	dataHome := t.TempDir()
	t.Setenv("LOCALAPPDATA", dataHome)
	t.Setenv("XDG_DATA_HOME", dataHome)

	m := New(&config.Config{}, "")
	nm, _ := m.Update(modal.AddConnSubmittedMsg{
		Name:       "prod",
		ConnString: "postgres://app:secret@localhost/mydb",
		Action:     modal.AddConnSaveOnly,
	})
	m = nm.(Model)

	store, err := connections.LoadManagedStore()
	if err != nil {
		t.Fatalf("LoadManagedStore() error = %v", err)
	}
	entry := store.Get("prod")
	if entry == nil {
		t.Fatalf("expected saved connection entry for prod")
	}
	if entry.Params["raw"] != "postgres://app@localhost/mydb" {
		t.Fatalf("stored raw DSN = %q, want password-free DSN", entry.Params["raw"])
	}
	password, ok, err := connections.LoadPassword("prod")
	if err != nil {
		t.Fatalf("LoadPassword() error = %v", err)
	}
	if !ok || password != "secret" {
		t.Fatalf("LoadPassword() = (%q,%v), want (%q,true)", password, ok, "secret")
	}
	if m.statusbar.View() == "" {
		t.Fatalf("statusbar should remain renderable after save-only flow")
	}
}

func TestF1OpensAndClosesHelpScreen(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("LOCALAPPDATA", dataHome)
	t.Setenv("XDG_DATA_HOME", dataHome)

	m := New(&config.Config{Editor: config.EditorConfig{TabSize: 4, VimMode: true}}, "")
	m.width = 100
	m.height = 32
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyF1})
	m = nm.(Model)
	if !m.help.Active() {
		t.Fatalf("help should be active after F1")
	}
	view := m.View()
	if !strings.Contains(view, "Editor") || !strings.Contains(view, "Ctrl+E") {
		t.Fatalf("help view should contain tab bar and editor bindings; got %q", view)
	}
	if !helpTabsContain(m.helpTabs(), "Ctrl+\\") {
		t.Fatalf("help tabs should include the updated comment shortcut")
	}
	if !helpTabsContain(m.helpTabs(), "Ctrl+Shift+F / Ctrl+F") {
		t.Fatalf("help tabs should include the active-block format shortcut")
	}
	if !helpTabsContain(m.helpTabs(), "Ctrl+R") {
		t.Fatalf("help tabs should include the refactor popup shortcut")
	}
	if !helpTabsContain(m.helpTabs(), "Alt+Up / Alt+Down") {
		t.Fatalf("help tabs should include the query-block navigation shortcut")
	}
	if !helpTabsContain(m.helpTabs(), "tab_size=4") {
		t.Fatalf("help tabs should include current editor settings")
	}
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyF1})
	m = nm.(Model)
	if m.help.Active() {
		t.Fatalf("help should close on F1")
	}
}

func TestExecuteBlockMsgEmptySQLIsNoOp(t *testing.T) {
	m := New(&config.Config{}, "")
	nm, cmd := m.Update(editor.ExecuteBlockMsg{SQL: "   "})
	m = nm.(Model)
	if cmd != nil {
		t.Fatalf("empty ExecuteBlockMsg should not queue a command")
	}
	if m.results.View() == "" {
		t.Fatalf("results view should remain renderable after no-op execute block")
	}
}

func TestNewRestoresPersistedVimMode(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("LOCALAPPDATA", dataHome)
	t.Setenv("XDG_DATA_HOME", dataHome)

	dataDir, err := config.DataDir()
	if err != nil {
		t.Fatalf("DataDir() error = %v", err)
	}
	ws := workspace.New(filepath.Join(dataDir, "workspace"))
	if err := ws.SaveVimMode(true); err != nil {
		t.Fatalf("SaveVimMode() error = %v", err)
	}

	m := New(&config.Config{Editor: config.EditorConfig{VimMode: false}}, "")
	if !m.editor.VimEnabled() {
		t.Fatalf("editor should restore persisted vim mode")
	}
	if m.statusbar.View() == "" {
		t.Fatalf("statusbar should remain renderable after restoring vim mode")
	}
}

func TestToggleVimPersistsAcrossSessions(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("LOCALAPPDATA", dataHome)
	t.Setenv("XDG_DATA_HOME", dataHome)

	m := New(&config.Config{}, "")
	nm, _ := m.Update(ToggleVimMsg{})
	m = nm.(Model)
	if !m.editor.VimEnabled() {
		t.Fatalf("editor should enable vim mode after toggle")
	}

	dataDir, err := config.DataDir()
	if err != nil {
		t.Fatalf("DataDir() error = %v", err)
	}
	ws := workspace.New(filepath.Join(dataDir, "workspace"))
	got, ok, err := ws.LoadVimMode()
	if err != nil {
		t.Fatalf("LoadVimMode() error = %v", err)
	}
	if !ok || !got {
		t.Fatalf("LoadVimMode() = (%v,%v), want (true,true)", got, ok)
	}

	restored := New(&config.Config{}, "")
	if !restored.editor.VimEnabled() {
		t.Fatalf("new app instance should restore persisted vim mode")
	}
}

func TestMouseClickFocusesResultsPane(t *testing.T) {
	m := New(&config.Config{}, "")
	m.width = 100
	m.height = 30
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = nm.(Model)

	nm, _ = m.Update(tea.MouseMsg{X: 10, Y: 25, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = nm.(Model)
	if m.focused != PaneResults {
		t.Fatalf("focused pane after clicking results = %v, want %v", m.focused, PaneResults)
	}
}

func TestSchemaBrowserPopupOpens(t *testing.T) {
	m := New(&config.Config{}, "")
	m.width = 100
	m.height = 30
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = nm.(Model)
	nm, _ = m.openSchemaBrowser()
	m = nm.(Model)

	if !m.schema.Active() {
		t.Fatalf("schema popup should be active after openSchemaBrowser()")
	}
}

func TestMouseDragSelectsEditorTextThroughAppRouting(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("LOCALAPPDATA", dataHome)
	t.Setenv("XDG_DATA_HOME", dataHome)
	m := New(&config.Config{Editor: config.EditorConfig{VimMode: false}}, "")
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = nm.(Model)
	m.editor = m.editor.SetTabs([]editor.TabState{{Path: "query1.sql", Content: "hello world"}})
	m.focused = PaneEditor

	press := tea.MouseMsg{X: 0, Y: 1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
	motion := tea.MouseMsg{X: 5, Y: 1, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft}
	release := tea.MouseMsg{X: 5, Y: 1, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft}

	nm, _ = m.Update(press)
	m = nm.(Model)
	if m.focused != PaneEditor {
		t.Fatalf("focused pane after editor drag press = %v, want %v", m.focused, PaneEditor)
	}
	if !m.editor.MouseSelecting() {
		t.Fatalf("editor should enter mouse-selecting state after press")
	}
	nm, _ = m.Update(motion)
	m = nm.(Model)
	if !m.editor.MouseSelecting() {
		t.Fatalf("editor should remain mouse-selecting during drag motion")
	}
	nm, _ = m.Update(release)
	m = nm.(Model)
	if m.editor.MouseSelecting() {
		t.Fatalf("editor mouse-selecting state should clear on release")
	}
}

func TestCtrlBackslashRoutesToEditorCommentShortcut(t *testing.T) {
	m := New(&config.Config{}, "")
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = nm.(Model)
	m.editor = m.editor.SetTabs([]editor.TabState{{Path: "query1.sql", Content: "select 1\nselect 2"}})
	m.focused = PaneEditor

	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlBackslash})
	m = nm.(Model)
	if got := m.editor.Value(); got != "-- select 1\nselect 2" {
		t.Fatalf("editor value after KeyCtrlBackslash = %q, want first line commented", got)
	}
}

func TestSchemaCompletionsIncludeKinds(t *testing.T) {
	s := &db.Schema{Databases: []db.Database{{Name: "app", Schemas: []db.SchemaNode{{
		Name:       "public",
		Tables:     []db.Table{{Name: "users", Columns: []db.ColumnDef{{Name: "email"}}}},
		Views:      []db.Table{{Name: "active_users", Columns: []db.ColumnDef{{Name: "user_id"}}}},
		Procedures: []db.Routine{{Name: "refresh_users"}},
		Functions:  []db.Routine{{Name: "user_count"}},
	}}}}}

	items := schemaCompletions(s)
	got := map[string]editor.CompletionKind{}
	for _, item := range items {
		got[item.Text] = item.Kind
	}
	if got["users"] != editor.CompletionKindTable {
		t.Fatalf("users kind = %q, want %q", got["users"], editor.CompletionKindTable)
	}
	if got["email"] != editor.CompletionKindColumn {
		t.Fatalf("email kind = %q, want %q", got["email"], editor.CompletionKindColumn)
	}
	if got["active_users"] != editor.CompletionKindView {
		t.Fatalf("active_users kind = %q, want %q", got["active_users"], editor.CompletionKindView)
	}
	if got["refresh_users"] != editor.CompletionKindProcedure {
		t.Fatalf("refresh_users kind = %q, want %q", got["refresh_users"], editor.CompletionKindProcedure)
	}
	if got["user_count"] != editor.CompletionKindFunction {
		t.Fatalf("user_count kind = %q, want %q", got["user_count"], editor.CompletionKindFunction)
	}
}

func helpTabsContain(tabs []uihelp.Tab, needle string) bool {
	for _, tab := range tabs {
		for _, section := range tab.Sections {
			for _, line := range section.Lines {
				if strings.Contains(line, needle) {
					return true
				}
			}
		}
	}
	return false
}
