package results

import (
	"fmt"
	"regexp"
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

	cursorStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#264f78")).
			Foreground(lipgloss.Color("#ffffff"))

	cursorNullStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#264f78")).
			Foreground(lipgloss.Color("#aaaaaa")).
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

	filterPromptStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#007acc")).
				Bold(true)

	filterColStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#4ec9b0"))

	filterErrStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#f44747"))

	filterActiveStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#4ec9b0"))

	filterHistSelStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#264f78")).
				Foreground(lipgloss.Color("#ffffff"))

	filterHistStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#888888"))

	noMatchStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#555555")).
			Italic(true)
)

// CellYankMsg is sent when the user yanks (copies) the current cell value.
type CellYankMsg struct {
	Text string
}

// FilterConfirmedMsg is sent when the user confirms a filter so the app can persist history.
type FilterConfirmedMsg struct {
	Pattern string
}

// Model is the results pane.
type Model struct {
	width     int
	height    int
	results   []db.QueryResult
	active    int
	loading   bool
	errMsg    string
	scrollRow int
	scrollCol int
	colWidths []int
	focused   bool
	cursorRow int
	cursorCol int

	// Column display filter — narrows what is shown in the filtered column's cells.
	filterOpen    bool
	filterInput   []rune
	filterCursor  int
	filterCol     int    // column index being filtered; -1 = none
	filterColName string // name of the filtered column (for persistence check across refreshes)
	filterPattern string // confirmed active regex pattern
	filterRE      *regexp.Regexp
	filterErr     string // live regex parse error while typing
	filterHistory []string
	filterHistSel int // -1 = not showing history
}

func New() Model {
	return Model{filterCol: -1, filterHistSel: -1}
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.filterOpen {
			return m.updateFilter(msg)
		}

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
			if m.cursorRow > 0 {
				m.cursorRow--
				if m.cursorRow < m.scrollRow {
					m.scrollRow = m.cursorRow
				}
			}
			return m, nil
		case "down", "j":
			if total > 0 && m.cursorRow < total-1 {
				m.cursorRow++
				if m.cursorRow >= m.scrollRow+vis {
					m.scrollRow = m.cursorRow - vis + 1
				}
			}
			return m, nil
		case "pgdown":
			if total > 0 {
				m.cursorRow += vis
				if m.cursorRow >= total {
					m.cursorRow = total - 1
				}
				if m.cursorRow >= m.scrollRow+vis {
					m.scrollRow = m.cursorRow - vis + 1
				}
				if m.scrollRow > maxScroll {
					m.scrollRow = maxScroll
				}
			}
			return m, nil
		case "pgup":
			m.cursorRow -= vis
			if m.cursorRow < 0 {
				m.cursorRow = 0
			}
			if m.cursorRow < m.scrollRow {
				m.scrollRow = m.cursorRow
			}
			return m, nil
		case "home":
			m.cursorRow = 0
			m.scrollRow = 0
			return m, nil
		case "end":
			if total > 0 {
				m.cursorRow = total - 1
			}
			m.scrollRow = maxScroll
			return m, nil
		case "left", "h":
			if m.cursorCol > 0 {
				m.cursorCol--
				if m.cursorCol < m.scrollCol {
					m.scrollCol = m.cursorCol
				}
			}
			return m, nil
		case "right", "l":
			if m.cursorCol < maxScrollCol {
				m.cursorCol++
				if len(m.colWidths) > 0 {
					_, colEnd := m.visibleColRange(m.colWidths)
					if m.cursorCol >= colEnd {
						m.scrollCol++
					}
				}
			}
			return m, nil
		case "alt+pgdown":
			if m.active < len(m.results)-1 {
				m.active++
				m.scrollRow, m.scrollCol, m.cursorRow, m.cursorCol = 0, 0, 0, 0
				m.colWidths = computeColWidths(m.results[m.active])
			}
			return m, nil
		case "alt+pgup":
			if m.active > 0 {
				m.active--
				m.scrollRow, m.scrollCol, m.cursorRow, m.cursorCol = 0, 0, 0, 0
				m.colWidths = computeColWidths(m.results[m.active])
			}
			return m, nil
		case "y":
			if rs != nil && m.cursorRow < len(rs.Rows) && m.cursorCol < len(rs.Rows[m.cursorRow]) {
				text := formatCellRaw(rs.Rows[m.cursorRow][m.cursorCol])
				return m, func() tea.Msg { return CellYankMsg{Text: text} }
			}
			return m, nil
		}
	}
	return m, nil
}

// updateFilter handles all key input when the filter bar is open.
func (m Model) updateFilter(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		pattern := string(m.filterInput)
		m.filterOpen = false
		m.filterHistSel = -1
		m.filterErr = ""
		if pattern == "" {
			m.filterPattern = ""
			m.filterRE = nil
			if rs := m.activeResult(); rs != nil {
				m.colWidths = computeColWidths(*rs)
			}
		} else {
			re, err := regexp.Compile(pattern)
			if err != nil {
				m.filterErr = err.Error()
			} else {
				m.filterPattern = pattern
				m.filterRE = re
				if rs := m.activeResult(); rs != nil {
					m.colWidths = computeColWidthsFiltered(*rs, m.filterCol, re)
				}
				// prepend to history, deduplicate
				newHist := []string{pattern}
				for _, h := range m.filterHistory {
					if h != pattern {
						newHist = append(newHist, h)
					}
				}
				m.filterHistory = newHist
				return m, func() tea.Msg { return FilterConfirmedMsg{Pattern: pattern} }
			}
		}
		return m, nil

	case "esc":
		m.filterOpen = false
		m.filterHistSel = -1
		m.filterErr = ""
		return m, nil

	case "up":
		if len(m.filterHistory) == 0 {
			return m, nil
		}
		next := m.filterHistSel + 1
		if next >= len(m.filterHistory) {
			next = len(m.filterHistory) - 1
		}
		m.filterHistSel = next
		m.filterInput = []rune(m.filterHistory[next])
		m.filterCursor = len(m.filterInput)
		m.liveFilter()
		return m, nil

	case "down":
		if m.filterHistSel < 0 {
			return m, nil
		}
		next := m.filterHistSel - 1
		if next < 0 {
			m.filterHistSel = -1
		} else {
			m.filterHistSel = next
			m.filterInput = []rune(m.filterHistory[next])
			m.filterCursor = len(m.filterInput)
		}
		m.liveFilter()
		return m, nil

	case "left":
		if m.filterCursor > 0 {
			m.filterCursor--
		}
		return m, nil
	case "right":
		if m.filterCursor < len(m.filterInput) {
			m.filterCursor++
		}
		return m, nil
	case "home", "ctrl+a":
		m.filterCursor = 0
		return m, nil
	case "end", "ctrl+e":
		m.filterCursor = len(m.filterInput)
		return m, nil
	case "backspace":
		if m.filterCursor > 0 {
			m.filterInput = append(m.filterInput[:m.filterCursor-1], m.filterInput[m.filterCursor:]...)
			m.filterCursor--
			m.filterHistSel = -1
			m.liveFilter()
		}
		return m, nil
	case "delete":
		if m.filterCursor < len(m.filterInput) {
			m.filterInput = append(m.filterInput[:m.filterCursor], m.filterInput[m.filterCursor+1:]...)
			m.filterHistSel = -1
			m.liveFilter()
		}
		return m, nil
	case "ctrl+u":
		m.filterInput = m.filterInput[:0]
		m.filterCursor = 0
		m.filterHistSel = -1
		m.liveFilter()
		return m, nil

	default:
		if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace {
			ins := []rune(msg.String())
			newInput := make([]rune, 0, len(m.filterInput)+len(ins))
			newInput = append(newInput, m.filterInput[:m.filterCursor]...)
			newInput = append(newInput, ins...)
			newInput = append(newInput, m.filterInput[m.filterCursor:]...)
			m.filterInput = newInput
			m.filterCursor += len(ins)
			m.filterHistSel = -1
			m.liveFilter()
		}
		return m, nil
	}
}

// liveFilter compiles the current filter input and stores it for live preview.
// Unlike confirming (Enter), this does not update filterPattern / history.
func (m *Model) liveFilter() {
	input := string(m.filterInput)
	if input == "" {
		m.filterErr = ""
		// temporarily clear live RE so cells revert to normal while bar is empty
		m.filterRE = nil
		return
	}
	re, err := regexp.Compile(input)
	if err != nil {
		m.filterErr = err.Error()
		m.filterRE = nil
		return
	}
	m.filterErr = ""
	m.filterRE = re
}

// applyFilterRE applies re to src and returns all matches joined by "  |  ".
// When the regex has a capture group, the first non-empty group is used per match.
// Returns ("", false) if nothing matched.
func applyFilterRE(src string, re *regexp.Regexp) (string, bool) {
	allSubs := re.FindAllStringSubmatch(src, -1)
	if len(allSubs) == 0 {
		return "", false
	}
	parts := make([]string, 0, len(allSubs))
	for _, subs := range allSubs {
		result := subs[0]
		for g := 1; g < len(subs); g++ {
			if subs[g] != "" {
				result = subs[g]
				break
			}
		}
		result = strings.ReplaceAll(result, "\r\n", "↵")
		result = strings.ReplaceAll(result, "\r", "↵")
		result = strings.ReplaceAll(result, "\n", "↵")
		parts = append(parts, result)
	}
	return strings.Join(parts, "  |  "), true
}

// filterCellDisplay returns what to show for a cell in the filtered column.
// Collects all matches so OR patterns show every matched field.
func (m Model) filterCellDisplay(val any, col int) (string, bool) {
	if m.filterRE == nil || col != m.filterCol {
		return formatCell(val), false
	}
	result, matched := applyFilterRE(formatCellRaw(val), m.filterRE)
	if !matched {
		return formatCell(val), false
	}
	return result, true
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

	metaText := fmt.Sprintf("  %d rows  ·  %s  ·  result set %d of %d",
		len(rs.Rows), rs.Duration.Round(time.Millisecond), m.active+1, len(m.results))
	if m.filterRE != nil {
		colName := ""
		if m.filterCol >= 0 && m.filterCol < len(rs.Columns) {
			colName = rs.Columns[m.filterCol].Name
		}
		metaText += filterActiveStyle.Render(fmt.Sprintf("  ⊞ [%s]: %s", colName, m.filterPattern))
	}
	meta := metaStyle.Render(metaText)

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

func (m Model) renderGrid(rs db.QueryResult) string {
	if len(rs.Columns) == 0 {
		return metaStyle.Render("  (no columns)")
	}

	natural := m.colWidths
	if len(natural) != len(rs.Columns) {
		natural = computeColWidths(rs)
	}

	widths := make([]int, len(natural))
	for i, w := range natural {
		if w > maxColWidth {
			widths[i] = maxColWidth
		} else {
			widths[i] = w
		}
	}

	totalW := 0
	for i, w := range widths {
		totalW += w + 2
		if i < len(widths)-1 {
			totalW++
		}
	}
	if totalW <= m.width {
		leftover := m.width - totalW
		for leftover > 0 {
			grew := false
			for i := range widths {
				if widths[i] < natural[i] && leftover > 0 {
					widths[i]++
					leftover--
					grew = true
				}
			}
			if !grew {
				break
			}
		}
	}

	total := len(rs.Rows)
	vis := m.visibleRows()
	rowStart := m.scrollRow
	if rowStart > total {
		rowStart = total
	}
	rowEnd := rowStart + vis
	if rowEnd > total {
		rowEnd = total
	}

	colStart, colEnd := m.visibleColRange(widths)
	moreLeft := colStart > 0
	moreRight := colEnd < len(rs.Columns)

	if colEnd < len(rs.Columns) {
		used := colEnd - colStart - 1
		for i := colStart; i < colEnd; i++ {
			used += widths[i] + 2
		}
		if colEnd > colStart {
			used++
		}
		capW := m.width - used - 2
		if capW >= 4 {
			adjusted := make([]int, len(widths))
			copy(adjusted, widths)
			adjusted[colEnd] = capW
			widths = adjusted
			colEnd++
			moreRight = colEnd < len(rs.Columns)
		}
	}

	var sb strings.Builder

	// Header row — underline filtered column.
	for i := colStart; i < colEnd; i++ {
		name := rs.Columns[i].Name
		if runeLen(name) > widths[i] {
			name = string([]rune(name)[:widths[i]-1]) + "…"
		}
		cell := padRight(name, widths[i])
		sty := headerStyle
		if m.filterRE != nil && i == m.filterCol {
			sty = sty.Underline(true)
		}
		sb.WriteString(sty.Render(" " + cell + " "))
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

	// Data rows.
	for ri, row := range rs.Rows[rowStart:rowEnd] {
		absRow := rowStart + ri
		for i := colStart; i < colEnd; i++ {
			if i >= len(row) {
				break
			}
			val := row[i]
			raw, matched := m.filterCellDisplay(val, i)
			if runeLen(raw) > widths[i] {
				runes := []rune(raw)
				raw = string(runes[:widths[i]-1]) + "…"
			}
			padded := padRight(raw, widths[i])
			isCursor := m.focused && absRow == m.cursorRow && i == m.cursorCol
			switch {
			case isCursor && val == nil:
				sb.WriteString(cursorNullStyle.Render(" " + padded + " "))
			case isCursor:
				sb.WriteString(cursorStyle.Render(" " + padded + " "))
			case val == nil:
				sb.WriteString(nullStyle.Render(" " + padded + " "))
			case m.filterRE != nil && i == m.filterCol && !matched:
				sb.WriteString(noMatchStyle.Render(" " + padded + " "))
			default:
				sb.WriteString(cellStyle.Render(" " + padded + " "))
			}
			if i < colEnd-1 {
				sb.WriteString("│")
			}
		}
		sb.WriteByte('\n')
	}

	// Scroll hint.
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
		sb.WriteByte('\n')
	}

	if m.filterOpen {
		sb.WriteString(m.renderFilterBar(rs))
	}

	return sb.String()
}

func (m Model) renderFilterBar(rs db.QueryResult) string {
	var sb strings.Builder

	// History dropdown (oldest→newest top→bottom; newest just above filter bar).
	if m.filterHistSel >= 0 && len(m.filterHistory) > 0 {
		n := len(m.filterHistory)
		if n > 10 {
			n = 10
		}
		sb.WriteString(metaStyle.Render(strings.Repeat("─", m.width)) + "\n")
		for i := n - 1; i >= 0; i-- {
			entry := m.filterHistory[i]
			if runeLen(entry) > m.width-4 {
				entry = string([]rune(entry)[:m.width-5]) + "…"
			}
			line := "  " + entry
			if i == m.filterHistSel {
				line = "> " + entry
				sb.WriteString(filterHistSelStyle.Render(padRight(line, m.width)) + "\n")
			} else {
				sb.WriteString(filterHistStyle.Render(padRight(line, m.width)) + "\n")
			}
		}
		sb.WriteString(metaStyle.Render(strings.Repeat("─", m.width)) + "\n")
	}

	// Prompt.
	colName := ""
	if m.filterCol >= 0 && m.filterCol < len(rs.Columns) {
		colName = rs.Columns[m.filterCol].Name
		if runeLen(colName) > 15 {
			colName = string([]rune(colName)[:15]) + "…"
		}
	}
	prompt := filterPromptStyle.Render("/") + " " + filterColStyle.Render("["+colName+"]") + ": "

	inputStr := m.renderFilterInput()

	if m.filterErr != "" {
		errShort := m.filterErr
		if runeLen(errShort) > 40 {
			errShort = string([]rune(errShort)[:40]) + "…"
		}
		sb.WriteString(prompt + inputStr + "  " + filterErrStyle.Render("✗ "+errShort) + "\n")
	} else {
		hint := metaStyle.Render("  Enter confirm  Esc cancel  ↑↓ history")
		sb.WriteString(prompt + inputStr + hint + "\n")
	}

	return sb.String()
}

func (m Model) renderFilterInput() string {
	var sb strings.Builder
	for i, ch := range m.filterInput {
		if i == m.filterCursor {
			sb.WriteString(cursorStyle.Render(string(ch)))
		} else {
			sb.WriteString(string(ch))
		}
	}
	if m.filterCursor == len(m.filterInput) {
		sb.WriteString(cursorStyle.Render(" "))
	}
	return sb.String()
}

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
		colW := widths[i] + 3
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
	m.filterOpen = false
	m.filterErr = ""
	m.scrollRow, m.scrollCol, m.cursorRow, m.cursorCol = 0, 0, 0, 0
	if len(sets) > 0 {
		rs := sets[0]
		// Keep the active filter only if the column still exists at the same
		// position with the same name.
		if m.filterRE != nil {
			if m.filterCol < 0 || m.filterCol >= len(rs.Columns) ||
				rs.Columns[m.filterCol].Name != m.filterColName {
				m.filterPattern = ""
				m.filterRE = nil
				m.filterCol = -1
				m.filterColName = ""
			}
		}
		m.colWidths = computeColWidthsFiltered(rs, m.filterCol, m.filterRE)
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

func (m Model) SetFilterHistory(h []string) Model { m.filterHistory = h; return m }
func (m Model) FilterHistory() []string           { return m.filterHistory }
func (m Model) FilterOpen() bool                  { return m.filterOpen }

// OpenFilter opens the filter bar targeting the current cursor column.
func (m Model) OpenFilter() Model {
	rs := m.activeResult()
	newCol := 0
	newColName := ""
	if rs != nil && m.cursorCol < len(rs.Columns) {
		newCol = m.cursorCol
		newColName = rs.Columns[m.cursorCol].Name
	}
	// If opening on a different column than the active filter, clear the pattern.
	if newCol != m.filterCol || newColName != m.filterColName {
		m.filterPattern = ""
		m.filterRE = nil
	}
	m.filterCol = newCol
	m.filterColName = newColName
	m.filterOpen = true
	m.filterInput = []rune(m.filterPattern)
	m.filterCursor = len(m.filterInput)
	m.filterHistSel = -1
	m.filterErr = ""
	m.liveFilter()
	return m
}

// visibleRows returns the number of data rows that fit in the allocated height.
// Overhead: border-top(1) + title(1) + meta(1) + "\n"(1) + header(1) + separator(1) + scroll-hint(1) = 7
// Filter bar adds 1; history dropdown adds up to 12.
func (m Model) visibleRows() int {
	overhead := 7
	if m.filterOpen {
		overhead++
		if m.filterHistSel >= 0 && len(m.filterHistory) > 0 {
			n := len(m.filterHistory)
			if n > 10 {
				n = 10
			}
			overhead += n + 2
		}
	}
	n := m.height - overhead
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

func (m Model) ActiveResult() *db.QueryResult { return m.activeResult() }

func (m Model) CurrentCellRaw() (string, bool) {
	rs := m.activeResult()
	if rs == nil || m.cursorRow >= len(rs.Rows) {
		return "", false
	}
	row := rs.Rows[m.cursorRow]
	if m.cursorCol >= len(row) {
		return "", false
	}
	return formatCellRaw(row[m.cursorCol]), true
}

const maxColWidth = 40

func computeColWidths(rs db.QueryResult) []int {
	return computeColWidthsFiltered(rs, -1, nil)
}

// computeColWidthsFiltered computes natural column widths, using the filtered
// display value for filterCol when filterRE is non-nil.
func computeColWidthsFiltered(rs db.QueryResult, filterCol int, filterRE *regexp.Regexp) []int {
	widths := make([]int, len(rs.Columns))
	for i, c := range rs.Columns {
		widths[i] = runeLen(c.Name)
	}
	for _, row := range rs.Rows {
		for i, val := range row {
			if i >= len(widths) {
				break
			}
			var s string
			if filterRE != nil && i == filterCol {
				if result, matched := applyFilterRE(formatCellRaw(val), filterRE); matched {
					s = result
				} else {
					s = formatCell(val)
				}
			} else {
				s = formatCell(val)
			}
			if l := runeLen(s); l > widths[i] {
				widths[i] = l
			}
		}
	}
	return widths
}

func fmtGUID(b []byte) string {
	return fmt.Sprintf("%X-%X-%X-%X-%X", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func formatCellRaw(v any) string {
	if v == nil {
		return ""
	}
	if b, ok := v.([]byte); ok {
		printable := true
		for _, c := range b {
			if (c < 0x20 && c != '\n' && c != '\r' && c != '\t') || c > 0x7E {
				printable = false
				break
			}
		}
		if !printable {
			if len(b) == 16 {
				return fmtGUID(b)
			}
			return fmt.Sprintf("<binary %d bytes>", len(b))
		}
		return string(b)
	}
	return fmt.Sprintf("%v", v)
}

func formatCell(v any) string {
	if v == nil {
		return "∅"
	}
	var s string
	if b, ok := v.([]byte); ok {
		printable := true
		for _, c := range b {
			if (c < 0x20 && c != '\n' && c != '\r' && c != '\t') || c > 0x7E {
				printable = false
				break
			}
		}
		if !printable {
			if len(b) == 16 {
				return fmtGUID(b)
			}
			return fmt.Sprintf("<binary %d bytes>", len(b))
		}
		s = string(b)
	} else {
		s = fmt.Sprintf("%v", v)
	}
	s = strings.ReplaceAll(s, "\r\n", "↵")
	s = strings.ReplaceAll(s, "\r", "↵")
	s = strings.ReplaceAll(s, "\n", "↵")
	return s
}

func runeLen(s string) int      { return len([]rune(s)) }
func padRight(s string, n int) string {
	r := runeLen(s)
	if r >= n {
		return s
	}
	return s + strings.Repeat(" ", n-r)
}
