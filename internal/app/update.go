package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sqltui/sql/internal/config"
	"github.com/sqltui/sql/internal/connections"
	"github.com/sqltui/sql/internal/db"
	"github.com/sqltui/sql/internal/export"
	"github.com/sqltui/sql/internal/mcp"
	"github.com/sqltui/sql/internal/screenshot"
	"github.com/sqltui/sql/internal/ui/celledit"
	"github.com/sqltui/sql/internal/ui/cellview"
	"github.com/sqltui/sql/internal/ui/editor"
	"github.com/sqltui/sql/internal/ui/modal"
	"github.com/sqltui/sql/internal/ui/palette"
	"github.com/sqltui/sql/internal/ui/results"
	"github.com/sqltui/sql/internal/ui/rowedit"
	"github.com/sqltui/sql/internal/ui/schema"
	"github.com/sqltui/sql/internal/ui/updatepreview"
	"github.com/sqltui/sql/internal/workspace"
)

const (
	commandPaletteConnectionSwitcher = "command.connection_switcher"
	commandPaletteAddConnection      = "command.add_connection"
	commandPaletteHelp               = "command.help"
	commandPaletteToggleSchema       = "command.toggle_schema"
	commandPaletteFocusEditor        = "command.focus_editor"
	commandPaletteFocusResults       = "command.focus_results"
	commandPaletteFocusSchema        = "command.focus_schema"
	commandPaletteToggleVim          = "command.toggle_vim"
	commandPaletteNewTab             = "command.new_tab"
	commandPaletteBeginTx            = "command.begin_transaction"
	commandPaletteCommitTx           = "command.commit_transaction"
	commandPaletteRollbackTx         = "command.rollback_transaction"
	commandPaletteExplainBlock       = "command.explain_block"
	commandPaletteExplainBuffer      = "command.explain_buffer"
	commandPaletteExecuteBlockTx     = "command.execute_block_transaction"
	commandPaletteExecuteBufferTx    = "command.execute_buffer_transaction"
	commandPaletteSaveSnippet        = "command.save_snippet"
	commandPaletteBrowseSnippets     = "command.browse_snippets"
	confirmRunFullBuffer             = "confirm.run_full_buffer"
	confirmRunFullBufferTx           = "confirm.run_full_buffer_transaction"
	confirmDeleteConnection          = "confirm.delete_connection."

	exportCSV       = "export.csv"
	exportMarkdown  = "export.markdown"
	exportJSON      = "export.json"
	exportSQLInsert = "export.sql_insert"
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
		// F10 is global — intercept before any overlay consumes it.
		if msg.String() == "f10" {
			return m.handleScreenshot()
		}
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
		if m.palette.Active() && m.palette.QuickAddEnabled() && msg.String() == "ctrl+n" {
			m = m.closePalette()
			return m.openAddConnectionModal()
		}
		if m.palette.Active() && m.palette.Kind() == palette.KindExport && msg.String() == "tab" {
			m.exportToClipboard = !m.exportToClipboard
			return m.openExportPalette()
		}
		if m.palette.Active() {
			var cmd tea.Cmd
			m.palette, cmd = m.palette.Update(msg)
			return m, cmd
		}
		if m.schema.Active() {
			var cmd tea.Cmd
			m.schema, cmd = m.schema.Update(msg)
			return m, cmd
		}
		if m.cellView.Active() {
			var cmd tea.Cmd
			m.cellView, cmd = m.cellView.Update(msg)
			return m, cmd
		}
		// When the snippet save prompt is open, handle it exclusively.
		if m.snippetSaveOpen {
			return m.handleSnippetSaveKey(msg)
		}
		// When the row edit form is open, handle it exclusively.
		if m.rowEdit.Active() {
			var cmd tea.Cmd
			m.rowEdit, cmd = m.rowEdit.Update(msg)
			return m, cmd
		}
		// When the cell edit overlay is open, handle it exclusively.
		if m.cellEdit.Active() {
			var cmd tea.Cmd
			m.cellEdit, cmd = m.cellEdit.Update(msg)
			return m, cmd
		}
		// When the update preview panel is open, handle it exclusively.
		if m.updatePreview.Active() {
			var cmd tea.Cmd
			m.updatePreview, cmd = m.updatePreview.Update(msg)
			return m, cmd
		}
		// When the results filter, find, poll, limit bar, or row detail is open, bypass global key shortcuts.
		if m.results.FilterOpen() || m.results.FindOpen() || m.results.PollOpen() || m.results.LimitOpen() || m.results.RowDetailOpen() {
			return m.routeToFocused(msg)
		}
		// When the editor goto-line or rename bar is open, bypass global key shortcuts.
		if m.editor.GotoOpen() || m.editor.RenameTabOpen() {
			return m.routeToFocused(msg)
		}
		return m.handleGlobalKey(msg)

	case tea.MouseMsg:
		if m.help.Active() || m.modal.Active() || m.palette.Active() {
			return m, nil
		}
		return m.handleMouse(msg)

	case editor.QuitRequestMsg:
		m.saveSession()
		return m, tea.Quit

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
		m.reconnectStr = msg.ConnectStr
		m.reconnecting = false
		m.statusbar = m.statusbar.SetConnection(msg.DisplayName)
		m.statusbar = m.statusbar.SetTx(msg.Session != nil && msg.Session.InTransaction())
		m.statusbar = m.statusbar.SetRows(0).SetDuration(0)
		m.statusbar = m.statusbar.SetError("")
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
		switch msg.Kind {
		case palette.KindConnections:
			return m, func() tea.Msg { return ConnectMsg{Name: msg.Key} }
		case palette.KindCommands:
			return m.handleCommandPaletteSelection(msg.Key)
		case palette.KindHistory:
			return m.handleHistoryPaletteSelection(msg.Key)
		case palette.KindExport:
			return m.handleExportSelection(msg.Key)
		case palette.KindSnippets:
			return m.handleSnippetPaste(msg.Key)
		default:
			return m, nil
		}

	case palette.DeleteMsg:
		if m.palette.Kind() == palette.KindConnections {
			name := msg.Key
			return m.openDeleteConnectionConfirm(name)
		}
		// Delete snippet by key (which is the numeric ID as a string).
		return m.handleSnippetDelete(msg.Key)

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
		m.pendingBuffer = ""
		m.pendingBufferTx = false
		m = m.closeModal()
		return m, nil

	case modal.TestConnectionMsg:
		cs := msg.ConnString
		return m, func() tea.Msg {
			_, _, err := db.DetectAndConnect(context.Background(), cs)
			return TestConnectionResultMsg{Err: err}
		}

	case TestConnectionResultMsg:
		if msg.Err != nil {
			m.modal = m.modal.SetTestStatus("✗ " + msg.Err.Error())
		} else {
			m.modal = m.modal.SetTestStatus("✓ Connected")
		}
		return m, nil

	case modal.ConfirmedMsg:
		switch msg.ID {
		case confirmRunFullBuffer:
			sql := m.pendingBuffer
			m.pendingBuffer = ""
			m.pendingBufferTx = false
			m = m.closeModal()
			return m.executeBufferSQL(sql)
		case confirmRunFullBufferTx:
			sql := m.pendingBuffer
			m.pendingBuffer = ""
			m.pendingBufferTx = false
			m = m.closeModal()
			return m.executeSQLInTransaction("BUFFER_TX", sql)
		default:
			if strings.HasPrefix(msg.ID, confirmDeleteConnection) {
				name := strings.TrimPrefix(msg.ID, confirmDeleteConnection)
				m = m.closeModal()
				if err := connections.DeleteManaged(name, m.cfg); err != nil {
					m.statusbar = m.statusbar.SetError("delete connection: " + err.Error())
				} else {
					m.statusbar = m.statusbar.SetError("Deleted connection: " + name)
				}
				return m, nil
			}
			m = m.closeModal()
			return m, nil
		}

	case SchemaLoadedMsg:
		m.schema = m.schema.SetSchema(msg.Schema, msg.ConnName, msg.DriverName)
		m.editor = m.editor.SetSchema(msg.Schema).SetSchemaCompletions(schemaCompletions(msg.Schema))
		return m, nil

	case schema.RowCountRequestMsg:
		if m.session == nil {
			return m, nil
		}
		sess := m.session
		qn := msg.QualifiedName
		driver := sess.DriverName
		return m, func() tea.Msg {
			ctx := context.Background()
			var query string
			switch driver {
			case "mssql":
				// Use fast approximate from partition stats when possible.
				query = fmt.Sprintf(
					"SELECT SUM(p.rows) FROM sys.partitions p INNER JOIN sys.tables t ON p.object_id = t.object_id "+
						"INNER JOIN sys.schemas s ON t.schema_id = s.schema_id "+
						"WHERE p.index_id IN (0,1) AND s.name + '.' + t.name = '%s'",
					strings.ReplaceAll(qn, "'", "''"),
				)
			default:
				query = fmt.Sprintf("SELECT COUNT(*) FROM %s", qn)
			}
			results, err := sess.Execute(ctx, query)
			if err != nil {
				return schema.RowCountResultMsg{QualifiedName: qn, Err: err}
			}
			if len(results) == 0 || len(results[0].Rows) == 0 || len(results[0].Rows[0]) == 0 {
				return schema.RowCountResultMsg{QualifiedName: qn, Count: 0}
			}
			cell := results[0].Rows[0][0]
			var count int64
			switch v := cell.(type) {
			case int64:
				count = v
			case int32:
				count = int64(v)
			case int:
				count = int64(v)
			case float64:
				count = int64(v)
			default:
				count = 0
			}
			return schema.RowCountResultMsg{QualifiedName: qn, Count: count}
		}

	case schema.RowCountResultMsg:
		var cmd tea.Cmd
		m.schema, cmd = m.schema.Update(msg)
		return m, cmd

	case schema.TableSelectedMsg:
		var focusCmd tea.Cmd
		m, focusCmd = m.applyPaneFocus(PaneEditor)
		var insertCmd tea.Cmd
		m.editor, insertCmd = m.editor.InsertText(msg.SQL)
		return m, tea.Batch(focusCmd, insertCmd)

	case schema.CancelledMsg:
		return m.applyPaneFocus(PaneEditor)

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
		m.statusbar = m.statusbar.SetError("query cancelled")
		m.statusbar = m.statusbar.SetRows(0).SetDuration(0)
		return m, nil

	case BeginTransactionMsg:
		return m.handleTransactionAction("begin", func() error {
			return m.session.BeginTx(context.Background())
		})

	case CommitTransactionMsg:
		return m.handleTransactionAction("commit", func() error {
			return m.session.Commit()
		})

	case RollbackTransactionMsg:
		return m.handleTransactionAction("rollback", func() error {
			return m.session.Rollback()
		})

	case ExplainBlockMsg:
		return m.explainSQL(m.editor.CurrentBlock())

	case ExplainBufferMsg:
		return m.explainSQL(m.editor.Value())

	case ExecuteBlockInTransactionMsg:
		return m.executeSQLInTransaction("BLOCK_TX", m.editor.CurrentBlock())

	case ExecuteBufferInTransactionMsg:
		if strings.TrimSpace(m.editor.Value()) == "" {
			return m, nil
		}
		if m.session == nil {
			m.statusbar = m.statusbar.SetError("not connected")
			return m, nil
		}
		m.pendingBuffer = m.editor.Value()
		m.pendingBufferTx = true
		return m.openRunFullBufferInTransactionConfirmModal()

	case editor.ExecuteBlockMsg:
		if strings.TrimSpace(msg.SQL) == "" {
			return m, nil
		}
		if m.session == nil {
			m.statusbar = m.statusbar.SetError("not connected — use Ctrl+B or pass a connection string")
			return m, nil
		}
		m.lastSQL = msg.SQL
		m.recordHistory("BLOCK", msg.SQL)
		m.results = m.results.SetLoading(true)
		m.statusbar = m.statusbar.SetError("")
		m.statusbar = m.statusbar.SetRows(0).SetDuration(0)
		m.statusbar = m.statusbar.SetTx(m.session.InTransaction())
		return m, executeCmd(m.session, msg.SQL)

	case editor.ExecuteBufferMsg:
		if strings.TrimSpace(msg.SQL) == "" {
			return m, nil
		}
		if m.session == nil {
			m.statusbar = m.statusbar.SetError("not connected")
			return m, nil
		}
		m.pendingBuffer = msg.SQL
		return m.openRunFullBufferConfirmModal()

	case QueryDoneMsg:
		m.results = m.results.SetResults(msg.Results)
		m.statusbar = m.statusbar.SetError("")
		m.statusbar = m.statusbar.SetTx(m.session != nil && m.session.InTransaction())
		m.statusbar = m.statusbar.SetRows(totalResultRows(msg.Results)).SetDuration(resultDurationMs(msg.Results))
		if m.focused == PaneResults {
			m.statusbar = m.statusbar.SetColType(m.results.CurrentColumnType())
		}
		// Notify any pending MCP execute_query.
		if m.mcpQueryReply != nil {
			m.mcpQueryReply <- mcp.Reply{Result: mcpResultsJSON(msg.Results)}
			m.mcpQueryReply = nil
		}
		return m, nil

	case QueryErrorMsg:
		m.results = m.results.SetError(msg.Err.Error())
		m.statusbar = m.statusbar.SetError(msg.Err.Error())
		m.statusbar = m.statusbar.SetTx(m.session != nil && m.session.InTransaction())
		m.statusbar = m.statusbar.SetRows(0).SetDuration(0)
		// Notify any pending MCP execute_query.
		if m.mcpQueryReply != nil {
			m.mcpQueryReply <- mcp.Reply{Err: msg.Err.Error()}
			m.mcpQueryReply = nil
		}
		return m, nil

	case mcp.RequestMsg:
		return m.handleMCPRequest(msg)

	case ReconnectAndRetryMsg:
		if m.reconnectStr == "" {
			m.statusbar = m.statusbar.SetError("connection lost (no reconnect info)")
			return m, nil
		}
		m.reconnecting = true
		m.statusbar = m.statusbar.SetError("Reconnecting…")
		return m, reconnectAndExecuteCmd(m.cfg, m.reconnectStr, msg.SQL)

	case ReconnectDoneMsg:
		m.reconnecting = false
		m.session = msg.Session
		m.reconnectStr = msg.ConnectStr
		m.statusbar = m.statusbar.SetError("")
		m.statusbar = m.statusbar.SetConnection(msg.Session.Name)
		m.results = m.results.SetResults(msg.Results)
		m.statusbar = m.statusbar.SetRows(totalResultRows(msg.Results)).SetDuration(resultDurationMs(msg.Results))
		if m.focused == PaneResults {
			m.statusbar = m.statusbar.SetColType(m.results.CurrentColumnType())
		}
		return m, nil

	case ReconnectErrMsg:
		m.reconnecting = false
		m.results = m.results.SetError(msg.Err.Error())
		m.statusbar = m.statusbar.SetError("Reconnect failed: " + msg.Err.Error())
		return m, nil

	case results.CellYankMsg:
		if err := writeClipboard(msg.Text); err != nil {
			m.statusbar = m.statusbar.SetError("copy: " + err.Error())
		} else {
			m.statusbar = m.statusbar.SetError("copied to clipboard")
		}
		return m, nil

	case schema.CopyTableNameMsg:
		if err := writeClipboard(msg.Name); err != nil {
			m.statusbar = m.statusbar.SetError("copy: " + err.Error())
		} else {
			m.statusbar = m.statusbar.SetError("copied: " + msg.Name)
		}
		return m, nil

	case results.FilterConfirmedMsg:
		_ = saveFilterHistory(m.results.FilterHistory())
		return m, nil

	case results.StartPollMsg:
		m.pollSecs = msg.Seconds
		m.results = m.results.SetPollSecs(m.pollSecs)
		if m.pollSecs > 0 {
			return m, pollTickCmd(m.pollSecs)
		}
		return m, nil

	case results.StartLimitMsg:
		lim := msg.Limit
		if lim <= 0 {
			lim = m.cfg.Editor.ResultLimit
		}
		m.schema = m.schema.SetResultLimit(lim)
		return m, nil

	case PollTickMsg:
		if m.pollSecs <= 0 || m.lastSQL == "" || m.session == nil {
			return m, nil
		}
		m.results = m.results.SetLoading(true)
		m.statusbar = m.statusbar.SetError("")
		m.statusbar = m.statusbar.SetRows(0).SetDuration(0)
		return m, tea.Batch(executeCmd(m.session, m.lastSQL), pollTickCmd(m.pollSecs))

	case cellview.CloseMsg:
		m.cellView = m.cellView.Close()
		return m, nil

	case cellview.CopyMsg:
		if err := writeClipboard(msg.Text); err != nil {
			m.statusbar = m.statusbar.SetError("copy: " + err.Error())
		} else {
			m.statusbar = m.statusbar.SetError("copied to clipboard")
		}
		return m, nil

	case rowedit.SubmittedMsg:
		return m.handleRowEditSubmitted(msg)

	case rowedit.CancelledMsg:
		return m, nil

	case celledit.SubmittedMsg:
		ctx := m.cellEditCtx
		if ctx.ColName == "" {
			return m, nil
		}
		var updateSQL string
		if msg.SetNull {
			updateSQL = generateUpdateSQL(ctx, nil, m.lastSQL)
		} else {
			updateSQL = generateUpdateSQL(ctx, msg.NewValue, m.lastSQL)
		}
		if updateSQL == "" {
			m.statusbar = m.statusbar.SetError("cannot generate UPDATE: could not determine table or PK")
			return m, nil
		}
		m.statusbar = m.statusbar.SetError("")
		m.updatePreview = m.updatePreview.Open(updateSQL)
		return m, nil

	case celledit.CancelledMsg:
		return m, nil

	case results.EditCellMsg:
		return m.openCellEditWithContext(msg.Ctx)

	case updatepreview.ExecuteMsg:
		if m.session == nil {
			m.updatePreview = m.updatePreview.SetResult(0, fmt.Errorf("not connected"))
			return m, nil
		}
		sql := msg.SQL
		sess := m.session
		return m, func() tea.Msg {
			n, err := sess.Exec(context.Background(), sql)
			return UpdateExecDoneMsg{RowsAffected: n, Err: err}
		}

	case UpdateExecDoneMsg:
		m.updatePreview = m.updatePreview.SetResult(msg.RowsAffected, msg.Err)
		if msg.Err == nil && m.session != nil && m.lastSQL != "" {
			m.results = m.results.SetLoading(true)
			return m, executeCmd(m.session, m.lastSQL)
		}
		return m, nil

	case updatepreview.CopyMsg:
		if err := writeClipboard(msg.SQL); err != nil {
			m.statusbar = m.statusbar.SetError("clipboard: " + err.Error())
		} else {
			m.statusbar = m.statusbar.SetError("UPDATE copied to clipboard")
		}
		return m, nil

	case updatepreview.CloseMsg:
		m.updatePreview = m.updatePreview.Close()
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

	case "ctrl+p":
		return m.openCommandPalette()

	case "ctrl+h":
		return m.openHistoryPalette()

	case "X":
		// Export results (moved from E to make room for row edit form).
		if m.focused == PaneResults {
			return m.openExportPalette()
		}

	case "E":
		// Row edit form — multi-column UPDATE from the current row.
		if m.focused == PaneResults {
			return m.openRowEdit()
		}
		m.statusbar = m.statusbar.SetError("focus results pane first (F4)")
		return m.routeToFocused(msg)

	case "e":
		if m.focused == PaneResults {
			return m.openCellEdit()
		}
		m.statusbar = m.statusbar.SetError("focus results pane first (F4)")
		return m.routeToFocused(msg)

	case "v":
		if m.focused == PaneResults {
			return m.openCellView()
		}
		m.statusbar = m.statusbar.SetError("focus results pane first (F4)")
		return m.routeToFocused(msg)

	case "f":
		if m.focused == PaneResults {
			m.results = m.results.OpenFilter()
			return m, nil
		}
		m.statusbar = m.statusbar.SetError("focus results pane first (F4)")
		return m.routeToFocused(msg)

	case "r":
		if m.focused == PaneResults && m.lastSQL != "" && m.session != nil {
			m.results = m.results.SetLoading(true)
			m.statusbar = m.statusbar.SetError("")
			m.statusbar = m.statusbar.SetRows(0).SetDuration(0)
			return m, executeCmd(m.session, m.lastSQL)
		}
		if m.focused != PaneResults {
			m.statusbar = m.statusbar.SetError("focus results pane first (F4)")
			return m.routeToFocused(msg)
		}

	case "s":
		if m.focused != PaneResults {
			m.statusbar = m.statusbar.SetError("focus results pane first (F4)")
			return m.routeToFocused(msg)
		}

	case "/":
		if m.focused != PaneResults {
			m.statusbar = m.statusbar.SetError("focus results pane first (F4)")
			return m.routeToFocused(msg)
		}

	case "P":
		if m.focused == PaneResults {
			m.results = m.results.OpenPoll()
			return m, nil
		}

	case "x":
		if m.focused == PaneResults && m.pollSecs > 0 {
			m.pollSecs = 0
			m.results = m.results.SetPollSecs(0)
			return m, nil
		}

	// Open schema browser popup.
	case "ctrl+b", "f2":
		return m.openSchemaBrowser()

	// Focus editor.
	case "f3", "alt+1":
		m.resultsFullscreen = false
		return m.focusPane(PaneEditor)

	// Focus results.
	case "f4", "alt+2":
		return m.focusPane(PaneResults)

	// Toggle results fullscreen.
	case "ctrl+l":
		m.resultsFullscreen = !m.resultsFullscreen
		return m.applySize()

	// Toggle vim mode.
	case "alt+v", "ctrl+alt+v":
		return m, func() tea.Msg { return ToggleVimMsg{} }

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
			Key:       info.Name,
			Title:     info.Name,
			Badge:     paletteDriverBadge(info.Driver),
			Driver:    info.Driver,
			Summary:   info.Summary,
			Search:    info.Driver + " connection",
			Deletable: info.Managed,
		})
	}
	var cmd tea.Cmd
	m.palette, cmd = m.palette.OpenConnections(items)
	m.statusbar = m.statusbar.SetPane("CONNECTIONS")
	m.statusbar = m.statusbar.SetError("")
	return m, cmd
}

func (m Model) openCommandPalette() (tea.Model, tea.Cmd) {
	items := []palette.Item{{
		Key:     commandPaletteConnectionSwitcher,
		Title:   "Connection switcher",
		Badge:   "Ctrl+K",
		Summary: "Open saved/raw connection chooser",
		Search:  "connections switch connect",
	}, {
		Key:     commandPaletteAddConnection,
		Title:   "Add connection",
		Badge:   "Ctrl+N",
		Summary: "Open the add/save connection modal",
		Search:  "connections save modal",
	}, {
		Key:     commandPaletteHelp,
		Title:   "Help & settings",
		Badge:   "F1",
		Summary: "Show runtime help, settings, and keybindings",
		Search:  "docs help settings keybindings",
	}, {
		Key:     commandPaletteToggleSchema,
		Title:   "Toggle schema browser",
		Badge:   "Ctrl+B",
		Summary: "Open or close the schema overlay",
		Search:  "schema browser tree overlay",
	}, {
		Key:     commandPaletteFocusEditor,
		Title:   "Focus editor",
		Badge:   "F3",
		Summary: "Move keyboard focus to the SQL editor",
		Search:  "pane editor focus",
	}, {
		Key:     commandPaletteFocusResults,
		Title:   "Focus results",
		Badge:   "F4",
		Summary: "Move keyboard focus to the results grid",
		Search:  "pane results focus",
	}, {
		Key:     commandPaletteToggleVim,
		Title:   "Toggle vim mode",
		Badge:   "Alt+V",
		Summary: "Switch between vim and non-vim editing",
		Search:  "vim editor mode",
	}, {
		Key:     commandPaletteNewTab,
		Title:   "New query tab",
		Badge:   "Ctrl+N",
		Summary: "Create a new query tab/workspace file",
		Search:  "tab query new",
	}}
	items = append(items,
		palette.Item{Key: commandPaletteSaveSnippet, Title: "Save current block as snippet", Badge: "✂", Summary: "Save the active SQL block as a named snippet for later reuse", Search: "snippet save block named"},
		palette.Item{Key: commandPaletteBrowseSnippets, Title: "Browse snippets", Badge: "✂", Summary: "Open snippet browser — fuzzy filter, Enter pastes, d deletes", Search: "snippet browse paste"},
	)
	if m.session != nil {
		items = append(items,
			palette.Item{Key: commandPaletteExplainBlock, Title: "Explain current block", Badge: "PLAN", Summary: "Show an estimated plan for the logical SQL block under the cursor", Search: "explain plan block current estimated"},
			palette.Item{Key: commandPaletteExplainBuffer, Title: "Explain full buffer", Badge: "PLAN", Summary: "Show estimated plans for each explainable statement in the current buffer", Search: "explain plan buffer full estimated"},
			palette.Item{Key: commandPaletteExecuteBlockTx, Title: "Run current block in transaction", Badge: "TX", Summary: "Begin a transaction if needed, run the current block, and leave the transaction open", Search: "execute run block transaction current"},
			palette.Item{Key: commandPaletteExecuteBufferTx, Title: "Run full buffer in transaction", Badge: "TX", Summary: "Confirm, then begin a transaction if needed, run the full buffer, and leave the transaction open", Search: "execute run buffer transaction full"},
		)
		if m.session.InTransaction() {
			items = append(items,
				palette.Item{Key: commandPaletteCommitTx, Title: "Commit transaction", Badge: "TX", Summary: "Commit the current manual transaction", Search: "transaction commit save"},
				palette.Item{Key: commandPaletteRollbackTx, Title: "Rollback transaction", Badge: "TX", Summary: "Rollback the current manual transaction", Search: "transaction rollback undo"},
			)
		} else {
			items = append(items, palette.Item{Key: commandPaletteBeginTx, Title: "Begin transaction", Badge: "TX", Summary: "Enter manual transaction mode", Search: "transaction begin start"})
		}
	}
	var cmd tea.Cmd
	m.palette, cmd = m.palette.OpenCommands(items)
	m.statusbar = m.statusbar.SetPane("COMMANDS")
	m.statusbar = m.statusbar.SetError("")
	return m, cmd
}

func (m Model) handleCommandPaletteSelection(key string) (tea.Model, tea.Cmd) {
	switch key {
	case commandPaletteConnectionSwitcher:
		return m.openConnectionSwitcher()
	case commandPaletteAddConnection:
		return m.openAddConnectionModal()
	case commandPaletteHelp:
		return m.openHelpScreen()
	case commandPaletteToggleSchema:
		return m.openSchemaBrowser()
	case commandPaletteFocusEditor:
		return m.focusPane(PaneEditor)
	case commandPaletteFocusResults:
		return m.focusPane(PaneResults)
	case commandPaletteFocusSchema:
		return m.openSchemaBrowser()
	case commandPaletteToggleVim:
		return m, func() tea.Msg { return ToggleVimMsg{} }
	case commandPaletteNewTab:
		return m, func() tea.Msg { return editor.NewTabMsg{} }
	case commandPaletteBeginTx:
		return m, func() tea.Msg { return BeginTransactionMsg{} }
	case commandPaletteCommitTx:
		return m, func() tea.Msg { return CommitTransactionMsg{} }
	case commandPaletteRollbackTx:
		return m, func() tea.Msg { return RollbackTransactionMsg{} }
	case commandPaletteExplainBlock:
		return m, func() tea.Msg { return ExplainBlockMsg{} }
	case commandPaletteExplainBuffer:
		return m, func() tea.Msg { return ExplainBufferMsg{} }
	case commandPaletteExecuteBlockTx:
		return m, func() tea.Msg { return ExecuteBlockInTransactionMsg{} }
	case commandPaletteExecuteBufferTx:
		return m, func() tea.Msg { return ExecuteBufferInTransactionMsg{} }
	case commandPaletteSaveSnippet:
		return m.openSnippetSave()
	case commandPaletteBrowseSnippets:
		return m.openSnippetBrowser()
	default:
		return m, nil
	}
}

func (m Model) handleTransactionAction(action string, run func() error) (tea.Model, tea.Cmd) {
	if m.session == nil {
		m.statusbar = m.statusbar.SetError("not connected")
		return m, nil
	}
	if err := run(); err != nil {
		m.statusbar = m.statusbar.SetError(action + " transaction: " + err.Error())
		m.statusbar = m.statusbar.SetTx(m.session.InTransaction())
		return m, nil
	}
	m.statusbar = m.statusbar.SetError("")
	m.statusbar = m.statusbar.SetTx(m.session.InTransaction())
	return m, nil
}

func (m Model) explainSQL(sql string) (tea.Model, tea.Cmd) {
	if strings.TrimSpace(sql) == "" {
		m.statusbar = m.statusbar.SetError("nothing to explain")
		return m, nil
	}
	if m.session == nil {
		m.statusbar = m.statusbar.SetError("not connected")
		return m, nil
	}
	m.results = m.results.SetLoading(true)
	m.statusbar = m.statusbar.SetError("")
	m.statusbar = m.statusbar.SetRows(0).SetDuration(0)
	m.statusbar = m.statusbar.SetTx(m.session.InTransaction())
	return m, explainCmd(m.session, sql)
}

func (m Model) executeSQLInTransaction(mode, sql string) (tea.Model, tea.Cmd) {
	if strings.TrimSpace(sql) == "" {
		m.statusbar = m.statusbar.SetError("nothing to execute")
		return m, nil
	}
	if m.session == nil {
		m.statusbar = m.statusbar.SetError("not connected")
		return m, nil
	}
	if !m.session.InTransaction() {
		if err := m.session.BeginTx(context.Background()); err != nil {
			m.statusbar = m.statusbar.SetError("begin transaction: " + err.Error())
			m.statusbar = m.statusbar.SetTx(false)
			return m, nil
		}
	}
	m.lastSQL = sql
	m.recordHistory(mode, sql)
	m.results = m.results.SetLoading(true)
	m.statusbar = m.statusbar.SetError("")
	m.statusbar = m.statusbar.SetRows(0).SetDuration(0)
	m.statusbar = m.statusbar.SetTx(true)
	return m, executeCmd(m.session, sql)
}

func (m Model) openHistoryPalette() (tea.Model, tea.Cmd) {
	entries, err := m.ws.LoadHistory(200)
	if err != nil {
		m.statusbar = m.statusbar.SetError("history: " + err.Error())
		return m, nil
	}
	items := make([]palette.Item, 0, len(entries))
	for _, entry := range entries {
		items = append(items, palette.Item{
			Key:     entry.SQL,
			Title:   historyPreviewTitle(entry.SQL),
			Badge:   entry.Mode,
			Summary: historyPreviewSummary(entry),
			Search:  entry.SQL + " " + entry.Connection + " " + entry.Mode,
		})
	}
	var cmd tea.Cmd
	m.palette, cmd = m.palette.OpenHistory(items)
	m.statusbar = m.statusbar.SetPane("HISTORY")
	m.statusbar = m.statusbar.SetError("")
	return m, cmd
}

func (m Model) handleHistoryPaletteSelection(sqlText string) (tea.Model, tea.Cmd) {
	var focusCmd tea.Cmd
	m, focusCmd = m.applyPaneFocus(PaneEditor)
	var insertCmd tea.Cmd
	m.editor, insertCmd = m.editor.InsertText(sqlText)
	return m, tea.Batch(focusCmd, insertCmd)
}

// openSnippetSave opens the inline name-prompt for saving a snippet.
func (m Model) openSnippetSave() (tea.Model, tea.Cmd) {
	sql := strings.TrimSpace(m.editor.CurrentBlock())
	if sql == "" {
		m.statusbar = m.statusbar.SetError("nothing to save (cursor not inside a query block)")
		return m, nil
	}
	m.snippetSaveOpen = true
	m.snippetSaveInput = nil
	m.snippetSaveSQL = sql
	m.statusbar = m.statusbar.SetError("Snippet name: ")
	return m, nil
}

// handleSnippetSaveKey processes keystrokes while the snippet name prompt is open.
func (m Model) handleSnippetSaveKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.snippetSaveOpen = false
		m.snippetSaveInput = nil
		m.snippetSaveSQL = ""
		m.statusbar = m.statusbar.SetError("")
		return m, nil
	case "enter":
		name := strings.TrimSpace(string(m.snippetSaveInput))
		if name == "" || strings.ContainsAny(name, "/\\") {
			m.statusbar = m.statusbar.SetError("invalid name (empty or contains / \\)")
			return m, nil
		}
		sql := m.snippetSaveSQL
		m.snippetSaveOpen = false
		m.snippetSaveInput = nil
		m.snippetSaveSQL = ""
		if m.ws != nil {
			if err := m.ws.AddSnippet(name, sql); err != nil {
				m.statusbar = m.statusbar.SetError("save snippet: " + err.Error())
			} else {
				m.statusbar = m.statusbar.SetError("snippet saved: " + name)
			}
		}
		return m, nil
	case "backspace", "ctrl+h":
		if len(m.snippetSaveInput) > 0 {
			m.snippetSaveInput = m.snippetSaveInput[:len(m.snippetSaveInput)-1]
		}
		m.statusbar = m.statusbar.SetError("Snippet name: " + string(m.snippetSaveInput))
		return m, nil
	default:
		for _, r := range msg.String() {
			m.snippetSaveInput = append(m.snippetSaveInput, r)
		}
		m.statusbar = m.statusbar.SetError("Snippet name: " + string(m.snippetSaveInput))
		return m, nil
	}
}

// openSnippetBrowser loads snippets and opens the snippet palette.
func (m Model) openSnippetBrowser() (tea.Model, tea.Cmd) {
	var items []palette.Item
	if m.ws != nil {
		if snippets, err := m.ws.ListSnippets(); err == nil {
			for _, s := range snippets {
				items = append(items, palette.Item{
					Key:     fmt.Sprintf("%d", s.ID),
					Title:   s.Name,
					Summary: truncateSQL(s.SQL, 60),
					Search:  s.Name + " " + s.SQL,
				})
			}
		}
	}
	var cmd tea.Cmd
	m.palette, cmd = m.palette.OpenSnippets(items)
	m.statusbar = m.statusbar.SetPane("SNIPPETS")
	m.statusbar = m.statusbar.SetError("")
	return m, cmd
}

// handleSnippetPaste pastes the snippet SQL into the editor.
func (m Model) handleSnippetPaste(key string) (tea.Model, tea.Cmd) {
	if m.ws == nil {
		return m, nil
	}
	snippets, err := m.ws.ListSnippets()
	if err != nil {
		m.statusbar = m.statusbar.SetError("load snippets: " + err.Error())
		return m, nil
	}
	for _, s := range snippets {
		if fmt.Sprintf("%d", s.ID) == key {
			var focusCmd tea.Cmd
			m, focusCmd = m.applyPaneFocus(PaneEditor)
			var insertCmd tea.Cmd
			m.editor, insertCmd = m.editor.InsertText(s.SQL)
			return m, tea.Batch(focusCmd, insertCmd)
		}
	}
	return m, nil
}

// handleSnippetDelete deletes a snippet by its string ID key.
func (m Model) handleSnippetDelete(key string) (tea.Model, tea.Cmd) {
	if m.ws == nil {
		return m, nil
	}
	var id int64
	if _, err := fmt.Sscan(key, &id); err != nil {
		return m, nil
	}
	if err := m.ws.DeleteSnippet(id); err != nil {
		m.statusbar = m.statusbar.SetError("delete snippet: " + err.Error())
	} else {
		m.statusbar = m.statusbar.SetError("snippet deleted")
	}
	return m, nil
}

// openRowEdit opens the multi-column row edit form for the cursor row.
func (m Model) openRowEdit() (tea.Model, tea.Cmd) {
	ctx, ok := m.results.CurrentCellContext()
	if !ok {
		m.statusbar = m.statusbar.SetError("no row selected")
		return m, nil
	}
	if len(ctx.Columns) == 0 {
		m.statusbar = m.statusbar.SetError("no columns in result set")
		return m, nil
	}
	vimEnabled := m.editor.VimMode() != ""
	m.rowEdit = m.rowEdit.SetSize(m.width, m.height)
	var cmd tea.Cmd
	m.rowEdit, cmd = m.rowEdit.Open(ctx.Columns, ctx.Row, vimEnabled)
	return m, cmd
}

// handleRowEditSubmitted generates a multi-column UPDATE from the row edit form result.
func (m Model) handleRowEditSubmitted(msg rowedit.SubmittedMsg) (tea.Model, tea.Cmd) {
	if len(msg.Updates) == 0 {
		m.statusbar = m.statusbar.SetError("no changes to apply")
		return m, nil
	}
	updateSQL := generateMultiUpdateSQL(msg, m.lastSQL)
	if updateSQL == "" {
		m.statusbar = m.statusbar.SetError("cannot generate UPDATE: could not determine table or PK")
		return m, nil
	}
	m.statusbar = m.statusbar.SetError("")
	m.updatePreview = m.updatePreview.Open(updateSQL)
	return m, nil
}

// generateMultiUpdateSQL produces an UPDATE statement for multiple modified fields.
func generateMultiUpdateSQL(msg rowedit.SubmittedMsg, lastSQL string) string {
	table := export.ExtractTableName(lastSQL)
	if table == "" {
		return ""
	}
	// Detect PK column using same heuristic as generateUpdateSQL.
	pkCol := -1
	for i, col := range msg.AllColumns {
		if strings.EqualFold(col.Name, "id") {
			pkCol = i
			break
		}
	}
	if pkCol < 0 {
		for i, col := range msg.AllColumns {
			if strings.HasSuffix(strings.ToLower(col.Name), "id") {
				pkCol = i
				break
			}
		}
	}
	if pkCol < 0 && len(msg.AllColumns) > 0 {
		pkCol = 0
	}
	if pkCol < 0 || pkCol >= len(msg.Row) {
		return ""
	}
	pkVal := formatSQLLiteral(msg.Row[pkCol])
	pkName := msg.AllColumns[pkCol].Name

	setClauses := make([]string, 0, len(msg.Updates))
	for _, u := range msg.Updates {
		var val string
		if u.SetNull {
			val = "NULL"
		} else {
			val = formatSQLLiteral(u.NewValue)
		}
		setClauses = append(setClauses, fmt.Sprintf("    %s = %s", quoteIdent(u.ColName), val))
	}
	return fmt.Sprintf("UPDATE %s\nSET\n%s\nWHERE %s = %s",
		table,
		strings.Join(setClauses, ",\n"),
		quoteIdent(pkName),
		pkVal,
	)
}

// openCellEdit opens the cell edit popup overlay for the cursor cell.
func (m Model) openCellEdit() (tea.Model, tea.Cmd) {
	ctx, ok := m.results.CurrentCellContext()
	if !ok {
		m.statusbar = m.statusbar.SetError("no cell selected")
		return m, nil
	}
	return m.openCellEditWithContext(ctx)
}

func (m Model) openCellEditWithContext(ctx results.CellContext) (tea.Model, tea.Cmd) {
	m.cellEditCtx = ctx
	m.cellEdit = m.cellEdit.SetVimMode(m.editor.VimMode() != "")
	var cmd tea.Cmd
	m.cellEdit, cmd = m.cellEdit.Open(ctx.ColName, ctx.Value)
	return m, cmd
}

// generateUpdateSQL produces an UPDATE statement for the given cell edit.
// newVal may be a string (user-typed value) or nil (to set NULL).
func generateUpdateSQL(ctx results.CellContext, newVal any, lastSQL string) string {
	table := export.ExtractTableName(lastSQL)
	if table == "" {
		return ""
	}
	// Find the first likely PK column. Priority:
	//   1. Exact name "id"
	//   2. Name ends with "id" (e.g. lngTextMessageID, userId)
	//   3. Column 0 as last resort
	pkCol := -1
	for i, col := range ctx.Columns {
		if strings.EqualFold(col.Name, "id") {
			pkCol = i
			break
		}
	}
	if pkCol < 0 {
		for i, col := range ctx.Columns {
			if strings.HasSuffix(strings.ToLower(col.Name), "id") {
				pkCol = i
				break
			}
		}
	}
	if pkCol < 0 && len(ctx.Columns) > 0 {
		pkCol = 0
	}
	if pkCol < 0 || pkCol >= len(ctx.Row) {
		return ""
	}
	pkVal := formatSQLLiteral(ctx.Row[pkCol])
	pkName := ctx.Columns[pkCol].Name
	return fmt.Sprintf("UPDATE %s\nSET %s = %s\nWHERE %s = %s",
		table,
		quoteIdent(ctx.ColName),
		formatSQLLiteral(newVal),
		quoteIdent(pkName),
		pkVal,
	)
}

func formatSQLLiteral(v any) string {
	if v == nil {
		return "NULL"
	}
	// time.Time — format as SQL datetime string.
	if t, ok := v.(time.Time); ok {
		return "'" + formatTimeSQL(t) + "'"
	}
	// []byte comes back from some drivers (e.g. MSSQL varchar/binary columns).
	if b, ok := v.([]byte); ok {
		if len(b) == 16 {
			// MSSQL uniqueidentifier: mixed-endian UUID bytes.
			// First three components are little-endian; last two are big-endian.
			return fmt.Sprintf("'%02x%02x%02x%02x-%02x%02x-%02x%02x-%02x%02x-%02x%02x%02x%02x%02x%02x'",
				b[3], b[2], b[1], b[0],
				b[5], b[4],
				b[7], b[6],
				b[8], b[9],
				b[10], b[11], b[12], b[13], b[14], b[15])
		}
		v = string(b)
	}
	s := fmt.Sprintf("%v", v)
	// If numeric (after trimming whitespace), return unquoted trimmed value.
	trimmed := strings.TrimSpace(s)
	if trimmed != "" {
		if _, err := strconv.ParseFloat(trimmed, 64); err == nil {
			return trimmed
		}
	}
	// Escape single quotes.
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func formatTimeSQL(t time.Time) string {
	t = t.UTC()
	if t.Hour() == 0 && t.Minute() == 0 && t.Second() == 0 && t.Nanosecond() == 0 {
		return t.Format("2006-01-02")
	}
	if t.Nanosecond() == 0 {
		return t.Format("2006-01-02 15:04:05")
	}
	return t.Format("2006-01-02 15:04:05.000")
}

func quoteIdent(name string) string {
	return "[" + strings.ReplaceAll(name, "]", "]]") + "]"
}

// truncateSQL returns a short single-line preview of a SQL string.
func truncateSQL(sql string, maxLen int) string {
	sql = strings.ReplaceAll(sql, "\n", " ")
	sql = strings.ReplaceAll(sql, "\t", " ")
	runes := []rune(sql)
	if len(runes) <= maxLen {
		return sql
	}
	return string(runes[:maxLen-1]) + "…"
}

// handleMCPRequest processes an MCP tool call from the background server goroutine.
// All state access is safe here since we're inside the bubbletea Update function.
func (m Model) handleMCPRequest(msg mcp.RequestMsg) (tea.Model, tea.Cmd) {
	reply := func(result any, err string) (tea.Model, tea.Cmd) {
		msg.ReplyCh <- mcp.Reply{Result: result, Err: err}
		return m, nil
	}

	switch msg.Method {
	case "read_editor":
		return reply(m.editor.Value(), "")

	case "write_editor":
		sql, _ := msg.Params["sql"].(string)
		mode, _ := msg.Params["mode"].(string)
		if mode == "" {
			mode = "new_tab"
		}
		var insertCmd tea.Cmd
		switch mode {
		case "new_tab":
			connName := m.activeConn
			if connName == "" {
				connName = "_adhoc"
			}
			if m.ws != nil {
				if path, err := m.ws.NewQueryFile(connName); err == nil {
					m.editor = m.editor.AddTab(path, sql)
				} else {
					m.editor = m.editor.AddTab("", sql)
				}
			} else {
				m.editor = m.editor.AddTab("", sql)
			}
		case "replace":
			m.editor = m.editor.SetActiveTabContent(sql)
		case "append", "insert":
			m.editor, insertCmd = m.editor.InsertText(sql)
		}
		saveCmd := m.editor.SaveActiveTabCmd()
		msg.ReplyCh <- mcp.Reply{Result: "ok"}
		return m, tea.Batch(insertCmd, saveCmd)

	case "list_tabs":
		info := m.editor.TabsInfo()
		names := make([]string, len(info))
		for i, t := range info {
			names[i] = t.Title
		}
		b, _ := json.Marshal(names)
		return reply(string(b), "")

	case "get_results":
		rows := m.results.ActiveRows()
		rs := m.results.ActiveResult()
		if rs == nil {
			return reply("[]", "")
		}
		return reply(mcpFormatResults(*rs, rows), "")

	case "get_schema":
		search, _ := msg.Params["search"].(string)
		return reply(m.schema.SchemaJSON(search), "")

	case "execute_query":
		sql, _ := msg.Params["sql"].(string)
		if strings.TrimSpace(sql) == "" {
			return reply(nil, "sql parameter is required")
		}
		if m.session == nil {
			return reply(nil, "not connected")
		}
		m.lastSQL = sql
		// Store reply channel — will be signalled by QueryDoneMsg/QueryErrorMsg.
		m.mcpQueryReply = msg.ReplyCh
		m.results = m.results.SetLoading(true)
		return m, executeCmd(m.session, sql)

	case "switch_tab":
		name, _ := msg.Params["name"].(string)
		for i, t := range m.editor.TabsInfo() {
			if t.Title == name || t.Path == name {
				m.editor = m.editor.SetActiveTab(i)
				break
			}
		}
		msg.ReplyCh <- mcp.Reply{Result: "ok"}
		return m, nil

	default:
		return reply(nil, "unknown tool: "+msg.Method)
	}
}

// mcpResultsJSON serialises query results for MCP replies.
func mcpResultsJSON(sets []db.QueryResult) string {
	if len(sets) == 0 {
		return "[]"
	}
	rs := sets[0]
	return mcpFormatResults(rs, rs.Rows)
}

// mcpFormatResults converts a QueryResult to a JSON string.
const mcpMaxRows = 50
const mcpMaxStrLen = 120

func mcpFormatResults(rs db.QueryResult, rows [][]any) string {
	type rowObj = map[string]any
	truncated := len(rows) > mcpMaxRows
	if truncated {
		rows = rows[:mcpMaxRows]
	}
	out := make([]rowObj, 0, len(rows))
	for _, row := range rows {
		obj := make(rowObj, len(rs.Columns))
		for i, col := range rs.Columns {
			if i >= len(row) {
				obj[col.Name] = nil
				continue
			}
			v := row[i]
			// Truncate long strings to keep context usage low.
			if s, ok := v.(string); ok && len(s) > mcpMaxStrLen {
				v = s[:mcpMaxStrLen] + "…"
			}
			obj[col.Name] = v
		}
		out = append(out, obj)
	}
	result := map[string]any{"rows": out}
	if truncated {
		result["truncated"] = true
		result["note"] = fmt.Sprintf("showing first %d of %d rows", mcpMaxRows, len(rs.Rows))
	}
	b, _ := json.Marshal(result)
	return string(b)
}

func (m Model) openCellView() (tea.Model, tea.Cmd) {
	text, ok := m.results.CurrentCellRaw()
	if !ok {
		m.statusbar = m.statusbar.SetError("no cell selected")
		return m, nil
	}
	m.cellView = m.cellView.Open(text)
	return m, nil
}

func (m Model) openExportPalette() (tea.Model, tea.Cmd) {
	rs := m.results.ActiveResult()
	if rs == nil {
		m.statusbar = m.statusbar.SetError("no results to export")
		return m, nil
	}
	dest := "clipboard"
	if !m.exportToClipboard {
		dest = "file"
	}
	items := []palette.Item{
		{Key: exportCSV, Title: "CSV", Badge: ".csv", Summary: "Comma-separated values (RFC 4180) → " + dest, Search: "csv comma"},
		{Key: exportMarkdown, Title: "Markdown", Badge: ".md", Summary: "GitHub-flavored Markdown table → " + dest, Search: "markdown md table"},
		{Key: exportJSON, Title: "JSON", Badge: ".json", Summary: "JSON array of objects → " + dest, Search: "json"},
		{Key: exportSQLInsert, Title: "SQL INSERT", Badge: ".sql", Summary: "INSERT INTO statements → " + dest, Search: "sql insert"},
	}
	var cmd tea.Cmd
	m.palette, cmd = m.palette.OpenExport(items, dest)
	m.statusbar = m.statusbar.SetPane("EXPORT")
	m.statusbar = m.statusbar.SetError("")
	return m, cmd
}

func (m Model) handleScreenshot() (tea.Model, tea.Cmd) {
	if m.screenshotToClipboard {
		html := screenshot.ToHTML(m.View())
		if err := writeClipboard(html); err != nil {
			m.statusbar = m.statusbar.SetError("screenshot: " + err.Error())
		} else {
			m.statusbar = m.statusbar.SetError("Screenshot → clipboard  (next F10 → file)")
		}
	} else {
		doc := screenshot.ToDocument(m.View())
		filename := "screenshot_" + time.Now().Format("20060102_150405") + ".html"
		if err := os.WriteFile(filename, []byte(doc), 0644); err != nil {
			m.statusbar = m.statusbar.SetError("screenshot: " + err.Error())
		} else {
			m.statusbar = m.statusbar.SetError("Saved → " + filename + "  (next F10 → clipboard)")
		}
	}
	m.screenshotToClipboard = !m.screenshotToClipboard
	return m, nil
}

func (m Model) handleExportSelection(key string) (tea.Model, tea.Cmd) {
	rs := m.results.ActiveResult()
	if rs == nil {
		m.statusbar = m.statusbar.SetError("no results to export")
		return m, nil
	}
	// Use only tagged rows if any rows are tagged.
	if tagged := m.results.TaggedResult(); tagged != nil {
		rs = tagged
	}

	content, err := exportContent(key, *rs, m.lastSQL)
	if err != nil {
		m.statusbar = m.statusbar.SetError("export: " + err.Error())
		return m, nil
	}

	m.statusbar = m.statusbar.SetPane(m.paneLabel())

	if m.exportToClipboard {
		if err := writeClipboard(content); err != nil {
			m.statusbar = m.statusbar.SetError("clipboard: " + err.Error())
			return m, nil
		}
		m.statusbar = m.statusbar.SetError("Copied to clipboard")
		return m, nil
	}

	ext := exportExt(key)
	filename := fmt.Sprintf("results_%s.%s", time.Now().Format("20060102_150405"), ext)
	if err := os.WriteFile(filename, []byte(content), 0644); err != nil {
		m.statusbar = m.statusbar.SetError("export: " + err.Error())
		return m, nil
	}
	m.statusbar = m.statusbar.SetError("Exported → " + filename)
	return m, nil
}

func exportContent(key string, rs db.QueryResult, lastSQL string) (string, error) {
	switch key {
	case exportCSV:
		return export.CSV(rs), nil
	case exportMarkdown:
		return export.Markdown(rs), nil
	case exportJSON:
		return export.JSON(rs)
	case exportSQLInsert:
		return export.SQLInsert(rs, export.ExtractTableName(lastSQL)), nil
	}
	return "", nil
}

func exportExt(key string) string {
	switch key {
	case exportCSV:
		return "csv"
	case exportMarkdown:
		return "md"
	case exportJSON:
		return "json"
	case exportSQLInsert:
		return "sql"
	}
	return ""
}

// writeClipboard writes text to the system clipboard using platform-native commands.
func writeClipboard(text string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("clip")
	case "darwin":
		cmd = exec.Command("pbcopy")
	default: // linux / bsd
		if _, err := exec.LookPath("xclip"); err == nil {
			cmd = exec.Command("xclip", "-selection", "clipboard")
		} else {
			cmd = exec.Command("xsel", "--clipboard", "--input")
		}
	}
	cmd.Stdin = bytes.NewBufferString(text)
	return cmd.Run()
}

func loadFilterHistory() []string {
	dataDir, err := config.DataDir()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(dataDir, "filter_history.json"))
	if err != nil {
		return nil
	}
	var hist []string
	if err := json.Unmarshal(data, &hist); err != nil {
		return nil
	}
	return hist
}

func saveFilterHistory(hist []string) error {
	dataDir, err := config.DataDir()
	if err != nil {
		return err
	}
	data, err := json.Marshal(hist)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dataDir, "filter_history.json"), data, 0644)
}

func (m Model) recordHistory(mode, sqlText string) {
	if m.ws == nil || strings.TrimSpace(sqlText) == "" {
		return
	}
	_ = m.ws.AppendHistory(workspace.HistoryEntry{
		ExecutedAt: time.Now().UTC(),
		Connection: activeWorkspaceName(m.activeConn),
		Mode:       strings.ToUpper(mode),
		SQL:        sqlText,
	})
}

func historyPreviewTitle(sqlText string) string {
	compact := strings.Join(strings.Fields(strings.TrimSpace(sqlText)), " ")
	if compact == "" {
		return "(empty query)"
	}
	const maxLen = 72
	if len([]rune(compact)) <= maxLen {
		return compact
	}
	r := []rune(compact)
	return string(r[:maxLen-1]) + "…"
}

func historyPreviewSummary(entry workspace.HistoryEntry) string {
	ts := "unknown time"
	if !entry.ExecutedAt.IsZero() {
		ts = entry.ExecutedAt.Local().Format("2006-01-02 15:04")
	}
	if entry.Connection == "" {
		return ts
	}
	return entry.Connection + " • " + ts
}

func totalResultRows(results []db.QueryResult) int {
	total := 0
	for _, result := range results {
		total += len(result.Rows)
	}
	return total
}

func resultDurationMs(results []db.QueryResult) int64 {
	var maxDuration time.Duration
	for _, result := range results {
		if result.Duration > maxDuration {
			maxDuration = result.Duration
		}
	}
	return maxDuration.Milliseconds()
}

func activeWorkspaceName(name string) string {
	if strings.TrimSpace(name) == "" {
		return "_adhoc"
	}
	return name
}

func paletteDriverBadge(driver string) string {
	switch strings.ToLower(driver) {
	case "postgres":
		return "PG"
	case "mssql":
		return "MS"
	case "sqlite":
		return "SQ"
	default:
		return "DB"
	}
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

func (m Model) openDeleteConnectionConfirm(name string) (tea.Model, tea.Cmd) {
	m = m.closePalette()
	var cmd tea.Cmd
	m.modal, cmd = m.modal.OpenConfirm(
		confirmDeleteConnection+name,
		"Delete connection?",
		"Delete \""+name+"\" from saved connections? This cannot be undone.",
		"Delete",
	)
	m.statusbar = m.statusbar.SetPane("CONFIRM")
	m.statusbar = m.statusbar.SetError("")
	return m, cmd
}

func (m Model) openRunFullBufferConfirmModal() (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.modal, cmd = m.modal.OpenConfirm(confirmRunFullBuffer, "Run full buffer?", "This will execute the entire current SQL buffer. Are you sure you want to continue?", "Run full buffer")
	m.statusbar = m.statusbar.SetPane("CONFIRM")
	m.statusbar = m.statusbar.SetError("")
	return m, cmd
}

func (m Model) openRunFullBufferInTransactionConfirmModal() (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.modal, cmd = m.modal.OpenConfirm(confirmRunFullBufferTx, "Run full buffer in transaction?", "This will run the entire current SQL buffer inside a transaction and leave that transaction open so you can commit or roll it back afterward. Continue?", "Run in transaction")
	m.statusbar = m.statusbar.SetPane("CONFIRM")
	m.statusbar = m.statusbar.SetError("")
	return m, cmd
}

func (m Model) executeBufferSQL(sql string) (tea.Model, tea.Cmd) {
	if strings.TrimSpace(sql) == "" || m.session == nil {
		return m, nil
	}
	m.lastSQL = sql
	m.recordHistory("BUFFER", sql)
	m.results = m.results.SetLoading(true)
	m.statusbar = m.statusbar.SetError("")
	m.statusbar = m.statusbar.SetRows(0).SetDuration(0)
	m.statusbar = m.statusbar.SetTx(m.session.InTransaction())
	return m, executeCmd(m.session, sql)
}

func (m Model) closeModal() Model {
	m.modal = m.modal.Close()
	m.statusbar = m.statusbar.SetPane(m.paneLabel())
	return m
}

func (m Model) openHelpScreen() (tea.Model, tea.Cmd) {
	initialTab := HelpTabEditor
	if m.focused == PaneResults {
		initialTab = HelpTabResults
	}
	m.help = m.help.SetSize(m.width-6, minInt(m.height-2, 26)).Open(m.helpTabs(), initialTab)
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

func (m Model) openSchemaBrowser() (tea.Model, tea.Cmd) {
	word := m.editor.WordAtCursor()
	var cmd tea.Cmd
	m.schema, cmd = m.schema.Open(word)
	m.statusbar = m.statusbar.SetPane("SCHEMA")
	return m, cmd
}

func (m Model) applyPaneFocus(p FocusedPane) (Model, tea.Cmd) {
	var cmd tea.Cmd
	switch p {
	case PaneEditor:
		m.editor, cmd = m.editor.Focus()
		m.results = m.results.Blur()
		m.focused = PaneEditor
		m.statusbar = m.statusbar.SetPane("EDITOR")
		m.statusbar = m.statusbar.SetCursorPos(m.editor.CursorPos())
		m.statusbar = m.statusbar.SetError("")
		// Stop polling when the user moves to the editor.
		if m.pollSecs > 0 {
			m.pollSecs = 0
			m.results = m.results.SetPollSecs(0)
		}
	case PaneResults:
		m.editor = m.editor.Blur()
		m.results = m.results.Focus()
		m.focused = PaneResults
		m.statusbar = m.statusbar.SetPane("RESULTS")
		m.statusbar = m.statusbar.SetCursorPos("")
		m.statusbar = m.statusbar.SetColType(m.results.CurrentColumnType())
	case PaneSchema:
		// Schema is now a popup; open the browser instead of switching layout pane
		return m.applyPaneFocus(PaneEditor)
	}
	return m, cmd
}

func (m Model) focusPane(p FocusedPane) (tea.Model, tea.Cmd) {
	m, cmd := m.applyPaneFocus(p)
	nm, sizeCmd := m.applySize()
	return nm, tea.Batch(cmd, sizeCmd)
}

func (m Model) routeToFocused(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.focused {
	case PaneEditor:
		m.editor, cmd = m.editor.Update(msg)
		// Keep vim mode indicator and cursor position in sync after every key.
		m.statusbar = m.statusbar.SetVimMode(m.editor.VimMode())
		m.statusbar = m.statusbar.SetCursorPos(m.editor.CursorPos())
		m.statusbar = m.statusbar.SetColType("")
	case PaneResults:
		m.results, cmd = m.results.Update(msg)
		m.statusbar = m.statusbar.SetColType(m.results.CurrentColumnType())
	}
	return m, cmd
}

func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	layout := m.layoutMetrics()
	if layout.contentW <= 0 {
		return m, nil
	}
	if m.editor.MouseSelecting() && (msg.Action == tea.MouseActionMotion || msg.Action == tea.MouseActionRelease) {
		localX, localY := clampMouseToEditor(0, layout, msg.X, msg.Y)
		m.editor = m.editor.Mouse(msg, localX, localY)
		m.statusbar = m.statusbar.SetVimMode(m.editor.VimMode())
		return m, nil
	}
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return m, nil
	}
	if msg.X < 0 || msg.X >= layout.contentW || msg.Y < 0 {
		return m, nil
	}
	if msg.Y < layout.editorH {
		nm, cmd := m.applyPaneFocus(PaneEditor)
		nm.editor = nm.editor.Mouse(msg, msg.X, msg.Y)
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
	m.schema = m.schema.SetSize(m.width, m.height)
	m.help = m.help.SetSize(m.width-6, minInt(m.height-2, 24))
	m.cellView = m.cellView.SetSize(m.width, m.height)
	m.cellEdit = m.cellEdit.SetSize(m.width, m.height)
	m.rowEdit = m.rowEdit.SetSize(m.width, m.height)
	m.updatePreview = m.updatePreview.SetSize(m.width, m.height)
	m.palette = m.palette.SetSize(layout.contentW-6, minInt(m.height-4, 12))
	m.modal = m.modal.SetSize(layout.contentW-4, minInt(m.height-4, 22))
	m.statusbar = m.statusbar.SetWidth(m.width)

	return m, nil
}

type paneLayout struct {
	editorH  int
	resultsH int
	contentW int
}

func (m Model) layoutMetrics() paneLayout {
	const statusH = 1
	available := m.height - statusH
	if available < 2 {
		available = 2
	}

	if m.resultsFullscreen {
		return paneLayout{editorH: 0, resultsH: available, contentW: m.width}
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
	return paneLayout{editorH: editorH, resultsH: resultsH, contentW: contentW}
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
		return ConnectedMsg{DisplayName: target.DisplayName, WorkspaceKey: target.WorkspaceKey, Session: session, ConnectStr: nameOrDSN}
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
func pollTickCmd(secs int) tea.Cmd {
	return tea.Tick(time.Duration(secs)*time.Second, func(time.Time) tea.Msg {
		return PollTickMsg{}
	})
}

func executeCmd(session *db.Session, sql string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		results, err := session.Execute(ctx, sql)
		if err != nil {
			if isDroppedConnection(err, session.DriverName) {
				return ReconnectAndRetryMsg{SQL: sql}
			}
			return QueryErrorMsg{Err: err}
		}
		return QueryDoneMsg{Results: results}
	}
}

// isDroppedConnection reports whether err looks like a lost DB connection.
func isDroppedConnection(err error, driverName string) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	// Common cross-driver signals.
	for _, needle := range []string{
		"connection reset",
		"broken pipe",
		"connection refused",
		"eof",
		"i/o timeout",
		"use of closed network connection",
	} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	// Driver-specific.
	switch driverName {
	case "mssql":
		for _, needle := range []string{
			"server closed the connection",
			"connection was closed",
			"connection has been lost",
		} {
			if strings.Contains(msg, needle) {
				return true
			}
		}
	case "postgres":
		for _, needle := range []string{
			"unexpected eof",
			"server closed",
		} {
			if strings.Contains(msg, needle) {
				return true
			}
		}
	}
	return false
}

// reconnectAndExecuteCmd reconnects using connectStr and then retries sql.
func reconnectAndExecuteCmd(cfg *config.Config, connectStr, sql string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		target, err := connections.Resolve(cfg, connectStr)
		if err != nil {
			return ReconnectErrMsg{Err: err}
		}
		session, err := db.Connect(ctx, target.Driver, target.DSN)
		if err != nil {
			return ReconnectErrMsg{Err: err}
		}
		session.Name = target.DisplayName
		// Retry the original query.
		results, err := session.Execute(ctx, sql)
		if err != nil {
			return ReconnectErrMsg{Err: fmt.Errorf("reconnected but query failed: %w", err)}
		}
		return ReconnectDoneMsg{Session: session, Results: results, ConnectStr: connectStr}
	}
}

func explainCmd(session *db.Session, sql string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		results, err := session.Explain(ctx, sql)
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
		s, err := session.Introspect(ctx)
		if err != nil {
			return nil // silently ignore introspection errors
		}
		return SchemaLoadedMsg{Schema: s, ConnName: connName, DriverName: session.DriverName}
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
