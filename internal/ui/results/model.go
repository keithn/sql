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
	"github.com/sahilm/fuzzy"
	"github.com/sqltui/sql/internal/db"
)

var (
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#ffffff")).
			Background(lipgloss.Color("#2d2d2d"))

	headerSelectedStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#ffffff")).
				Background(lipgloss.Color("#6a0dad"))

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
			BorderForeground(lipgloss.Color("#6a0dad"))

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

	findMatchStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#5a4a00")).
			Foreground(lipgloss.Color("#ffffff"))

	findCurrentMatchStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#b87200")).
				Foreground(lipgloss.Color("#ffffff"))

	findPromptStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#dcdcaa")).
			Bold(true)
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

	// Column selection for selective export.
	selectedCols map[int]bool // column index → selected; empty = all columns included

	// Pin / diff mode.
	pinnedResult *db.QueryResult // baseline pinned result; nil = not pinned
	diffMode     bool            // true when showing a diff against pinnedResult
	diffRows     []diffRow       // computed diff rows; valid when diffMode == true

	// Find-in-results (/ key).
	findOpen    bool
	findInput   []rune
	findCursor  int
	findRE      *regexp.Regexp
	findErr     string
	findMatches [][2]int // [rowIdx, colIdx] into activeRows()
	findCurrent int      // index into findMatches; -1 = none

	// Column picker (\ key).
	colPickerOpen         bool
	colPickerSearchActive bool         // true = typing goes to search; false = list navigation mode
	colPickerInput        []rune
	colPickerCursor       int          // cursor within input
	colPickerSel          int          // selected item index in filtered list
	colPickerScroll       int          // scroll offset in filtered list
	colPickerFiltered     []int        // column indices matching current input
	colPickerTagged       map[int]bool // staged per-picker selection (Space key)
	colPickerWindowH      int          // full terminal height, for listH computation

	// Selected-only view (t key): show only tagged rows and/or selected columns.
	showSelectedOnly bool
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
	return Model{filterCol: -1, filterHistSel: -1, sortCol: -1, findCurrent: -1}
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.colPickerOpen {
			return m.updateColPicker(msg)
		}
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
		if m.findOpen {
			return m.updateFind(msg)
		}

		rs := m.activeResult()
		activeRows := m.activeRows()
		// In selected-only mode with tagged rows, only those rows are shown.
		if m.showSelectedOnly && len(m.tags) > 0 {
			filtered := activeRows[:0:0]
			for i, row := range activeRows {
				if m.tags[i] {
					filtered = append(filtered, row)
				}
			}
			activeRows = filtered
		}
		total := len(activeRows)
		vis := m.visibleRows()
		maxScroll := total - vis
		if maxScroll < 0 {
			maxScroll = 0
		}
		maxScrollCol := 0
		if rs != nil {
			displayCols := len(rs.Columns)
			if m.showSelectedOnly && len(m.selectedCols) > 0 {
				displayCols = len(m.selectedCols)
			}
			if displayCols > 0 {
				maxScrollCol = displayCols - 1
			}
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
		case "0":
			m.cursorCol = 0
			m.scrollCol = 0
			return m, nil
		case "$":
			m.cursorCol = maxScrollCol
			if len(m.colWidths) > 0 {
				_, colEnd := m.visibleColRange(m.colWidths)
				for m.cursorCol >= colEnd {
					m.scrollCol++
					_, colEnd = m.visibleColRange(m.colWidths)
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
		case "|":
			// Toggle current column in/out of column selection.
			if rs == nil {
				return m, nil
			}
			col := m.cursorCol
			if col < 0 || col >= len(rs.Columns) {
				return m, nil
			}
			if m.selectedCols == nil {
				m.selectedCols = make(map[int]bool)
			}
			if m.selectedCols[col] {
				delete(m.selectedCols, col)
			} else {
				m.selectedCols[col] = true
			}
			return m, nil
		case "ctrl+\\":
			// Clear all column selections.
			m.selectedCols = nil
			return m, nil
		case "\\":
			if rs != nil {
				m = m.openColPicker()
			}
			return m, nil
		case "t":
			m.showSelectedOnly = !m.showSelectedOnly
			m.scrollRow, m.scrollCol, m.cursorRow, m.cursorCol = 0, 0, 0, 0
			return m, nil
		case "/":
			m = m.OpenFind()
			return m, nil
		case "n":
			if len(m.findMatches) > 0 {
				m.findCurrent = (m.findCurrent + 1) % len(m.findMatches)
				m.jumpToFind(vis)
			}
			return m, nil
		case "N":
			if len(m.findMatches) > 0 {
				m.findCurrent = (m.findCurrent - 1 + len(m.findMatches)) % len(m.findMatches)
				m.jumpToFind(vis)
			}
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

// ExportResult returns a QueryResult scoped to tagged rows (if any) and
// selected columns (if any). Falls back to the full active result set.
func (m Model) ExportResult() *db.QueryResult {
	rs := m.TaggedResult()
	if rs == nil {
		rs = m.activeResult()
	}
	if rs == nil || len(m.selectedCols) == 0 {
		return rs
	}
	// Build ordered list of selected column indices.
	colIndices := make([]int, 0, len(m.selectedCols))
	for i := range rs.Columns {
		if m.selectedCols[i] {
			colIndices = append(colIndices, i)
		}
	}
	if len(colIndices) == 0 {
		return rs
	}
	filtered := &db.QueryResult{
		Duration: rs.Duration,
		Columns:  make([]db.Column, len(colIndices)),
		Rows:     make([][]any, len(rs.Rows)),
	}
	for j, ci := range colIndices {
		filtered.Columns[j] = rs.Columns[ci]
	}
	for i, row := range rs.Rows {
		filteredRow := make([]any, len(colIndices))
		for j, ci := range colIndices {
			if ci < len(row) {
				filteredRow[j] = row[ci]
			}
		}
		filtered.Rows[i] = filteredRow
	}
	return filtered
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

// FindOpen reports whether the find bar is open.
func (m Model) FindOpen() bool { return m.findOpen }

// FindMatches returns the current set of find matches as [rowIdx, colIdx] pairs.
func (m Model) FindMatches() [][2]int { return m.findMatches }

// FindCurrent returns the index of the current find match, or -1 if none.
func (m Model) FindCurrent() int { return m.findCurrent }

// clearFind resets all find state to empty.
func (m *Model) clearFind() {
	m.findOpen = false
	m.findInput = nil
	m.findCursor = 0
	m.findRE = nil
	m.findErr = ""
	m.findMatches = nil
	m.findCurrent = -1
}

// OpenFind opens the find bar.
func (m Model) OpenFind() Model {
	m.clearFind()
	m.findOpen = true
	return m
}

// liveFindRE compiles the current find input into findRE and recomputes matches.
func (m *Model) liveFindRE() {
	input := string(m.findInput)
	if input == "" {
		m.findErr = ""
		m.findRE = nil
		m.findMatches = nil
		m.findCurrent = -1
		return
	}
	re, err := regexp.Compile("(?i)" + regexp.QuoteMeta(input))
	if err != nil {
		m.findErr = err.Error()
		m.findRE = nil
		m.findMatches = nil
		m.findCurrent = -1
		return
	}
	m.findErr = ""
	m.findRE = re
	m.computeFinds()
}

// computeFinds builds the findMatches slice for the current findRE.
func (m *Model) computeFinds() {
	m.findMatches = nil
	m.findCurrent = -1
	if m.findRE == nil {
		return
	}
	rows := m.activeRows()
	for ri, row := range rows {
		for ci, val := range row {
			if m.findRE.MatchString(formatCellRaw(val)) {
				m.findMatches = append(m.findMatches, [2]int{ri, ci})
			}
		}
	}
	if len(m.findMatches) > 0 {
		// Start at first match at or after current cursor position.
		m.findCurrent = 0
		for i, match := range m.findMatches {
			if match[0] > m.cursorRow || (match[0] == m.cursorRow && match[1] >= m.cursorCol) {
				m.findCurrent = i
				break
			}
		}
	}
}

// jumpToFind moves the cursor to findMatches[findCurrent] and scrolls to show it.
func (m *Model) jumpToFind(vis int) {
	if m.findCurrent < 0 || m.findCurrent >= len(m.findMatches) {
		return
	}
	match := m.findMatches[m.findCurrent]
	m.cursorRow = match[0]
	m.cursorCol = match[1]
	if m.cursorRow < m.scrollRow {
		m.scrollRow = m.cursorRow
	}
	if m.cursorRow >= m.scrollRow+vis {
		m.scrollRow = m.cursorRow - vis + 1
	}
}

// updateFind handles key input when the find bar is open.
func (m Model) updateFind(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.clearFind()
		return m, nil
	case "enter":
		// Close bar but keep find active; jump to current match.
		m.findOpen = false
		if len(m.findMatches) > 0 {
			vis := m.visibleRows()
			m.jumpToFind(vis)
		}
		return m, nil
	case "ctrl+n", "down":
		if len(m.findMatches) > 0 {
			m.findCurrent = (m.findCurrent + 1) % len(m.findMatches)
			vis := m.visibleRows()
			m.jumpToFind(vis)
		}
		return m, nil
	case "ctrl+p", "up":
		if len(m.findMatches) > 0 {
			m.findCurrent = (m.findCurrent - 1 + len(m.findMatches)) % len(m.findMatches)
			vis := m.visibleRows()
			m.jumpToFind(vis)
		}
		return m, nil
	case "left":
		if m.findCursor > 0 {
			m.findCursor--
		}
	case "right":
		if m.findCursor < len(m.findInput) {
			m.findCursor++
		}
	case "home", "ctrl+a":
		m.findCursor = 0
	case "end", "ctrl+e":
		m.findCursor = len(m.findInput)
	case "backspace":
		if m.findCursor > 0 {
			m.findInput = append(m.findInput[:m.findCursor-1], m.findInput[m.findCursor:]...)
			m.findCursor--
			m.liveFindRE()
		}
	case "delete":
		if m.findCursor < len(m.findInput) {
			m.findInput = append(m.findInput[:m.findCursor], m.findInput[m.findCursor+1:]...)
			m.liveFindRE()
		}
	case "ctrl+u":
		m.findInput = nil
		m.findCursor = 0
		m.liveFindRE()
	default:
		if msg.Type == tea.KeyRunes {
			ins := msg.Runes
			m.findInput = append(m.findInput[:m.findCursor], append(ins, m.findInput[m.findCursor:]...)...)
			m.findCursor += len(ins)
			m.liveFindRE()
		}
	}
	return m, nil
}

// openColPicker initialises and opens the column picker overlay.
func (m Model) openColPicker() Model {
	m.colPickerOpen = true
	m.colPickerSearchActive = true
	m.colPickerInput = nil
	m.colPickerCursor = 0
	m.colPickerSel = 0
	m.colPickerScroll = 0
	m.colPickerTagged = nil
	m.syncColPickerFilter()
	return m
}

// ColPickerOpen reports whether the column picker overlay is showing.
func (m Model) ColPickerOpen() bool { return m.colPickerOpen }

// syncColPickerFilter rebuilds colPickerFiltered from the current input.
func (m *Model) syncColPickerFilter() {
	rs := m.activeResult()
	if rs == nil {
		m.colPickerFiltered = nil
		return
	}
	query := strings.TrimSpace(string(m.colPickerInput))
	if query == "" {
		m.colPickerFiltered = make([]int, len(rs.Columns))
		for i := range rs.Columns {
			m.colPickerFiltered[i] = i
		}
		return
	}
	names := make([]string, len(rs.Columns))
	for i, c := range rs.Columns {
		names[i] = c.Name
	}
	matches := fuzzy.Find(strings.ToLower(query), lowerAllStrings(names))
	m.colPickerFiltered = make([]int, 0, len(matches))
	for _, match := range matches {
		m.colPickerFiltered = append(m.colPickerFiltered, match.Index)
	}
	if m.colPickerSel >= len(m.colPickerFiltered) {
		m.colPickerSel = max(0, len(m.colPickerFiltered)-1)
	}
}

// SetColPickerWindowH stores the full terminal height so the picker's listH
// matches what ColPickerView actually renders.
func (m Model) SetColPickerWindowH(h int) Model { m.colPickerWindowH = h; return m }

// colPickerListH returns the number of visible list rows in the picker panel.
func (m Model) colPickerListH() int {
	h := m.colPickerWindowH
	if h == 0 {
		h = m.height
	}
	listH := h - 8
	if listH < 3 {
		listH = 3
	}
	return listH
}

// colPickerScrollToSel scrolls down only when the selected item goes below the visible window.
func (m *Model) colPickerScrollToSel() {
	listH := m.colPickerListH()
	if m.colPickerSel >= m.colPickerScroll+listH {
		m.colPickerScroll = m.colPickerSel - listH + 1
	}
}

func lowerAllStrings(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = strings.ToLower(s)
	}
	return out
}

// updateColPicker handles keys while the column picker overlay is open.
func (m Model) updateColPicker(msg tea.KeyMsg) (Model, tea.Cmd) {
	rs := m.activeResult()

	// Keys that work in both modes.
	switch msg.String() {
	case "esc":
		m.colPickerOpen = false
		m.colPickerTagged = nil
		return m, nil
	case "enter":
		if len(m.colPickerTagged) > 0 {
			if m.selectedCols == nil {
				m.selectedCols = make(map[int]bool)
			}
			for idx := range m.colPickerTagged {
				if m.selectedCols[idx] {
					delete(m.selectedCols, idx)
				} else {
					m.selectedCols[idx] = true
				}
			}
		} else if len(m.colPickerFiltered) > 0 && m.colPickerSel < len(m.colPickerFiltered) {
			colIdx := m.colPickerFiltered[m.colPickerSel]
			m.cursorCol = colIdx
			if colIdx < m.scrollCol {
				m.scrollCol = colIdx
			} else if rs != nil {
				cw := m.colWidths
				if len(cw) != len(rs.Columns) {
					cw = computeColWidths(*rs)
				}
				_, colEnd := m.visibleColRange(cw)
				if colIdx >= colEnd {
					m.scrollCol = colIdx
				}
			}
		}
		m.colPickerOpen = false
		m.colPickerTagged = nil
		return m, nil
	}

	if m.colPickerSearchActive {
		// Search mode: typing updates the filter; down arrow enters list mode.
		switch msg.String() {
		case "down", "tab":
			m.colPickerSearchActive = false
			return m, nil
		case "backspace":
			if m.colPickerCursor > 0 {
				m.colPickerInput = append(m.colPickerInput[:m.colPickerCursor-1], m.colPickerInput[m.colPickerCursor:]...)
				m.colPickerCursor--
				m.colPickerSel = 0
				m.colPickerScroll = 0
				m.syncColPickerFilter()
			}
		case "delete":
			if m.colPickerCursor < len(m.colPickerInput) {
				m.colPickerInput = append(m.colPickerInput[:m.colPickerCursor], m.colPickerInput[m.colPickerCursor+1:]...)
				m.colPickerSel = 0
				m.colPickerScroll = 0
				m.syncColPickerFilter()
			}
		case "left":
			if m.colPickerCursor > 0 {
				m.colPickerCursor--
			}
		case "right":
			if m.colPickerCursor < len(m.colPickerInput) {
				m.colPickerCursor++
			}
		case "home", "ctrl+a":
			m.colPickerCursor = 0
		case "end", "ctrl+e":
			m.colPickerCursor = len(m.colPickerInput)
		case "ctrl+u":
			m.colPickerInput = nil
			m.colPickerCursor = 0
			m.colPickerSel = 0
			m.colPickerScroll = 0
			m.syncColPickerFilter()
		default:
			if msg.Type == tea.KeyRunes {
				ins := msg.Runes
				m.colPickerInput = append(m.colPickerInput[:m.colPickerCursor], append(ins, m.colPickerInput[m.colPickerCursor:]...)...)
				m.colPickerCursor += len(ins)
				m.colPickerSel = 0
				m.colPickerScroll = 0
				m.syncColPickerFilter()
			}
		}
	} else {
		// List navigation mode: \ returns to search mode.
		switch msg.String() {
		case "\\":
			m.colPickerSearchActive = true
			return m, nil
		case "up", "shift+tab":
			if m.colPickerSel > 0 {
				m.colPickerSel--
				if m.colPickerSel < m.colPickerScroll {
					m.colPickerScroll = m.colPickerSel
				}
			}
		case "down", "tab":
			if m.colPickerSel < len(m.colPickerFiltered)-1 {
				m.colPickerSel++
				m.colPickerScrollToSel()
			}
		case " ":
			if len(m.colPickerFiltered) > 0 && m.colPickerSel < len(m.colPickerFiltered) {
				colIdx := m.colPickerFiltered[m.colPickerSel]
				if m.colPickerTagged == nil {
					m.colPickerTagged = make(map[int]bool)
				}
				if m.colPickerTagged[colIdx] {
					delete(m.colPickerTagged, colIdx)
				} else {
					m.colPickerTagged[colIdx] = true
				}
				if m.colPickerSel < len(m.colPickerFiltered)-1 {
					m.colPickerSel++
					m.colPickerScrollToSel()
				}
			}
		}
	}
	return m, nil
}

// ColPickerView renders the column picker overlay, sized to fit in w×h.
func (m Model) ColPickerView(w, h int) string {
	rs := m.activeResult()

	// Panel sizing: up to 50 wide, up to h-4 tall (leaving space around it).
	panelW := 50
	if w-4 < panelW {
		panelW = w - 4
	}
	if panelW < 24 {
		panelW = 24
	}
	innerW := panelW - 4 // border (2) + padding (2)

	// Number of list rows: h minus title, input, separator, footer, border = h - 8
	listH := h - 8
	if listH < 3 {
		listH = 3
	}
	if rs != nil && len(rs.Columns) < listH {
		listH = len(rs.Columns)
	}

	var colPickerTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ffffff")).Background(lipgloss.Color("#6a0dad")).Padding(0, 1)
	var colPickerBorderStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#6a0dad")).Padding(0, 1)
	var colPickerSelStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#ffffff")).Background(lipgloss.Color("#264f78"))
	var colPickerTagStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#dcdcaa"))
	var colPickerTagSelStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#dcdcaa")).Background(lipgloss.Color("#264f78"))
	var colPickerDimStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	var colPickerHintStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#555555"))

	var sb strings.Builder

	// Title.
	sb.WriteString(colPickerTitleStyle.Render("Column Picker"))
	sb.WriteByte('\n')

	// Search input line — dimmed when in list navigation mode.
	totalCols := 0
	if rs != nil {
		totalCols = len(rs.Columns)
	}
	countStr := colPickerDimStyle.Render(fmt.Sprintf("%d/%d", len(m.colPickerFiltered), totalCols))
	countLen := len(fmt.Sprintf("%d/%d", len(m.colPickerFiltered), totalCols))
	var inputLine string
	if m.colPickerSearchActive {
		prompt := filterPromptStyle.Render("\\ ")
		inputStr := m.renderColPickerInput()
		inputLine = prompt + inputStr
	} else {
		inputLine = colPickerDimStyle.Render("\\ " + string(m.colPickerInput))
	}
	inputLineLen := 2 + len(m.colPickerInput) + 1
	pad := innerW - inputLineLen - countLen
	if pad < 1 {
		pad = 1
	}
	sb.WriteString(inputLine + strings.Repeat(" ", pad) + countStr + "\n")

	// Separator — brighter when list is active.
	sepStyle := colPickerDimStyle
	if !m.colPickerSearchActive {
		sepStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#6a0dad"))
	}
	sb.WriteString(sepStyle.Render(strings.Repeat("─", innerW)) + "\n")

	// List.
	for i := 0; i < listH; i++ {
		idx := m.colPickerScroll + i
		if rs == nil || idx >= len(m.colPickerFiltered) {
			sb.WriteString(strings.Repeat(" ", innerW) + "\n")
			continue
		}
		colIdx := m.colPickerFiltered[idx]
		name := rs.Columns[colIdx].Name
		tagged := m.colPickerTagged[colIdx]
		alreadySel := m.selectedCols[colIdx]
		isSel := idx == m.colPickerSel

		prefix := "  "
		if tagged {
			prefix = "◆ "
		} else if alreadySel {
			prefix = "◈ "
		}
		line := prefix + name
		runes := []rune(line)
		if len(runes) > innerW {
			line = string(runes[:innerW-1]) + "…"
		} else {
			line = padRight(line, innerW)
		}
		switch {
		case isSel && tagged:
			sb.WriteString(colPickerTagSelStyle.Render(line))
		case isSel:
			sb.WriteString(colPickerSelStyle.Render(line))
		case tagged:
			sb.WriteString(colPickerTagStyle.Render(line))
		default:
			sb.WriteString(line)
		}
		sb.WriteByte('\n')
	}

	// Footer hint — changes based on active mode.
	sb.WriteString(colPickerDimStyle.Render(strings.Repeat("─", innerW)) + "\n")
	var hintText string
	if m.colPickerSearchActive {
		hintText = "↓/Tab  enter list  Enter  jump  Esc  close"
	} else {
		hintText = "↑↓  navigate  Space  tag  \\  search  Enter  apply  Esc  close"
	}
	if len([]rune(hintText)) > innerW {
		if m.colPickerSearchActive {
			hintText = "↓ list  Enter jump  Esc close"
		} else {
			hintText = "↑↓ nav  Space tag  \\ search  Enter"
		}
	}
	sb.WriteString(colPickerHintStyle.Render(hintText))

	panel := colPickerBorderStyle.Width(panelW).Render(sb.String())
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, panel)
}

func (m Model) renderColPickerInput() string {
	var sb strings.Builder
	for i, ch := range m.colPickerInput {
		if i == m.colPickerCursor {
			sb.WriteString(cursorStyle.Render(string(ch)))
		} else {
			sb.WriteString(string(ch))
		}
	}
	if m.colPickerCursor == len(m.colPickerInput) {
		sb.WriteString(cursorStyle.Render(" "))
	}
	return sb.String()
}

func (m Model) renderFindBar() string {
	total := len(m.findMatches)
	var countStr string
	if m.findRE != nil {
		if total == 0 {
			countStr = filterErrStyle.Render("  no matches")
		} else {
			cur := m.findCurrent + 1
			countStr = findPromptStyle.Render(fmt.Sprintf("  %d/%d", cur, total))
		}
	}
	prompt := findPromptStyle.Render("/ ") + metaStyle.Render("find: ")
	input := m.renderFindInput()
	if m.findErr != "" {
		errShort := m.findErr
		if runeLen(errShort) > 40 {
			errShort = string([]rune(errShort)[:40]) + "…"
		}
		return prompt + input + "  " + filterErrStyle.Render("✗ "+errShort) + "\n"
	}
	hint := metaStyle.Render("  Enter close  Esc clear  ↑↓ navigate")
	return prompt + input + countStr + hint + "\n"
}

func (m Model) renderFindInput() string {
	var sb strings.Builder
	for i, ch := range m.findInput {
		if i == m.findCursor {
			sb.WriteString(cursorStyle.Render(string(ch)))
		} else {
			sb.WriteString(string(ch))
		}
	}
	if m.findCursor == len(m.findInput) {
		sb.WriteString(cursorStyle.Render(" "))
	}
	return sb.String()
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
	if n := len(m.selectedCols); n > 0 {
		metaText += filterActiveStyle.Render(fmt.Sprintf("  ◆ %d cols", n))
	}
	if m.pinnedResult != nil {
		if m.diffMode {
			metaText += filterActiveStyle.Render("  📌 DIFF")
		} else {
			metaText += filterActiveStyle.Render("  📌 pinned")
		}
	}
	if m.findRE != nil {
		if n := len(m.findMatches); n > 0 {
			metaText += findPromptStyle.Render(fmt.Sprintf("  🔍 %d/%d", m.findCurrent+1, n))
		} else {
			metaText += filterErrStyle.Render("  🔍 no matches")
		}
	}
	meta := metaStyle.Render(metaText)

	var grid string
	if m.diffMode && len(m.diffRows) > 0 {
		grid = m.renderDiffGrid(rs)
	} else {
		gridRS, gridRows, allSel := m.gridData()
		grid = m.renderGrid(gridRS, gridRows, allSel)
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
			Background(lipgloss.Color("#6a0dad")).
			Foreground(lipgloss.Color("#ffffff")).
			Bold(true)
	}
	title := sty.Render("RESULTS")
	if m.showSelectedOnly {
		badge := lipgloss.NewStyle().
			Padding(0, 1).
			Background(lipgloss.Color("#1a6b1a")).
			Foreground(lipgloss.Color("#4ec9b0")).
			Bold(true).
			Render("SELECTED ONLY")
		title += " " + badge
	}
	return title + "\n"
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

// gridData returns the QueryResult and rows to render, filtered to the
// selected columns and/or tagged rows when showSelectedOnly is true.
// allSelected is true when columns were filtered (all shown cols are "selected").
func (m Model) gridData() (rs db.QueryResult, rows [][]any, allSelected bool) {
	orig := m.activeResult()
	if orig == nil {
		return db.QueryResult{}, nil, false
	}
	rows = m.activeRows()

	filterCols := m.showSelectedOnly && len(m.selectedCols) > 0
	filterRows := m.showSelectedOnly && len(m.tags) > 0

	if !filterCols && !filterRows {
		return *orig, rows, false
	}

	// Build column map: newIdx → origIdx.
	var colMap []int
	if filterCols {
		for i := range orig.Columns {
			if m.selectedCols[i] {
				colMap = append(colMap, i)
			}
		}
	}
	if len(colMap) == 0 {
		colMap = make([]int, len(orig.Columns))
		for i := range colMap {
			colMap[i] = i
		}
		filterCols = false
	}

	filtCols := make([]db.Column, len(colMap))
	for i, oi := range colMap {
		filtCols[i] = orig.Columns[oi]
	}

	// Filter rows.
	if filterRows {
		var filtered [][]any
		for i, row := range rows {
			if m.tags[i] {
				filtered = append(filtered, row)
			}
		}
		rows = filtered
	}

	// Remap row data to filtered columns.
	if filterCols {
		mapped := make([][]any, len(rows))
		for ri, row := range rows {
			nr := make([]any, len(colMap))
			for ni, oi := range colMap {
				if oi < len(row) {
					nr[ni] = row[oi]
				}
			}
			mapped[ri] = nr
		}
		rows = mapped
	}

	rs = db.QueryResult{
		Columns:  filtCols,
		Rows:     rows,
		Duration: orig.Duration,
	}
	return rs, rows, filterCols
}

var rowNumStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#555555"))

func (m Model) renderGrid(rs db.QueryResult, rows [][]any, allColsSelected bool) string {
	if len(rs.Columns) == 0 {
		return metaStyle.Render("  (no columns)")
	}

	// Row-number column: width is the digit count of the last visible row + 1 for the "#" header.
	rowNumW := 0
	if m.showRowNums {
		rowNumW = len(fmt.Sprintf("%d", len(rows)))
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
	// In selected-only mode all visible rows are tagged, so no gutter needed.
	showTagGutter := !m.showSelectedOnly && (len(m.tags) > 0 || m.tagRange)
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
		// Selected columns get a "◆ " prefix (2 chars) — expand width so the name never truncates.
		if allColsSelected || m.selectedCols[i] {
			widths[i] += 2
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
		// Prefix for selected columns — width was already expanded by 2 to fit it.
		prefix := ""
		if allColsSelected || m.selectedCols[i] {
			prefix = "◆ "
		}
		if runeLen(prefix+name) > widths[i] {
			avail := widths[i] - runeLen(prefix) - 1
			if avail < 1 {
				avail = 1
			}
			name = string([]rune(name)[:avail]) + "…"
		}
		cell := padRight(prefix+name, widths[i])
		sty := headerStyle
		if allColsSelected || m.selectedCols[i] {
			sty = headerSelectedStyle
		}
		if m.hasFilter(i) {
			sty = sty.Underline(true)
		}
		sb.WriteString(sty.Render(" " + cell + " "))
		if i < colEnd-1 {
			sep := headerStyle
			if allColsSelected || m.selectedCols[i] || m.selectedCols[i+1] {
				sep = headerSelectedStyle
			}
			sb.WriteString(sep.Render("│"))
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
			isFindCurrent := m.findCurrent >= 0 && m.findCurrent < len(m.findMatches) &&
				m.findMatches[m.findCurrent][0] == absRow && m.findMatches[m.findCurrent][1] == i
			isFindMatch := !isFindCurrent && m.findRE != nil && m.findRE.MatchString(formatCellRaw(val))
			switch {
			case isCursor && val == nil:
				sb.WriteString(cursorNullStyle.Render(" " + padded + " "))
			case isCursor:
				sb.WriteString(cursorStyle.Render(" " + padded + " "))
			case isFindCurrent:
				sb.WriteString(findCurrentMatchStyle.Render(" " + padded + " "))
			case isFindMatch:
				sb.WriteString(findMatchStyle.Render(" " + padded + " "))
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
	if m.findOpen {
		sb.WriteString(m.renderFindBar())
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
	m.selectedCols = nil
	m.showSelectedOnly = false
	m.clearFind()
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
	if m.findOpen {
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
