package app

import (
	"context"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sqltui/sql/internal/config"
	"github.com/sqltui/sql/internal/connections"
	"github.com/sqltui/sql/internal/db"
	"github.com/sqltui/sql/internal/ui/editor"
	"github.com/sqltui/sql/internal/ui/modal"
	"github.com/sqltui/sql/internal/ui/palette"
	"github.com/sqltui/sql/internal/workspace"
)

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		nm, sizeCmd := m.applySize()
		model := nm.(Model)
		var cmds []tea.Cmd
		cmds = append(cmds, sizeCmd)
		if model.pendingConnect != "" {
			pending := model.pendingConnect
			model.pendingConnect = ""
			cmds = append(cmds, connectCmd(model.cfg, pending))
		}
		return model, tea.Batch(cmds...)

	case tea.KeyMsg:
		if m.help.Active() {
			var cmd tea.Cmd
			m.help, cmd = m.help.Update(msg)
			if !m.help.Active() {
				m.statusbar = m.statusbar.SetPane(m.paneLabel())
			}
			return m, cmd
		}
		if m.modal.Active() {
			var cmd tea.Cmd
			m.modal, cmd = m.modal.Update(msg)
			return m, cmd
		}
		if m.palette.Active() && (msg.String() == "ctrl+n" || msg.String() == "a") {
			m = m.closePalette()
			return m.openAddConnectionModal()
		}
		if m.palette.Active() {
			var cmd tea.Cmd
			m.palette, cmd = m.palette.Update(msg)
			return m, cmd
		}
		return m.handleGlobalKey(msg)

	case tea.MouseMsg:
		if m.help.Active() || m.modal.Active() || m.palette.Active() {
			return m, nil
		}
		return m.handleMouse(msg)

	case ToggleVimMsg:
		m.editor = m.editor.ToggleVim()
		m.statusbar = m.statusbar.SetVimMode(m.editor.VimMode())
		if m.ws != nil {
			_ = m.ws.SaveVimMode(m.editor.VimEnabled())
		}
		return m, nil

	case ConnectMsg:
		return m, connectCmd(m.cfg, msg.Name)

	case ConnectedMsg:
		m.session = msg.Session
		m.activeConn = msg.WorkspaceKey
		m.statusbar = m.statusbar.SetConnection(msg.DisplayName)
		if m.ws != nil {
			m.saveSession() // persist current (adhoc) session
			if msg.WorkspaceKey == "_adhoc" {
				_ = m.ws.SaveLastConnection("")
			} else {
				_ = m.ws.SaveLastConnection(msg.WorkspaceKey)
			}
			if dir, err := m.ws.ConnDir(msg.WorkspaceKey); err == nil {
				m.wsDir = dir
				m.editor = restoreEditorTabs(m.editor, m.ws, msg.WorkspaceKey, dir)
				m.statusbar = m.statusbar.SetVimMode(m.editor.VimMode())
			}
		}
		return m, tea.Batch(introspectCmd(msg.Session, msg.DisplayName))

	case palette.AcceptedMsg:
		m = m.closePalette()
		return m, func() tea.Msg { return ConnectMsg{Name: msg.Key} }

	case palette.CancelledMsg:
		m = m.closePalette()
		return m, nil

	case modal.AddConnSubmittedMsg:
		switch msg.Action {
		case modal.AddConnConnect:
			m = m.closeModal()
			return m, func() tea.Msg { return ConnectMsg{Name: msg.ConnString} }
		case modal.AddConnSaveOnly:
			_, err := connections.SaveManaged(msg.Name, msg.ConnString)
			if err != nil {
				m.statusbar = m.statusbar.SetError("connections: " + err.Error())
				return m, nil
			}
			m = m.closeModal()
			m.statusbar = m.statusbar.SetError("")
			return m, nil
		case modal.AddConnSaveConnect:
			_, err := connections.SaveManaged(msg.Name, msg.ConnString)
			if err != nil {
				m.statusbar = m.statusbar.SetError("connections: " + err.Error())
				return m, nil
			}
			m = m.closeModal()
			m.statusbar = m.statusbar.SetError("")
			return m, func() tea.Msg { return ConnectMsg{Name: msg.Name} }
		default:
			return m, nil
		}

	case modal.CancelledMsg:
		m = m.closeModal()
		return m, nil

	case SchemaLoadedMsg:
		m.schema = m.schema.SetSchema(msg.Schema, msg.ConnName)
		m.editor = m.editor.SetSchema(msg.Schema).SetSchemaCompletions(schemaCompletions(msg.Schema))
		return m, nil

	case editor.NewTabMsg:
		connName := m.activeConn
		if connName == "" {
			connName = "_adhoc"
		}
		if m.ws != nil {
			if path, err := m.ws.NewQueryFile(connName); err == nil {
				m.editor = m.editor.AddTab(path, "")
				return m, nil
			}
		}
		m.editor = m.editor.AddTab("", "")
		return m, nil

	case ConnectErrMsg:
		m.statusbar = m.statusbar.SetError("connect: " + msg.Err.Error())
		return m, nil

	case CancelQueryMsg:
		if m.session != nil {
			m.session.CancelActive()
		}
		m.results = m.results.SetLoading(false)
		return m, nil

	case editor.ExecuteBlockMsg:
		if strings.TrimSpace(msg.SQL) == "" {
			return m, nil
		}
		if m.session == nil {
			m.statusbar = m.statusbar.SetError("not connected — use Ctrl+B or pass a connection string")
			return m, nil
		}
		m.results = m.results.SetLoading(true)
		m.statusbar = m.statusbar.SetError("")
		return m, executeCmd(m.session, msg.SQL)

	case editor.ExecuteBufferMsg:
		if m.session == nil {
			m.statusbar = m.statusbar.SetError("not connected")
			return m, nil
		}
		m.results = m.results.SetLoading(true)
		m.statusbar = m.statusbar.SetError("")
		return m, executeCmd(m.session, msg.SQL)

	case QueryDoneMsg:
		m.results = m.results.SetResults(msg.Results)
		return m, nil

	case QueryErrorMsg:
		m.results = m.results.SetError(msg.Err.Error())
		return m, nil
	}

	return m.routeToFocused(msg)
}

func (m Model) handleGlobalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {

	case "ctrl+q":
		m.saveSession()
		return m, tea.Quit

	case "f1":
		return m.openHelpScreen()

	case "ctrl+n":
		if m.focused != PaneEditor {
			return m.openAddConnectionModal()
		}

	case "ctrl+k":
		return m.openConnectionSwitcher()

	// Toggle schema overlay.
	case "ctrl+b", "f2":
		return m.toggleSchema()

	// Focus editor.
	case "f3", "alt+1":
		return m.focusPane(PaneEditor)

	// Focus results.
	case "f4", "alt+2":
		return m.focusPane(PaneResults)

	// Toggle vim mode.
	case "alt+v", "ctrl+alt+v":
		return m, func() tea.Msg { return ToggleVimMsg{} }

	// Resize schema overlay wider/narrower (only when open).
	case "alt+right":
		if m.schemaOpen && m.schemaWidth < 50 {
			m.schemaWidth += 2
			return m.applySize()
		}

	case "alt+left":
		if m.schemaOpen && m.schemaWidth > 10 {
			m.schemaWidth -= 2
			return m.applySize()
		}

	// Resize editor/results split.
	case "alt+up":
		// handled by applySize with a stored split ratio (future)

	case "alt+down":
		// handled by applySize with a stored split ratio (future)
	}

	return m.routeToFocused(msg)
}

func (m Model) openConnectionSwitcher() (tea.Model, tea.Cmd) {
	infos, err := connections.List(m.cfg)
	if err != nil {
		m.statusbar = m.statusbar.SetError("connections: " + err.Error())
		return m, nil
	}
	items := make([]palette.Item, 0, len(infos))
	for _, info := range infos {
		items = append(items, palette.Item{
			Key:     info.Name,
			Title:   info.Name,
			Driver:  info.Driver,
			Summary: info.Summary,
		})
	}
	var cmd tea.Cmd
	m.palette, cmd = m.palette.OpenConnections(items)
	m.statusbar = m.statusbar.SetPane("CONNECTIONS")
	m.statusbar = m.statusbar.SetError("")
	return m, cmd
}

func (m Model) closePalette() Model {
	m.palette = m.palette.Close()
	m.statusbar = m.statusbar.SetPane(m.paneLabel())
	return m
}

func (m Model) openAddConnectionModal() (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.modal, cmd = m.modal.OpenAddConnection()
	m.statusbar = m.statusbar.SetPane("ADD CONNECTION")
	m.statusbar = m.statusbar.SetError("")
	return m, cmd
}

func (m Model) closeModal() Model {
	m.modal = m.modal.Close()
	m.statusbar = m.statusbar.SetPane(m.paneLabel())
	return m
}

func (m Model) openHelpScreen() (tea.Model, tea.Cmd) {
	m.help = m.help.SetSize(m.width-6, minInt(m.height-2, 24)).Open(m.helpSections())
	m.statusbar = m.statusbar.SetPane("HELP")
	m.statusbar = m.statusbar.SetError("")
	return m, nil
}

func (m Model) paneLabel() string {
	switch m.focused {
	case PaneResults:
		return "RESULTS"
	case PaneSchema:
		return "SCHEMA"
	default:
		return "EDITOR"
	}
}

func (m Model) toggleSchema() (tea.Model, tea.Cmd) {
	m.schemaOpen = !m.schemaOpen
	if m.schemaOpen {
		m.schema = m.schema.Focus()
		m.editor = m.editor.Blur()
		m.results = m.results.Blur()
		m.focused = PaneSchema
		m.statusbar = m.statusbar.SetPane("SCHEMA")
	} else {
		m.schema = m.schema.Blur()
		var cmd tea.Cmd
		m.editor, cmd = m.editor.Focus()
		m.focused = PaneEditor
		m.statusbar = m.statusbar.SetPane("EDITOR")
		nm, sizeCmd := m.applySize()
		return nm, tea.Batch(cmd, sizeCmd)
	}
	nm, cmd := m.applySize()
	return nm, cmd
}

func (m Model) applyPaneFocus(p FocusedPane) (Model, tea.Cmd) {
	var cmd tea.Cmd
	switch p {
	case PaneEditor:
		m.schema = m.schema.Blur()
		m.editor, cmd = m.editor.Focus()
		m.results = m.results.Blur()
		m.focused = PaneEditor
		m.statusbar = m.statusbar.SetPane("EDITOR")
	case PaneResults:
		m.schema = m.schema.Blur()
		m.editor = m.editor.Blur()
		m.results = m.results.Focus()
		m.focused = PaneResults
		m.statusbar = m.statusbar.SetPane("RESULTS")
	case PaneSchema:
		if !m.schemaOpen {
			return m, nil
		}
		m.schema = m.schema.Focus()
		m.editor = m.editor.Blur()
		m.results = m.results.Blur()
		m.focused = PaneSchema
		m.statusbar = m.statusbar.SetPane("SCHEMA")
	}
	return m, cmd
}

func (m Model) focusPane(p FocusedPane) (tea.Model, tea.Cmd) {
	m.schemaOpen = false
	m, cmd := m.applyPaneFocus(p)
	nm, sizeCmd := m.applySize()
	return nm, tea.Batch(cmd, sizeCmd)
}

func (m Model) routeToFocused(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.focused {
	case PaneEditor:
		m.editor, cmd = m.editor.Update(msg)
		// Keep vim mode indicator in sync after every key.
		m.statusbar = m.statusbar.SetVimMode(m.editor.VimMode())
	case PaneResults:
		m.results, cmd = m.results.Update(msg)
	case PaneSchema:
		m.schema, cmd = m.schema.Update(msg)
	}
	return m, cmd
}

func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	layout := m.layoutMetrics()
	if layout.contentW <= 0 {
		return m, nil
	}
	contentX := 0
	if m.schemaOpen {
		contentX = layout.schemaW
	}
	if m.editor.MouseSelecting() && (msg.Action == tea.MouseActionMotion || msg.Action == tea.MouseActionRelease) {
		localX, localY := clampMouseToEditor(contentX, layout, msg.X, msg.Y)
		m.editor = m.editor.Mouse(msg, localX, localY)
		m.statusbar = m.statusbar.SetVimMode(m.editor.VimMode())
		return m, nil
	}
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return m, nil
	}
	if m.schemaOpen && msg.X < layout.schemaW && msg.Y >= 0 && msg.Y < m.height-1 {
		nm, cmd := m.applyPaneFocus(PaneSchema)
		return nm, cmd
	}
	if msg.X < contentX || msg.X >= contentX+layout.contentW || msg.Y < 0 {
		return m, nil
	}
	localX := msg.X - contentX
	if msg.Y < layout.editorH {
		nm, cmd := m.applyPaneFocus(PaneEditor)
		nm.editor = nm.editor.Mouse(msg, localX, msg.Y)
		nm.statusbar = nm.statusbar.SetVimMode(nm.editor.VimMode())
		return nm, cmd
	}
	if msg.Y > layout.editorH && msg.Y < layout.editorH+1+layout.resultsH {
		return m.applyPaneFocus(PaneResults)
	}
	return m, nil
}

func clampMouseToEditor(contentX int, layout paneLayout, x, y int) (int, int) {
	localX := clampInt(x-contentX, 0, maxInt(0, layout.contentW-1))
	localY := clampInt(y, 1, maxInt(1, layout.editorH-1))
	return localX, localY
}

// applySize distributes terminal dimensions to all sub-models.
func (m Model) applySize() (tea.Model, tea.Cmd) {
	if m.width == 0 || m.height == 0 {
		return m, nil
	}
	layout := m.layoutMetrics()
	m.editor = m.editor.SetSize(layout.contentW, layout.editorH)
	m.results = m.results.SetSize(layout.contentW, layout.resultsH)
	m.schema = m.schema.SetSize(layout.schemaW, m.height-1)
	m.help = m.help.SetSize(m.width-6, minInt(m.height-2, 24))
	m.palette = m.palette.SetSize(layout.contentW-6, minInt(m.height-4, 12))
	m.modal = m.modal.SetSize(layout.contentW-4, minInt(m.height-4, 14))
	m.statusbar = m.statusbar.SetWidth(m.width)

	return m, nil
}

type paneLayout struct {
	editorH  int
	resultsH int
	contentW int
	schemaW  int
}

func (m Model) layoutMetrics() paneLayout {
	const statusH = 1
	available := m.height - statusH
	if available < 2 {
		available = 2
	}

	editorH := (available * 6) / 10
	if editorH < 2 {
		editorH = 2
	}
	resultsH := available - editorH
	if resultsH < 1 {
		resultsH = 1
	}

	contentW := m.width
	schemaW := 0
	if m.schemaOpen {
		schemaW = (m.width * m.schemaWidth) / 100
		if schemaW < 20 {
			schemaW = 20
		}
		contentW = m.width - schemaW
		if contentW < 20 {
			contentW = 20
		}
	}
	return paneLayout{editorH: editorH, resultsH: resultsH, contentW: contentW, schemaW: schemaW}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// connectCmd returns a tea.Cmd that opens a DB connection.
// nameOrDSN can be a saved connection name or a raw connection string.
func connectCmd(cfg *config.Config, nameOrDSN string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		target, err := connections.Resolve(cfg, nameOrDSN)
		if err != nil {
			return ConnectErrMsg{Err: err}
		}
		session, err := db.Connect(ctx, target.Driver, target.DSN)
		if err != nil {
			return ConnectErrMsg{Err: err}
		}
		session.Name = target.DisplayName
		return ConnectedMsg{DisplayName: target.DisplayName, WorkspaceKey: target.WorkspaceKey, Session: session}
	}
}

// saveSession writes session.json for the current workspace dir.
func (m Model) saveSession() {
	if m.ws == nil || m.wsDir == "" {
		return
	}
	_ = m.ws.SaveVimMode(m.editor.VimEnabled())
	tabs := m.editor.TabsInfo()
	sess := &workspace.Session{ActiveTab: m.editor.ActiveTabIdx()}
	for _, t := range tabs {
		rec := workspace.TabRecord{File: t.Path}
		rec.Cursor.Line = t.CursorLine
		rec.Cursor.Col = t.CursorCol
		sess.Tabs = append(sess.Tabs, rec)
	}
	_ = workspace.SaveSession(m.wsDir, sess)
}

// restoreEditorTabs loads session.json from connDir and populates the editor.
// If no session exists, a fresh query file is created.
func restoreEditorTabs(ed editor.Model, ws *workspace.Workspace, connName, connDir string) editor.Model {
	sess, err := workspace.LoadSession(connDir)
	if err == nil && len(sess.Tabs) > 0 {
		var tabs []editor.TabState
		for _, tr := range sess.Tabs {
			if tr.File == "" {
				continue
			}
			content, _ := os.ReadFile(tr.File)
			tabs = append(tabs, editor.TabState{
				Path:       tr.File,
				Content:    string(content),
				CursorLine: tr.Cursor.Line,
				CursorCol:  tr.Cursor.Col,
			})
		}
		if len(tabs) > 0 {
			ed = ed.SetTabs(tabs)
			ed = ed.SetActiveTab(sess.ActiveTab)
			return ed
		}
	}
	// No saved session — create the first query file.
	path, err := ws.NewQueryFile(connName)
	if err != nil {
		return ed // leave default tab in place
	}
	return ed.SetTabs([]editor.TabState{{Path: path}})
}

// executeCmd returns a tea.Cmd that runs SQL and returns results.
func executeCmd(session *db.Session, sql string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		results, err := session.Execute(ctx, sql)
		if err != nil {
			return QueryErrorMsg{Err: err}
		}
		return QueryDoneMsg{Results: results}
	}
}

// introspectCmd returns a tea.Cmd that introspects the connected database.
func introspectCmd(session *db.Session, connName string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		schema, err := session.Introspect(ctx)
		if err != nil {
			return nil // silently ignore introspection errors
		}
		return SchemaLoadedMsg{Schema: schema, ConnName: connName}
	}
}

// schemaCompletions extracts typed schema items for autocomplete.
func schemaCompletions(s *db.Schema) []editor.CompletionItem {
	var items []editor.CompletionItem
	seen := map[string]bool{}
	add := func(name string, kind editor.CompletionKind) {
		key := strings.ToUpper(name)
		if name != "" && !seen[key] {
			items = append(items, editor.CompletionItem{Text: name, Kind: kind})
			seen[key] = true
		}
	}
	for _, database := range s.Databases {
		for _, schema := range database.Schemas {
			for _, t := range schema.Tables {
				add(t.Name, editor.CompletionKindTable)
				for _, c := range t.Columns {
					add(c.Name, editor.CompletionKindColumn)
				}
			}
			for _, v := range schema.Views {
				add(v.Name, editor.CompletionKindView)
				for _, c := range v.Columns {
					add(c.Name, editor.CompletionKindColumn)
				}
			}
			for _, p := range schema.Procedures {
				add(p.Name, editor.CompletionKindProcedure)
			}
			for _, f := range schema.Functions {
				add(f.Name, editor.CompletionKindFunction)
			}
		}
	}
	return items
}
