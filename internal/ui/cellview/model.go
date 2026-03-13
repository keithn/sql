package cellview

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// CloseMsg is sent when the user dismisses the cell viewer.
type CloseMsg struct{}

// CopyMsg is sent when the user copies text from the cell viewer.
type CopyMsg struct{ Text string }

var (
	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#007acc")).
			Padding(0, 1)

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#4ec9b0"))

	textStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#d4d4d4"))

	selStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#264f78")).
			Foreground(lipgloss.Color("#ffffff"))

	cursorStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#007acc")).
			Foreground(lipgloss.Color("#ffffff"))

	hintStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#888888"))

	selHintStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#4ec9b0"))
)

// vertOverhead = top border (1) + title line (1) + hint line (1) + bottom border (1)
const vertOverhead = 4

type pos struct{ row, col int }

func posLess(a, b pos) bool {
	if a.row != b.row {
		return a.row < b.row
	}
	return a.col < b.col
}

// Model is a scrollable cell value viewer with optional text selection.
type Model struct {
	active    bool
	rawText   string
	lines     []string // word-wrapped display lines
	wrapWidth int

	width  int // total overlay width  (including border+padding)
	height int // total overlay height (including border+padding)

	scrollRow int
	cursor    pos
	selActive bool
	selLine   bool // true = linewise (V), false = charwise (v)
	selStart  pos
}

func New() Model { return Model{} }

func (m Model) Active() bool { return m.active }

// SetSize sets the available screen dimensions so the overlay can be sized.
// Call this when the terminal resizes (same pattern as help.Model).
func (m Model) SetSize(w, h int) Model {
	overlayW := w - 4
	if overlayW > 100 {
		overlayW = 100
	}
	if overlayW < 24 {
		overlayW = 24
	}
	overlayH := h - 4
	if overlayH > 40 {
		overlayH = 40
	}
	if overlayH < 6 {
		overlayH = 6
	}
	m.width = overlayW
	m.height = overlayH
	m.wrapWidth = overlayW - 4 // subtract border (2) + padding (2)
	if len(m.lines) > 0 {
		// re-wrap if already open
		m.lines = wrapLines(m.rawText, m.wrapWidth)
	}
	return m
}

// Open opens the viewer with the given cell text.
func (m Model) Open(text string) Model {
	m.active = true
	m.rawText = text
	m.scrollRow = 0
	m.cursor = pos{}
	m.selActive = false
	m.lines = wrapLines(text, m.wrapWidth)
	return m
}

// Close closes the viewer.
func (m Model) Close() Model {
	m.active = false
	m.selActive = false
	m.selLine = false
	return m
}

// CurrentCellRaw returns the raw text for the current cell.
func (m Model) RawText() string { return m.rawText }

// SelectedText returns the selected text and whether a selection exists.
func (m Model) SelectedText() (string, bool) {
	if !m.selActive {
		return "", false
	}
	return m.extractSelection(), true
}

func (m Model) visibleLines() int {
	n := m.height - vertOverhead
	if n < 1 {
		return 1
	}
	return n
}

func (m *Model) scrollToCursor() {
	vis := m.visibleLines()
	if m.cursor.row < m.scrollRow {
		m.scrollRow = m.cursor.row
	}
	if m.cursor.row >= m.scrollRow+vis {
		m.scrollRow = m.cursor.row - vis + 1
	}
}

func (m *Model) clampCol() {
	if m.cursor.row >= len(m.lines) {
		return
	}
	max := runeLen(m.lines[m.cursor.row])
	if m.cursor.col > max {
		m.cursor.col = max
	}
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch km.String() {
	case "esc":
		if m.selActive {
			m.selActive = false
			m.selLine = false
			return m, nil
		}
		m.active = false
		return m, func() tea.Msg { return CloseMsg{} }

	case "y", "ctrl+c":
		var text string
		if m.selActive {
			text = m.extractSelection()
			m.selActive = false
			m.selLine = false
		} else {
			text = m.rawText
		}
		return m, func() tea.Msg { return CopyMsg{Text: text} }

	case "v":
		if m.selActive && !m.selLine {
			m.selActive = false
		} else {
			m.selActive = true
			m.selLine = false
			m.selStart = m.cursor
		}
		return m, nil

	case "V":
		if m.selActive && m.selLine {
			m.selActive = false
			m.selLine = false
		} else {
			m.selActive = true
			m.selLine = true
			m.selStart = pos{row: m.cursor.row}
		}
		return m, nil

	case "up", "k":
		if m.cursor.row > 0 {
			m.cursor.row--
			m.clampCol()
			m.scrollToCursor()
		}

	case "down", "j":
		if m.cursor.row < len(m.lines)-1 {
			m.cursor.row++
			m.clampCol()
			m.scrollToCursor()
		}

	case "left", "h":
		if m.cursor.col > 0 {
			m.cursor.col--
		} else if m.cursor.row > 0 {
			m.cursor.row--
			m.cursor.col = runeLen(m.lines[m.cursor.row])
			m.scrollToCursor()
		}

	case "right", "l":
		lineLen := runeLen(m.lines[m.cursor.row])
		if m.cursor.col < lineLen {
			m.cursor.col++
		} else if m.cursor.row < len(m.lines)-1 {
			m.cursor.row++
			m.cursor.col = 0
			m.scrollToCursor()
		}

	case "home", "0":
		m.cursor.col = 0

	case "end", "$":
		if m.cursor.row < len(m.lines) {
			m.cursor.col = runeLen(m.lines[m.cursor.row])
		}

	case "pgup":
		vis := m.visibleLines()
		m.cursor.row -= vis
		if m.cursor.row < 0 {
			m.cursor.row = 0
		}
		m.clampCol()
		m.scrollToCursor()

	case "pgdown":
		vis := m.visibleLines()
		m.cursor.row += vis
		if m.cursor.row >= len(m.lines) {
			m.cursor.row = len(m.lines) - 1
		}
		m.clampCol()
		m.scrollToCursor()

	case "g":
		m.cursor = pos{}
		m.scrollRow = 0

	case "G":
		m.cursor.row = len(m.lines) - 1
		m.cursor.col = 0
		m.scrollToCursor()
	}
	return m, nil
}

func (m Model) inSelection(r, c int) bool {
	if !m.selActive {
		return false
	}
	if m.selLine {
		// Linewise: entire rows between anchor and cursor are selected.
		startRow, endRow := m.selStart.row, m.cursor.row
		if endRow < startRow {
			startRow, endRow = endRow, startRow
		}
		return r >= startRow && r <= endRow
	}
	start, end := m.selStart, m.cursor
	if posLess(end, start) {
		start, end = end, start
	}
	p := pos{r, c}
	// inclusive start, exclusive end
	return !posLess(p, start) && posLess(p, end)
}

func (m Model) extractSelection() string {
	if m.selLine {
		startRow, endRow := m.selStart.row, m.cursor.row
		if endRow < startRow {
			startRow, endRow = endRow, startRow
		}
		var parts []string
		for r := startRow; r <= endRow && r < len(m.lines); r++ {
			parts = append(parts, m.lines[r])
		}
		return strings.Join(parts, "\n")
	}
	start, end := m.selStart, m.cursor
	if posLess(end, start) {
		start, end = end, start
	}
	var parts []string
	for r := start.row; r <= end.row && r < len(m.lines); r++ {
		runes := []rune(m.lines[r])
		cStart, cEnd := 0, len(runes)
		if r == start.row {
			cStart = start.col
		}
		if r == end.row {
			cEnd = end.col
		}
		if cStart > len(runes) {
			cStart = len(runes)
		}
		if cEnd > len(runes) {
			cEnd = len(runes)
		}
		parts = append(parts, string(runes[cStart:cEnd]))
	}
	return strings.Join(parts, "\n")
}

func (m Model) View() string {
	if !m.active {
		return ""
	}

	vis := m.visibleLines()
	total := len(m.lines)
	rowStart := m.scrollRow
	rowEnd := rowStart + vis
	if rowEnd > total {
		rowEnd = total
	}

	innerW := m.wrapWidth

	var sb strings.Builder

	// Title line
	title := "cell value"
	if m.selActive && m.selLine {
		title += selHintStyle.Render("  — LINE SELECT")
	} else if m.selActive {
		title += selHintStyle.Render("  — SELECT")
	}
	sb.WriteString(titleStyle.Render(title) + "\n")

	// Content lines
	for r := rowStart; r < rowEnd; r++ {
		sb.WriteString(m.renderLine(r, innerW))
		sb.WriteByte('\n')
	}

	// Blank fill so the box has a consistent height
	for i := rowEnd - rowStart; i < vis; i++ {
		sb.WriteString(strings.Repeat(" ", innerW) + "\n")
	}

	// Hint line
	var hints []string
	if m.selActive {
		hints = append(hints, "y/Ctrl+C copy sel", "ESC cancel")
	} else {
		hints = append(hints, "y/Ctrl+C copy all", "v char-select", "V line-select", "ESC close")
	}
	if total > vis {
		pct := 0
		if total > vis {
			pct = (rowStart * 100) / (total - vis)
		}
		hints = append(hints, fmt.Sprintf("%d/%d (%d%%)", rowStart+1, total, pct))
	}
	sb.WriteString(hintStyle.Render(strings.Join(hints, "  ·  ")))

	return panelStyle.Width(innerW).Render(sb.String())
}

func (m Model) renderLine(r, maxW int) string {
	runes := []rune(m.lines[r])
	var sb strings.Builder
	for c, ch := range runes {
		isCursor := r == m.cursor.row && c == m.cursor.col
		isSel := m.inSelection(r, c)
		s := string(ch)
		switch {
		case isCursor:
			sb.WriteString(cursorStyle.Render(s))
		case isSel:
			sb.WriteString(selStyle.Render(s))
		default:
			sb.WriteString(textStyle.Render(s))
		}
	}
	// Cursor past end of line
	if r == m.cursor.row && m.cursor.col == len(runes) {
		sb.WriteString(cursorStyle.Render(" "))
	}
	// Pad to maxW (visual width)
	visual := len(runes)
	if r == m.cursor.row && m.cursor.col == len(runes) {
		visual++
	}
	if visual < maxW {
		sb.WriteString(strings.Repeat(" ", maxW-visual))
	}
	return sb.String()
}

// wrapLines splits text on newlines, then hard-wraps any line longer than width.
func wrapLines(text string, width int) []string {
	if width <= 0 {
		width = 80
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	var out []string
	for _, line := range strings.Split(text, "\n") {
		runes := []rune(line)
		if len(runes) <= width {
			out = append(out, line)
			continue
		}
		for len(runes) > 0 {
			if len(runes) <= width {
				out = append(out, string(runes))
				break
			}
			out = append(out, string(runes[:width]))
			runes = runes[width:]
		}
	}
	return out
}

func runeLen(s string) int { return len([]rune(s)) }
