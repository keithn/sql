// Package rowedit provides a multi-field row edit form overlay.
// Pressing Ctrl+S generates a multi-column UPDATE statement for all
// modified fields and emits SubmittedMsg; Esc emits CancelledMsg.
package rowedit

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sqltui/sql/internal/db"
	"github.com/sqltui/sql/internal/ui/editor/vim"
)

// SubmittedMsg is sent when the user confirms the edit with Ctrl+S.
type SubmittedMsg struct {
	Updates []FieldUpdate
	// AllColumns and Row carry the full row context for UPDATE generation.
	AllColumns []db.Column
	Row        []interface{}
}

// FieldUpdate represents a single modified field.
type FieldUpdate struct {
	ColIndex int
	ColName  string
	NewValue string
	SetNull  bool
}

// CancelledMsg is sent when the user cancels with Esc.
type CancelledMsg struct{}

// ─── styles ──────────────────────────────────────────────────────────────────

var (
	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#007acc")).
			Padding(0, 1)

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#4ec9b0"))

	hintStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#808080"))

	labelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#9cdcfe"))

	labelReadonlyStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#555555"))

	valueStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#d4d4d4"))

	valueReadonlyStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#555555"))

	modifiedStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#ce9178"))

	typeTagStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#808080"))

	activeBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("#007acc")).
				Padding(0, 0)

	modeStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#dcdcaa"))

	cursorNormalStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#007acc")).
				Foreground(lipgloss.Color("#ffffff"))

	cursorInsertStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#d4d4d4")).
				Underline(true)
)

// ─── field state ─────────────────────────────────────────────────────────────

type fieldState struct {
	name     string
	typeStr  string
	original string
	current  string
	readOnly bool
	setNull  bool
}

func (f fieldState) modified() bool {
	return f.current != f.original || f.setNull
}

// ─── Model ───────────────────────────────────────────────────────────────────

// Model is the row edit form overlay.
type Model struct {
	active     bool
	fields     []fieldState
	allCols    []db.Column  // full column list for UPDATE generation
	allRow     []interface{} // original row values

	cursor    int // index of focused field
	scrollTop int // index of first visible field row

	vimEnabled bool
	vs         *vim.State
	ta         textarea.Model

	width  int
	height int
	taW    int // textarea inner width
	taH    int // textarea height (fixed at 1 for form)
}

// New returns an inactive Model.
func New() Model {
	ta := textarea.New()
	ta.Prompt = ""
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.SetWidth(60)
	ta.SetHeight(1)
	return Model{ta: ta, taH: 1}
}

// Active reports whether the form is open.
func (m Model) Active() bool { return m.active }

// SetVimMode configures vim keybindings.
func (m Model) SetVimMode(on bool) Model {
	m.vimEnabled = on
	return m
}

// SetSize adjusts the overlay to fit within (w, h).
func (m Model) SetSize(w, h int) Model {
	overlayW := w - 6
	if overlayW > 120 {
		overlayW = 120
	}
	if overlayW < 40 {
		overlayW = 40
	}
	m.width = overlayW
	m.height = h - 6
	if m.height < 8 {
		m.height = 8
	}
	// textarea width: overlay - borders(2) - padding(2) - label column - gutter
	m.taW = overlayW - 4 - labelColWidth - 4
	if m.taW < 10 {
		m.taW = 10
	}
	m.ta.SetWidth(m.taW)
	m.ta.SetHeight(m.taH)
	if m.vs != nil {
		m.vs.SetSize(m.taW, m.taH)
	}
	return m
}

const labelColWidth = 20 // max width of the column-name label column

// Open opens the row edit form for the given row context.
// columns and row come from results.CellContext.Columns / .Row.
func (m Model) Open(columns []db.Column, row []interface{}, vimEnabled bool) (Model, tea.Cmd) {
	m.active = true
	m.vimEnabled = vimEnabled
	m.allCols = columns
	m.allRow = row

	m.fields = make([]fieldState, len(columns))
	for i, col := range columns {
		val := ""
		if i < len(row) && row[i] != nil {
			val = formatDisplayValue(row[i])
		}
		m.fields[i] = fieldState{
			name:     col.Name,
			typeStr:  col.Type,
			original: val,
			current:  val,
			readOnly: isReadOnly(col),
		}
	}
	m.cursor = 0
	m.scrollTop = 0
	// Advance cursor to first editable field.
	for m.cursor < len(m.fields) && m.fields[m.cursor].readOnly {
		m.cursor++
	}
	if m.cursor >= len(m.fields) {
		m.cursor = 0 // all readonly — still allow navigation
	}
	return m.loadActiveField()
}

// formatDisplayValue converts a raw driver value to a human-readable string.
func formatDisplayValue(v interface{}) string {
	if b, ok := v.([]byte); ok {
		if len(b) == 16 {
			// MSSQL uniqueidentifier: mixed-endian UUID bytes.
			return fmt.Sprintf("%02x%02x%02x%02x-%02x%02x-%02x%02x-%02x%02x-%02x%02x%02x%02x%02x%02x",
				b[3], b[2], b[1], b[0],
				b[5], b[4],
				b[7], b[6],
				b[8], b[9],
				b[10], b[11], b[12], b[13], b[14], b[15])
		}
		return string(b)
	}
	return fmt.Sprintf("%v", v)
}

// isReadOnly returns true for PK/identity/computed columns.
func isReadOnly(col db.Column) bool {
	t := strings.ToLower(col.Type)
	return strings.Contains(t, "identity") ||
		strings.Contains(t, "computed") ||
		strings.Contains(t, "rowversion") ||
		strings.Contains(t, "timestamp")
}

// Close closes the form.
func (m Model) Close() Model {
	m.active = false
	m.vs = nil
	m.ta.Reset()
	return m
}

// loadActiveField saves the current field and re-initialises the editor for the cursor field.
func (m Model) loadActiveField() (Model, tea.Cmd) {
	if m.cursor >= len(m.fields) {
		return m, nil
	}
	val := m.fields[m.cursor].current
	if m.fields[m.cursor].setNull {
		val = ""
	}
	if m.vimEnabled {
		m.vs = vim.NewState()
		m.vs.SetSize(m.taW, m.taH)
		m.vs.Buf.SetValue(val)
		m.vs.Mode = vim.ModeInsert
		return m, nil
	}
	m.vs = nil
	m.ta.Reset()
	m.ta.SetWidth(m.taW)
	m.ta.SetHeight(m.taH)
	m.ta.SetValue(val)
	return m, m.ta.Focus()
}

// saveActiveField writes the editor's current content back to fields[cursor].
// setNull is only cleared if the user has typed a non-empty value; if the field
// was set to null with Ctrl+D and the editor is still empty, setNull is preserved.
func (m *Model) saveActiveField() {
	if m.cursor >= len(m.fields) {
		return
	}
	var newVal string
	if m.vimEnabled && m.vs != nil {
		newVal = m.vs.Buf.Value()
	} else {
		newVal = m.ta.Value()
	}
	m.fields[m.cursor].current = newVal
	if newVal != "" {
		m.fields[m.cursor].setNull = false
	}
}

// ─── Update ──────────────────────────────────────────────────────────────────

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if !m.active {
		return m, nil
	}
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		if !m.vimEnabled {
			var cmd tea.Cmd
			m.ta, cmd = m.ta.Update(msg)
			return m, cmd
		}
		return m, nil
	}

	switch key.String() {
	case "ctrl+s":
		m.saveActiveField()
		return m.submit()

	case "ctrl+d":
		// Set active field to NULL.
		if m.cursor < len(m.fields) && !m.fields[m.cursor].readOnly {
			m.fields[m.cursor].current = ""
			m.fields[m.cursor].setNull = true
			// Clear the editor.
			if m.vimEnabled && m.vs != nil {
				m.vs.Buf.SetValue("")
			} else {
				m.ta.SetValue("")
			}
		}
		return m, nil

	case "tab", "down":
		m.saveActiveField()
		m.cursor = m.nextEditable(m.cursor)
		m.clampScroll()
		return m.loadActiveField()

	case "shift+tab", "up":
		m.saveActiveField()
		m.cursor = m.prevEditable(m.cursor)
		m.clampScroll()
		return m.loadActiveField()
	}

	if m.vimEnabled && m.vs != nil {
		k := key.String()
		if k == "esc" {
			if m.vs.Mode == vim.ModeInsert {
				m.vs.HandleKey("esc")
				return m, nil
			}
			// Esc in normal mode → cancel form.
			return m.Close(), func() tea.Msg { return CancelledMsg{} }
		}
		m.vs.HandleKey(k)
		return m, nil
	}

	if key.String() == "esc" {
		return m.Close(), func() tea.Msg { return CancelledMsg{} }
	}

	var cmd tea.Cmd
	m.ta, cmd = m.ta.Update(msg)
	return m, cmd
}

func (m Model) submit() (Model, tea.Cmd) {
	var updates []FieldUpdate
	for i, f := range m.fields {
		if f.readOnly || !f.modified() {
			continue
		}
		u := FieldUpdate{ColIndex: i, ColName: f.name}
		if f.setNull {
			u.SetNull = true
		} else {
			u.NewValue = f.current
		}
		updates = append(updates, u)
	}
	allCols := m.allCols
	allRow := m.allRow
	return m.Close(), func() tea.Msg {
		return SubmittedMsg{Updates: updates, AllColumns: allCols, Row: allRow}
	}
}

// nextEditable returns the index of the next editable field after cur (wraps).
func (m Model) nextEditable(cur int) int {
	n := len(m.fields)
	for i := 1; i <= n; i++ {
		next := (cur + i) % n
		if !m.fields[next].readOnly {
			return next
		}
	}
	return cur
}

// prevEditable returns the index of the previous editable field (wraps).
func (m Model) prevEditable(cur int) int {
	n := len(m.fields)
	for i := 1; i <= n; i++ {
		prev := (cur - i + n) % n
		if !m.fields[prev].readOnly {
			return prev
		}
	}
	return cur
}

// visibleFieldCount returns how many field rows fit in the overlay.
func (m Model) visibleFieldCount() int {
	// overlay height minus: title(1) + blank(1) + hint(1) + blank(1) + border(2) = 6
	h := m.height - 6
	if h < 2 {
		h = 2
	}
	return h
}

// clampScroll ensures the cursor field is within the visible window.
func (m *Model) clampScroll() {
	vis := m.visibleFieldCount()
	if m.cursor < m.scrollTop {
		m.scrollTop = m.cursor
	}
	if m.cursor >= m.scrollTop+vis {
		m.scrollTop = m.cursor - vis + 1
	}
	if m.scrollTop < 0 {
		m.scrollTop = 0
	}
}

// ─── View ────────────────────────────────────────────────────────────────────

func (m Model) View() string {
	if !m.active {
		return ""
	}

	modCount := 0
	for _, f := range m.fields {
		if !f.readOnly && f.modified() {
			modCount++
		}
	}

	title := titleStyle.Render("Row Edit")
	if modCount > 0 {
		title += modifiedStyle.Render(fmt.Sprintf("  %d modified", modCount))
	}

	vis := m.visibleFieldCount()
	end := m.scrollTop + vis
	if end > len(m.fields) {
		end = len(m.fields)
	}

	var rows []string
	for i := m.scrollTop; i < end; i++ {
		rows = append(rows, m.renderField(i))
	}
	// Scroll indicator.
	if len(m.fields) > vis {
		indicator := fmt.Sprintf("%d/%d", m.cursor+1, len(m.fields))
		rows = append(rows, hintStyle.Render("  "+indicator))
	}

	body := strings.Join(rows, "\n")

	hint := m.renderHint()

	inner := strings.Join([]string{title, "", body, "", hint}, "\n")
	innerW := m.width - 4
	if innerW < 20 {
		innerW = 20
	}
	return panelStyle.Width(innerW).Render(inner)
}

func (m Model) renderField(i int) string {
	f := m.fields[i]
	active := i == m.cursor

	// Modified marker.
	marker := "  "
	if !f.readOnly && f.modified() {
		marker = modifiedStyle.Render("* ")
	}

	// Column label (truncated/padded).
	label := f.name
	if len(label) > labelColWidth {
		label = label[:labelColWidth-1] + "…"
	}
	labelPad := fmt.Sprintf("%-*s", labelColWidth, label)
	var labelRendered string
	if f.readOnly {
		labelRendered = labelReadonlyStyle.Render(labelPad)
	} else {
		labelRendered = labelStyle.Render(labelPad)
	}

	// Type tag.
	typeTag := ""
	if f.readOnly {
		tag := "RO"
		if strings.Contains(strings.ToLower(f.typeStr), "identity") {
			tag = "ID"
		}
		typeTag = " " + typeTagStyle.Render("["+tag+"]")
	}

	// Value portion.
	var valueStr string
	if active && !f.readOnly {
		valueStr = m.renderActiveEditor()
	} else {
		v := f.current
		if f.setNull {
			v = "∅"
		}
		// Truncate preview to available width.
		maxV := m.taW
		if maxV < 10 {
			maxV = 10
		}
		runes := []rune(v)
		if len(runes) > maxV {
			v = string(runes[:maxV-1]) + "…"
		}
		if f.readOnly {
			valueStr = valueReadonlyStyle.Render(v)
		} else {
			valueStr = valueStyle.Render(v)
		}
	}

	return marker + labelRendered + "  " + valueStr + typeTag
}

func (m Model) renderActiveEditor() string {
	if m.vimEnabled && m.vs != nil {
		return m.renderVimLine()
	}
	return m.ta.View()
}

// renderVimLine renders a single-line vim buffer for the active field.
func (m Model) renderVimLine() string {
	if m.vs == nil {
		return ""
	}
	buf := m.vs.Buf
	row := buf.CursorRow()
	line := buf.Line(row)
	col := buf.CursorCol()

	cs := cursorNormalStyle
	if m.vs.Mode == vim.ModeInsert {
		cs = cursorInsertStyle
	}

	w := m.taW
	if w < 1 {
		w = 1
	}

	// Truncate line to visible width.
	if len(line) > w {
		line = line[:w]
	}

	var sb strings.Builder
	for c, ch := range line {
		if c == col {
			sb.WriteString(cs.Render(string(ch)))
		} else {
			sb.WriteString(valueStyle.Render(string(ch)))
		}
	}
	// Cursor past end.
	if col >= len(line) && len(line) < w {
		sb.WriteString(cs.Render(" "))
	}
	// Pad.
	vis := len(line)
	if col >= len(line) && len(line) < w {
		vis++
	}
	if vis < w {
		sb.WriteString(strings.Repeat(" ", w-vis))
	}
	return sb.String()
}

func (m Model) renderHint() string {
	if m.vimEnabled && m.vs != nil {
		mode := "NORMAL"
		if m.vs.Mode == vim.ModeInsert {
			mode = "INSERT"
		}
		return modeStyle.Render(mode) + hintStyle.Render("  Tab next  •  Ctrl+S save  •  Ctrl+D null  •  Esc cancel/exit")
	}
	return hintStyle.Render("Tab next  •  Shift+Tab prev  •  Ctrl+S save  •  Ctrl+D null  •  Esc cancel  •  * modified")
}
