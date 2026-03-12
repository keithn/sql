package results

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sqltui/sql/internal/db"
)

var (
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#ffffff")).
			Background(lipgloss.Color("#2d2d2d"))

	cellStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#d4d4d4"))

	nullStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#666666")).
			Italic(true)

	placeholderStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#555555")).
				Italic(true)

	metaStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#888888"))

	borderFocused = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), true, false, false, false).
			BorderForeground(lipgloss.Color("#007acc"))

	borderBlurred = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), true, false, false, false).
			BorderForeground(lipgloss.Color("#333333"))
)

// Model is the results pane. It uses virtual scrolling: only visible rows and
// columns are rendered each frame, so large/wide result sets don't cause
// per-frame performance problems.
type Model struct {
	width     int
	height    int
	results   []db.QueryResult
	active    int // active result set index
	loading   bool
	errMsg    string
	scrollRow int   // index of first visible data row
	scrollCol int   // index of first visible column
	colWidths []int // cached column widths for the active result set
	focused   bool
}

func New() Model {
	return Model{}
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		rs := m.activeResult()
		total := 0
		if rs != nil {
			total = len(rs.Rows)
		}
		vis := m.visibleRows()
		maxScroll := total - vis
		if maxScroll < 0 {
			maxScroll = 0
		}

		maxScrollCol := 0
		if rs != nil {
			maxScrollCol = len(rs.Columns) - 1
		}

		switch msg.String() {
		case "up", "k":
			if m.scrollRow > 0 {
				m.scrollRow--
			}
			return m, nil
		case "down", "j":
			if m.scrollRow < maxScroll {
				m.scrollRow++
			}
			return m, nil
		case "pgdown":
			m.scrollRow += vis
			if m.scrollRow > maxScroll {
				m.scrollRow = maxScroll
			}
			return m, nil
		case "pgup":
			m.scrollRow -= vis
			if m.scrollRow < 0 {
				m.scrollRow = 0
			}
			return m, nil
		case "home":
			m.scrollRow = 0
			return m, nil
		case "end":
			m.scrollRow = maxScroll
			return m, nil
		case "left", "h":
			if m.scrollCol > 0 {
				m.scrollCol--
			}
			return m, nil
		case "right", "l":
			if m.scrollCol < maxScrollCol {
				m.scrollCol++
			}
			return m, nil
		case "alt+pgdown":
			if m.active < len(m.results)-1 {
				m.active++
				m.scrollRow = 0
				m.scrollCol = 0
				m.colWidths = computeColWidths(m.results[m.active])
			}
			return m, nil
		case "alt+pgup":
			if m.active > 0 {
				m.active--
				m.scrollRow = 0
				m.scrollCol = 0
				m.colWidths = computeColWidths(m.results[m.active])
			}
			return m, nil
		}
	}
	return m, nil
}

func (m Model) View() string {
	border := borderBlurred.Width(m.width)
	if m.focused {
		border = borderFocused.Width(m.width)
	}

	title := m.renderTitle()

	if m.loading {
		return border.Render(title + placeholderStyle.Render("  Running query…"))
	}

	if m.errMsg != "" {
		errSty := lipgloss.NewStyle().Foreground(lipgloss.Color("#f44747"))
		return border.Render(title + errSty.Render("  Error: "+m.errMsg))
	}

	if len(m.results) == 0 {
		return border.Render(title + placeholderStyle.Render("  No results — run a query with Ctrl+E or F5"))
	}

	rs := m.results[m.active]

	header := ""
	if len(m.results) > 1 {
		header = m.renderResultTabs() + "\n"
	}

	meta := metaStyle.Render(fmt.Sprintf(
		"  %d rows  ·  %s  ·  result set %d of %d",
		len(rs.Rows), rs.Duration.Round(time.Millisecond), m.active+1, len(m.results),
	))

	grid := m.renderGrid(rs)

	return border.Render(title + header + meta + "\n" + grid)
}

func (m Model) renderTitle() string {
	sty := lipgloss.NewStyle().
		Padding(0, 1).
		Background(lipgloss.Color("#333333")).
		Foreground(lipgloss.Color("#666666"))
	if m.focused {
		sty = sty.
			Background(lipgloss.Color("#007acc")).
			Foreground(lipgloss.Color("#ffffff")).
			Bold(true)
	}
	return sty.Render("RESULTS") + "\n"
}

func (m Model) renderResultTabs() string {
	var tabs []string
	for i := range m.results {
		label := fmt.Sprintf(" Result %d ", i+1)
		if i == m.active {
			tabs = append(tabs, lipgloss.NewStyle().
				Bold(true).Foreground(lipgloss.Color("#007acc")).Render(label))
		} else {
			tabs = append(tabs, metaStyle.Render(label))
		}
	}
	return strings.Join(tabs, "│")
}

// renderGrid renders only the visible window of rows and columns.
// Both axes are O(visible) per frame regardless of total result set size.
func (m Model) renderGrid(rs db.QueryResult) string {
	if len(rs.Columns) == 0 {
		return metaStyle.Render("  (no columns)")
	}

	widths := m.colWidths
	if len(widths) != len(rs.Columns) {
		widths = computeColWidths(rs)
	}

	// Visible row window.
	vis := m.visibleRows()
	total := len(rs.Rows)
	rowStart := m.scrollRow
	if rowStart > total {
		rowStart = total
	}
	rowEnd := rowStart + vis
	if rowEnd > total {
		rowEnd = total
	}

	// Visible column window — only render columns that fit in m.width.
	colStart, colEnd := m.visibleColRange(widths)
	moreLeft := colStart > 0
	moreRight := colEnd < len(rs.Columns)

	var sb strings.Builder

	// Header row.
	for i := colStart; i < colEnd; i++ {
		cell := padRight(rs.Columns[i].Name, widths[i])
		sb.WriteString(headerStyle.Render(" " + cell + " "))
		if i < colEnd-1 {
			sb.WriteString(headerStyle.Render("│"))
		}
	}
	sb.WriteByte('\n')

	// Separator.
	for i := colStart; i < colEnd; i++ {
		sb.WriteString(strings.Repeat("─", widths[i]+2))
		if i < colEnd-1 {
			sb.WriteString("┼")
		}
	}
	sb.WriteByte('\n')

	// Visible rows, visible columns only.
	for _, row := range rs.Rows[rowStart:rowEnd] {
		for i := colStart; i < colEnd; i++ {
			if i >= len(row) {
				break
			}
			val := row[i]
			raw := formatCell(val)
			if runeLen(raw) > widths[i] {
				runes := []rune(raw)
				raw = string(runes[:widths[i]-1]) + "…"
			}
			padded := padRight(raw, widths[i])
			if val == nil {
				sb.WriteString(nullStyle.Render(" " + padded + " "))
			} else {
				sb.WriteString(cellStyle.Render(" " + padded + " "))
			}
			if i < colEnd-1 {
				sb.WriteString("│")
			}
		}
		sb.WriteByte('\n')
	}

	// Scroll position hint.
	var hintParts []string
	if total > vis {
		pct := 0
		if total > vis {
			pct = (rowStart * 100) / (total - vis)
		}
		hintParts = append(hintParts, fmt.Sprintf("rows %d–%d of %d (%d%%)", rowStart+1, rowEnd, total, pct))
		hintParts = append(hintParts, "↑↓/jk PgUp/PgDn Home/End")
	}
	if moreLeft || moreRight {
		colHint := fmt.Sprintf("cols %d–%d of %d", colStart+1, colEnd, len(rs.Columns))
		if moreLeft {
			colHint = "← " + colHint
		}
		if moreRight {
			colHint = colHint + " →"
		}
		hintParts = append(hintParts, colHint)
		hintParts = append(hintParts, "←→/hl")
	}
	if len(hintParts) > 0 {
		sb.WriteString(metaStyle.Render("  " + strings.Join(hintParts, "  ·  ")))
	}

	return sb.String()
}

// visibleColRange returns the [start, end) column indices that fit within m.width.
// Each non-last column occupies widths[i]+3 chars (" content " + "│"),
// the last visible column occupies widths[i]+2 chars.
func (m Model) visibleColRange(widths []int) (start, end int) {
	if len(widths) == 0 {
		return 0, 0
	}
	start = m.scrollCol
	if start >= len(widths) {
		start = len(widths) - 1
	}
	available := m.width
	used := 0
	for i := start; i < len(widths); i++ {
		colW := widths[i] + 3 // " content " + "│"
		if i == len(widths)-1 {
			colW = widths[i] + 2
		}
		if used+colW > available && i > start {
			return start, i
		}
		used += colW
	}
	return start, len(widths)
}

func (m Model) SetSize(w, h int) Model {
	m.width = w
	m.height = h
	return m
}

func (m Model) SetResults(sets []db.QueryResult) Model {
	m.results = sets
	m.active = 0
	m.loading = false
	m.errMsg = ""
	m.scrollRow = 0
	m.scrollCol = 0
	if len(sets) > 0 {
		m.colWidths = computeColWidths(sets[0])
	}
	return m
}

func (m Model) SetError(err string) Model {
	m.errMsg = err
	m.loading = false
	return m
}

func (m Model) SetLoading(v bool) Model {
	m.loading = v
	return m
}

func (m Model) Focus() Model { m.focused = true; return m }
func (m Model) Blur() Model  { m.focused = false; return m }

// visibleRows returns how many data rows fit in the allocated height.
// Fixed overhead: 1 border-top + 1 title + 1 meta + 1 newline + 1 col-header + 1 separator + 1 scroll-hint = 7.
func (m Model) visibleRows() int {
	n := m.height - 7
	if n < 1 {
		return 1
	}
	return n
}

func (m Model) activeResult() *db.QueryResult {
	if len(m.results) == 0 || m.active >= len(m.results) {
		return nil
	}
	return &m.results[m.active]
}

// ActiveResult returns a copy of the currently displayed result set, or nil if none.
func (m Model) ActiveResult() *db.QueryResult {
	return m.activeResult()
}

// computeColWidths scans all rows once to find max display widths, capped at 40.
func computeColWidths(rs db.QueryResult) []int {
	widths := make([]int, len(rs.Columns))
	for i, c := range rs.Columns {
		widths[i] = runeLen(c.Name)
	}
	for _, row := range rs.Rows {
		for i, val := range row {
			if i >= len(widths) {
				break
			}
			if l := runeLen(formatCell(val)); l > widths[i] {
				widths[i] = l
			}
		}
	}
	for i := range widths {
		if widths[i] > 40 {
			widths[i] = 40
		}
	}
	return widths
}

func formatCell(v any) string {
	if v == nil {
		return "∅"
	}
	var s string
	if b, ok := v.([]byte); ok {
		for _, c := range b {
			if c < 0x20 && c != '\n' && c != '\r' && c != '\t' {
				return fmt.Sprintf("<binary %d bytes>", len(b))
			}
			if c > 0x7E {
				return fmt.Sprintf("<binary %d bytes>", len(b))
			}
		}
		s = string(b)
	} else {
		s = fmt.Sprintf("%v", v)
	}
	// Collapse embedded newlines so multi-line values don't break the grid layout.
	s = strings.ReplaceAll(s, "\r\n", "↵")
	s = strings.ReplaceAll(s, "\r", "↵")
	s = strings.ReplaceAll(s, "\n", "↵")
	return s
}

func runeLen(s string) int {
	return len([]rune(s))
}

func padRight(s string, n int) string {
	r := runeLen(s)
	if r >= n {
		return s
	}
	return s + strings.Repeat(" ", n-r)
}
