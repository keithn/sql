package schema

import (
	"fmt"
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
	detailScroll int   // scroll offset into the all[] slice in renderDetail
	detailFocus  bool  // true when arrow keys navigate the column list
	detailCursor int   // which column is highlighted (0-based index into e.columns)
	selectedCols []bool // per-column selection state; nil = no selection
	input        textinput.Model
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

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if !m.active {
		return m, nil
	}
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc", "ctrl+c":
			if m.detailFocus {
				m.detailFocus = false
				return m, nil
			}
			return m.Close(), func() tea.Msg { return CancelledMsg{} }

		case "tab":
			if len(m.filtered) == 0 || m.cursor >= len(m.filtered) {
				return m, nil
			}
			m.detailFocus = !m.detailFocus
			if m.detailFocus {
				e := m.filtered[m.cursor]
				if len(m.selectedCols) != len(e.columns) {
					m.selectedCols = make([]bool, len(e.columns))
				}
				// Ensure detailCursor is visible.
				m.ensureDetailCursorVisible()
			}

		case "enter":
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
			}

		case "k":
			if m.detailFocus && m.detailCursor > 0 {
				m.detailCursor--
				m.ensureDetailCursorVisible()
			}

		case "j":
			if m.detailFocus && len(m.filtered) > 0 && m.cursor < len(m.filtered) {
				e := m.filtered[m.cursor]
				if m.detailCursor < len(e.columns)-1 {
					m.detailCursor++
					m.ensureDetailCursorVisible()
				}
			}

		case "down", "ctrl+n":
			if m.detailFocus {
				e := m.filtered[m.cursor]
				if m.detailCursor < len(e.columns)-1 {
					m.detailCursor++
					m.ensureDetailCursorVisible()
				}
			} else {
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
				// Scroll the column detail pane down while in list focus.
				if len(m.filtered) > 0 && m.cursor < len(m.filtered) {
					cols := len(m.filtered[m.cursor].columns)
					vis := m.visibleListRows()
					maxDS := cols + 1 - vis // +1 for the header line
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
			} else {
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

		default:
			// Route typing to input only when in list focus.
			if !m.detailFocus {
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

func (m Model) selectSQL(e tableEntry) string {
	table := m.quotedTableName(e)
	switch m.driver {
	case "mssql":
		return fmt.Sprintf("SELECT TOP 500 *\nFROM %s", table)
	case "postgres":
		return fmt.Sprintf("SELECT *\nFROM %s\nLIMIT 500", table)
	default: // sqlite and others
		return fmt.Sprintf("SELECT *\nFROM %s\nLIMIT 500", table)
	}
}

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

	switch m.driver {
	case "mssql":
		return fmt.Sprintf("SELECT TOP 500\n%s\nFROM %s", colList, from)
	default:
		return fmt.Sprintf("SELECT\n%s\nFROM %s\nLIMIT 500", colList, from)
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

	filterRow := m.input.View()
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
	} else {
		footer = metaStyle.Render("↑↓ navigate  Tab: select columns  PgDn/PgUp: scroll cols  Enter: SELECT *  Esc: close")
	}

	inner := titleStyle.Render("Schema") + "\n" +
		filterRow + "\n" +
		dividerStyle.Render(sep) + "\n" +
		strings.Join(rows, "\n") + "\n" +
		dividerStyle.Render(strings.Repeat("─", innerW)) + "\n" +
		footer

	// outerStyle has Padding(0,1) which subtracts 2 from Width for the content area.
	// Pass m.width-2 so that content area = (m.width-2)-2 = m.width-4 = innerW.
	return outerStyle.Width(m.width - 2).Render(inner)
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
		// Truncate to fit: 1 leading space + label must fit in w chars.
		if len([]rune(label)) > w-1 {
			runes := []rune(label)
			label = string(runes[:w-2]) + "…"
		}
		// Always use Width(w) so every item is exactly w terminal columns wide.
		if absIdx == m.cursor {
			lines = append(lines, selectedStyle.Width(w).Render(" "+label))
		} else if e.isView {
			lines = append(lines, viewStyle.Width(w).Render(" "+label))
		} else {
			lines = append(lines, tableStyle.Width(w).Render(" "+label))
		}
	}

	if len(m.filtered) == 0 {
		lines = append(lines, metaStyle.Width(w).Render(" (no matches)"))
	}
	return lines
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
