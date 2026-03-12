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
	detailScroll int // scroll offset for the right-pane column list
	input        textinput.Model
}

var (
	outerStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#4ec9b0"))
	dividerStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#444444"))
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#ffffff")).Background(lipgloss.Color("#007acc"))
	tableStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#d4d4d4"))
	viewStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#9cdcfe"))
	colNameStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#d4d4d4"))
	colTypeStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#808080"))
	pkStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("#dcdcaa")).Bold(true)
	fkStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("#4ec9b0"))
	metaStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#555555"))
	promptStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#c586c0"))
	headerStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ffffff")).Background(lipgloss.Color("#5a3e85")).Padding(0, 1)
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
	m.input.SetValue(initialFilter)
	m.syncFiltered()
	return m, m.input.Focus()
}

func (m Model) Close() Model {
	m.active = false
	m.cursor = 0
	m.scroll = 0
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
			return m.Close(), func() tea.Msg { return CancelledMsg{} }
		case "enter":
			if len(m.filtered) > 0 && m.cursor < len(m.filtered) {
				e := m.filtered[m.cursor]
				sql := m.selectSQL(e)
				return m.Close(), func() tea.Msg { return TableSelectedMsg{SQL: sql} }
			}
		case "up", "ctrl+p":
			if m.cursor > 0 {
				m.cursor--
				m.detailScroll = 0
				if m.cursor < m.scroll {
					m.scroll = m.cursor
				}
			}
		case "down", "ctrl+n":
			if m.cursor < len(m.filtered)-1 {
				m.cursor++
				m.detailScroll = 0
				visRows := m.visibleListRows()
				if m.cursor >= m.scroll+visRows {
					m.scroll = m.cursor - visRows + 1
				}
			}
		case "pgdown":
			// Scroll the column detail pane down
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
		case "pgup":
			// Scroll the column detail pane up
			vis := m.visibleListRows()
			m.detailScroll -= vis / 2
			if m.detailScroll < 0 {
				m.detailScroll = 0
			}
		default:
			// Route typing to input
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			prev := m.cursor
			m.syncFiltered()
			if prev >= len(m.filtered) {
				m.cursor = 0
				m.scroll = 0
			}
			m.detailScroll = 0
			return m, cmd
		}
	}
	return m, nil
}

func (m Model) selectSQL(e tableEntry) string {
	var table string
	switch m.driver {
	case "mssql":
		if e.schemaName != "" {
			table = fmt.Sprintf("[%s].[%s]", e.schemaName, e.tableName)
		} else {
			table = fmt.Sprintf("[%s]", e.tableName)
		}
		return fmt.Sprintf("SELECT TOP 500 *\nFROM %s", table)
	case "postgres":
		if e.schemaName != "" && e.schemaName != "public" {
			table = fmt.Sprintf("%q.%q", e.schemaName, e.tableName)
		} else {
			table = fmt.Sprintf("%q", e.tableName)
		}
		return fmt.Sprintf("SELECT *\nFROM %s\nLIMIT 500", table)
	default: // sqlite and others
		table = fmt.Sprintf("%q", e.tableName)
		return fmt.Sprintf("SELECT *\nFROM %s\nLIMIT 500", table)
	}
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
		// the visual content is bounded by the fixed nameW/typeW column widths.
		rows = append(rows, leftLines[i]+divChar+rightLines[i])
	}

	sep := strings.Repeat("─", leftW) + "┼" + strings.Repeat("─", rightW)
	footer := metaStyle.Render("↑↓ navigate  PgDn/PgUp scroll columns  Enter: SELECT *  Esc: close")

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

	// Build all lines (header + one per column).
	var all []string

	header := e.qualifiedName()
	if e.isView {
		header += " (view)"
	}
	all = append(all, headerStyle.Width(w).Align(lipgloss.Center).Render(header))

	nameW := 24
	typeW := 14
	// fixed visual chars per line: 1 (prefix) + nameW + 1 + typeW + 1 = 41
	suffixW := w - 41
	if suffixW < 4 {
		suffixW = 4
	}
	for _, c := range e.columns {
		namePart := padTo(c.Name, nameW)
		typePart := padTo(c.Type, typeW)
		suffix := ""
		if c.PrimaryKey {
			suffix = pkStyle.Render("PK")
		} else if c.ForeignKey != nil {
			ref := truncate(c.ForeignKey.RefTable, suffixW-3) // 3 = len("-> ")
			suffix = fkStyle.Render("-> " + ref)
		} else if c.Nullable {
			suffix = metaStyle.Render("null")
		}
		line := " " + colNameStyle.Render(namePart) + " " + colTypeStyle.Render(typePart) + " " + suffix
		all = append(all, line)
	}

	// Apply scroll offset, clamped.
	start := m.detailScroll
	if start >= len(all) {
		start = len(all) - 1
	}
	if start < 0 {
		start = 0
	}
	end := start + visH
	if end > len(all) {
		end = len(all)
	}

	lines := all[start:end]

	// If there are more lines below, replace the last visible line with a "more" indicator.
	if end < len(all) {
		remaining := len(all) - end
		lines[len(lines)-1] = metaStyle.Render(fmt.Sprintf(" … %d more columns (PgDn)", remaining))
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
