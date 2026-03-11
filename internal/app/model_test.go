package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sqltui/sql/internal/config"
	"github.com/sqltui/sql/internal/connections"
	"github.com/sqltui/sql/internal/db"
	"github.com/sqltui/sql/internal/ui/editor"
	uihelp "github.com/sqltui/sql/internal/ui/help"
	"github.com/sqltui/sql/internal/ui/modal"
	"github.com/sqltui/sql/internal/ui/palette"
	"github.com/sqltui/sql/internal/workspace"
	keyring "github.com/zalando/go-keyring"
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
	if !strings.Contains(view, "Help & Settings") || !strings.Contains(view, "Ctrl+K") {
		t.Fatalf("help view should contain title, bindings, and current settings; got %q", view)
	}
	if !helpSectionsContain(m.helpSections(), "tab_size=4") {
		t.Fatalf("help sections should include current editor settings")
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

func TestMouseClickFocusesSchemaPaneWhenOpen(t *testing.T) {
	m := New(&config.Config{}, "")
	m.width = 100
	m.height = 30
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = nm.(Model)
	nm, _ = m.toggleSchema()
	m = nm.(Model)

	nm, _ = m.Update(tea.MouseMsg{X: 5, Y: 5, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = nm.(Model)
	if !m.schemaOpen {
		t.Fatalf("schema should remain open after clicking schema pane")
	}
	if m.focused != PaneSchema {
		t.Fatalf("focused pane after clicking schema = %v, want %v", m.focused, PaneSchema)
	}
}

func TestCtrlUnderscoreRoutesToEditorCommentShortcut(t *testing.T) {
	m := New(&config.Config{}, "")
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = nm.(Model)
	m.editor = m.editor.SetTabs([]editor.TabState{{Path: "query1.sql", Content: "select 1\nselect 2"}})
	m.focused = PaneEditor

	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlUnderscore})
	m = nm.(Model)
	if got := m.editor.Value(); got != "-- select 1\nselect 2" {
		t.Fatalf("editor value after KeyCtrlUnderscore = %q, want first line commented", got)
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

func helpSectionsContain(sections []uihelp.Section, needle string) bool {
	for _, section := range sections {
		for _, line := range section.Lines {
			if strings.Contains(line, needle) {
				return true
			}
		}
	}
	return false
}
