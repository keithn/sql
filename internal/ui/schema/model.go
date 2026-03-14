package schema

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"
	"github.com/sqltui/sql/internal/db"
)

// TableSelectedMsg is emitted when the user presses Enter on a table.
type TableSelectedMsg struct {
	SQL string
}

// CancelledMsg is emitted when the user presses Esc.
type CancelledMsg struct{}

// CopyTableNameMsg asks the app to copy a qualified table name to the clipboard.
type CopyTableNameMsg struct {
	Name string
}

// RowCountRequestMsg is emitted when the user presses 'r' on a table to request its row count.
type RowCountRequestMsg struct {
	QualifiedName string // e.g. "dbo.Orders"
}

// RowCountResultMsg carries the result of a background row count query.
type RowCountResultMsg struct {
	QualifiedName string
	Count         int64
	Err           error
}

type tableEntry struct {
	schemaName string
	tableName  string
	isView     bool
	columns    []db.ColumnDef
}

func (e tableEntry) qualifiedName() string {
	if e.schemaName == "" {
		return e.tableName
	}
	return e.schemaName + "." + e.tableName
}

// Model is the schema browser popup.
type Model struct {
	active       bool
	width        int
	height       int
	driver       string // "mssql", "postgres", "sqlite"
	entries      []tableEntry
	filtered     []tableEntry
	cursor       int
	scroll       int
	detailScroll int    // scroll offset into the all[] slice in renderDetail
	detailFocus  bool   // true when arrow keys navigate the column list
	detailCursor int    // which column is highlighted (0-based index into e.columns)
	selectedCols []bool // per-column selection state; nil = no selection
	searchFocus  bool   // true when the search input is active (vs the table list)
	input        textinput.Model
	resultLimit  int              // rows limit for generated SELECT statements (0 = use default 500)
	rowCounts    map[string]int64 // qualified table name → cached row count (-1 = loading)
	actionOpen   bool             // true when the 'a' action menu overlay is shown
}

var (
	outerStyle       = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	titleStyle       = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#4ec9b0"))
	dividerStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#444444"))
	selectedStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#ffffff")).Background(lipgloss.Color("#007acc"))
	detailCursorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#ffffff")).Background(lipgloss.Color("#264f78"))
	tableStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("#d4d4d4"))
	viewStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("#9cdcfe"))
	colNameStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#d4d4d4"))
	colTypeStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#808080"))
	pkStyle          = lipgloss.NewStyle().Foreground(lipgloss.Color("#dcdcaa")).Bold(true)
	fkStyle          = lipgloss.NewStyle().Foreground(lipgloss.Color("#4ec9b0"))
	metaStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("#555555"))
	promptStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("#c586c0"))
	headerStyle      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ffffff")).Background(lipgloss.Color("#5a3e85")).Padding(0, 1)
	checkStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("#dcdcaa")).Bold(true)
)

func New() Model {
	input := textinput.New()
	input.Prompt = "> "
	input.Placeholder = "Filter tables…"
	input.CharLimit = 128
	input.PromptStyle = promptStyle
	return Model{input: input}
}

func (m Model) Active() bool { return m.active }

// Open opens the popup, optionally pre-filtering to initialFilter.
func (m Model) Open(initialFilter string) (Model, tea.Cmd) {
	m.active = true
	m.cursor = 0
	m.scroll = 0
	m.detailScroll = 0
	m.detailFocus = false
	m.detailCursor = 0
	m.selectedCols = nil
	m.searchFocus = true
	m.input.SetValue(initialFilter)
	m.syncFiltered()
	return m, m.input.Focus()
}

func (m Model) Close() Model {
	m.active = false
	m.cursor = 0
	m.scroll = 0
	m.detailScroll = 0
	m.detailFocus = false
	m.detailCursor = 0
	m.selectedCols = nil
	m.searchFocus = false
	m.input.Blur()
	m.input.SetValue("")
	m.filtered = nil
	return m
}

// SetSchema rebuilds the flat entry list from an introspected schema.
func (m Model) SetSchema(s *db.Schema, connName, driver string) Model {
	m.driver = driver
	m.entries = nil
	for _, database := range s.Databases {
		for _, schema := range database.Schemas {
			for _, t := range schema.Tables {
				m.entries = append(m.entries, tableEntry{
					schemaName: schema.Name,
					tableName:  t.Name,
					isView:     false,
					columns:    t.Columns,
				})
			}
			for _, v := range schema.Views {
				m.entries = append(m.entries, tableEntry{
					schemaName: schema.Name,
					tableName:  v.Name,
					isView:     true,
					columns:    v.Columns,
				})
			}
		}
	}
	m.syncFiltered()
	return m
}

func (m *Model) syncFiltered() {
	q := strings.TrimSpace(m.input.Value())
	if q == "" {
		cp := make([]tableEntry, len(m.entries))
		copy(cp, m.entries)
		m.filtered = cp
		return
	}
	// Build search strings: schemaName.tableName
	strs := make([]string, len(m.entries))
	for i, e := range m.entries {
		strs[i] = e.qualifiedName()
	}
	results := fuzzy.Find(q, strs)
	m.filtered = make([]tableEntry, 0, len(results))
	for _, r := range results {
		m.filtered = append(m.filtered, m.entries[r.Index])
	}
}

func (m Model) SetSize(w, h int) Model {
	// Popup is up to 92% of terminal width, max 140 chars, min 60
	pw := (w * 92) / 100
	if pw > 140 {
		pw = 140
	}
	if pw < 60 {
		pw = 60
	}
	ph := (h * 80) / 100
	if ph > 32 {
		ph = 32
	}
	if ph < 10 {
		ph = 10
	}
	m.width = pw
	m.height = ph
	m.input.Width = pw - 6
	return m
}

// SetRowCount stores a cached row count for a qualified table name.
func (m Model) SetRowCount(qualifiedName string, count int64) Model {
	if m.rowCounts == nil {
		m.rowCounts = make(map[string]int64)
	}
	m.rowCounts[qualifiedName] = count
	return m
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if !m.active {
		return m, nil
	}
	switch msg := msg.(type) {
	case RowCountResultMsg:
		if msg.Err == nil {
			m = m.SetRowCount(msg.QualifiedName, msg.Count)
		}
		return m, nil
	case tea.KeyMsg:
		// Action menu overlay intercepts all keys.
		if m.actionOpen {
			return m.handleActionMenu(msg)
		}
		switch msg.String() {
		case "esc", "ctrl+c":
			if m.detailFocus {
				m.detailFocus = false
				return m, nil
			}
			if !m.searchFocus {
				// Esc from list mode returns to search.
				m.searchFocus = true
				return m, m.input.Focus()
			}
			return m.Close(), func() tea.Msg { return CancelledMsg{} }

		case "/":
			// Always jump to search mode.
			if !m.searchFocus && !m.detailFocus {
				m.searchFocus = true
				m.input.Blur()
				return m, m.input.Focus()
			}
			// In search mode, let the input handle it.
			if m.searchFocus {
				var cmd tea.Cmd
				m.input, cmd = m.input.Update(msg)
				m.syncFiltered()
				return m, cmd
			}

		case "tab":
			if len(m.filtered) == 0 || m.cursor >= len(m.filtered) {
				return m, nil
			}
			m.detailFocus = !m.detailFocus
			if m.detailFocus {
				m.searchFocus = false
				m.input.Blur()
				e := m.filtered[m.cursor]
				if len(m.selectedCols) != len(e.columns) {
					m.selectedCols = make([]bool, len(e.columns))
				}
				m.ensureDetailCursorVisible()
			}

		case "enter":
			if m.searchFocus {
				// Enter from search: move to list mode.
				m.searchFocus = false
				m.input.Blur()
				return m, nil
			}
			if len(m.filtered) > 0 && m.cursor < len(m.filtered) {
				e := m.filtered[m.cursor]
				var sql string
				if m.hasSelection() {
					sql = m.buildSelectedSQL(e)
				} else {
					sql = m.selectSQL(e)
				}
				return m.Close(), func() tea.Msg { return TableSelectedMsg{SQL: sql} }
			}

		case "up", "ctrl+p":
			if m.detailFocus {
				if m.detailCursor > 0 {
					m.detailCursor--
					m.ensureDetailCursorVisible()
				}
			} else {
				if m.cursor > 0 {
					m.cursor--
					m.resetDetailState()
					if m.cursor < m.scroll {
						m.scroll = m.cursor
					}
				}
				// Move up from list top returns to search.
				if m.cursor == 0 && m.searchFocus == false {
					// stay in list; don't snap back to search on up
				}
			}

		case "k":
			if m.detailFocus {
				if m.detailCursor > 0 {
					m.detailCursor--
					m.ensureDetailCursorVisible()
				}
			} else if !m.searchFocus {
				// k navigates list when list is active.
				if m.cursor > 0 {
					m.cursor--
					m.resetDetailState()
					if m.cursor < m.scroll {
						m.scroll = m.cursor
					}
				}
			} else {
				// k in search mode: goes to input.
				var cmd tea.Cmd
				m.input, cmd = m.input.Update(msg)
				m.syncFiltered()
				return m, cmd
			}

		case "j":
			if m.detailFocus {
				if len(m.filtered) > 0 && m.cursor < len(m.filtered) {
					e := m.filtered[m.cursor]
					if m.detailCursor < len(e.columns)-1 {
						m.detailCursor++
						m.ensureDetailCursorVisible()
					}
				}
			} else if !m.searchFocus {
				// j navigates list when list is active.
				if m.cursor < len(m.filtered)-1 {
					m.cursor++
					m.resetDetailState()
					visRows := m.visibleListRows()
					if m.cursor >= m.scroll+visRows {
						m.scroll = m.cursor - visRows + 1
					}
				}
			} else {
				// j in search mode: goes to input.
				var cmd tea.Cmd
				m.input, cmd = m.input.Update(msg)
				m.syncFiltered()
				return m, cmd
			}

		case "down", "ctrl+n":
			if m.detailFocus {
				e := m.filtered[m.cursor]
				if m.detailCursor < len(e.columns)-1 {
					m.detailCursor++
					m.ensureDetailCursorVisible()
				}
			} else {
				// Down from search: switch to list mode.
				if m.searchFocus {
					m.searchFocus = false
					m.input.Blur()
					return m, nil
				}
				if m.cursor < len(m.filtered)-1 {
					m.cursor++
					m.resetDetailState()
					visRows := m.visibleListRows()
					if m.cursor >= m.scroll+visRows {
						m.scroll = m.cursor - visRows + 1
					}
				}
			}

		case "pgdown":
			if m.detailFocus {
				e := m.filtered[m.cursor]
				step := m.visibleListRows() / 2
				m.detailCursor += step
				if m.detailCursor >= len(e.columns) {
					m.detailCursor = len(e.columns) - 1
				}
				m.ensureDetailCursorVisible()
			} else {
				if m.searchFocus {
					m.searchFocus = false
					m.input.Blur()
					return m, nil
				}
				if len(m.filtered) > 0 && m.cursor < len(m.filtered) {
					cols := len(m.filtered[m.cursor].columns)
					vis := m.visibleListRows()
					maxDS := cols + 1 - vis
					if maxDS < 0 {
						maxDS = 0
					}
					m.detailScroll += vis / 2
					if m.detailScroll > maxDS {
						m.detailScroll = maxDS
					}
				}
			}

		case "pgup":
			if m.detailFocus {
				step := m.visibleListRows() / 2
				m.detailCursor -= step
				if m.detailCursor < 0 {
					m.detailCursor = 0
				}
				m.ensureDetailCursorVisible()
			} else if !m.searchFocus {
				vis := m.visibleListRows()
				m.detailScroll -= vis / 2
				if m.detailScroll < 0 {
					m.detailScroll = 0
				}
			}

		case " ":
			if m.detailFocus && len(m.filtered) > 0 && m.cursor < len(m.filtered) {
				e := m.filtered[m.cursor]
				if len(m.selectedCols) != len(e.columns) {
					m.selectedCols = make([]bool, len(e.columns))
				}
				if m.detailCursor < len(e.columns) {
					m.selectedCols[m.detailCursor] = !m.selectedCols[m.detailCursor]
				}
			}

		case "r":
			if !m.detailFocus && !m.searchFocus && len(m.filtered) > 0 && m.cursor < len(m.filtered) {
				e := m.filtered[m.cursor]
				qn := e.qualifiedName()
				if m.rowCounts == nil {
					m.rowCounts = make(map[string]int64)
				}
				m.rowCounts[qn] = -1
				return m, func() tea.Msg { return RowCountRequestMsg{QualifiedName: qn} }
			}
			// r in search mode goes to input.
			if m.searchFocus {
				var cmd tea.Cmd
				m.input, cmd = m.input.Update(msg)
				m.syncFiltered()
				return m, cmd
			}

		case "a":
			if !m.detailFocus && !m.searchFocus && len(m.filtered) > 0 && m.cursor < len(m.filtered) {
				m.actionOpen = true
				return m, nil
			}
			if m.searchFocus {
				var cmd tea.Cmd
				m.input, cmd = m.input.Update(msg)
				m.syncFiltered()
				return m, cmd
			}

		default:
			// Route typing to input only when search is active.
			if m.searchFocus {
				var cmd tea.Cmd
				m.input, cmd = m.input.Update(msg)
				prev := m.cursor
				m.syncFiltered()
				if prev >= len(m.filtered) {
					m.cursor = 0
					m.scroll = 0
				}
				m.resetDetailState()
				return m, cmd
			}
		}
	}
	return m, nil
}

// resetDetailState clears detail-pane navigation and selection when the table cursor moves.
func (m *Model) resetDetailState() {
	m.detailScroll = 0
	m.detailCursor = 0
	m.selectedCols = nil
}

// ensureDetailCursorVisible adjusts detailScroll so the cursor line is within the visible window.
// The cursor occupies all[1+detailCursor]; all[0] is the header.
func (m *Model) ensureDetailCursorVisible() {
	vis := m.visibleListRows()
	cursorLine := 1 + m.detailCursor // index into all[]
	if cursorLine < m.detailScroll {
		m.detailScroll = cursorLine
	}
	if cursorLine >= m.detailScroll+vis {
		m.detailScroll = cursorLine - vis + 1
	}
}

// hasSelection reports whether any column is selected.
func (m Model) hasSelection() bool {
	for _, sel := range m.selectedCols {
		if sel {
			return true
		}
	}
	return false
}

func (m Model) limit() int {
	if m.resultLimit > 0 {
		return m.resultLimit
	}
	return 500
}

func (m Model) selectSQL(e tableEntry) string {
	table := m.quotedTableName(e)
	lim := m.limit()
	switch m.driver {
	case "mssql":
		return fmt.Sprintf("SELECT TOP %d *\nFROM %s", lim, table)
	case "postgres":
		return fmt.Sprintf("SELECT *\nFROM %s\nLIMIT %d", table, lim)
	default: // sqlite and others
		return fmt.Sprintf("SELECT *\nFROM %s\nLIMIT %d", table, lim)
	}
}

// SetResultLimit sets the row limit for generated SELECT statements.
func (m Model) SetResultLimit(n int) Model { m.resultLimit = n; return m }

// buildSelectedSQL generates a SELECT for only the selected columns, adding
// LEFT JOINs for any selected FK columns.
func (m Model) buildSelectedSQL(e tableEntry) string {
	type fkJoin struct {
		alias    string
		refTable string
		localCol string
		refCol   string
	}

	var joins []fkJoin
	joinMap := map[string]string{}  // refTable -> alias
	usedAliases := map[string]bool{}
	mainAlias := tableAlias(e.tableName)
	usedAliases[mainAlias] = true
	var colParts []string

	for i, col := range e.columns {
		if i >= len(m.selectedCols) || !m.selectedCols[i] {
			continue
		}
		colParts = append(colParts, "    "+mainAlias+"."+m.quoteIdent(col.Name))
		if col.ForeignKey != nil {
			ref := col.ForeignKey.RefTable
			if _, ok := joinMap[ref]; !ok {
				// Derive the table base name (strip schema prefix).
				refBase := ref
				if idx := strings.LastIndex(ref, "."); idx >= 0 {
					refBase = ref[idx+1:]
				}
				alias := uniqueTableAlias(refBase, usedAliases)
				usedAliases[alias] = true
				joinMap[ref] = alias
				joins = append(joins, fkJoin{
					alias:    alias,
					refTable: ref,
					localCol: col.Name,
					refCol:   col.ForeignKey.RefColumn,
				})
			}
		}
	}

	if len(colParts) == 0 {
		return m.selectSQL(e)
	}

	// Append alias.* for each joined table.
	for _, j := range joins {
		colParts = append(colParts, "    "+j.alias+".*")
	}

	mainTable := m.quotedTableName(e)
	fromParts := []string{mainTable + " AS " + mainAlias}
	for _, j := range joins {
		refQ := m.quoteQualifiedName(j.refTable)
		fromParts = append(fromParts, fmt.Sprintf(
			"INNER JOIN %s AS %s ON %s.%s = %s.%s",
			refQ, j.alias,
			mainAlias, m.quoteIdent(j.localCol),
			j.alias, m.quoteIdent(j.refCol),
		))
	}

	colList := strings.Join(colParts, ",\n")
	from := strings.Join(fromParts, "\n")

	lim := m.limit()
	switch m.driver {
	case "mssql":
		return fmt.Sprintf("SELECT TOP %d\n%s\nFROM %s", lim, colList, from)
	default:
		return fmt.Sprintf("SELECT\n%s\nFROM %s\nLIMIT %d", colList, from, lim)
	}
}

// quoteIdent quotes an identifier for the active driver.
func (m Model) quoteIdent(name string) string {
	switch m.driver {
	case "mssql":
		return "[" + strings.ReplaceAll(name, "]", "]]") + "]"
	default:
		return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
	}
}

// quotedTableName returns the fully-quoted table reference for the main FROM clause.
func (m Model) quotedTableName(e tableEntry) string {
	switch m.driver {
	case "mssql":
		if e.schemaName != "" {
			return m.quoteIdent(e.schemaName) + "." + m.quoteIdent(e.tableName)
		}
		return m.quoteIdent(e.tableName)
	case "postgres":
		if e.schemaName != "" && e.schemaName != "public" {
			return m.quoteIdent(e.schemaName) + "." + m.quoteIdent(e.tableName)
		}
		return m.quoteIdent(e.tableName)
	default:
		return m.quoteIdent(e.tableName)
	}
}

// quoteQualifiedName quotes a possibly schema-qualified name like "dbo.tblFoo".
func (m Model) quoteQualifiedName(name string) string {
	parts := strings.SplitN(name, ".", 2)
	if len(parts) == 2 {
		return m.quoteIdent(parts[0]) + "." + m.quoteIdent(parts[1])
	}
	return m.quoteIdent(name)
}

func (m Model) View() string {
	if !m.active {
		return ""
	}
	// outerStyle has border(l+r=2) + padding(l+r=2) = 4 chars of overhead.
	// innerW is the usable content width inside the box.
	innerW := m.width - 4
	if innerW < 20 {
		innerW = 20
	}

	// Derive leftW from the longest visible entry name so the right pane gets
	// as much space as possible without clipping table names unnecessarily.
	maxNameLen := 0
	for _, e := range m.filtered {
		if n := len([]rune(e.qualifiedName())); n > maxNameLen {
			maxNameLen = n
		}
	}
	leftW := maxNameLen + 2 // +1 leading space, +1 margin
	if leftW < 18 {
		leftW = 18
	}
	// Cap leftW at 40% of innerW so the right pane always has room.
	if maxLeft := (innerW * 40) / 100; leftW > maxLeft {
		leftW = maxLeft
	}
	rightW := innerW - leftW - 1 // 1 for the divider char

	var filterRow string
	if m.searchFocus {
		filterRow = m.input.View()
	} else {
		// Dim the search bar when the list (or detail) is active.
		filterVal := m.input.Value()
		if filterVal == "" {
			filterRow = metaStyle.Render("> Filter tables…")
		} else {
			filterRow = metaStyle.Render("> " + filterVal + "  (/ to edit)")
		}
	}
	leftLines := m.renderTableList(leftW)
	rightLines := m.renderDetail(rightW, m.visibleListRows())

	listH := m.visibleListRows()
	emptyLeft := strings.Repeat(" ", leftW)
	for len(leftLines) < listH {
		leftLines = append(leftLines, emptyLeft)
	}
	for len(rightLines) < listH {
		rightLines = append(rightLines, "")
	}

	divChar := dividerStyle.Render("│")
	var rows []string
	for i := 0; i < listH; i++ {
		// leftLines[i] is already exactly leftW wide via lipgloss Width().
		// rightLines[i] contains ANSI codes — do NOT truncate with rune-counting;
		// the visual content is bounded by the fixed column widths in renderDetail.
		rows = append(rows, leftLines[i]+divChar+rightLines[i])
	}

	sep := strings.Repeat("─", leftW) + "┼" + strings.Repeat("─", rightW)

	var footer string
	if m.detailFocus {
		selCount := 0
		for _, s := range m.selectedCols {
			if s {
				selCount++
			}
		}
		if selCount > 0 {
			footer = metaStyle.Render(fmt.Sprintf(
				"↑↓ navigate  Space: toggle  Tab: back  Enter: SELECT %d col(s)  Esc: back", selCount))
		} else {
			footer = metaStyle.Render("↑↓ navigate  Space: select col / FK adds JOIN  Tab: back  Enter: SELECT *  Esc: back")
		}
	} else if m.searchFocus {
		footer = metaStyle.Render("Type to filter  ↓/Enter: go to list  Esc: close")
	} else {
		footer = metaStyle.Render("↑↓/jk navigate  Tab: select cols  a: actions  r: row count  Enter: SELECT  /: search  Esc: back")
	}

	inner := titleStyle.Render("Schema") + "\n" +
		filterRow + "\n" +
		dividerStyle.Render(sep) + "\n" +
		strings.Join(rows, "\n") + "\n" +
		dividerStyle.Render(strings.Repeat("─", innerW)) + "\n" +
		footer

	// outerStyle has Padding(0,1) which subtracts 2 from Width for the content area.
	// Pass m.width-2 so that content area = (m.width-2)-2 = m.width-4 = innerW.
	base := outerStyle.Width(m.width - 2).Render(inner)
	if m.actionOpen && len(m.filtered) > 0 && m.cursor < len(m.filtered) {
		overlay := m.renderActionMenu(innerW)
		return lipgloss.Place(m.width, lipgloss.Height(base), lipgloss.Center, lipgloss.Center, overlay,
			lipgloss.WithWhitespaceChars(" "))
	}
	return base
}

// handleActionMenu processes key input while the 'a' action overlay is open.
func (m Model) handleActionMenu(msg tea.KeyMsg) (Model, tea.Cmd) {
	e := m.filtered[m.cursor]
	qn := e.qualifiedName()
	quotedQN := m.quoteQualifiedName(qn)
	switch msg.String() {
	case "esc", "ctrl+c", "a":
		m.actionOpen = false
		return m, nil
	case "c":
		m.actionOpen = false
		name := qn
		return m.Close(), func() tea.Msg { return CopyTableNameMsg{Name: name} }
	case "q":
		m.actionOpen = false
		sql := "SELECT COUNT(*) AS [Count]\nFROM " + quotedQN
		return m.Close(), func() tea.Msg { return TableSelectedMsg{SQL: sql} }
	case "d":
		m.actionOpen = false
		sql := m.ddlSQL(e)
		return m.Close(), func() tea.Msg { return TableSelectedMsg{SQL: sql} }
	case "i":
		m.actionOpen = false
		sql := m.indexSQL(e)
		return m.Close(), func() tea.Msg { return TableSelectedMsg{SQL: sql} }
	}
	return m, nil
}

// ddlSQL returns a driver-appropriate query to retrieve the DDL for a table.
func (m Model) ddlSQL(e tableEntry) string {
	qn := e.qualifiedName()
	switch m.driver {
	case "mssql":
		return "EXEC sp_helptext '" + qn + "'"
	case "postgres":
		schema := e.schemaName
		if schema == "" {
			schema = "public"
		}
		return fmt.Sprintf(
			"SELECT pg_get_tabledef('%s', '%s', false)", schema, e.tableName)
	default: // sqlite
		return fmt.Sprintf(
			"SELECT sql FROM sqlite_master WHERE name = '%s'", e.tableName)
	}
}

// indexSQL returns a driver-appropriate query to retrieve indexes for a table.
func (m Model) indexSQL(e tableEntry) string {
	qn := e.qualifiedName()
	switch m.driver {
	case "mssql":
		return "EXEC sp_helpindex '" + qn + "'"
	case "postgres":
		schema := e.schemaName
		if schema == "" {
			schema = "public"
		}
		return fmt.Sprintf(
			"SELECT indexname, indexdef\nFROM pg_indexes\nWHERE schemaname = '%s' AND tablename = '%s'\nORDER BY indexname",
			schema, e.tableName)
	default: // sqlite
		return fmt.Sprintf("PRAGMA index_list('%s')", e.tableName)
	}
}

// renderActionMenu renders the action overlay that appears when 'a' is pressed.
func (m Model) renderActionMenu(w int) string {
	actionStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#007acc")).
		Padding(0, 2)
	keyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#dcdcaa")).Bold(true)
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#d4d4d4"))

	e := m.filtered[m.cursor]
	title := titleStyle.Render("Actions: " + e.qualifiedName())
	row := func(key, label string) string {
		return keyStyle.Render("["+key+"]") + " " + labelStyle.Render(label)
	}
	body := strings.Join([]string{
		title,
		"",
		row("c", "Copy table name to clipboard"),
		row("q", "Run SELECT COUNT(*)"),
		row("d", "Show DDL"),
		row("i", "Show indexes"),
		"",
		metaStyle.Render("Esc: close"),
	}, "\n")
	return actionStyle.Render(body)
}

func (m Model) renderTableList(w int) []string {
	vis := m.visibleListRows()
	end := m.scroll + vis
	if end > len(m.filtered) {
		end = len(m.filtered)
	}
	visible := m.filtered[m.scroll:end]

	lines := make([]string, 0, len(visible))
	for i, e := range visible {
		absIdx := m.scroll + i
		label := e.qualifiedName()
		// Build optional row-count suffix.
		countSuffix := ""
		if cnt, ok := m.rowCounts[label]; ok {
			if cnt < 0 {
				countSuffix = " …"
			} else {
				countSuffix = " " + formatRowCount(cnt)
			}
		}
		// Truncate label to fit: 1 leading space + label + countSuffix must fit in w chars.
		maxLabel := w - 1 - len([]rune(countSuffix))
		if maxLabel < 1 {
			maxLabel = 1
		}
		if len([]rune(label)) > maxLabel {
			runes := []rune(label)
			label = string(runes[:maxLabel-1]) + "…"
		}
		// Always use Width(w) so every item is exactly w terminal columns wide.
		if absIdx == m.cursor {
			lines = append(lines, selectedStyle.Width(w).Render(" "+label+countSuffix))
		} else if e.isView {
			suffix := lipgloss.NewStyle().Foreground(lipgloss.Color("#555555")).Render(countSuffix)
			lines = append(lines, viewStyle.Width(w).Render(" "+label+suffix))
		} else {
			suffix := lipgloss.NewStyle().Foreground(lipgloss.Color("#555555")).Render(countSuffix)
			lines = append(lines, tableStyle.Width(w).Render(" "+label+suffix))
		}
	}

	if len(m.filtered) == 0 {
		lines = append(lines, metaStyle.Width(w).Render(" (no matches)"))
	}
	return lines
}

// formatRowCount formats an int64 row count with comma separators (e.g. 1234567 → "1,234,567").
func formatRowCount(n int64) string {
	s := strconv.FormatInt(n, 10)
	out := make([]byte, 0, len(s)+len(s)/3)
	offset := len(s) % 3
	for i, c := range []byte(s) {
		if i > 0 && (i-offset)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	return string(out)
}

func (m Model) renderDetail(w, visH int) []string {
	if len(m.filtered) == 0 || m.cursor >= len(m.filtered) {
		return nil
	}
	e := m.filtered[m.cursor]

	// Build all lines: all[0] = header, all[1+i] = column[i].
	var all []string
	all = append(all, headerStyle.Width(w).Align(lipgloss.Center).Render(e.qualifiedName()))

	// Column layout: 2-char mark + nameW + 1 + typeW + 1 + suffix
	// mark(2) + name(22) + sp(1) + type(14) + sp(1) = 40 fixed; suffixW = w-40.
	const nameW = 22
	const typeW = 14
	suffixW := w - 40
	if suffixW < 4 {
		suffixW = 4
	}

	for i, c := range e.columns {
		isCursor := m.detailFocus && i == m.detailCursor
		isSelected := i < len(m.selectedCols) && m.selectedCols[i]

		mark := "  "
		if isSelected {
			mark = "✓ "
		}

		namePart := padTo(c.Name, nameW)
		typePart := padTo(c.Type, typeW)

		if isCursor {
			// Cursor line: plain text rendered with cursor style so color is uniform.
			suffixPlain := ""
			if c.PrimaryKey {
				suffixPlain = " PK"
			} else if c.ForeignKey != nil {
				suffixPlain = " -> " + truncate(c.ForeignKey.RefTable, suffixW-4)
			} else if c.Nullable {
				suffixPlain = " null"
			}
			all = append(all, detailCursorStyle.Width(w).Render(mark+namePart+" "+typePart+suffixPlain))
		} else {
			suffixStr := ""
			if c.PrimaryKey {
				suffixStr = pkStyle.Render("PK")
			} else if c.ForeignKey != nil {
				ref := truncate(c.ForeignKey.RefTable, suffixW-3) // 3 = len("-> ")
				suffixStr = fkStyle.Render("-> " + ref)
			} else if c.Nullable {
				suffixStr = metaStyle.Render("null")
			}
			markRendered := mark
			if isSelected {
				markRendered = checkStyle.Render(mark)
			}
			line := markRendered + colNameStyle.Render(namePart) + " " + colTypeStyle.Render(typePart) + " " + suffixStr
			all = append(all, line)
		}
	}

	// Apply scroll offset, clamped.
	start := m.detailScroll
	if start < 0 {
		start = 0
	}
	if start >= len(all) {
		start = len(all) - 1
	}
	end := start + visH
	if end > len(all) {
		end = len(all)
	}

	// Copy the slice so we can safely replace the "more" indicator line.
	lines := make([]string, end-start)
	copy(lines, all[start:end])

	// If more lines exist below, replace the last visible line with a "more"
	// indicator — unless that line is the cursor (we never hide the cursor).
	if end < len(all) {
		lastIdx := len(lines) - 1
		cursorAllIdx := 1 + m.detailCursor
		lastAllIdx := start + lastIdx
		if lastAllIdx != cursorAllIdx {
			remaining := len(all) - end
			lines[lastIdx] = metaStyle.Render(fmt.Sprintf(" … %d more columns (↓/PgDn)", remaining))
		}
	}

	return lines
}

func (m Model) visibleListRows() int {
	// total height minus: border(2) + title(1) + filter(1) + sep(1) + bottom-sep(1) + footer(1) = 7
	n := m.height - 7
	if n < 2 {
		return 2
	}
	return n
}

// Init satisfies tea.Model.
func (m Model) Init() tea.Cmd { return nil }

// Focus/Blur are kept for interface compatibility but the popup doesn't use them.
func (m Model) Focus() Model { return m }
func (m Model) Blur() Model  { return m }

// SchemaJSON returns a JSON array summarising the loaded schema for MCP consumers.
// If search is non-empty, only tables whose name contains search (case-insensitive) are included.
// The result is capped at ~40 KB to avoid exceeding MCP response size limits.
func (m Model) SchemaJSON(search string) string {
	search = strings.ToLower(search)
	const maxBytes = 40 * 1024
	var sb strings.Builder
	sb.WriteByte('[')
	first := true
	for _, e := range m.entries {
		if search != "" && !strings.Contains(strings.ToLower(e.tableName), search) {
			continue
		}
		if sb.Len() > maxBytes {
			break
		}
		if !first {
			sb.WriteByte(',')
		}
		first = false
		sb.WriteString(fmt.Sprintf(`{"schema":%q,"name":%q,"columns":[`, e.schemaName, e.tableName))
		for j, c := range e.columns {
			if j > 0 {
				sb.WriteByte(',')
			}
			sb.WriteString(fmt.Sprintf(`{"name":%q,"type":%q}`, c.Name, c.Type))
		}
		sb.WriteString("]}")
	}
	sb.WriteByte(']')
	return sb.String()
}

// padTo right-pads plain text s to exactly n runes (truncating if needed).
// Safe to pass to lipgloss Render() since it contains no ANSI codes.
func padTo(s string, n int) string {
	runes := []rune(s)
	if len(runes) >= n {
		return string(runes[:n])
	}
	return s + strings.Repeat(" ", n-len(runes))
}

// tableAlias derives a short, lowercase alias from a table name.
// It strips common Hungarian/ORM prefixes (tbl, vw, t_, v_) then takes the
// initials of CamelCase/PascalCase words, falling back to the first letter.
// Examples: "Alert" → "a", "AlarmQueue" → "aq", "tblHardware" → "h",
//            "tblAlarmQueue" → "aq", "AlertDetail" → "ad".
func tableAlias(name string) string {
	// Strip leading common prefixes (case-insensitive).
	stripped := name
	for _, pfx := range []string{"tbl", "vw", "t_", "v_"} {
		if len(name) > len(pfx) && strings.EqualFold(name[:len(pfx)], pfx) {
			stripped = name[len(pfx):]
			break
		}
	}
	if stripped == "" {
		stripped = name
	}

	// Collect initials of CamelCase words (transitions from lower→upper or digit→upper).
	var initials []rune
	runes := []rune(stripped)
	for i, r := range runes {
		if i == 0 {
			initials = append(initials, r)
			continue
		}
		prev := runes[i-1]
		// New word starts when we go from lower/digit to upper.
		if (r >= 'A' && r <= 'Z') && ((prev >= 'a' && prev <= 'z') || (prev >= '0' && prev <= '9')) {
			initials = append(initials, r)
		}
	}

	if len(initials) == 0 {
		return "t"
	}
	alias := strings.ToUpper(string(initials))
	if len(alias) > 3 {
		alias = alias[:3]
	}
	return alias
}

// uniqueTableAlias returns a tableAlias for name that doesn't collide with used.
// If the initial alias is taken, it progressively takes more characters from the
// uppercase stripped name until it finds an unused one, then falls back to suffix numbers.
func uniqueTableAlias(name string, used map[string]bool) string {
	base := tableAlias(name)
	if !used[base] {
		return base
	}
	// Strip prefix to get the raw name for extension.
	stripped := name
	for _, pfx := range []string{"tbl", "vw", "t_", "v_"} {
		if len(name) > len(pfx) && strings.EqualFold(name[:len(pfx)], pfx) {
			stripped = name[len(pfx):]
			break
		}
	}
	upper := strings.ToUpper(stripped)
	runes := []rune(upper)
	// Try progressively longer prefixes of the uppercase name.
	for l := len([]rune(base)) + 1; l <= len(runes); l++ {
		candidate := string(runes[:l])
		if !used[candidate] {
			return candidate
		}
	}
	// Last resort: append a number.
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s%d", base, i)
		if !used[candidate] {
			return candidate
		}
	}
}

// truncate clips s to at most n visible runes (plain text, no ANSI).
func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	if n <= 1 {
		return "…"
	}
	return string(runes[:n-1]) + "…"
}
