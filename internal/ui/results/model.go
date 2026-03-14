package results

import (
	"cmp"
	"fmt"
	"regexp"
	"slices"
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

// StartPollMsg is sent when the user confirms a poll interval.
// Seconds == 0 means stop polling.
type StartPollMsg struct {
	Seconds int
}

// StartLimitMsg is sent when the user confirms a result limit change.
type StartLimitMsg struct {
	Limit int // 0 means reset to default
}

// FilterConfirmedMsg is sent when the user confirms a filter so the app can persist history.
type FilterConfirmedMsg struct {
	Pattern string
}

// activeFilter is one confirmed column-level regex filter.
type activeFilter struct {
	Col     int
	ColName string
	Pattern string
	RE      *regexp.Regexp
}

// Model is the results pane.
type Model struct {
	width      int
	height     int
	results    []db.QueryResult
	active     int
	loading    bool
	errMsg     string
	scrollRow  int
	scrollCol  int
	colWidths  []int
	focused    bool
	cursorRow  int
	cursorCol  int
	showRowNums bool // # key toggles leading row-number column

	// Column sort state.
	sortCol  int  // column index being sorted; -1 = none
	sortAsc  bool // true = ascending, false = descending
	// sortedRows holds a sorted copy of the active result's rows (nil = no sort).
	sortedRows [][]any

	// Poll interval input bar.
	pollOpen  bool
	pollInput []rune
	pollSecs  int // active poll interval in seconds (0 = off), set by app via SetPollSecs

	// Result limit input bar.
	limitOpen        bool
	limitInput       []rune
	resultLimit      int // active result limit (0 = default, shown as 500)

	// Column display filter — narrows what is shown in the filtered column's cells.
	// filters holds all confirmed stacked filters (one per column).
	filters       []activeFilter
	filterOpen    bool
	filterInput   []rune
	filterCursor  int
	filterCol     int    // column index targeted by the open input bar; -1 = none
	filterColName string // column name targeted by the open input bar
	filterRE      *regexp.Regexp // live preview RE while the bar is open (not confirmed)
	filterErr     string // live regex parse error while typing
	filterHistory []string
	filterHistSel int // -1 = not showing history

	// Row detail view (Enter key while focused on results grid).
	rowDetailOpen   bool
	rowDetailCursor int // which field (column) is focused in the detail view

	// Row tagging for selective export.
	tags          map[int]bool // underlying row index → tagged
	tagRange      bool         // true when visual range-tag mode is active (V key)
	tagRangeStart int          // row index where V was pressed

	// Pin / diff mode.
	pinnedResult *db.QueryResult // baseline pinned result; nil = not pinned
	diffMode     bool            // true when showing a diff against pinnedResult
	diffRows     []diffRow       // computed diff rows; valid when diffMode == true
}

type diffStatus int

const (
	diffSame    diffStatus = iota
	diffAdded              // row is new (not in pinned)
	diffRemoved            // row was in pinned but not in new
	diffChanged            // row exists in both but has changed cells
)

type diffRow struct {
	status diffStatus
	row    []any     // current values (for added/same/changed) or pinned values (for removed)
	pinned []any     // pinned values (nil for added rows)
}

func New() Model {
	return Model{filterCol: -1, filterHistSel: -1, sortCol: -1}
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.rowDetailOpen {
			return m.updateRowDetail(msg)
		}
		if m.pollOpen {
			return m.updatePoll(msg)
		}
		if m.limitOpen {
			return m.updateLimit(msg)
		}
		if m.filterOpen {
			return m.updateFilter(msg)
		}

		rs := m.activeResult()
		activeRows := m.activeRows()
		total := len(activeRows)
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
		case "esc":
			if m.tagRange {
				m.tagRange = false
				return m, nil
			}
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
			if m.cursorRow < len(activeRows) && m.cursorCol < len(activeRows[m.cursorRow]) {
				text := formatCellRaw(activeRows[m.cursorRow][m.cursorCol])
				return m, func() tea.Msg { return CellYankMsg{Text: text} }
			}
			return m, nil
		case "#":
			m.showRowNums = !m.showRowNums
			return m, nil
		case "L":
			m = m.OpenLimit()
			return m, nil
		case "s":
			m = m.cycleSort()
			return m, nil
		case "p":
			m = m.togglePin()
			return m, nil
		case "F":
			// Clear all active stacked filters.
			m.filters = nil
			m.filterRE = nil
			if rs != nil {
				m.colWidths = computeColWidths(*rs)
			}
			return m, nil
		case " ":
			// Toggle tag on current row, advance cursor.
			if m.tagRange {
				// In range mode: confirm tagging the range.
				lo, hi := m.tagRangeStart, m.cursorRow
				if hi < lo {
					lo, hi = hi, lo
				}
				if m.tags == nil {
					m.tags = make(map[int]bool)
				}
				for i := lo; i <= hi; i++ {
					m.tags[i] = true
				}
				m.tagRange = false
			} else {
				if m.tags == nil {
					m.tags = make(map[int]bool)
				}
				m.tags[m.cursorRow] = !m.tags[m.cursorRow]
				if !m.tags[m.cursorRow] {
					delete(m.tags, m.cursorRow)
				}
				// Advance cursor for fast row tagging.
				if m.cursorRow < total-1 {
					m.cursorRow++
					if m.cursorRow >= m.scrollRow+vis {
						m.scrollRow = m.cursorRow - vis + 1
					}
				}
			}
			return m, nil
		case "V":
			// Toggle visual range-tag mode.
			if m.tagRange {
				m.tagRange = false
			} else {
				m.tagRange = true
				m.tagRangeStart = m.cursorRow
			}
			return m, nil
		case "ctrl+a":
			// Tag all rows if any are untagged, otherwise clear all.
			allTagged := total > 0 && len(m.tags) == total
			if allTagged {
				m.tags = nil
			} else {
				m.tags = make(map[int]bool, total)
				for i := 0; i < total; i++ {
					m.tags[i] = true
				}
			}
			m.tagRange = false
			return m, nil
		case "enter":
			if rs != nil && m.cursorRow < len(activeRows) {
				m.rowDetailOpen = true
				m.rowDetailCursor = 0
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
			// Remove any existing filter for this column.
			m.filters = removeFilter(m.filters, m.filterCol)
			m.filterRE = nil
			if rs := m.activeResult(); rs != nil {
				m.colWidths = computeColWidthsMulti(*rs, m.filters)
			}
		} else {
			re, err := regexp.Compile(pattern)
			if err != nil {
				m.filterErr = err.Error()
			} else {
				// Upsert: replace existing filter for this col, or append.
				m.filters = upsertFilter(m.filters, activeFilter{
					Col:     m.filterCol,
					ColName: m.filterColName,
					Pattern: pattern,
					RE:      re,
				})
				m.filterRE = re // keep live RE in sync
				if rs := m.activeResult(); rs != nil {
					m.colWidths = computeColWidthsMulti(*rs, m.filters)
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

// updatePoll handles all key input when the poll interval bar is open.
func (m Model) updatePoll(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		input := strings.TrimSpace(string(m.pollInput))
		m.pollOpen = false
		m.pollInput = nil
		var secs int
		if input != "" {
			n := 0
			for _, ch := range input {
				if ch < '0' || ch > '9' {
					return m, nil // invalid; ignore silently
				}
				n = n*10 + int(ch-'0')
			}
			secs = n
		}
		return m, func() tea.Msg { return StartPollMsg{Seconds: secs} }
	case "esc":
		m.pollOpen = false
		m.pollInput = nil
		return m, nil
	case "backspace":
		if len(m.pollInput) > 0 {
			m.pollInput = m.pollInput[:len(m.pollInput)-1]
		}
		return m, nil
	default:
		if msg.Type == tea.KeyRunes {
			for _, ch := range msg.Runes {
				if ch >= '0' && ch <= '9' {
					m.pollInput = append(m.pollInput, ch)
				}
			}
		}
		return m, nil
	}
}

// updateLimit handles all key input when the limit bar is open.
func (m Model) updateLimit(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		input := strings.TrimSpace(string(m.limitInput))
		m.limitOpen = false
		m.limitInput = nil
		var lim int
		if input != "" {
			n := 0
			for _, ch := range input {
				if ch < '0' || ch > '9' {
					return m, nil // invalid; ignore
				}
				n = n*10 + int(ch-'0')
			}
			lim = n
		}
		m.resultLimit = lim
		return m, func() tea.Msg { return StartLimitMsg{Limit: lim} }
	case "esc":
		m.limitOpen = false
		m.limitInput = nil
		return m, nil
	case "backspace":
		if len(m.limitInput) > 0 {
			m.limitInput = m.limitInput[:len(m.limitInput)-1]
		}
		return m, nil
	default:
		if msg.Type == tea.KeyRunes {
			for _, ch := range msg.Runes {
				if ch >= '0' && ch <= '9' {
					m.limitInput = append(m.limitInput, ch)
				}
			}
		}
		return m, nil
	}
}

// cycleSort cycles the sort state for the cursor column:
// unsorted → asc → desc → unsorted.
func (m Model) cycleSort() Model {
	rs := m.activeResult()
	if rs == nil || len(rs.Rows) == 0 {
		return m
	}
	col := m.cursorCol
	if col >= len(rs.Columns) {
		return m
	}
	if m.sortCol != col {
		// New column: start ascending.
		m.sortCol = col
		m.sortAsc = true
	} else if m.sortAsc {
		// Was ascending → descending.
		m.sortAsc = false
	} else {
		// Was descending → clear sort.
		m.sortCol = -1
		m.sortedRows = nil
		return m
	}
	m.sortedRows = m.buildSortedRows(*rs, col, m.sortAsc)
	// Reset scroll/cursor when sort changes.
	m.cursorRow = 0
	m.scrollRow = 0
	return m
}

func (m Model) buildSortedRows(rs db.QueryResult, col int, asc bool) [][]any {
	sorted := make([][]any, len(rs.Rows))
	copy(sorted, rs.Rows)
	slices.SortStableFunc(sorted, func(a, b []any) int {
		var av, bv any
		if col < len(a) {
			av = a[col]
		}
		if col < len(b) {
			bv = b[col]
		}
		r := compareCell(av, bv)
		if !asc {
			r = -r
		}
		return r
	})
	return sorted
}

// compareCell compares two cell values for sorting purposes.
func compareCell(a, b any) int {
	if a == nil && b == nil {
		return 0
	}
	if a == nil {
		return -1 // NULLs first
	}
	if b == nil {
		return 1
	}
	// Try numeric comparison.
	af := toFloat(a)
	bf := toFloat(b)
	if af != nil && bf != nil {
		return cmp.Compare(*af, *bf)
	}
	// Fall back to string comparison.
	return cmp.Compare(fmt.Sprintf("%v", a), fmt.Sprintf("%v", b))
}

func toFloat(v any) *float64 {
	var f float64
	switch n := v.(type) {
	case int:
		f = float64(n)
	case int32:
		f = float64(n)
	case int64:
		f = float64(n)
	case float32:
		f = float64(n)
	case float64:
		f = n
	default:
		return nil
	}
	return &f
}

// activeRows returns the rows to display (sorted if a sort is active).
func (m Model) activeRows() [][]any {
	if m.sortedRows != nil {
		return m.sortedRows
	}
	rs := m.activeResult()
	if rs == nil {
		return nil
	}
	return rs.Rows
}

// RowDetailOpen reports whether the row detail view is currently showing.
func (m Model) RowDetailOpen() bool { return m.rowDetailOpen }

// RowDetailView renders the row detail overlay sized to w×h (full terminal dimensions).
func (m Model) RowDetailView(w, h int) string {
	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#007acc")).
		Width(w - 2).
		Height(h - 2)
	innerW := w - 4
	innerH := h - 4
	if innerW < 20 {
		innerW = 20
	}
	if innerH < 5 {
		innerH = 5
	}
	return border.Render(m.renderRowDetail(innerW, innerH))
}

// TagCount returns the number of tagged rows.
func (m Model) TagCount() int { return len(m.tags) }

// TaggedResult returns a QueryResult containing only the tagged rows from the
// active result set.  Returns nil if no rows are tagged.
func (m Model) TaggedResult() *db.QueryResult {
	if len(m.tags) == 0 {
		return nil
	}
	rs := m.activeResult()
	if rs == nil {
		return nil
	}
	rows := m.activeRows()
	tagged := make([][]any, 0, len(m.tags))
	for i, row := range rows {
		if m.tags[i] {
			tagged = append(tagged, row)
		}
	}
	if len(tagged) == 0 {
		return nil
	}
	result := *rs
	result.Rows = tagged
	return &result
}

// ActiveResult returns the active QueryResult, or nil if none.
func (m Model) ActiveResult() *db.QueryResult { return m.activeResult() }

// Pinned reports whether a result is currently pinned as diff baseline.
func (m Model) Pinned() bool { return m.pinnedResult != nil }

// DiffMode reports whether the current view is showing a diff.
func (m Model) DiffMode() bool { return m.diffMode }

// togglePin pins the current result as a diff baseline, or clears the pin/diff.
func (m Model) togglePin() Model {
	if m.pinnedResult != nil {
		// Unpin.
		m.pinnedResult = nil
		m.diffMode = false
		m.diffRows = nil
		return m
	}
	rs := m.activeResult()
	if rs == nil {
		return m
	}
	// Deep-copy the result so future SetResults doesn't mutate it.
	pinned := *rs
	pinned.Rows = make([][]any, len(rs.Rows))
	for i, row := range rs.Rows {
		cp := make([]any, len(row))
		copy(cp, row)
		pinned.Rows[i] = cp
	}
	m.pinnedResult = &pinned
	m.diffMode = false
	m.diffRows = nil
	return m
}

// columnsMatch returns true if two column slices have the same names.
func columnsMatch(a, b []db.Column) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Name != b[i].Name {
			return false
		}
	}
	return true
}

// computeDiff produces a diffRow slice comparing pinned to current.
// Row matching: uses first PK column if available, otherwise row index.
func computeDiff(pinned, current db.QueryResult) []diffRow {
	// Find PK column index.
	pkCol := -1
	for i, col := range current.Columns {
		if col.Name == "id" || col.Name == "ID" {
			pkCol = i
			break
		}
	}

	if pkCol >= 0 {
		// Key-based matching.
		pinnedByKey := make(map[string][]any, len(pinned.Rows))
		for _, row := range pinned.Rows {
			if pkCol < len(row) {
				key := fmt.Sprintf("%v", row[pkCol])
				pinnedByKey[key] = row
			}
		}
		currentByKey := make(map[string][]any, len(current.Rows))
		for _, row := range current.Rows {
			if pkCol < len(row) {
				key := fmt.Sprintf("%v", row[pkCol])
				currentByKey[key] = row
			}
		}
		var out []diffRow
		// Added or changed.
		for _, row := range current.Rows {
			key := ""
			if pkCol < len(row) {
				key = fmt.Sprintf("%v", row[pkCol])
			}
			if pinnedRow, ok := pinnedByKey[key]; !ok {
				out = append(out, diffRow{status: diffAdded, row: row})
			} else if !rowsEqual(pinnedRow, row) {
				out = append(out, diffRow{status: diffChanged, row: row, pinned: pinnedRow})
			} else {
				out = append(out, diffRow{status: diffSame, row: row})
			}
		}
		// Removed.
		for _, row := range pinned.Rows {
			key := ""
			if pkCol < len(row) {
				key = fmt.Sprintf("%v", row[pkCol])
			}
			if _, ok := currentByKey[key]; !ok {
				out = append(out, diffRow{status: diffRemoved, row: row})
			}
		}
		return out
	}

	// Index-based matching.
	maxLen := len(current.Rows)
	if len(pinned.Rows) > maxLen {
		maxLen = len(pinned.Rows)
	}
	out := make([]diffRow, 0, maxLen)
	for i := 0; i < maxLen; i++ {
		switch {
		case i >= len(pinned.Rows):
			out = append(out, diffRow{status: diffAdded, row: current.Rows[i]})
		case i >= len(current.Rows):
			out = append(out, diffRow{status: diffRemoved, row: pinned.Rows[i]})
		case !rowsEqual(pinned.Rows[i], current.Rows[i]):
			out = append(out, diffRow{status: diffChanged, row: current.Rows[i], pinned: pinned.Rows[i]})
		default:
			out = append(out, diffRow{status: diffSame, row: current.Rows[i]})
		}
	}
	return out
}

func rowsEqual(a, b []any) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if fmt.Sprintf("%v", a[i]) != fmt.Sprintf("%v", b[i]) {
			return false
		}
	}
	return true
}

// EditCellMsg is sent when the user presses e in the row detail view.
type EditCellMsg struct{ Ctx CellContext }

// updateRowDetail handles keys while the row detail view is open.
func (m Model) updateRowDetail(msg tea.KeyMsg) (Model, tea.Cmd) {
	rs := m.activeResult()
	rows := m.activeRows()
	if rs == nil || m.cursorRow >= len(rows) {
		m.rowDetailOpen = false
		return m, nil
	}
	numFields := len(rs.Columns)
	switch msg.String() {
	case "esc", "q":
		m.rowDetailOpen = false
	case "up", "k":
		if m.rowDetailCursor > 0 {
			m.rowDetailCursor--
		}
	case "down", "j":
		if m.rowDetailCursor < numFields-1 {
			m.rowDetailCursor++
		}
	case "left", "h":
		if m.cursorRow > 0 {
			m.cursorRow--
		}
	case "right", "l":
		if m.cursorRow < len(rows)-1 {
			m.cursorRow++
		}
	case "y":
		row := rows[m.cursorRow]
		if m.rowDetailCursor < len(row) {
			text := formatCellRaw(row[m.rowDetailCursor])
			return m, func() tea.Msg { return CellYankMsg{Text: text} }
		}
	case "e":
		if ctx, ok := m.RowDetailCellContext(); ok {
			return m, func() tea.Msg { return EditCellMsg{Ctx: ctx} }
		}
	}
	return m, nil
}

// RowDetailCellContext returns context for the focused field in the row detail view.
func (m Model) RowDetailCellContext() (CellContext, bool) {
	rs := m.activeResult()
	rows := m.activeRows()
	if rs == nil || m.cursorRow >= len(rows) || m.rowDetailCursor >= len(rs.Columns) {
		return CellContext{}, false
	}
	row := rows[m.cursorRow]
	val := ""
	if m.rowDetailCursor < len(row) && row[m.rowDetailCursor] != nil {
		val = formatCellRaw(row[m.rowDetailCursor])
	}
	return CellContext{
		ColName:  rs.Columns[m.rowDetailCursor].Name,
		ColIndex: m.rowDetailCursor,
		Value:    val,
		Columns:  rs.Columns,
		Row:      row,
	}, true
}

// renderRowDetail renders a full-height vertical detail view for the cursor row.
func (m Model) renderRowDetail(w, h int) string {
	rs := m.activeResult()
	rows := m.activeRows()
	if rs == nil || m.cursorRow >= len(rows) {
		return ""
	}
	row := rows[m.cursorRow]

	// Compute column-name width.
	nameW := 10
	for _, col := range rs.Columns {
		if n := len([]rune(col.Name)); n > nameW {
			nameW = n
		}
	}
	if nameW > 30 {
		nameW = 30
	}
	valueW := w - nameW - 3 // 3 = " │ "
	if valueW < 10 {
		valueW = 10
	}

	var rdNameStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#9cdcfe"))
	var rdValueStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#d4d4d4"))
	var rdCursorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#ffffff")).Background(lipgloss.Color("#264f78"))
	var rdNullStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#555555"))
	var rdDivStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#444444"))
	var rdHintStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#555555"))
	var rdTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#4ec9b0"))

	contentH := h - 3 // title + hint + footer separator
	if contentH < 1 {
		contentH = 1
	}

	// Scroll so rowDetailCursor is visible.
	scroll := 0
	if m.rowDetailCursor >= contentH {
		scroll = m.rowDetailCursor - contentH + 1
	}

	var sb strings.Builder
	// Title
	sb.WriteString(rdTitleStyle.Render(fmt.Sprintf("Row %d / %d", m.cursorRow+1, len(rows))))
	sb.WriteByte('\n')
	// Separator
	sb.WriteString(rdDivStyle.Render(strings.Repeat("─", w)))
	sb.WriteByte('\n')

	for i := 0; i < contentH; i++ {
		fieldIdx := scroll + i
		if fieldIdx >= len(rs.Columns) {
			sb.WriteString(strings.Repeat(" ", w) + "\n")
			continue
		}
		col := rs.Columns[fieldIdx]
		var rawVal any
		if fieldIdx < len(row) {
			rawVal = row[fieldIdx]
		}
		// Format name.
		name := padRight(col.Name, nameW)
		// Format value.
		var valStr string
		if rawVal == nil {
			valStr = "∅"
		} else {
			valStr = formatCellRaw(rawVal)
		}
		// Truncate value to fit.
		valRunes := []rune(valStr)
		if len(valRunes) > valueW {
			valStr = string(valRunes[:valueW-1]) + "…"
		} else {
			valStr = padRight(valStr, valueW)
		}
		div := rdDivStyle.Render(" │ ")
		if fieldIdx == m.rowDetailCursor {
			nameRendered := rdCursorStyle.Render(name)
			valRendered := rdCursorStyle.Render(valStr)
			sb.WriteString(nameRendered + div + valRendered + "\n")
		} else if rawVal == nil {
			sb.WriteString(rdNameStyle.Render(name) + div + rdNullStyle.Render(valStr) + "\n")
		} else {
			sb.WriteString(rdNameStyle.Render(name) + div + rdValueStyle.Render(valStr) + "\n")
		}
	}

	// Footer hint
	sb.WriteString(rdDivStyle.Render(strings.Repeat("─", w)) + "\n")
	sb.WriteString(rdHintStyle.Render(
		fmt.Sprintf("j/k: field  h/l: row  y: copy  e: edit  Esc: close  field %d/%d",
			m.rowDetailCursor+1, len(rs.Columns))))

	return sb.String()
}

// OpenLimit opens the result limit input bar.
func (m Model) OpenLimit() Model {
	m.limitOpen = true
	if m.resultLimit > 0 {
		m.limitInput = []rune(fmt.Sprintf("%d", m.resultLimit))
	} else {
		m.limitInput = []rune("500")
	}
	return m
}

func (m Model) LimitOpen() bool { return m.limitOpen }

// SetResultLimit sets the active result limit value (used for display).
func (m Model) SetResultLimit(n int) Model { m.resultLimit = n; return m }

// ResultLimit returns the active result limit (0 = default 500).
func (m Model) ResultLimit() int { return m.resultLimit }

// OpenPoll opens the poll interval input bar.
func (m Model) OpenPoll() Model {
	m.pollOpen = true
	if m.pollSecs > 0 {
		m.pollInput = []rune(fmt.Sprintf("%d", m.pollSecs))
	} else {
		m.pollInput = []rune("15")
	}
	return m
}

func (m Model) PollOpen() bool { return m.pollOpen }

// SetPollSecs records the active poll interval so the view can display it.
func (m Model) SetPollSecs(n int) Model { m.pollSecs = n; return m }

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

// filterCellDisplay returns what to show for a cell in a filtered column.
// While the filter bar is open, the live RE is used for the target column.
// For all confirmed stacked filters the confirmed RE is used.
// Returns (display, matched) where matched=false means the cell should be dimmed.
func (m Model) filterCellDisplay(val any, col int) (string, bool) {
	// If the filter bar is open and targeting this column, use live RE.
	if m.filterOpen && col == m.filterCol {
		if m.filterRE == nil {
			return formatCell(val), true
		}
		result, matched := applyFilterRE(formatCellRaw(val), m.filterRE)
		if !matched {
			return formatCell(val), false
		}
		return result, true
	}
	// Check confirmed filters.
	for _, f := range m.filters {
		if f.Col == col {
			result, matched := applyFilterRE(formatCellRaw(val), f.RE)
			if !matched {
				return formatCell(val), false
			}
			return result, true
		}
	}
	return formatCell(val), false
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
	if m.resultLimit > 0 && m.resultLimit != 500 {
		metaText += filterActiveStyle.Render(fmt.Sprintf("  limit %d", m.resultLimit))
	}
	if m.pollSecs > 0 {
		metaText += filterActiveStyle.Render(fmt.Sprintf("  ↻ every %ds", m.pollSecs))
	}
	for _, f := range m.filters {
		metaText += filterActiveStyle.Render(fmt.Sprintf("  ⊞ [%s]: %s", f.ColName, f.Pattern))
	}
	if n := len(m.tags); n > 0 {
		metaText += filterActiveStyle.Render(fmt.Sprintf("  ● %d tagged", n))
	}
	if m.pinnedResult != nil {
		if m.diffMode {
			metaText += filterActiveStyle.Render("  📌 DIFF")
		} else {
			metaText += filterActiveStyle.Render("  📌 pinned")
		}
	}
	meta := metaStyle.Render(metaText)

	var grid string
	if m.diffMode && len(m.diffRows) > 0 {
		grid = m.renderDiffGrid(rs)
	} else {
		grid = m.renderGrid(rs)
	}
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

var rowNumStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#555555"))

func (m Model) renderGrid(rs db.QueryResult) string {
	if len(rs.Columns) == 0 {
		return metaStyle.Render("  (no columns)")
	}

	// Row-number column: width is the digit count of the last visible row + 1 for the "#" header.
	rowNumW := 0
	if m.showRowNums {
		rowNumW = len(fmt.Sprintf("%d", len(rs.Rows)))
		if rowNumW < 1 {
			rowNumW = 1
		}
	}
	// rowNumColW is total chars consumed by the row-num column (content + padding + separator).
	rowNumColW := 0
	if m.showRowNums {
		rowNumColW = rowNumW + 3 // " " + digits + " " + "│"
	}
	// Tag gutter: 1 char (●) + │ = 2 chars, shown when any rows are tagged or range is active.
	showTagGutter := len(m.tags) > 0 || m.tagRange
	tagGutterW := 0
	if showTagGutter {
		tagGutterW = 2 // "●" + "│"
	}

	natural := m.colWidths
	if len(natural) != len(rs.Columns) {
		natural = computeColWidths(rs)
	}

	availW := m.width - rowNumColW - tagGutterW

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
	if totalW <= availW {
		leftover := availW - totalW
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

	rows := m.activeRows()
	total := len(rows)
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
		capW := availW - used - 2
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

	tagStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#dcdcaa"))
	tagRangeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#4ec9b0"))

	// Header row.
	if showTagGutter {
		sb.WriteString(rowNumStyle.Render("●") + headerStyle.Render("│"))
	}
	if m.showRowNums {
		hdr := padRight("#", rowNumW)
		sb.WriteString(rowNumStyle.Render(" "+hdr+" ") + headerStyle.Render("│"))
	}
	for i := colStart; i < colEnd; i++ {
		name := rs.Columns[i].Name
		// Append sort indicator.
		if m.sortCol == i {
			if m.sortAsc {
				name += " ▲"
			} else {
				name += " ▼"
			}
		}
		if runeLen(name) > widths[i] {
			name = string([]rune(name)[:widths[i]-1]) + "…"
		}
		cell := padRight(name, widths[i])
		sty := headerStyle
		if m.hasFilter(i) {
			sty = sty.Underline(true)
		}
		sb.WriteString(sty.Render(" " + cell + " "))
		if i < colEnd-1 {
			sb.WriteString(headerStyle.Render("│"))
		}
	}
	sb.WriteByte('\n')

	// Separator.
	if showTagGutter {
		sb.WriteString("─┼")
	}
	if m.showRowNums {
		sb.WriteString(strings.Repeat("─", rowNumW+2))
		sb.WriteString("┼")
	}
	for i := colStart; i < colEnd; i++ {
		sb.WriteString(strings.Repeat("─", widths[i]+2))
		if i < colEnd-1 {
			sb.WriteString("┼")
		}
	}
	sb.WriteByte('\n')

	// Data rows.
	for ri, row := range rows[rowStart:rowEnd] {
		absRow := rowStart + ri
		if showTagGutter {
			isTagged := m.tags[absRow]
			inRange := m.tagRange && ((absRow >= m.tagRangeStart && absRow <= m.cursorRow) ||
				(absRow <= m.tagRangeStart && absRow >= m.cursorRow))
			switch {
			case inRange:
				sb.WriteString(tagRangeStyle.Render("◉") + "│")
			case isTagged:
				sb.WriteString(tagStyle.Render("●") + "│")
			default:
				sb.WriteString(" │")
			}
		}
		if m.showRowNums {
			numStr := padRight(fmt.Sprintf("%d", absRow+1), rowNumW)
			sb.WriteString(rowNumStyle.Render(" "+numStr+" ") + "│")
		}
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
			case m.hasFilter(i) && !matched:
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
	if m.pollOpen {
		sb.WriteString(m.renderPollBar())
	}
	if m.limitOpen {
		sb.WriteString(m.renderLimitBar())
	}

	return sb.String()
}

// renderDiffGrid renders the results with diff highlighting.
func (m Model) renderDiffGrid(rs db.QueryResult) string {
	if len(rs.Columns) == 0 {
		return metaStyle.Render("  (no columns)")
	}

	diffAddedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#4ec9b0"))   // teal/green
	diffRemovedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#f44747")) // red
	diffChangedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#dcdcaa")) // yellow
	diffChangedCellStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#ffb347")) // orange

	natural := m.colWidths
	if len(natural) != len(rs.Columns) {
		natural = computeColWidths(rs)
	}
	availW := m.width - 2 // 2 = diff gutter ("+"/"-"/" " + "│")

	widths := make([]int, len(natural))
	for i, w := range natural {
		if w > maxColWidth {
			widths[i] = maxColWidth
		} else {
			widths[i] = w
		}
	}

	colStart, colEnd := m.visibleColRangeW(widths, availW)

	rows := m.diffRows
	total := len(rows)
	vis := m.visibleRows()
	rowStart := m.scrollRow
	if rowStart > total {
		rowStart = total
	}
	rowEnd := rowStart + vis
	if rowEnd > total {
		rowEnd = total
	}

	var sb strings.Builder

	// Header.
	sb.WriteString(headerStyle.Render("Δ") + headerStyle.Render("│"))
	for i := colStart; i < colEnd; i++ {
		name := rs.Columns[i].Name
		padded := padRight(name, widths[i])
		sb.WriteString(headerStyle.Render(" " + padded + " "))
		if i < colEnd-1 {
			sb.WriteString(headerStyle.Render("│"))
		}
	}
	sb.WriteByte('\n')

	// Separator.
	sb.WriteString("──")
	for i := colStart; i < colEnd; i++ {
		sb.WriteString(strings.Repeat("─", widths[i]+2))
		if i < colEnd-1 {
			sb.WriteString("┼")
		}
	}
	sb.WriteByte('\n')

	// Data rows.
	for ri, dr := range rows[rowStart:rowEnd] {
		absRow := rowStart + ri
		var marker string
		var rowSty lipgloss.Style
		switch dr.status {
		case diffAdded:
			marker = diffAddedStyle.Render("+")
			rowSty = diffAddedStyle
		case diffRemoved:
			marker = diffRemovedStyle.Render("-")
			rowSty = diffRemovedStyle
		case diffChanged:
			marker = diffChangedStyle.Render("~")
			rowSty = diffChangedStyle
		default:
			marker = " "
			rowSty = cellStyle
		}
		sb.WriteString(marker + "│")
		row := dr.row
		for i := colStart; i < colEnd; i++ {
			var val any
			if i < len(row) {
				val = row[i]
			}
			raw := formatCellRaw(val)
			if runeLen(raw) > widths[i] {
				runes := []rune(raw)
				raw = string(runes[:widths[i]-1]) + "…"
			}
			padded := padRight(raw, widths[i])
			isCursor := m.focused && absRow == m.cursorRow && i == m.cursorCol
			switch {
			case isCursor:
				sb.WriteString(cursorStyle.Render(" " + padded + " "))
			case dr.status == diffChanged && dr.pinned != nil && i < len(dr.pinned) &&
				fmt.Sprintf("%v", dr.pinned[i]) != fmt.Sprintf("%v", val):
				sb.WriteString(diffChangedCellStyle.Render(" " + padded + " "))
			default:
				sb.WriteString(rowSty.Render(" " + padded + " "))
			}
			if i < colEnd-1 {
				sb.WriteString("│")
			}
		}
		sb.WriteByte('\n')
	}

	// Summary hint.
	added, removed, changed := 0, 0, 0
	for _, dr := range rows {
		switch dr.status {
		case diffAdded:
			added++
		case diffRemoved:
			removed++
		case diffChanged:
			changed++
		}
	}
	hint := fmt.Sprintf("  DIFF MODE (p: unpin)  +%d added  -%d removed  ~%d changed", added, removed, changed)
	sb.WriteString(metaStyle.Render(hint) + "\n")

	return sb.String()
}

// visibleColRangeW computes visible column range given an explicit available width.
func (m Model) visibleColRangeW(widths []int, availW int) (int, int) {
	colStart := m.scrollCol
	if colStart >= len(widths) {
		colStart = 0
	}
	used := 0
	colEnd := colStart
	for colEnd < len(widths) {
		needed := widths[colEnd] + 2
		if colEnd > colStart {
			needed++ // separator
		}
		if used+needed > availW && colEnd > colStart {
			break
		}
		used += needed
		colEnd++
	}
	if colEnd <= colStart {
		colEnd = colStart + 1
	}
	return colStart, colEnd
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

func (m Model) renderLimitBar() string {
	prompt := filterPromptStyle.Render("⊕") + " Result limit (blank=500): "
	var sb strings.Builder
	for _, ch := range m.limitInput {
		sb.WriteRune(ch)
	}
	sb.WriteString(cursorStyle.Render(" "))
	hint := metaStyle.Render("  Enter confirm  Esc cancel")
	return prompt + sb.String() + hint + "\n"
}

func (m Model) renderPollBar() string {
	prompt := filterPromptStyle.Render("↻") + " Poll interval (seconds, blank=stop): "
	var sb strings.Builder
	for _, ch := range m.pollInput {
		sb.WriteRune(ch)
	}
	sb.WriteString(cursorStyle.Render(" "))
	hint := metaStyle.Render("  Enter confirm  Esc cancel")
	return prompt + sb.String() + hint + "\n"
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
	m.filters = nil
	m.filterRE = nil
	m.tags = nil
	m.tagRange = false
	m.scrollRow, m.scrollCol, m.cursorRow, m.cursorCol = 0, 0, 0, 0
	// Reset sort and row detail state on new results.
	m.sortCol = -1
	m.sortedRows = nil
	m.rowDetailOpen = false
	m.rowDetailCursor = 0
	if len(sets) > 0 {
		rs := sets[0]
		m.colWidths = computeColWidths(rs)
		// Compute diff if pinned and columns match.
		if m.pinnedResult != nil && columnsMatch(m.pinnedResult.Columns, rs.Columns) {
			m.diffMode = true
			m.diffRows = computeDiff(*m.pinnedResult, rs)
		} else {
			m.diffMode = false
			m.diffRows = nil
		}
	} else {
		m.diffMode = false
		m.diffRows = nil
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
// If a confirmed filter already exists for that column it is pre-filled.
func (m Model) OpenFilter() Model {
	rs := m.activeResult()
	newCol := 0
	newColName := ""
	if rs != nil && m.cursorCol < len(rs.Columns) {
		newCol = m.cursorCol
		newColName = rs.Columns[m.cursorCol].Name
	}
	m.filterCol = newCol
	m.filterColName = newColName
	// Prefill from any existing confirmed filter for this column.
	existingPattern := ""
	for _, f := range m.filters {
		if f.Col == newCol {
			existingPattern = f.Pattern
			break
		}
	}
	m.filterRE = nil
	m.filterOpen = true
	m.filterInput = []rune(existingPattern)
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
	if m.pollOpen {
		overhead++
	}
	if m.limitOpen {
		overhead++
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

// ActiveRows returns the current rows for the active result set, respecting
// any active sort.  Returns nil if no results are loaded.
func (m Model) ActiveRows() [][]any { return m.activeRows() }

// CurrentColumnType returns the SQL type string for the cursor column,
// or "" if no results are loaded.
func (m Model) CurrentColumnType() string {
	rs := m.activeResult()
	if rs == nil || m.cursorCol >= len(rs.Columns) {
		return ""
	}
	return rs.Columns[m.cursorCol].Type
}

func (m Model) CurrentCellRaw() (string, bool) {
	rows := m.activeRows()
	if rows == nil || m.cursorRow >= len(rows) {
		return "", false
	}
	row := rows[m.cursorRow]
	if m.cursorCol >= len(row) {
		return "", false
	}
	return formatCellRaw(row[m.cursorCol]), true
}

// CellContext holds everything needed to generate an UPDATE statement for the cursor cell.
type CellContext struct {
	ColName  string
	ColIndex int
	Value    string // current string representation
	Columns  []db.Column
	Row      []any
}

// CurrentCellContext returns context for the cursor cell, or (CellContext{}, false) if unavailable.
func (m Model) CurrentCellContext() (CellContext, bool) {
	rs := m.activeResult()
	rows := m.activeRows()
	if rs == nil || m.cursorRow >= len(rows) || m.cursorCol >= len(rs.Columns) {
		return CellContext{}, false
	}
	row := rows[m.cursorRow]
	val := ""
	if m.cursorCol < len(row) && row[m.cursorCol] != nil {
		val = formatCellRaw(row[m.cursorCol])
	}
	return CellContext{
		ColName:  rs.Columns[m.cursorCol].Name,
		ColIndex: m.cursorCol,
		Value:    val,
		Columns:  rs.Columns,
		Row:      row,
	}, true
}

const maxColWidth = 40

func computeColWidths(rs db.QueryResult) []int {
	return computeColWidthsFiltered(rs, -1, nil)
}

// computeColWidthsMulti computes column widths applying all active stacked filters.
func computeColWidthsMulti(rs db.QueryResult, filters []activeFilter) []int {
	widths := make([]int, len(rs.Columns))
	for i, c := range rs.Columns {
		widths[i] = runeLen(c.Name)
	}
	// Build a map from col → RE for fast lookup.
	reMap := make(map[int]*regexp.Regexp, len(filters))
	for _, f := range filters {
		reMap[f.Col] = f.RE
	}
	for _, row := range rs.Rows {
		for i, val := range row {
			if i >= len(widths) {
				break
			}
			var s string
			if re, ok := reMap[i]; ok {
				if result, matched := applyFilterRE(formatCellRaw(val), re); matched {
					s = result
				} else {
					s = formatCell(val)
				}
			} else {
				s = formatCell(val)
			}
			if n := runeLen(s); n > widths[i] && n <= maxColWidth {
				widths[i] = n
			}
		}
	}
	for i := range widths {
		if widths[i] > maxColWidth {
			widths[i] = maxColWidth
		}
		if widths[i] < 3 {
			widths[i] = 3
		}
	}
	return widths
}

// hasFilter reports whether a confirmed filter is active for the given column.
func (m Model) hasFilter(col int) bool {
	for _, f := range m.filters {
		if f.Col == col {
			return true
		}
	}
	// Also include the live filter target while the bar is open.
	return m.filterOpen && col == m.filterCol
}

// upsertFilter replaces an existing filter for the same column or appends a new one.
func upsertFilter(filters []activeFilter, f activeFilter) []activeFilter {
	for i, existing := range filters {
		if existing.Col == f.Col {
			filters[i] = f
			return filters
		}
	}
	return append(filters, f)
}

// removeFilter removes any filter for the given column.
func removeFilter(filters []activeFilter, col int) []activeFilter {
	out := filters[:0]
	for _, f := range filters {
		if f.Col != col {
			out = append(out, f)
		}
	}
	return out
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
	if t, ok := v.(time.Time); ok {
		return FormatTimeSQL(t)
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

// FormatTimeSQL formats a time.Time as a SQL-friendly string.
// Dates at midnight are formatted as date-only; otherwise datetime with optional sub-seconds.
func FormatTimeSQL(t time.Time) string {
	t = t.UTC()
	if t.Hour() == 0 && t.Minute() == 0 && t.Second() == 0 && t.Nanosecond() == 0 {
		return t.Format("2006-01-02")
	}
	if t.Nanosecond() == 0 {
		return t.Format("2006-01-02 15:04:05")
	}
	return t.Format("2006-01-02 15:04:05.000")
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
