package editor

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/sqltui/sql/internal/config"
)

// ExecuteBlockMsg asks the app to run the logical block under the cursor.
type ExecuteBlockMsg struct{ SQL string }

// ExecuteBufferMsg asks the app to run the full buffer.
type ExecuteBufferMsg struct{ SQL string }

// NewTabMsg asks the app to create a new query file and add a tab.
type NewTabMsg struct{}

// TabInfo carries metadata about a tab for session persistence.
type TabInfo struct {
	Path  string
	Title string
}

// TabState is used to restore tabs from saved session data.
type TabState struct {
	Path    string
	Content string
}

// Tab is a single open query buffer.
type Tab struct {
	Title string
	Path  string
	ta    textarea.Model
	Dirty bool
}

// Model is the editor pane: a tab bar + textarea.
type Model struct {
	cfg         *config.Config
	width       int
	height      int // total height including tab bar
	tabs        []Tab
	active      int
	focused     bool
	popup       completionPopup
	schemaNames []string // table/column names pushed from the app
}

// styles
var (
	tabBarStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#1e1e1e"))

	activeTabStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#ffffff")).
			Background(lipgloss.Color("#007acc")).
			Padding(0, 1)

	inactiveTabStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#888888")).
				Background(lipgloss.Color("#2d2d2d")).
				Padding(0, 1)

	editorBorderFocused = lipgloss.NewStyle().
				Border(lipgloss.NormalBorder(), false, false, true, false).
				BorderForeground(lipgloss.Color("#007acc"))

	editorBorderBlurred = lipgloss.NewStyle().
				Border(lipgloss.NormalBorder(), false, false, true, false).
				BorderForeground(lipgloss.Color("#333333"))

	popupStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#555555")).
			Background(lipgloss.Color("#1e1e1e")).
			Padding(0, 1)

	popupItemStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#cccccc")).
			Background(lipgloss.Color("#1e1e1e"))

	popupSelectedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#ffffff")).
				Background(lipgloss.Color("#007acc")).
				Bold(true)
)

func newTab(title string, theme config.ThemeConfig) Tab {
	ta := textarea.New()
	ta.Placeholder = "Enter SQL query…"
	ta.ShowLineNumbers = true
	ta.CharLimit = 0 // unlimited
	ta.SetWidth(80)
	ta.SetHeight(20)
	// Remove the default key bindings we override at the app level.
	ta.KeyMap.InsertNewline.SetKeys("enter")
	applyLineNumberTheme(&ta, theme)
	return Tab{Title: title, ta: ta}
}

// applyLineNumberTheme sets the textarea gutter colours from the theme.
func applyLineNumberTheme(ta *textarea.Model, theme config.ThemeConfig) {
	ln := lipgloss.NewStyle().Foreground(lipgloss.Color(theme.LineNumber))
	cln := lipgloss.NewStyle().Foreground(lipgloss.Color(theme.CursorLineNumber))
	ta.FocusedStyle.LineNumber = ln
	ta.FocusedStyle.CursorLineNumber = cln
	ta.BlurredStyle.LineNumber = ln
	ta.BlurredStyle.CursorLineNumber = cln
}

// New creates the editor model with one blank tab.
func New(cfg *config.Config) Model {
	return Model{
		cfg:     cfg,
		tabs:    []Tab{newTab("query1.sql", cfg.Theme)},
		focused: true, // editor has focus on startup
	}
}

func (m Model) Init() tea.Cmd {
	cmd := m.tabs[0].ta.Focus()
	return cmd
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if !m.focused {
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Popup navigation takes priority over everything except hard exits.
		if m.popup.visible {
			switch msg.String() {
			case "esc":
				m.popup.visible = false
				return m, nil
			case "up", "ctrl+p":
				if m.popup.selected > 0 {
					m.popup.selected--
				}
				return m, nil
			case "down":
				if m.popup.selected < len(m.popup.items)-1 {
					m.popup.selected++
				}
				return m, nil
			case "tab", "shift+tab":
				// Tab cycles through items; shift+tab goes back.
				if msg.String() == "shift+tab" {
					if m.popup.selected > 0 {
						m.popup.selected--
					}
				} else {
					if m.popup.selected < len(m.popup.items)-1 {
						m.popup.selected++
					} else {
						m.popup.selected = 0
					}
				}
				return m, nil
			case "enter", "ctrl+e":
				// Accept current completion.
				m = m.acceptCompletion()
				// ctrl+e with popup: just accept, don't also execute.
				return m, nil
			}
			// A non-word character closes the popup before routing.
			if len(msg.String()) == 1 && !isWordRune(rune(msg.String()[0])) {
				m.popup.visible = false
			}
		} else {
			// Global shortcuts (no popup).
			switch msg.String() {
			case "ctrl+e":
				sql := m.blockAtCursor()
				return m, func() tea.Msg { return ExecuteBlockMsg{SQL: sql} }
			case "f5":
				sql := m.tabs[m.active].ta.Value()
				return m, func() tea.Msg { return ExecuteBufferMsg{SQL: sql} }
			case "ctrl+n":
				return m, func() tea.Msg { return NewTabMsg{} }
			case "ctrl+w":
				m.closeTab(m.active)
				return m, nil
			case "ctrl+pgdown", "alt+l":
				m.active = (m.active + 1) % len(m.tabs)
				return m, nil
			case "ctrl+pgup", "alt+h":
				m.active = (m.active - 1 + len(m.tabs)) % len(m.tabs)
				return m, nil
			case "tab":
				// Tab with no popup: insert spaces.
				m.tabs[m.active].ta.InsertString("    ")
				return m, nil
			}
		}
	}

	// Route remaining messages to the active textarea, auto-saving on change.
	var cmd tea.Cmd
	oldVal := m.tabs[m.active].ta.Value()
	m.tabs[m.active].ta, cmd = m.tabs[m.active].ta.Update(msg)
	newVal := m.tabs[m.active].ta.Value()

	// Auto-save if content changed.
	var saveCmd tea.Cmd
	if newVal != oldVal && m.tabs[m.active].Path != "" {
		path := m.tabs[m.active].Path
		saveCmd = func() tea.Msg {
			_ = os.WriteFile(path, []byte(newVal), 0644)
			return nil
		}
	}

	// Update popup completions after content change.
	if newVal != oldVal {
		m.updatePopup()
	}

	return m, tea.Batch(cmd, saveCmd)
}

func (m Model) View() string {
	tabBar := m.renderTabBar()
	content := m.renderContent()
	base := lipgloss.JoinVertical(lipgloss.Left, tabBar, content)

	if !m.popup.visible || len(m.popup.items) == 0 {
		return base
	}

	popup := m.renderPopup()
	li := m.tabs[m.active].ta.LineInfo()
	cursorLine := m.tabs[m.active].ta.Line()

	// Row: 1 (tab bar) + cursor logical line + 1 (place below the cursor line).
	// ta.Line() is the 0-indexed logical line; li.RowOffset is the wrap offset
	// within the current line (0 for non-wrapped lines, which is typical for SQL).
	popupRow := 1 + cursorLine + li.RowOffset + 1

	// Col: cursor character offset + estimated line-number gutter width.
	// The gutter renders as " N │" so width ≈ digits(lineCount) + 2.
	lineCount := strings.Count(m.tabs[m.active].ta.Value(), "\n") + 1
	gutterW := len(fmt.Sprintf("%d", lineCount)) + 2
	popupCol := li.CharOffset + gutterW

	popupLines := strings.Split(popup, "\n")
	// If popup would overflow the editor bottom, flip it above the cursor.
	if popupRow+len(popupLines) > m.height {
		popupRow = 1 + cursorLine + li.RowOffset - len(popupLines)
	}
	if popupRow < 0 {
		popupRow = 0
	}
	return overlayView(base, popup, popupRow, popupCol)
}

// renderContent returns the syntax-highlighted textarea content.
// It post-processes the textarea's rendered output, replacing each line's
// text portion with chroma-highlighted SQL. The gutter (prompt + line number)
// is left unchanged so line numbers, borders, and colors are preserved.
//
// Gutter layout (bubbles textarea defaults, MaxHeight=99):
//
//	"┃ "  (prompt, 2 visual cols)  +  " %2d " (line number, 4 visual cols) = 6 cols total
func (m Model) renderContent() string {
	ta := &m.tabs[m.active].ta
	taView := ta.View()
	text := ta.Value()
	if strings.TrimSpace(text) == "" {
		return taView
	}

	const gutterW = 6 // visual columns: 2 (prompt) + 4 (line number field)

	hlLines := highlightLines(text)
	cursorLine := ta.Line()
	cursorCol := ta.LineInfo().CharOffset

	taLines := strings.Split(taView, "\n")
	result := make([]string, len(taLines))

	for i, line := range taLines {
		stripped := xansi.Strip(line)
		runes := []rune(stripped)

		if len(runes) < gutterW {
			result[i] = line
			continue
		}

		// Extract line number from the stripped gutter: runes[2:6] = " N " field.
		lineNumStr := strings.TrimSpace(string(runes[2:gutterW]))
		lineNum, err := strconv.Atoi(lineNumStr)
		if err != nil || lineNum <= 0 {
			// Wrapped continuation line or empty filler — pass through unchanged.
			result[i] = line
			continue
		}

		srcLine := lineNum - 1

		// Preserve the ANSI-coded gutter prefix.
		gutterPrefix := xansi.Truncate(line, gutterW, "")

		// Select highlighted text for this source line.
		var hlText string
		if srcLine < len(hlLines) {
			hlText = hlLines[srcLine]
		} else {
			// Fallback: raw text from the rendered line.
			if gutterW < len(runes) {
				hlText = string(runes[gutterW:])
			}
		}

		// Inject the virtual cursor on the cursor line.
		if srcLine == cursorLine && m.focused {
			hlText = injectCursor(hlText, cursorCol)
		}

		result[i] = gutterPrefix + hlText
	}

	return strings.Join(result, "\n")
}

// overlayView paints fg (the popup) over bg at (startRow, startCol), both
// 0-indexed. startCol is a visual column count; ANSI sequences are handled
// correctly by muesli/ansi.Truncate so styled background lines aren't broken.
func overlayView(bg, fg string, startRow, startCol int) string {
	bgLines := strings.Split(bg, "\n")
	fgLines := strings.Split(fg, "\n")
	for i, fgLine := range fgLines {
		r := startRow + i
		if r < 0 || r >= len(bgLines) {
			continue
		}
		left := xansi.Truncate(bgLines[r], startCol, "")
		bgLines[r] = left + fgLine
	}
	return strings.Join(bgLines, "\n")
}

func (m Model) renderTabBar() string {
	// Label on the left identifies the pane and shows focus state.
	labelStyle := lipgloss.NewStyle().
		Padding(0, 1).
		Background(lipgloss.Color("#333333")).
		Foreground(lipgloss.Color("#666666"))
	if m.focused {
		labelStyle = labelStyle.
			Background(lipgloss.Color("#007acc")).
			Foreground(lipgloss.Color("#ffffff")).
			Bold(true)
	}
	label := labelStyle.Render("EDITOR")

	var tabs []string
	for i, t := range m.tabs {
		title := t.Title
		if t.Dirty {
			title += " •"
		}
		if i == m.active && m.focused {
			tabs = append(tabs, activeTabStyle.Render(title))
		} else if i == m.active {
			// Active tab but pane is blurred: subtle highlight so you know
			// which tab will be active when you return focus.
			tabs = append(tabs, lipgloss.NewStyle().
				Foreground(lipgloss.Color("#aaaaaa")).
				Background(lipgloss.Color("#2d2d2d")).
				Padding(0, 1).Render(title))
		} else {
			tabs = append(tabs, inactiveTabStyle.Render(title))
		}
	}
	bar := label + strings.Join(tabs, "")
	bar = tabBarStyle.Width(m.width).Render(bar)
	return bar
}

func (m Model) SetSize(w, h int) Model {
	m.width = w
	m.height = h
	m.syncTextareaHeight()
	for i := range m.tabs {
		m.tabs[i].ta.SetWidth(w)
	}
	return m
}

// syncTextareaHeight sets the textarea height to fill the editor minus the tab bar.
func (m *Model) syncTextareaHeight() {
	taH := m.height - 1 // subtract tab bar row
	if taH < 1 {
		taH = 1
	}
	for i := range m.tabs {
		m.tabs[i].ta.SetHeight(taH)
	}
}

// Focus gives the editor keyboard focus.
func (m Model) Focus() (Model, tea.Cmd) {
	m.focused = true
	cmd := m.tabs[m.active].ta.Focus()
	return m, cmd
}

// Blur removes keyboard focus from the editor.
func (m Model) Blur() Model {
	m.focused = false
	m.tabs[m.active].ta.Blur()
	return m
}

// Value returns the active tab's text content.
func (m Model) Value() string {
	return m.tabs[m.active].ta.Value()
}

// AddTab adds a new tab backed by path and returns the updated model.
// If content is non-empty it is loaded into the textarea.
func (m Model) AddTab(path, content string) Model {
	title := filepath.Base(path)
	if title == "" || title == "." {
		title = fmt.Sprintf("query%d.sql", len(m.tabs)+1)
	}
	t := newTab(title, m.cfg.Theme)
	t.Path = path
	t.ta.SetWidth(m.width)
	t.ta.SetHeight(m.height - 1)
	if content != "" {
		t.ta.SetValue(content)
	}
	m.tabs = append(m.tabs, t)
	m.active = len(m.tabs) - 1
	return m
}

// SetTabs replaces all open tabs with the provided states.
// Width/height are applied from the current model dimensions.
func (m Model) SetTabs(tabs []TabState) Model {
	if len(tabs) == 0 {
		return m
	}
	m.tabs = nil
	for _, ts := range tabs {
		title := filepath.Base(ts.Path)
		if title == "" || title == "." {
			title = "query.sql"
		}
		t := newTab(title, m.cfg.Theme)
		t.Path = ts.Path
		t.ta.SetWidth(m.width)
		t.ta.SetHeight(m.height - 1)
		if ts.Content != "" {
			t.ta.SetValue(ts.Content)
		}
		m.tabs = append(m.tabs, t)
	}
	m.active = 0
	// If the editor is focused, focus the active textarea so it accepts input.
	if m.focused {
		_ = m.tabs[0].ta.Focus()
	}
	return m
}

// SetActiveTab sets the active tab index (clamped to valid range).
func (m Model) SetActiveTab(idx int) Model {
	if idx >= 0 && idx < len(m.tabs) {
		m.active = idx
	}
	return m
}

// TabsInfo returns path/title metadata for all open tabs.
func (m Model) TabsInfo() []TabInfo {
	info := make([]TabInfo, len(m.tabs))
	for i, t := range m.tabs {
		info[i] = TabInfo{Path: t.Path, Title: t.Title}
	}
	return info
}

// ActiveTabIdx returns the index of the currently active tab.
func (m Model) ActiveTabIdx() int {
	return m.active
}

// SetSchemaNames provides table/column names for schema-aware completion.
func (m Model) SetSchemaNames(names []string) Model {
	m.schemaNames = names
	return m
}

// updatePopup recomputes completions based on the word at the current cursor.
func (m *Model) updatePopup() {
	ta := &m.tabs[m.active].ta
	lines := strings.Split(ta.Value(), "\n")
	lineNum := ta.Line()
	lineText := ""
	if lineNum < len(lines) {
		lineText = lines[lineNum]
	}
	col := ta.LineInfo().CharOffset
	word := wordBefore(lineText, col)

	items := getCompletions(word, m.schemaNames, 8)
	if len(items) == 0 || word == "" {
		m.popup.visible = false
		return
	}

	m.popup = completionPopup{
		items:    items,
		selected: 0,
		visible:  true,
		word:     word,
	}
}

// acceptCompletion replaces the typed word with the selected completion.
// It deletes the typed word via backspaces then inserts the full completion,
// preserving lowercase style when the typed word was all-lowercase.
func (m Model) acceptCompletion() Model {
	if !m.popup.visible || m.popup.selected >= len(m.popup.items) {
		return m
	}
	completion := m.popup.items[m.popup.selected]
	word := m.popup.word

	// Delete the typed word one character at a time using the textarea's own
	// backspace handler so cursor state stays consistent.
	for range []rune(word) {
		m.tabs[m.active].ta, _ = m.tabs[m.active].ta.Update(
			tea.KeyMsg{Type: tea.KeyBackspace},
		)
	}

	// Match case: all-lowercase typed word → lowercase completion.
	insert := completion
	if word == strings.ToLower(word) {
		insert = strings.ToLower(completion)
	}
	m.tabs[m.active].ta.InsertString(insert)

	m.popup.visible = false
	return m
}

// renderPopup renders the completion dropdown.
func (m Model) renderPopup() string {
	const maxVisible = 8
	items := m.popup.items
	if len(items) > maxVisible {
		items = items[:maxVisible]
	}

	// Compute width from longest item.
	maxW := 12
	for _, it := range items {
		if len(it) > maxW {
			maxW = len(it)
		}
	}

	var rows []string
	for i, it := range items {
		label := fmt.Sprintf("%-*s", maxW, it)
		if i == m.popup.selected {
			rows = append(rows, popupSelectedStyle.Render("  "+label+"  "))
		} else {
			rows = append(rows, popupItemStyle.Render("  "+label+"  "))
		}
	}
	inner := strings.Join(rows, "\n")
	return popupStyle.Render(inner)
}

func (m *Model) closeTab(i int) {
	if len(m.tabs) == 1 {
		return // always keep at least one tab
	}
	m.tabs = append(m.tabs[:i], m.tabs[i+1:]...)
	if m.active >= len(m.tabs) {
		m.active = len(m.tabs) - 1
	}
}

// blockAtCursor returns the logical SQL block at the current cursor position.
func (m Model) blockAtCursor() string {
	text := m.tabs[m.active].ta.Value()
	line := m.tabs[m.active].ta.Line()
	return detectBlock(text, line)
}

// detectBlock extracts the logical SQL block around cursorLine from text.
// A block is delimited by:
//   - a GO separator (a line whose trimmed uppercase content is exactly "GO")
//   - two or more consecutive blank lines
//   - a line ending with a semicolon
//   - the start or end of the file
//
// If no delimiter is found, the full trimmed text is returned.
func detectBlock(text string, cursorLine int) string {
	lines := strings.Split(text, "\n")
	if len(lines) == 0 {
		return strings.TrimSpace(text)
	}
	if cursorLine >= len(lines) {
		cursorLine = len(lines) - 1
	}

	isGO := func(line string) bool {
		return strings.TrimSpace(strings.ToUpper(line)) == "GO"
	}

	// Walk backward from cursorLine to find block start.
	start := 0
	consecutiveBlanks := 0
	for i := cursorLine; i >= 0; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if isGO(lines[i]) {
			if i == cursorLine {
				// cursor is on a GO line — include it alone
				return "GO"
			}
			start = i + 1
			break
		}
		if trimmed == "" {
			consecutiveBlanks++
			if consecutiveBlanks >= 2 {
				start = i + consecutiveBlanks
				break
			}
		} else {
			consecutiveBlanks = 0
		}
		// Check if the previous line ends with a semicolon (that block has ended).
		if i < cursorLine && strings.HasSuffix(strings.TrimSpace(lines[i]), ";") {
			start = i + 1
			break
		}
	}

	// Walk forward from cursorLine to find block end.
	end := len(lines) - 1
	consecutiveBlanks = 0
	for i := cursorLine; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if isGO(lines[i]) {
			if i == cursorLine {
				end = i
			} else {
				end = i - 1
			}
			break
		}
		if trimmed == "" {
			consecutiveBlanks++
			if consecutiveBlanks >= 2 {
				end = i - consecutiveBlanks
				break
			}
		} else {
			consecutiveBlanks = 0
		}
		if strings.HasSuffix(trimmed, ";") {
			end = i
			break
		}
	}

	if start > end {
		return strings.TrimSpace(text)
	}

	block := strings.Join(lines[start:end+1], "\n")
	block = strings.TrimSpace(block)
	if block == "" {
		return strings.TrimSpace(text)
	}
	return block
}
