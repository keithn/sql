package editor

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/sqltui/sql/internal/config"
	"github.com/sqltui/sql/internal/ui/editor/vim"
)

// ExecuteBlockMsg asks the app to run the logical block under the cursor.
type ExecuteBlockMsg struct{ SQL string }

type vimInsertCursorBlinkMsg struct{ id int }

const vimInsertCursorBlinkInterval = 600 * time.Millisecond

// ExecuteBufferMsg asks the app to run the full buffer.
type ExecuteBufferMsg struct{ SQL string }

// NewTabMsg asks the app to create a new query file and add a tab.
type NewTabMsg struct{}

// TabInfo carries metadata about a tab for session persistence.
type TabInfo struct {
	Path       string
	Title      string
	CursorLine int
	CursorCol  int
}

// TabState is used to restore tabs from saved session data.
type TabState struct {
	Path       string
	Content    string
	CursorLine int
	CursorCol  int
}

type textPos struct {
	Line int
	Col  int
}

type textareaSelection struct {
	Active bool
	Anchor textPos
}

// Tab is a single open query buffer.
type Tab struct {
	Title     string
	Path      string
	ta        textarea.Model
	selection textareaSelection
	vim       *vim.State // non-nil when vim mode is active
	Dirty     bool
}

// Model is the editor pane: a tab bar + textarea (or vim buffer).
type Model struct {
	cfg         *config.Config
	width       int
	height      int // total height including tab bar
	tabs        []Tab
	active      int
	focused     bool
	popup       completionPopup
	schemaItems []CompletionItem // typed schema items pushed from the app
	vimEnabled  bool
	blinkID     int
	blinkOn     bool
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

	selectionStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#264f78"))
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
	ta.KeyMap.WordBackward.SetKeys("alt+left", "alt+b", "ctrl+left")
	ta.KeyMap.WordForward.SetKeys("alt+right", "alt+f", "ctrl+right")
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
	m := Model{
		cfg:        cfg,
		tabs:       []Tab{newTab("query1.sql", cfg.Theme)},
		focused:    true,
		vimEnabled: cfg.Editor.VimMode,
		blinkOn:    true,
	}
	if m.vimEnabled {
		m.tabs[0].vim = vim.NewState()
	}
	return m
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
	case vimInsertCursorBlinkMsg:
		if !m.shouldBlinkVimInsertCursor() || msg.id != m.blinkID {
			return m, nil
		}
		m.blinkOn = !m.blinkOn
		return m, m.scheduleVimInsertCursorBlink()

	case tea.KeyMsg:
		if isCommentShortcut(msg) {
			if m.vimEnabled {
				return m.commentCurrentLineVim()
			}
			return m.commentCurrentLineTextarea()
		}

		// ── Vim mode path ────────────────────────────────────────────────
		if m.vimEnabled {
			return m.updateVim(msg)
		}

		// ── Non-vim path: popup then textarea ────────────────────────────
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
				m = m.acceptCompletion()
				return m, nil
			}
			if len(msg.String()) == 1 && !isWordRune(rune(msg.String()[0])) {
				m.popup.visible = false
			}
		} else {
			if handled, nm, cmd := m.handleTextareaSelectionKey(msg); handled {
				return nm, cmd
			}
			m.clearTextareaSelectionOnNonSelectionKey(msg)
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
				m.tabs[m.active].ta.InsertString("    ")
				return m, nil
			}
		}
	}

	if !m.vimEnabled {
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			if handled, nm, cmd := m.handleTextareaSelectionEditKey(keyMsg); handled {
				return nm, cmd
			}
			m.prepareTextareaSelectionForInput(keyMsg)
		}
	}

	// Route remaining messages to the active textarea, auto-saving on change.
	var cmd tea.Cmd
	oldVal := m.tabs[m.active].ta.Value()
	m.tabs[m.active].ta, cmd = m.tabs[m.active].ta.Update(msg)
	newVal := m.tabs[m.active].ta.Value()

	var saveCmd tea.Cmd
	if newVal != oldVal && m.tabs[m.active].Path != "" {
		path := m.tabs[m.active].Path
		saveCmd = func() tea.Msg {
			_ = os.WriteFile(path, []byte(newVal), 0644)
			return nil
		}
	}

	if newVal != oldVal {
		m.updatePopup()
	}

	return m, tea.Batch(cmd, saveCmd)
}

// updateVim handles a KeyMsg when vim mode is active.
func (m Model) updateVim(msg tea.KeyMsg) (Model, tea.Cmd) {
	// App-level shortcuts that work regardless of vim mode.
	switch msg.String() {
	case "ctrl+e":
		sql := m.blockAtCursor()
		return m, func() tea.Msg { return ExecuteBlockMsg{SQL: sql} }
	case "f5":
		sql := m.tabs[m.active].vim.Buf.Value()
		return m, func() tea.Msg { return ExecuteBufferMsg{SQL: sql} }
	case "ctrl+n":
		return m, func() tea.Msg { return NewTabMsg{} }
	case "ctrl+w":
		m.closeTab(m.active)
		return m, m.refreshVimInsertCursorBlink()
	case "ctrl+pgdown", "alt+l":
		m.active = (m.active + 1) % len(m.tabs)
		return m, m.refreshVimInsertCursorBlink()
	case "ctrl+pgup", "alt+h":
		m.active = (m.active - 1 + len(m.tabs)) % len(m.tabs)
		return m, m.refreshVimInsertCursorBlink()
	}

	tab := &m.tabs[m.active]
	if tab.vim == nil {
		return m, nil
	}

	// In Insert mode, popup takes priority over vim key handling.
	if tab.vim.Mode == vim.ModeInsert && m.popup.visible {
		switch msg.String() {
		case "esc":
			m.popup.visible = false
			return m, m.restartVimInsertCursorBlink()
		case "up", "ctrl+p":
			if m.popup.selected > 0 {
				m.popup.selected--
			}
			return m, m.restartVimInsertCursorBlink()
		case "down":
			if m.popup.selected < len(m.popup.items)-1 {
				m.popup.selected++
			}
			return m, m.restartVimInsertCursorBlink()
		case "tab", "shift+tab":
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
			return m, m.restartVimInsertCursorBlink()
		case "enter", "ctrl+e":
			m = m.acceptCompletionVim()
			return m, m.restartVimInsertCursorBlink()
		}
		if len(msg.String()) == 1 && !isWordRune(rune(msg.String()[0])) {
			m.popup.visible = false
		}
	}

	oldVal := tab.vim.Buf.Value()
	oldReg, oldRegLine := tab.vim.Buf.Register()
	tab.vim.HandleKey(msg.String())
	newVal := tab.vim.Buf.Value()
	newReg, newRegLine := tab.vim.Buf.Register()
	var clipboardCmd tea.Cmd
	if newVal == oldVal && (newReg != oldReg || newRegLine != oldRegLine) {
		clipboardCmd = copyToClipboardCmd(newReg, newRegLine)
	}

	// Update or dismiss popup based on current mode.
	if tab.vim.Mode == vim.ModeInsert {
		m.updatePopupVim()
	} else {
		m.popup.visible = false
	}

	if newVal != oldVal && tab.Path != "" {
		path := tab.Path
		saveCmd := func() tea.Msg {
			_ = os.WriteFile(path, []byte(newVal), 0644)
			return nil
		}
		return m, tea.Batch(saveCmd, clipboardCmd, m.refreshVimInsertCursorBlink())
	}
	return m, tea.Batch(clipboardCmd, m.refreshVimInsertCursorBlink())
}

func (m Model) commentCurrentLineTextarea() (Model, tea.Cmd) {
	if m.active < 0 || m.active >= len(m.tabs) {
		return m, nil
	}
	tab := &m.tabs[m.active]
	line, col := tab.ta.Line(), tab.ta.LineInfo().CharOffset
	text, nextLine, nextCol := commentLineAndAdvance(tab.ta.Value(), line, col)
	tab.ta.SetValue(text)
	setTextareaCursor(&tab.ta, nextLine, nextCol)
	tab.selection.Active = false
	if m.popup.visible {
		m.updatePopup()
	}
	return m, m.saveActiveTextareaCmd()
}

func (m Model) commentCurrentLineVim() (Model, tea.Cmd) {
	if m.active < 0 || m.active >= len(m.tabs) {
		return m, nil
	}
	tab := &m.tabs[m.active]
	if tab.vim == nil {
		return m, nil
	}
	buf := tab.vim.Buf
	line, col := buf.CursorRow(), buf.CursorCol()
	text, nextLine, nextCol := commentLineAndAdvance(buf.Value(), line, col)
	buf.PushUndo()
	buf.SetValue(text)
	buf.SetCursor(nextLine, nextCol)
	if tab.vim.Mode == vim.ModeVisual || tab.vim.Mode == vim.ModeVisualLine {
		tab.vim.Mode = vim.ModeNormal
	}
	if m.popup.visible {
		m.popup.visible = false
	}
	return m, tea.Batch(saveTextToPathCmd(tab.Path, buf.Value()), m.refreshVimInsertCursorBlink())
}

func commentLineAndAdvance(text string, line, col int) (string, int, int) {
	lines := strings.Split(text, "\n")
	if len(lines) == 0 {
		lines = []string{""}
	}
	if line < 0 {
		line = 0
	}
	if line >= len(lines) {
		line = len(lines) - 1
	}
	commented, insertedAt, insertedLen := commentSQLLine(lines[line])
	lines[line] = commented

	nextLine, nextCol := line, col
	if line < len(lines)-1 {
		nextLine = line + 1
		nextCol = clampInt(col, 0, len([]rune(lines[nextLine])))
	} else if col >= insertedAt {
		nextCol = col + insertedLen
	}

	return strings.Join(lines, "\n"), nextLine, nextCol
}

func commentSQLLine(line string) (string, int, int) {
	runes := []rune(line)
	insertAt := 0
	for insertAt < len(runes) && (runes[insertAt] == ' ' || runes[insertAt] == '\t') {
		insertAt++
	}
	comment := []rune("-- ")
	commented := string(runes[:insertAt]) + string(comment) + string(runes[insertAt:])
	return commented, insertAt, len(comment)
}

func isCommentShortcut(msg tea.KeyMsg) bool {
	if msg.Type == tea.KeyCtrlUnderscore {
		return true
	}
	if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && msg.Runes[0] == rune(31) {
		return true
	}
	key := msg.String()
	return key == "ctrl+/" || key == "ctrl+_"
}

func (m Model) handleTextareaSelectionKey(msg tea.KeyMsg) (bool, Model, tea.Cmd) {
	base, ok := baseTextareaSelectionKey(msg)
	if !ok {
		return false, m, nil
	}
	tab := &m.tabs[m.active]
	if !tab.selection.Active {
		tab.selection = textareaSelection{Active: true, Anchor: textareaCursorPos(&tab.ta)}
	}
	if m.popup.visible {
		m.popup.visible = false
	}
	var cmd tea.Cmd
	tab.ta, cmd = tab.ta.Update(base)
	if textareaCursorPos(&tab.ta) == tab.selection.Anchor {
		tab.selection.Active = false
	}
	return true, m, cmd
}

func (m *Model) clearTextareaSelectionOnNonSelectionKey(msg tea.KeyMsg) {
	if m.vimEnabled || m.active < 0 || m.active >= len(m.tabs) {
		return
	}
	if !m.tabs[m.active].selection.Active {
		return
	}
	if _, ok := baseTextareaSelectionKey(msg); ok {
		return
	}
	if msg.Type == tea.KeyBackspace || msg.Type == tea.KeyDelete {
		return
	}
	if shouldReplaceTextareaSelection(msg) {
		return
	}
	m.tabs[m.active].selection.Active = false
}

func (m Model) handleTextareaSelectionEditKey(msg tea.KeyMsg) (bool, Model, tea.Cmd) {
	if !m.hasTextareaSelection() {
		return false, m, nil
	}
	switch msg.Type {
	case tea.KeyBackspace, tea.KeyDelete:
		m.deleteTextareaSelection()
		m.updatePopup()
		return true, m, m.saveActiveTextareaCmd()
	case tea.KeyTab:
		m.deleteTextareaSelection()
		m.tabs[m.active].ta.InsertString("    ")
		m.updatePopup()
		return true, m, m.saveActiveTextareaCmd()
	}
	return false, m, nil
}

func (m *Model) prepareTextareaSelectionForInput(msg tea.KeyMsg) {
	if !m.hasTextareaSelection() || !shouldReplaceTextareaSelection(msg) {
		return
	}
	m.deleteTextareaSelection()
}

func (m *Model) deleteTextareaSelection() {
	start, end, ok := m.textareaSelectionRange()
	if !ok {
		m.tabs[m.active].selection.Active = false
		return
	}
	text := deleteTextRange(m.tabs[m.active].ta.Value(), start, end)
	m.tabs[m.active].ta.SetValue(text)
	setTextareaCursor(&m.tabs[m.active].ta, start.Line, start.Col)
	m.tabs[m.active].selection.Active = false
	if m.popup.visible {
		m.updatePopup()
	}
}

func (m Model) saveActiveTextareaCmd() tea.Cmd {
	if m.active < 0 || m.active >= len(m.tabs) || m.tabs[m.active].Path == "" {
		return nil
	}
	return saveTextToPathCmd(m.tabs[m.active].Path, m.tabs[m.active].ta.Value())
}

func saveTextToPathCmd(path, value string) tea.Cmd {
	if path == "" {
		return nil
	}
	return func() tea.Msg {
		_ = os.WriteFile(path, []byte(value), 0644)
		return nil
	}
}

func (m Model) View() string {
	tabBar := m.renderTabBar()

	var content string
	if m.vimEnabled {
		content = m.renderVimContent()
	} else {
		content = m.renderContent()
	}
	base := lipgloss.JoinVertical(lipgloss.Left, tabBar, content)

	if !m.popup.visible || len(m.popup.items) == 0 {
		return base
	}

	popup := m.renderPopup()
	var popupRow, popupCol int

	if m.vimEnabled && m.tabs[m.active].vim != nil {
		vs := m.tabs[m.active].vim
		buf := vs.Buf
		gutterW := lineNumberGutterWidth(buf.LineCount())
		cursorRow := buf.CursorRow() - vs.TopRow
		popupRow = 1 + cursorRow + 1 // 1 for tab bar, +1 to place below cursor
		popupCol = buf.CursorCol() + gutterW
	} else {
		taView := m.tabs[m.active].ta.View()
		li := m.tabs[m.active].ta.LineInfo()
		cursorLine := m.tabs[m.active].ta.Line()
		popupRow = 1 + cursorLine + li.RowOffset + 1
		gutterW := textareaGutterWidth(taView, strings.Count(m.tabs[m.active].ta.Value(), "\n")+1)
		popupCol = li.CharOffset + gutterW
	}

	popupLines := strings.Split(popup, "\n")
	if popupRow+len(popupLines) > m.height {
		popupRow -= len(popupLines) + 1
	}
	if popupRow < 0 {
		popupRow = 0
	}
	return overlayView(base, popup, popupRow, popupCol)
}

// renderVimContent renders the active vim buffer with line numbers, syntax
// highlighting, cursor and optional selection highlighting.
func (m Model) renderVimContent() string {
	tab := &m.tabs[m.active]
	if tab.vim == nil {
		return ""
	}
	vs := tab.vim
	buf := vs.Buf

	taH := m.height - 1
	if taH < 1 {
		taH = 1
	}

	vs.ScrollToReveal(taH)

	lineCount := buf.LineCount()
	gutterW := lineNumberGutterWidth(lineCount)
	gutterDigits := gutterW - 3
	blockStart, blockEnd, blockOK := m.blockRangeAtCursor()

	allText := buf.Value()
	hlLines := highlightLines(allText)
	sourceLines := strings.Split(allText, "\n")

	cursorRow := buf.CursorRow()
	cursorCol := buf.CursorCol()
	mode := vs.Mode

	inVisual := mode == vim.ModeVisual || mode == vim.ModeVisualLine
	var selStart, selEnd vim.Pos
	if inVisual {
		selStart, selEnd = vs.SelectionRange()
	}

	result := make([]string, taH)
	for i := range result {
		row := vs.TopRow + i
		if row >= lineCount {
			// Filler line — render blank gutter + empty content.
			gutter := m.renderGutter(lineNumberGutterText(0, gutterDigits), false, false)
			result[i] = gutter
			continue
		}

		// Gutter.
		gutterStr := m.renderGutter(lineNumberGutterText(row+1, gutterDigits), row == cursorRow, isActiveBlockLine(sourceLines, row, blockStart, blockEnd, blockOK, cursorRow))

		// Highlighted content.
		var hlText string
		if row < len(hlLines) {
			hlText = hlLines[row]
		} else {
			hlText = string(buf.Line(row))
		}

		// Apply visual selection highlight.
		if inVisual {
			hlText = applySelection(hlText, row, selStart, selEnd, mode == vim.ModeVisualLine, buf)
		}

		// Apply cursor on cursor row.
		if row == cursorRow && m.focused {
			if mode == vim.ModeInsert {
				if m.blinkOn {
					insertStyle := lipgloss.NewStyle().
						Underline(true).
						Foreground(lipgloss.Color(m.cfg.Theme.InsertCursor))
					hlText = injectCursorStyled(hlText, cursorCol, insertStyle)
				}
			} else {
				blockStyle := lipgloss.NewStyle().
					Background(lipgloss.Color(m.cfg.Theme.Cursor)).
					Foreground(lipgloss.Color("#1e1e1e"))
				hlText = injectCursorStyled(hlText, cursorCol, blockStyle)
			}
		}

		result[i] = gutterStr + hlText
	}

	return strings.Join(result, "\n")
}

func (m Model) shouldBlinkVimInsertCursor() bool {
	if !m.focused || !m.vimEnabled || m.active < 0 || m.active >= len(m.tabs) {
		return false
	}
	return m.tabs[m.active].vim != nil && m.tabs[m.active].vim.Mode == vim.ModeInsert
}

func (m Model) scheduleVimInsertCursorBlink() tea.Cmd {
	id := m.blinkID
	return tea.Tick(vimInsertCursorBlinkInterval, func(time.Time) tea.Msg {
		return vimInsertCursorBlinkMsg{id: id}
	})
}

func (m *Model) restartVimInsertCursorBlink() tea.Cmd {
	m.blinkID++
	m.blinkOn = true
	if !m.shouldBlinkVimInsertCursor() {
		return nil
	}
	return m.scheduleVimInsertCursorBlink()
}

func (m *Model) refreshVimInsertCursorBlink() tea.Cmd {
	if !m.shouldBlinkVimInsertCursor() {
		m.blinkID++
		m.blinkOn = true
		return nil
	}
	return m.restartVimInsertCursorBlink()
}

// applySelection overlays the visual selection highlight on a line's ANSI string.
func applySelection(hlText string, row int, selStart, selEnd vim.Pos, lineWise bool, buf *vim.Buffer) string {
	stripped := xansi.Strip(hlText)
	runes := []rune(stripped)

	var startCol, endCol int
	if lineWise {
		if row < selStart.Row || row > selEnd.Row {
			return hlText
		}
		startCol = 0
		endCol = len(runes)
	} else {
		if row < selStart.Row || row > selEnd.Row {
			return hlText
		}
		if row == selStart.Row {
			startCol = selStart.Col
		}
		if row == selEnd.Row {
			endCol = selEnd.Col + 1
		} else {
			endCol = len(runes)
		}
	}
	if startCol >= len(runes) || startCol >= endCol {
		return hlText
	}
	if endCol > len(runes) {
		endCol = len(runes)
	}

	left := xansi.Truncate(hlText, startCol, "")
	mid := selectionStyle.Render(string(runes[startCol:endCol]))
	right := skipVisualCols(hlText, endCol)
	return left + mid + right
}

// renderContent returns the syntax-highlighted textarea content.
func (m Model) renderContent() string {
	ta := &m.tabs[m.active].ta
	taView := ta.View()
	text := ta.Value()
	if strings.TrimSpace(text) == "" {
		return taView
	}

	gutterW := textareaGutterWidth(taView, strings.Count(text, "\n")+1)
	gutterDigits := gutterW - 3
	blockStart, blockEnd, blockOK := m.blockRangeAtCursor()
	cursorLine := ta.Line()
	cursorCol := ta.LineInfo().CharOffset
	hlLines := highlightLines(text)
	sourceLines := strings.Split(text, "\n")
	selStart, selEnd, hasSelection := m.textareaSelectionRange()

	taLines := strings.Split(taView, "\n")
	result := make([]string, len(taLines))
	activeSrcLine := -1
	rowOffsets := make([]int, len(sourceLines))

	for i, line := range taLines {
		_, lineNum, hasLineNum, ok := parseTextareaLineNumberRow(line, gutterW)
		if !ok {
			result[i] = line
			continue
		}
		if hasLineNum {
			activeSrcLine = lineNum
		}
		if activeSrcLine < 0 {
			result[i] = line
			continue
		}

		srcLine := activeSrcLine
		gutterPrefix := m.renderGutter(lineNumberGutterText(0, gutterDigits), false, false)
		if hasLineNum {
			gutterPrefix = lineNumberGutterText(srcLine+1, gutterDigits)
			gutterPrefix = m.renderGutter(gutterPrefix, srcLine == cursorLine, isActiveBlockLine(sourceLines, srcLine, blockStart, blockEnd, blockOK, cursorLine))
		}
		stripped := xansi.Strip(line)
		runes := []rune(stripped)
		plainRest := string(runes[gutterW:])
		plainWidth := len([]rune(plainRest))
		rest := renderHighlightedRowSlice(hlLines, sourceLines, srcLine, rowOffsets[srcLine], plainWidth)
		if hasSelection {
			rest = applyTextareaSelectionSlice(rest, srcLine, rowOffsets[srcLine], plainWidth, selStart, selEnd, sourceLines)
		}
		if srcLine == cursorLine && m.focused {
			relCol := cursorCol - rowOffsets[srcLine]
			if relCol >= 0 && relCol <= plainWidth {
				blockStyle := lipgloss.NewStyle().
					Background(lipgloss.Color(m.cfg.Theme.Cursor)).
					Foreground(lipgloss.Color("#1e1e1e"))
				rest = injectCursorStyled(rest, relCol, blockStyle)
			}
		}
		rowOffsets[srcLine] += plainWidth
		result[i] = gutterPrefix + rest
	}

	return strings.Join(result, "\n")
}

func (m Model) renderGutter(text string, cursorLine bool, activeBlock bool) string {
	style := lipgloss.NewStyle().Foreground(lipgloss.Color(m.cfg.Theme.LineNumber))
	if cursorLine {
		style = style.Foreground(lipgloss.Color(m.cfg.Theme.CursorLineNumber))
	}
	if !strings.HasSuffix(text, "│") {
		if activeBlock {
			style = style.Background(lipgloss.Color(m.cfg.Theme.ActiveQueryGutter))
		}
		return style.Render(text)
	}
	prefix := strings.TrimSuffix(text, "│")
	separator := "│"
	prefixStyle := style
	if activeBlock {
		prefixStyle = prefixStyle.Background(lipgloss.Color(m.cfg.Theme.ActiveQueryGutter))
	}
	return prefixStyle.Render(prefix) + style.Render(separator)
}

func renderHighlightedRowSlice(hlLines, sourceLines []string, srcLine, offset, width int) string {
	if srcLine < 0 || srcLine >= len(sourceLines) || width <= 0 {
		return ""
	}
	hlText := sourceLines[srcLine]
	if srcLine < len(hlLines) {
		hlText = hlLines[srcLine]
	}
	return xansi.Truncate(skipVisualCols(hlText, offset), width, "")
}

func applyTextareaSelectionSlice(hlText string, srcLine, offset, width int, selStart, selEnd textPos, sourceLines []string) string {
	if width <= 0 || srcLine < selStart.Line || srcLine > selEnd.Line || srcLine >= len(sourceLines) {
		return hlText
	}
	startCol := 0
	if srcLine == selStart.Line {
		startCol = selStart.Col
	}
	endCol := len([]rune(sourceLines[srcLine]))
	if srcLine == selEnd.Line {
		endCol = selEnd.Col
	}
	if endCol <= startCol {
		return hlText
	}
	interStart := maxInt(startCol, offset)
	interEnd := minInt(endCol, offset+width)
	if interEnd <= interStart {
		return hlText
	}
	relStart := interStart - offset
	relEnd := interEnd - offset
	left := xansi.Truncate(hlText, relStart, "")
	mid := selectionStyle.Render(sliceTextRunes(sourceLines[srcLine], interStart, interEnd))
	right := skipVisualCols(hlText, relEnd)
	return left + mid + right
}

func sliceTextRunes(text string, start, end int) string {
	runes := []rune(text)
	if start < 0 {
		start = 0
	}
	if end < start {
		end = start
	}
	if start > len(runes) {
		start = len(runes)
	}
	if end > len(runes) {
		end = len(runes)
	}
	return string(runes[start:end])
}

func lineNumberGutterText(lineNum int, gutterDigits int) string {
	if lineNum <= 0 {
		return fmt.Sprintf(" %s │", strings.Repeat(" ", gutterDigits))
	}
	return fmt.Sprintf(" %*d │", gutterDigits, lineNum)
}

func baseTextareaSelectionKey(msg tea.KeyMsg) (tea.KeyMsg, bool) {
	switch msg.Type {
	case tea.KeyShiftLeft:
		return tea.KeyMsg{Type: tea.KeyLeft}, true
	case tea.KeyShiftRight:
		return tea.KeyMsg{Type: tea.KeyRight}, true
	case tea.KeyShiftUp:
		return tea.KeyMsg{Type: tea.KeyUp}, true
	case tea.KeyShiftDown:
		return tea.KeyMsg{Type: tea.KeyDown}, true
	}
	return tea.KeyMsg{}, false
}

func shouldReplaceTextareaSelection(msg tea.KeyMsg) bool {
	if len(msg.Runes) == 1 {
		return true
	}
	switch msg.Type {
	case tea.KeyEnter, tea.KeyTab, tea.KeySpace:
		return true
	default:
		return false
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func isActiveBlockLine(lines []string, row, start, end int, ok bool, cursorLine int) bool {
	if !ok || row < 0 || row >= len(lines) {
		return false
	}
	if row == cursorLine && cursorBlankBelongsToPrevious(lines, cursorLine, start, end, ok) {
		return true
	}
	if row < start || row > end {
		return false
	}
	return strings.TrimSpace(lines[row]) != ""
}

func cursorBlankBelongsToPrevious(lines []string, cursorLine, start, end int, ok bool) bool {
	if !ok || cursorLine <= 0 || cursorLine >= len(lines) {
		return false
	}
	if strings.TrimSpace(lines[cursorLine]) != "" {
		return false
	}
	prev := strings.TrimSpace(lines[cursorLine-1])
	if prev == "" || strings.EqualFold(prev, "GO") {
		return false
	}
	return cursorLine-1 >= start && cursorLine-1 <= end
}

func parseTextareaLineNumberRow(line string, gutterW int) (gutterText string, lineNum int, hasLineNum bool, ok bool) {
	stripped := xansi.Strip(line)
	runes := []rune(stripped)
	if len(runes) < gutterW {
		return "", 0, false, false
	}
	gutterText = string(runes[:gutterW])
	lineNumStr := strings.TrimSpace(string(runes[2:gutterW]))
	parsed, err := strconv.Atoi(lineNumStr)
	if err != nil || parsed <= 0 {
		return gutterText, 0, false, true
	}
	return gutterText, parsed - 1, true, true
}

func lineNumberGutterWidth(lineCount int) int {
	if lineCount < 1 {
		lineCount = 1
	}
	width := len(strconv.Itoa(lineCount)) + 3
	if width < 6 {
		return 6
	}
	return width
}

func textareaGutterWidth(view string, lineCount int) int {
	for _, line := range strings.Split(view, "\n") {
		stripped := xansi.Strip(line)
		if idx := strings.IndexRune(stripped, '│'); idx >= 0 {
			return idx + 2
		}
	}
	return lineNumberGutterWidth(lineCount)
}

func textareaRowContentWidth(row string, gutterW int) int {
	stripped := xansi.Strip(row)
	runes := []rune(stripped)
	if len(runes) <= gutterW {
		return 0
	}
	return len(runes[gutterW:])
}

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
		if m.tabs[i].vim != nil {
			m.tabs[i].vim.SetSize(w, h-1)
		}
	}
	return m
}

func (m *Model) syncTextareaHeight() {
	taH := m.height - 1
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
	return m, tea.Batch(cmd, m.refreshVimInsertCursorBlink())
}

// Blur removes keyboard focus from the editor.
func (m Model) Blur() Model {
	m.focused = false
	m.blinkID++
	m.blinkOn = true
	m.tabs[m.active].ta.Blur()
	return m
}

// Click handles a left-click at editor-local coordinates.
// x/y are relative to the editor view, including the tab bar at row 0.
func (m Model) Click(x, y int) Model {
	if m.active < 0 || m.active >= len(m.tabs) || x < 0 || y < 0 {
		return m
	}
	m.popup.visible = false
	if y == 0 {
		return m
	}
	if m.vimEnabled && m.tabs[m.active].vim != nil {
		return m.clickVimContent(x, y-1)
	}
	return m.clickTextareaContent(x, y-1)
}

func (m Model) clickVimContent(x, y int) Model {
	tab := &m.tabs[m.active]
	if tab.vim == nil {
		return m
	}
	buf := tab.vim.Buf
	if buf.LineCount() == 0 {
		return m
	}
	row := tab.vim.TopRow + y
	if row < 0 {
		row = 0
	}
	if row >= buf.LineCount() {
		row = buf.LineCount() - 1
	}
	col := x - lineNumberGutterWidth(buf.LineCount())
	if col < 0 {
		col = 0
	}
	if col > len(buf.Line(row)) {
		col = len(buf.Line(row))
	}
	buf.SetCursor(row, col)
	if tab.vim.Mode == vim.ModeVisual || tab.vim.Mode == vim.ModeVisualLine {
		tab.vim.Mode = vim.ModeNormal
	}
	tab.selection.Active = false
	tab.vim.ScrollToReveal(maxInt(1, m.height-1))
	return m
}

func (m Model) clickTextareaContent(x, y int) Model {
	tab := &m.tabs[m.active]
	ta := &tab.ta
	view := ta.View()
	text := ta.Value()
	lines := strings.Split(text, "\n")
	if len(lines) == 0 {
		lines = []string{""}
	}
	gutterW := textareaGutterWidth(view, len(lines))
	targetX := x - gutterW
	if targetX < 0 {
		targetX = 0
	}

	rows := strings.Split(view, "\n")
	activeSrcLine := -1
	rowOffsets := make([]int, len(lines))
	for i, row := range rows {
		_, lineNum, hasLineNum, ok := parseTextareaLineNumberRow(row, gutterW)
		if !ok {
			continue
		}
		if hasLineNum {
			activeSrcLine = lineNum
		}
		if activeSrcLine < 0 || activeSrcLine >= len(lines) {
			continue
		}
		plainWidth := textareaRowContentWidth(row, gutterW)
		if i == y {
			setTextareaCursor(ta, activeSrcLine, rowOffsets[activeSrcLine]+minInt(targetX, plainWidth))
			tab.selection.Active = false
			return m
		}
		rowOffsets[activeSrcLine] += plainWidth
	}

	lastLine := len(lines) - 1
	setTextareaCursor(ta, lastLine, len([]rune(lines[lastLine])))
	tab.selection.Active = false
	return m
}

// Value returns the active tab's text content.
func (m Model) Value() string {
	if m.vimEnabled && m.tabs[m.active].vim != nil {
		return m.tabs[m.active].vim.Buf.Value()
	}
	return m.tabs[m.active].ta.Value()
}

// VimMode returns the current vim mode label ("NORMAL", "INSERT", etc.)
// or "" when vim mode is disabled.
func (m Model) VimMode() string {
	if !m.vimEnabled || len(m.tabs) == 0 || m.tabs[m.active].vim == nil {
		return ""
	}
	return m.tabs[m.active].vim.ModeString()
}

// VimEnabled reports whether vim editing mode is enabled.
func (m Model) VimEnabled() bool { return m.vimEnabled }

// SetVimEnabled flips vim mode only when it differs from the current state.
func (m Model) SetVimEnabled(enabled bool) Model {
	if m.vimEnabled == enabled {
		return m
	}
	return m.ToggleVim()
}

// ToggleVim toggles vim mode on/off, syncing buffer content between the two paths.
func (m Model) ToggleVim() Model {
	cursors := make([]struct{ line, col int }, len(m.tabs))
	for i, tab := range m.tabs {
		cursors[i].line, cursors[i].col = cursorForTab(tab)
	}

	if m.vimEnabled {
		// Vim → textarea: push vim buffer content into textarea.
		for i := range m.tabs {
			if m.tabs[i].vim != nil {
				m.tabs[i].ta.SetValue(m.tabs[i].vim.Buf.Value())
				m.tabs[i].vim = nil
				restoreTabCursor(&m.tabs[i], cursors[i].line, cursors[i].col)
			}
		}
		m.vimEnabled = false
		// Re-focus active textarea.
		_ = m.tabs[m.active].ta.Focus()
	} else {
		// Textarea → vim: load textarea content into new vim states.
		for i := range m.tabs {
			vs := vim.NewState()
			vs.Buf.SetValue(m.tabs[i].ta.Value())
			vs.SetSize(m.width, m.height-1)
			m.tabs[i].vim = vs
			restoreTabCursor(&m.tabs[i], cursors[i].line, cursors[i].col)
		}
		m.vimEnabled = true
		m.tabs[m.active].ta.Blur()
	}
	return m
}

// AddTab adds a new tab backed by path and returns the updated model.
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
	if m.vimEnabled {
		vs := vim.NewState()
		vs.Buf.SetValue(content)
		vs.SetSize(m.width, m.height-1)
		t.vim = vs
	}
	restoreTabCursor(&t, 0, 0)
	m.tabs = append(m.tabs, t)
	m.active = len(m.tabs) - 1
	return m
}

// SetTabs replaces all open tabs with the provided states.
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
		if m.vimEnabled {
			vs := vim.NewState()
			vs.Buf.SetValue(ts.Content)
			vs.SetSize(m.width, m.height-1)
			t.vim = vs
		}
		restoreTabCursor(&t, ts.CursorLine, ts.CursorCol)
		m.tabs = append(m.tabs, t)
	}
	m.active = 0
	if m.focused && !m.vimEnabled {
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
		line, col := cursorForTab(t)
		info[i] = TabInfo{Path: t.Path, Title: t.Title, CursorLine: line, CursorCol: col}
	}
	return info
}

func cursorForTab(t Tab) (int, int) {
	if t.vim != nil {
		return t.vim.Buf.CursorRow(), t.vim.Buf.CursorCol()
	}
	return t.ta.Line(), t.ta.LineInfo().CharOffset
}

func textareaCursorPos(ta *textarea.Model) textPos {
	if ta == nil {
		return textPos{}
	}
	return textPos{Line: ta.Line(), Col: ta.LineInfo().CharOffset}
}

func (m Model) hasTextareaSelection() bool {
	_, _, ok := m.textareaSelectionRange()
	return ok
}

func (m Model) textareaSelectionRange() (textPos, textPos, bool) {
	if m.active < 0 || m.active >= len(m.tabs) {
		return textPos{}, textPos{}, false
	}
	tab := m.tabs[m.active]
	if !tab.selection.Active {
		return textPos{}, textPos{}, false
	}
	cursor := textareaCursorPos(&tab.ta)
	anchor := tab.selection.Anchor
	start, end := orderedTextRange(anchor, cursor)
	if start == end {
		return textPos{}, textPos{}, false
	}
	return clampTextPosToValue(tab.ta.Value(), start), clampTextPosToValue(tab.ta.Value(), end), true
}

func orderedTextRange(a, b textPos) (textPos, textPos) {
	if a.Line < b.Line || (a.Line == b.Line && a.Col <= b.Col) {
		return a, b
	}
	return b, a
}

func clampTextPosToValue(text string, pos textPos) textPos {
	lines := strings.Split(text, "\n")
	if len(lines) == 0 {
		return textPos{}
	}
	if pos.Line < 0 {
		pos.Line = 0
	}
	if pos.Line >= len(lines) {
		pos.Line = len(lines) - 1
	}
	lineLen := len([]rune(lines[pos.Line]))
	if pos.Col < 0 {
		pos.Col = 0
	}
	if pos.Col > lineLen {
		pos.Col = lineLen
	}
	return pos
}

func deleteTextRange(text string, start, end textPos) string {
	lines := strings.Split(text, "\n")
	if len(lines) == 0 {
		return text
	}
	start = clampTextPosToValue(text, start)
	end = clampTextPosToValue(text, end)
	if start.Line == end.Line {
		runes := []rune(lines[start.Line])
		lines[start.Line] = string(runes[:start.Col]) + string(runes[end.Col:])
		return strings.Join(lines, "\n")
	}
	startRunes := []rune(lines[start.Line])
	endRunes := []rune(lines[end.Line])
	merged := string(startRunes[:start.Col]) + string(endRunes[end.Col:])
	newLines := append([]string{}, lines[:start.Line]...)
	newLines = append(newLines, merged)
	newLines = append(newLines, lines[end.Line+1:]...)
	return strings.Join(newLines, "\n")
}

func restoreTabCursor(t *Tab, line, col int) {
	if t == nil {
		return
	}
	if t.vim != nil {
		t.vim.Buf.SetCursor(line, col)
		return
	}
	setTextareaCursor(&t.ta, line, col)
}

func setTextareaCursor(ta *textarea.Model, line, col int) {
	if ta == nil {
		return
	}
	if line < 0 {
		line = 0
	}
	if col < 0 {
		col = 0
	}
	for ta.Line() > 0 {
		ta.CursorUp()
	}
	for ta.Line() < line {
		current := ta.Line()
		ta.CursorDown()
		if ta.Line() == current {
			break
		}
	}
	ta.SetCursor(col)
}

// ActiveTabIdx returns the index of the currently active tab.
func (m Model) ActiveTabIdx() int { return m.active }

// SetSchemaCompletions provides typed schema-aware completion items.
func (m Model) SetSchemaCompletions(items []CompletionItem) Model {
	m.schemaItems = append([]CompletionItem(nil), items...)
	return m
}

// SetSchemaNames provides untyped schema-aware completion items.
func (m Model) SetSchemaNames(names []string) Model {
	items := make([]CompletionItem, 0, len(names))
	for _, name := range names {
		items = append(items, CompletionItem{Text: name, Kind: CompletionKindName})
	}
	m.schemaItems = items
	return m
}

// updatePopupVim recomputes completions from the vim buffer cursor position.
func (m *Model) updatePopupVim() {
	tab := &m.tabs[m.active]
	if tab.vim == nil {
		return
	}
	buf := tab.vim.Buf
	line := buf.Line(buf.CursorRow())
	col := buf.CursorCol()
	word := wordBefore(string(line), col)

	items := getCompletions(word, m.schemaItems, 8)
	if len(items) == 0 || word == "" {
		m.popup.visible = false
		return
	}
	m.popup = completionPopup{items: items, selected: 0, visible: true, word: word}
}

// acceptCompletionVim applies the selected completion into the vim buffer.
func (m Model) acceptCompletionVim() Model {
	if !m.popup.visible || m.popup.selected >= len(m.popup.items) {
		return m
	}
	completion := m.popup.items[m.popup.selected]
	word := m.popup.word

	buf := m.tabs[m.active].vim.Buf
	for range []rune(word) {
		buf.DeleteCharBefore()
	}
	insert := completion.Text
	if word == strings.ToLower(word) {
		insert = strings.ToLower(completion.Text)
	}
	for _, r := range insert {
		buf.InsertRune(r)
	}
	m.popup.visible = false
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

	items := getCompletions(word, m.schemaItems, 8)
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
func (m Model) acceptCompletion() Model {
	if !m.popup.visible || m.popup.selected >= len(m.popup.items) {
		return m
	}
	completion := m.popup.items[m.popup.selected]
	word := m.popup.word

	for range []rune(word) {
		m.tabs[m.active].ta, _ = m.tabs[m.active].ta.Update(
			tea.KeyMsg{Type: tea.KeyBackspace},
		)
	}

	insert := completion.Text
	if word == strings.ToLower(word) {
		insert = strings.ToLower(completion.Text)
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

	maxW := 12
	maxKindW := len("[keyword]")
	for _, it := range items {
		if len(it.Text) > maxW {
			maxW = len(it.Text)
		}
		if w := len(it.kindLabel()) + 2; w > maxKindW {
			maxKindW = w
		}
	}

	var rows []string
	for i, it := range items {
		kind := "[" + it.kindLabel() + "]"
		label := fmt.Sprintf("%-*s  %-*s", maxW, it.Text, maxKindW, kind)
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
		return
	}
	m.tabs = append(m.tabs[:i], m.tabs[i+1:]...)
	if m.active >= len(m.tabs) {
		m.active = len(m.tabs) - 1
	}
}

// blockAtCursor returns the logical SQL block at the current cursor position.
func (m Model) blockAtCursor() string {
	var text string
	var line int
	if m.vimEnabled && m.tabs[m.active].vim != nil {
		text = m.tabs[m.active].vim.Buf.Value()
		line = m.tabs[m.active].vim.Buf.CursorRow()
	} else {
		text = m.tabs[m.active].ta.Value()
		line = m.tabs[m.active].ta.Line()
	}
	return detectBlock(text, line)
}

// detectBlock extracts the logical SQL block around cursorLine from text.
func detectBlock(text string, cursorLine int) string {
	lines := strings.Split(text, "\n")
	start, end, ok := detectBlockRange(text, cursorLine)
	if !ok || len(lines) == 0 {
		return ""
	}
	block := strings.Join(lines[start:end+1], "\n")
	block = strings.TrimSpace(block)
	if block == "" {
		return ""
	}
	return block
}

func (m Model) blockRangeAtCursor() (int, int, bool) {
	if m.vimEnabled && m.tabs[m.active].vim != nil {
		return detectBlockRange(m.tabs[m.active].vim.Buf.Value(), m.tabs[m.active].vim.Buf.CursorRow())
	}
	return detectBlockRange(m.tabs[m.active].ta.Value(), m.tabs[m.active].ta.Line())
}

func detectBlockRange(text string, cursorLine int) (int, int, bool) {
	lines := strings.Split(text, "\n")
	if len(lines) == 0 {
		return 0, 0, false
	}
	if cursorLine >= len(lines) {
		cursorLine = len(lines) - 1
	}
	if cursorLine < 0 {
		cursorLine = 0
	}
	effectiveCursorLine := cursorLine
	if strings.TrimSpace(lines[effectiveCursorLine]) == "" {
		if effectiveCursorLine > 0 {
			prev := strings.TrimSpace(lines[effectiveCursorLine-1])
			if prev != "" && !strings.EqualFold(prev, "GO") {
				effectiveCursorLine--
			} else {
				return 0, 0, false
			}
		} else {
			return 0, 0, false
		}
	}
	if strings.TrimSpace(lines[effectiveCursorLine]) == "" {
		return 0, 0, false
	}

	isGO := func(line string) bool {
		return strings.TrimSpace(strings.ToUpper(line)) == "GO"
	}

	start := 0
	for i := effectiveCursorLine; i >= 0; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if isGO(lines[i]) {
			if i == effectiveCursorLine {
				return i, i, true
			}
			start = i + 1
			break
		}
		if i < effectiveCursorLine && trimmed == "" {
			start = i + 1
			break
		}
		if i < effectiveCursorLine && strings.HasSuffix(strings.TrimSpace(lines[i]), ";") {
			start = i + 1
			break
		}
	}

	end := len(lines) - 1
	for i := effectiveCursorLine; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if isGO(lines[i]) {
			if i == effectiveCursorLine {
				end = i
			} else {
				end = i - 1
			}
			break
		}
		if i > effectiveCursorLine && trimmed == "" {
			end = i - 1
			break
		}
		if strings.HasSuffix(trimmed, ";") {
			end = i
			break
		}
	}

	if start <= end {
		block := strings.Join(lines[start:end+1], "\n")
		if strings.TrimSpace(block) != "" {
			return start, end, true
		}
	}
	return 0, 0, false
}
