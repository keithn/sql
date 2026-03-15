package app

import (
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sqltui/sql/internal/config"
	"github.com/sqltui/sql/internal/db"
	"github.com/sqltui/sql/internal/mcp"
	"github.com/sqltui/sql/internal/ui/editor"
	"github.com/sqltui/sql/internal/ui/celledit"
	"github.com/sqltui/sql/internal/ui/cellview"
	uihelp "github.com/sqltui/sql/internal/ui/help"
	"github.com/sqltui/sql/internal/ui/modal"
	"github.com/sqltui/sql/internal/ui/palette"
	"github.com/sqltui/sql/internal/ui/results"
	"github.com/sqltui/sql/internal/ui/rowedit"
	"github.com/sqltui/sql/internal/ui/schema"
	"github.com/sqltui/sql/internal/ui/statusbar"
	"github.com/sqltui/sql/internal/ui/updatepreview"
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

	focused FocusedPane

	editor    editor.Model
	results   results.Model
	help      uihelp.Model
	palette   palette.Model
	schema    schema.Model
	statusbar statusbar.Model
	modal     modal.Model
	cellView        cellview.Model
	cellEdit        celledit.Model
	cellEditCtx     results.CellContext // context captured when cell edit was opened
	rowEdit         rowedit.Model
	updatePreview   updatepreview.Model

	activeConn        string
	session           *db.Session
	reconnectStr      string // nameOrDSN used for last successful connect (for auto-reconnect)
	reconnecting      bool   // true while a background reconnect is in progress
	pendingConnect    string
	pendingBuffer     string
	pendingBufferTx   bool
	exportToClipboard    bool   // true = copy to clipboard (default), false = write to file
	screenshotToClipboard bool  // F10 destination: true = clipboard (default), false = file; toggles each press
	lastSQL              string // most recently executed SQL, used for export table name inference
	snippetSaveOpen  bool   // true when the "save snippet name" prompt is visible
	snippetSaveInput []rune // current input for snippet name
	snippetSaveSQL   string // SQL body being saved

	mcpQueryReply chan<- mcp.Reply // pending MCP execute_query reply channel; nil if none
	pollSecs             int    // active poll interval in seconds (0 = off)
	resultsFullscreen    bool   // true when results pane is expanded to fill the whole window
	mcpMode              bool   // true when the MCP server is running
	mcpAddr              string // address the MCP server is listening on

	ws    *workspace.Workspace
	wsDir string // connDir for the active connection
}

// New creates the root model.
func New(cfg *config.Config, connectTo string) Model {
	dataDir, _ := config.DataDir()
	ws := workspace.New(filepath.Join(dataDir, "workspace"))

	m := Model{
		cfg:                   cfg,
		exportToClipboard:     true,
		screenshotToClipboard: true,
		editor:      editor.New(cfg),
		results:     results.New(),
		help:        uihelp.New(),
		palette:     palette.New(),
		schema:      schema.New().SetResultLimit(cfg.Editor.ResultLimit),
		statusbar:   statusbar.New(),
		modal:       modal.New(),
		cellEdit:      celledit.New(),
		rowEdit:       rowedit.New(),
		updatePreview: updatepreview.New(),
		ws:          ws,
	}
	m.results = m.results.SetFilterHistory(loadFilterHistory())

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

// SetMCPMode marks the app as running with an active MCP server, which shows
// a status bar indicator.
func (m Model) SetMCPMode(on bool) Model {
	m.mcpMode = on
	m.statusbar = m.statusbar.SetMCPMode(on)
	return m
}

// SetMCPAddr stores the listening address for display in the help overlay.
func (m Model) SetMCPAddr(addr string) Model {
	m.mcpAddr = addr
	return m
}

func (m Model) Init() tea.Cmd {
	editorCmd := m.editor.Init()
	return tea.Batch(editorCmd)
}
