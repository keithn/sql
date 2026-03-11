package app

import (
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sqltui/sql/internal/config"
	"github.com/sqltui/sql/internal/db"
	"github.com/sqltui/sql/internal/ui/editor"
	uihelp "github.com/sqltui/sql/internal/ui/help"
	"github.com/sqltui/sql/internal/ui/modal"
	"github.com/sqltui/sql/internal/ui/palette"
	"github.com/sqltui/sql/internal/ui/results"
	"github.com/sqltui/sql/internal/ui/schema"
	"github.com/sqltui/sql/internal/ui/statusbar"
	"github.com/sqltui/sql/internal/workspace"
)

// FocusedPane identifies which pane currently has keyboard focus.
type FocusedPane int

const (
	PaneEditor FocusedPane = iota
	PaneResults
	PaneSchema
)

// Model is the root bubbletea model.
type Model struct {
	cfg    *config.Config
	width  int
	height int

	focused     FocusedPane
	schemaOpen  bool
	schemaWidth int // overlay width as percentage of total

	editor    editor.Model
	results   results.Model
	help      uihelp.Model
	palette   palette.Model
	schema    schema.Model
	statusbar statusbar.Model
	modal     modal.Model

	activeConn     string
	session        *db.Session
	pendingConnect string

	ws    *workspace.Workspace
	wsDir string // connDir for the active connection
}

// New creates the root model.
func New(cfg *config.Config, connectTo string) Model {
	dataDir, _ := config.DataDir()
	ws := workspace.New(filepath.Join(dataDir, "workspace"))

	m := Model{
		cfg:         cfg,
		schemaWidth: 28,
		editor:      editor.New(cfg),
		results:     results.New(),
		help:        uihelp.New(),
		palette:     palette.New(),
		schema:      schema.New(),
		statusbar:   statusbar.New(),
		modal:       modal.New(),
		ws:          ws,
	}
	if vimEnabled, ok, err := ws.LoadVimMode(); err == nil && ok {
		m.editor = m.editor.SetVimEnabled(vimEnabled)
	}
	m.statusbar = m.statusbar.SetVimMode(m.editor.VimMode())

	// Load the adhoc workspace so there's always a persistent query file.
	if dir, err := ws.ConnDir("_adhoc"); err == nil {
		m.wsDir = dir
		m.editor = restoreEditorTabs(m.editor, ws, "_adhoc", dir)
	}

	if connectTo != "" {
		m.pendingConnect = connectTo
	} else if last, err := ws.LoadLastConnection(); err == nil {
		m.pendingConnect = last
	}
	return m
}

func (m Model) Init() tea.Cmd {
	editorCmd := m.editor.Init()
	return tea.Batch(editorCmd)
}
