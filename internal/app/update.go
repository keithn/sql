package app

import (
	"context"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sqltui/sql/internal/connections"
	"github.com/sqltui/sql/internal/db"
	"github.com/sqltui/sql/internal/ui/editor"
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
			cmds = append(cmds, connectCmd(pending))
		}
		return model, tea.Batch(cmds...)

	case tea.KeyMsg:
		return m.handleGlobalKey(msg)

	case ConnectMsg:
		return m, connectCmd(msg.Name)

	case ConnectedMsg:
		m.session = msg.Session
		m.activeConn = msg.Name
		m.statusbar = m.statusbar.SetConnection(msg.Name)
		if m.ws != nil {
			m.saveSession() // persist current (adhoc) session
			if dir, err := m.ws.ConnDir(msg.Name); err == nil {
				m.wsDir = dir
				m.editor = restoreEditorTabs(m.editor, m.ws, msg.Name, dir)
			}
		}
		return m, tea.Batch(introspectCmd(msg.Session, msg.Name))

	case SchemaLoadedMsg:
		m.schema = m.schema.SetSchema(msg.Schema, msg.ConnName)
		m.editor = m.editor.SetSchemaNames(schemaNames(msg.Schema))
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

	// Toggle schema overlay.
	case "ctrl+b", "f2":
		return m.toggleSchema()

	// Focus editor.
	case "f3", "alt+1":
		return m.focusPane(PaneEditor)

	// Focus results.
	case "f4", "alt+2":
		return m.focusPane(PaneResults)

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

func (m Model) focusPane(p FocusedPane) (tea.Model, tea.Cmd) {
	m.schemaOpen = false
	m.schema = m.schema.Blur()

	var cmd tea.Cmd
	switch p {
	case PaneEditor:
		m.editor, cmd = m.editor.Focus()
		m.results = m.results.Blur()
		m.focused = PaneEditor
		m.statusbar = m.statusbar.SetPane("EDITOR")
	case PaneResults:
		m.editor = m.editor.Blur()
		m.results = m.results.Focus()
		m.focused = PaneResults
		m.statusbar = m.statusbar.SetPane("RESULTS")
	}
	nm, sizeCmd := m.applySize()
	return nm, tea.Batch(cmd, sizeCmd)
}

func (m Model) routeToFocused(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.focused {
	case PaneEditor:
		m.editor, cmd = m.editor.Update(msg)
	case PaneResults:
		m.results, cmd = m.results.Update(msg)
	case PaneSchema:
		m.schema, cmd = m.schema.Update(msg)
	}
	return m, cmd
}

// applySize distributes terminal dimensions to all sub-models.
func (m Model) applySize() (tea.Model, tea.Cmd) {
	if m.width == 0 || m.height == 0 {
		return m, nil
	}

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

	m.editor = m.editor.SetSize(contentW, editorH)
	m.results = m.results.SetSize(contentW, resultsH)
	m.schema = m.schema.SetSize(schemaW, m.height-statusH)
	m.statusbar = m.statusbar.SetWidth(m.width)

	return m, nil
}

// connectCmd returns a tea.Cmd that opens a DB connection.
// nameOrDSN can be a saved connection name or a raw connection string.
func connectCmd(nameOrDSN string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		session, driverName, err := db.DetectAndConnect(ctx, nameOrDSN)
		if err != nil {
			return ConnectErrMsg{Err: err}
		}
		_ = driverName
		return ConnectedMsg{Name: connections.DisplayName(nameOrDSN), Session: session}
	}
}

// saveSession writes session.json for the current workspace dir.
func (m Model) saveSession() {
	if m.ws == nil || m.wsDir == "" {
		return
	}
	tabs := m.editor.TabsInfo()
	sess := &workspace.Session{ActiveTab: m.editor.ActiveTabIdx()}
	for _, t := range tabs {
		sess.Tabs = append(sess.Tabs, workspace.TabRecord{File: t.Path})
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
			tabs = append(tabs, editor.TabState{Path: tr.File, Content: string(content)})
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

// schemaNames extracts all table and column names from a schema for autocomplete.
func schemaNames(s *db.Schema) []string {
	var names []string
	seen := map[string]bool{}
	add := func(n string) {
		if !seen[n] {
			names = append(names, n)
			seen[n] = true
		}
	}
	for _, database := range s.Databases {
		for _, schema := range database.Schemas {
			for _, t := range schema.Tables {
				add(t.Name)
				for _, c := range t.Columns {
					add(c.Name)
				}
			}
			for _, v := range schema.Views {
				add(v.Name)
				for _, c := range v.Columns {
					add(c.Name)
				}
			}
		}
	}
	return names
}
