package editor

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unsafe"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	xansi "github.com/charmbracelet/x/ansi"
	"github.com/atotto/clipboard"
	"github.com/sqltui/sql/internal/config"
	"github.com/sqltui/sql/internal/db"
	sqlformat "github.com/sqltui/sql/internal/format"
	"github.com/sqltui/sql/internal/ui/editor/vim"
)

// ExecuteBlockMsg asks the app to run the logical block under the cursor.
type ExecuteBlockMsg struct{ SQL string }

// clipboardPasteMsg carries text read from the OS clipboard.
type clipboardPasteMsg string

// readClipboardCmd returns a Cmd that reads from the OS clipboard.
func readClipboardCmd() tea.Cmd {
	return func() tea.Msg {
		text, err := clipboard.ReadAll()
		if err != nil {
			return nil
		}
		return clipboardPasteMsg(text)
	}
}

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

type textareaModeSnapshot struct {
	Content   string
	Cursor    textPos
	Selection textareaSelection
	ScrollY   int
}

type vimModeSnapshot struct {
	Content  string
	Snapshot vim.Snapshot
}

type pendingTextareaReturn struct {
	Valid    bool
	Baseline vimModeSnapshot
	Target   textareaModeSnapshot
}

type pendingVimReturn struct {
	Valid    bool
	Baseline textareaModeSnapshot
	Target   vimModeSnapshot
}

// Tab is a single open query buffer.
type Tab struct {
	Title                 string
	Path                  string
	ta                    textarea.Model
	selection             textareaSelection
	pendingTextareaReturn pendingTextareaReturn
	pendingVimReturn      pendingVimReturn
	vim                   *vim.State // non-nil when vim mode is active
	Dirty                 bool
}

// Model is the editor pane: a tab bar + textarea (or vim buffer).
type Model struct {
	cfg               *config.Config
	width             int
	height            int // total height including tab bar
	tabs              []Tab
	active            int
	focused           bool
	mouseSelecting    bool
	mouseSelectingVim bool
	mouseVimPrevMode  vim.Mode
	popup             completionPopup
	schemaItems       []CompletionItem // typed schema items pushed from the app
	schema            *db.Schema
	vimEnabled        bool
	blinkID           int
	blinkOn           bool

	// Goto-line bar (Ctrl+G, or : in vim normal mode).
	gotoOpen  bool
	gotoInput []rune

	// Tab rename bar (T in refactor popup).
	renameTabOpen   bool
	renameTabInput  []rune
	renameTabCursor int
	renameTabErr    string
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

	popupDetailStyle = popupItemStyle.Copy().
				Foreground(lipgloss.Color("#8f8f8f"))

	popupSelectedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#ffffff")).
				Background(lipgloss.Color("#007acc")).
				Bold(true)

	popupSelectedDetailStyle = popupSelectedStyle.Copy().
					Foreground(lipgloss.Color("#d7efff")).
					Bold(false)

	selectionStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#264f78"))

	missingSchemaRefStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#f14c4c"))
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

	case clipboardPasteMsg:
		return m.insertPasteText(string(msg))

	case tea.KeyMsg:
		// When goto-line bar is open, route all keys to it.
		if m.gotoOpen {
			return m.updateGotoLine(msg)
		}
		// When rename tab bar is open, route all keys to it.
		if m.renameTabOpen {
			return m.updateRenameTab(msg)
		}

		// Intercept paste before anything else — read from OS clipboard
		// directly so we never emulate typing and never trigger autocomplete.
		// ctrl+v is intercepted here. shift+insert cannot be intercepted: the
		// terminal sends clipboard content as raw keystrokes with no bracketed
		// paste markers, so ctrl+v is the recommended paste shortcut.
		if msg.String() == "ctrl+v" {
			return m, readClipboardCmd()
		}
		// Bracketed paste (msg.Paste == true): same direct-insert path.
		if msg.Paste {
			return m.insertPasteText(string(msg.Runes))
		}

		if m.isCommentShortcut(msg) {
			if m.vimEnabled {
				return m.commentCurrentLineVim()
			}
			return m.commentCurrentLineTextarea()
		}
		if m.isRefactorShortcut(msg) {
			return m.openRefactorPopup()
		}
		if handled, nm, cmd := m.handleBlockNavigationShortcut(msg); handled {
			return nm, cmd
		}

		// ── Vim mode path ────────────────────────────────────────────────
		if m.vimEnabled {
			return m.updateVim(msg)
		}

		// ── Non-vim path: popup then textarea ────────────────────────────
		if m.popup.visible {
			if m.popup.mode == popupModeRefactor {
				return m.handleRefactorPopupKey(msg, false)
			}
			switch msg.String() {
			case "esc":
				m.popup.visible = false
				m.popup.suppressed = true
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
			case "ctrl+e", "enter":
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
			if m.isFormatShortcut(msg) {
				return m.formatActiveBlockTextarea()
			}
			switch msg.String() {
			case "ctrl+e":
				sql := m.blockAtCursor()
				return m, func() tea.Msg { return ExecuteBlockMsg{SQL: sql} }
			case "ctrl+r":
				return m.openRefactorPopup()
			case "ctrl+g":
				return m.openGotoLine()
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
// insertPasteText inserts text directly into the active buffer without
// triggering autocomplete or going through the vim state machine.
func (m Model) insertPasteText(text string) (Model, tea.Cmd) {
	m.popup.visible = false
	m.popup.suppressed = false
	tab := &m.tabs[m.active]
	if m.vimEnabled && tab.vim != nil {
		oldVal := tab.vim.Buf.Value()
		tab.vim.Buf.InsertText(text)
		newVal := tab.vim.Buf.Value()
		if newVal != oldVal && tab.Path != "" {
			path := tab.Path
			return m, tea.Batch(func() tea.Msg { _ = os.WriteFile(path, []byte(newVal), 0644); return nil }, m.refreshVimInsertCursorBlink())
		}
		return m, m.refreshVimInsertCursorBlink()
	}
	// Non-vim: insert into textarea.
	tab.ta.InsertString(text)
	if tab.Path != "" {
		val := tab.ta.Value()
		path := tab.Path
		return m, func() tea.Msg { _ = os.WriteFile(path, []byte(val), 0644); return nil }
	}
	return m, nil
}

func (m Model) updateVim(msg tea.KeyMsg) (Model, tea.Cmd) {
	// App-level shortcuts that work regardless of vim mode.
	if m.isFormatShortcut(msg) {
		return m.formatActiveBlockVim()
	}
	if m.isRefactorShortcut(msg) {
		return m.openRefactorPopup()
	}
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
	case "ctrl+g":
		return m.openGotoLine()
	}

	tab := &m.tabs[m.active]
	if tab.vim == nil {
		return m, nil
	}

	if m.popup.visible && m.popup.mode == popupModeRefactor {
		return m.handleRefactorPopupKey(msg, true)
	}

	// In Insert mode, popup takes priority over vim key handling.
	if tab.vim.Mode == vim.ModeInsert && m.popup.visible {
		switch msg.String() {
		case "esc":
			m.popup.visible = false
			m.popup.suppressed = true
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
		case "ctrl+e", "enter":
			m = m.acceptCompletionVim()
			return m, m.restartVimInsertCursorBlink()
		}
		if len(msg.String()) == 1 && !isWordRune(rune(msg.String()[0])) {
			m.popup.visible = false
		}
	}

	// In Normal mode, `:` opens the goto-line cmdline.
	if tab.vim.Mode == vim.ModeNormal && msg.String() == ":" {
		return m.openGotoLine()
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
		if msg.Type == tea.KeyDelete {
			m.popup.visible = false
		} else {
			m.updatePopupVim()
		}
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

func (m Model) handleBlockNavigationShortcut(msg tea.KeyMsg) (bool, Model, tea.Cmd) {
	if msg.Alt {
		switch msg.Type {
		case tea.KeyUp:
			if m.vimEnabled {
				nm, cmd := m.jumpToAdjacentBlockVim(-1)
				return true, nm, cmd
			}
			nm, cmd := m.jumpToAdjacentBlockTextarea(-1)
			return true, nm, cmd
		case tea.KeyDown:
			if m.vimEnabled {
				nm, cmd := m.jumpToAdjacentBlockVim(1)
				return true, nm, cmd
			}
			nm, cmd := m.jumpToAdjacentBlockTextarea(1)
			return true, nm, cmd
		}
	}
	switch msg.String() {
	case "alt+up":
		if m.vimEnabled {
			nm, cmd := m.jumpToAdjacentBlockVim(-1)
			return true, nm, cmd
		}
		nm, cmd := m.jumpToAdjacentBlockTextarea(-1)
		return true, nm, cmd
	case "alt+down":
		if m.vimEnabled {
			nm, cmd := m.jumpToAdjacentBlockVim(1)
			return true, nm, cmd
		}
		nm, cmd := m.jumpToAdjacentBlockTextarea(1)
		return true, nm, cmd
	default:
		return false, m, nil
	}
}

func (m Model) jumpToAdjacentBlockTextarea(direction int) (Model, tea.Cmd) {
	if m.active < 0 || m.active >= len(m.tabs) {
		return m, nil
	}
	tab := &m.tabs[m.active]
	line, col := tab.ta.Line(), tab.ta.LineInfo().CharOffset
	targetLine, ok := adjacentBlockLine(tab.ta.Value(), line, direction)
	if !ok {
		m.popup.visible = false
		return m, nil
	}
	setTextareaCursor(&tab.ta, targetLine, col)
	revealTextareaSourceLine(&tab.ta, targetLine)
	tab.selection = textareaSelection{}
	m.popup.visible = false
	return m, nil
}

func (m Model) jumpToAdjacentBlockVim(direction int) (Model, tea.Cmd) {
	if m.active < 0 || m.active >= len(m.tabs) {
		return m, nil
	}
	tab := &m.tabs[m.active]
	if tab.vim == nil {
		return m, nil
	}
	buf := tab.vim.Buf
	targetLine, ok := adjacentBlockLine(buf.Value(), buf.CursorRow(), direction)
	if !ok {
		m.popup.visible = false
		return m, m.refreshVimInsertCursorBlink()
	}
	buf.SetCursor(targetLine, buf.CursorCol())
	tab.vim.ScrollToReveal(maxInt(1, m.height-1))
	if tab.vim.Mode == vim.ModeVisual || tab.vim.Mode == vim.ModeVisualLine {
		tab.vim.Mode = vim.ModeNormal
	}
	m.popup.visible = false
	return m, m.refreshVimInsertCursorBlink()
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

func (m Model) formatActiveBlockTextarea() (Model, tea.Cmd) {
	if m.active < 0 || m.active >= len(m.tabs) {
		return m, nil
	}
	tab := &m.tabs[m.active]
	text := tab.ta.Value()
	line, col := tab.ta.Line(), tab.ta.LineInfo().CharOffset
	updated, nextLine, nextCol, changed := formatBlockInText(text, line, col)
	if !changed {
		return m, nil
	}
	tab.ta.SetValue(updated)
	setTextareaCursor(&tab.ta, nextLine, nextCol)
	tab.selection.Active = false
	if m.popup.visible {
		m.updatePopup()
	}
	return m, m.saveActiveTextareaCmd()
}

func (m Model) formatActiveBlockVim() (Model, tea.Cmd) {
	if m.active < 0 || m.active >= len(m.tabs) {
		return m, nil
	}
	tab := &m.tabs[m.active]
	if tab.vim == nil {
		return m, nil
	}
	buf := tab.vim.Buf
	text := buf.Value()
	line, col := buf.CursorRow(), buf.CursorCol()
	updated, nextLine, nextCol, changed := formatBlockInText(text, line, col)
	if !changed {
		return m, m.refreshVimInsertCursorBlink()
	}
	oldTopRow := tab.vim.TopRow
	buf.PushUndo()
	buf.SetValue(updated)
	buf.SetCursor(nextLine, nextCol)
	tab.vim.TopRow = clampInt(oldTopRow, 0, maxInt(0, buf.LineCount()-1))
	if tab.vim.Mode == vim.ModeVisual || tab.vim.Mode == vim.ModeVisualLine {
		tab.vim.Mode = vim.ModeNormal
	}
	if m.popup.visible {
		m.popup.visible = false
	}
	return m, tea.Batch(saveTextToPathCmd(tab.Path, buf.Value()), m.refreshVimInsertCursorBlink())
}

func (m Model) nameTableAliasRefactorTextarea() (Model, tea.Cmd) {
	if m.active < 0 || m.active >= len(m.tabs) {
		return m, nil
	}
	tab := &m.tabs[m.active]
	text := tab.ta.Value()
	line, col := tab.ta.Line(), tab.ta.LineInfo().CharOffset
	updated, nextLine, nextCol, changed := applyNameTableAliasRefactor(text, line, col)
	m.popup.visible = false
	if !changed {
		return m, nil
	}
	tab.ta.SetValue(updated)
	setTextareaCursor(&tab.ta, nextLine, nextCol)
	tab.selection.Active = false
	return m, m.saveActiveTextareaCmd()
}

func (m Model) expandSelectStarRefactorTextarea() (Model, tea.Cmd) {
	if m.active < 0 || m.active >= len(m.tabs) {
		return m, nil
	}
	tab := &m.tabs[m.active]
	text := tab.ta.Value()
	line, col := tab.ta.Line(), tab.ta.LineInfo().CharOffset
	updated, nextLine, nextCol, changed := applyExpandSelectStarRefactor(text, line, col, m.schema)
	m.popup.visible = false
	if !changed {
		return m, nil
	}
	tab.ta.SetValue(updated)
	setTextareaCursor(&tab.ta, nextLine, nextCol)
	tab.selection.Active = false
	return m, m.saveActiveTextareaCmd()
}

func (m Model) nameTableAliasRefactorVim() (Model, tea.Cmd) {
	if m.active < 0 || m.active >= len(m.tabs) {
		return m, nil
	}
	tab := &m.tabs[m.active]
	if tab.vim == nil {
		return m, nil
	}
	buf := tab.vim.Buf
	text := buf.Value()
	line, col := buf.CursorRow(), buf.CursorCol()
	updated, nextLine, nextCol, changed := applyNameTableAliasRefactor(text, line, col)
	m.popup.visible = false
	if !changed {
		return m, m.refreshVimInsertCursorBlink()
	}
	oldTopRow := tab.vim.TopRow
	buf.PushUndo()
	buf.SetValue(updated)
	buf.SetCursor(nextLine, nextCol)
	tab.vim.TopRow = clampInt(oldTopRow, 0, maxInt(0, buf.LineCount()-1))
	if tab.vim.Mode == vim.ModeVisual || tab.vim.Mode == vim.ModeVisualLine {
		tab.vim.Mode = vim.ModeNormal
	}
	return m, tea.Batch(saveTextToPathCmd(tab.Path, buf.Value()), m.refreshVimInsertCursorBlink())
}

func (m Model) expandSelectStarRefactorVim() (Model, tea.Cmd) {
	if m.active < 0 || m.active >= len(m.tabs) {
		return m, nil
	}
	tab := &m.tabs[m.active]
	if tab.vim == nil {
		return m, nil
	}
	buf := tab.vim.Buf
	text := buf.Value()
	line, col := buf.CursorRow(), buf.CursorCol()
	updated, nextLine, nextCol, changed := applyExpandSelectStarRefactor(text, line, col, m.schema)
	m.popup.visible = false
	if !changed {
		return m, m.refreshVimInsertCursorBlink()
	}
	oldTopRow := tab.vim.TopRow
	buf.PushUndo()
	buf.SetValue(updated)
	buf.SetCursor(nextLine, nextCol)
	tab.vim.TopRow = clampInt(oldTopRow, 0, maxInt(0, buf.LineCount()-1))
	if tab.vim.Mode == vim.ModeVisual || tab.vim.Mode == vim.ModeVisualLine {
		tab.vim.Mode = vim.ModeNormal
	}
	return m, tea.Batch(saveTextToPathCmd(tab.Path, buf.Value()), m.refreshVimInsertCursorBlink())
}

func (m Model) selectToUpdateRefactorTextarea(appendBelow bool) (Model, tea.Cmd) {
	if m.active < 0 || m.active >= len(m.tabs) {
		return m, nil
	}
	tab := &m.tabs[m.active]
	text := tab.ta.Value()
	line, col := tab.ta.Line(), tab.ta.LineInfo().CharOffset
	updated, nextLine, nextCol, changed := applySelectToUpdateRefactor(text, line, col, appendBelow, m.schema)
	m.popup.visible = false
	if !changed {
		return m, nil
	}
	tab.ta.SetValue(updated)
	setTextareaCursor(&tab.ta, nextLine, nextCol)
	tab.selection.Active = false
	return m, m.saveActiveTextareaCmd()
}

func (m Model) selectToUpdateRefactorVim(appendBelow bool) (Model, tea.Cmd) {
	if m.active < 0 || m.active >= len(m.tabs) {
		return m, nil
	}
	tab := &m.tabs[m.active]
	if tab.vim == nil {
		return m, nil
	}
	buf := tab.vim.Buf
	text := buf.Value()
	line, col := buf.CursorRow(), buf.CursorCol()
	updated, nextLine, nextCol, changed := applySelectToUpdateRefactor(text, line, col, appendBelow, m.schema)
	m.popup.visible = false
	if !changed {
		return m, m.refreshVimInsertCursorBlink()
	}
	oldTopRow := tab.vim.TopRow
	buf.PushUndo()
	buf.SetValue(updated)
	buf.SetCursor(nextLine, nextCol)
	tab.vim.TopRow = clampInt(oldTopRow, 0, maxInt(0, buf.LineCount()-1))
	if tab.vim.Mode == vim.ModeVisual || tab.vim.Mode == vim.ModeVisualLine {
		tab.vim.Mode = vim.ModeNormal
	}
	return m, tea.Batch(saveTextToPathCmd(tab.Path, buf.Value()), m.refreshVimInsertCursorBlink())
}

func (m Model) updateToSelectRefactorTextarea(appendBelow bool) (Model, tea.Cmd) {
	if m.active < 0 || m.active >= len(m.tabs) {
		return m, nil
	}
	tab := &m.tabs[m.active]
	text := tab.ta.Value()
	line, col := tab.ta.Line(), tab.ta.LineInfo().CharOffset
	updated, nextLine, nextCol, changed := applyUpdateToSelectRefactor(text, line, col, appendBelow)
	m.popup.visible = false
	if !changed {
		return m, nil
	}
	tab.ta.SetValue(updated)
	setTextareaCursor(&tab.ta, nextLine, nextCol)
	tab.selection.Active = false
	return m, m.saveActiveTextareaCmd()
}

func (m Model) updateToSelectRefactorVim(appendBelow bool) (Model, tea.Cmd) {
	if m.active < 0 || m.active >= len(m.tabs) {
		return m, nil
	}
	tab := &m.tabs[m.active]
	if tab.vim == nil {
		return m, nil
	}
	buf := tab.vim.Buf
	text := buf.Value()
	line, col := buf.CursorRow(), buf.CursorCol()
	updated, nextLine, nextCol, changed := applyUpdateToSelectRefactor(text, line, col, appendBelow)
	m.popup.visible = false
	if !changed {
		return m, m.refreshVimInsertCursorBlink()
	}
	oldTopRow := tab.vim.TopRow
	buf.PushUndo()
	buf.SetValue(updated)
	buf.SetCursor(nextLine, nextCol)
	tab.vim.TopRow = clampInt(oldTopRow, 0, maxInt(0, buf.LineCount()-1))
	if tab.vim.Mode == vim.ModeVisual || tab.vim.Mode == vim.ModeVisualLine {
		tab.vim.Mode = vim.ModeNormal
	}
	return m, tea.Batch(saveTextToPathCmd(tab.Path, buf.Value()), m.refreshVimInsertCursorBlink())
}

func (m Model) identityInsertRefactorTextarea() (Model, tea.Cmd) {
	if m.active < 0 || m.active >= len(m.tabs) {
		return m, nil
	}
	tab := &m.tabs[m.active]
	text := tab.ta.Value()
	line, col := tab.ta.Line(), tab.ta.LineInfo().CharOffset
	updated, nextLine, nextCol, changed := applyIdentityInsertRefactor(text, line, col)
	m.popup.visible = false
	if !changed {
		return m, nil
	}
	tab.ta.SetValue(updated)
	setTextareaCursor(&tab.ta, nextLine, nextCol)
	tab.selection.Active = false
	return m, m.saveActiveTextareaCmd()
}

func (m Model) identityInsertRefactorVim() (Model, tea.Cmd) {
	if m.active < 0 || m.active >= len(m.tabs) {
		return m, nil
	}
	tab := &m.tabs[m.active]
	if tab.vim == nil {
		return m, nil
	}
	buf := tab.vim.Buf
	text := buf.Value()
	line, col := buf.CursorRow(), buf.CursorCol()
	updated, nextLine, nextCol, changed := applyIdentityInsertRefactor(text, line, col)
	m.popup.visible = false
	if !changed {
		return m, m.refreshVimInsertCursorBlink()
	}
	oldTopRow := tab.vim.TopRow
	buf.PushUndo()
	buf.SetValue(updated)
	buf.SetCursor(nextLine, nextCol)
	tab.vim.TopRow = clampInt(oldTopRow, 0, maxInt(0, buf.LineCount()-1))
	if tab.vim.Mode == vim.ModeVisual || tab.vim.Mode == vim.ModeVisualLine {
		tab.vim.Mode = vim.ModeNormal
	}
	return m, tea.Batch(saveTextToPathCmd(tab.Path, buf.Value()), m.refreshVimInsertCursorBlink())
}

func formatBlockInText(text string, cursorLine, cursorCol int) (string, int, int, bool) {
	start, end, ok := detectBlockRange(text, cursorLine)
	if !ok {
		return text, cursorLine, cursorCol, false
	}
	lines := strings.Split(text, "\n")
	if len(lines) == 0 {
		return text, cursorLine, cursorCol, false
	}
	if cursorLine < start {
		cursorLine = start
	}
	if cursorLine > end {
		cursorLine = end
	}
	block := strings.Join(lines[start:end+1], "\n")
	formatted := sqlformat.Format(block)
	if normalizeLineEndings(formatted) == normalizeLineEndings(block) {
		return text, cursorLine, cursorCol, false
	}
	formattedLines := strings.Split(formatted, "\n")
	updatedLines := append(append([]string{}, lines[:start]...), formattedLines...)
	updatedLines = append(updatedLines, lines[end+1:]...)
	updated := strings.Join(updatedLines, "\n")
	relLine := cursorLine - start
	if relLine < 0 {
		relLine = 0
	}
	if relLine >= len(formattedLines) {
		relLine = len(formattedLines) - 1
	}
	nextLine := start + relLine
	nextCol := clampInt(cursorCol, 0, len([]rune(formattedLines[relLine])))
	return updated, nextLine, nextCol, true
}

func normalizeLineEndings(s string) string {
	return strings.ReplaceAll(s, "\r\n", "\n")
}

func (m Model) isFormatShortcut(msg tea.KeyMsg) bool {
	configured := "ctrl+shift+f"
	if m.cfg != nil && m.cfg.Keys.FormatQuery != "" {
		configured = strings.ToLower(m.cfg.Keys.FormatQuery)
	}
	key := strings.ToLower(msg.String())
	if key == configured {
		return true
	}
	return key == normalizeCtrlLetterShortcut(configured)
}

func (m Model) isRefactorShortcut(msg tea.KeyMsg) bool {
	return strings.EqualFold(msg.String(), "ctrl+r")
}

func normalizeCtrlLetterShortcut(key string) string {
	if !strings.HasPrefix(key, "ctrl+shift+") {
		return key
	}
	letter := strings.TrimPrefix(key, "ctrl+shift+")
	if len(letter) != 1 {
		return key
	}
	r := rune(letter[0])
	if r < 'a' || r > 'z' {
		return key
	}
	return "ctrl+" + letter
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
	commented, changedAt, delta := commentSQLLine(lines[line])
	lines[line] = commented

	nextLine, nextCol := line, col
	if line < len(lines)-1 {
		nextLine = line + 1
		nextCol = clampInt(col, 0, len([]rune(lines[nextLine])))
	} else {
		nextCol = adjustCommentToggleCol(col, changedAt, delta)
		nextCol = clampInt(nextCol, 0, len([]rune(lines[nextLine])))
	}

	return strings.Join(lines, "\n"), nextLine, nextCol
}

func commentSQLLine(line string) (string, int, int) {
	runes := []rune(line)
	insertAt := 0
	for insertAt < len(runes) && (runes[insertAt] == ' ' || runes[insertAt] == '\t') {
		insertAt++
	}
	if insertAt+1 < len(runes) && runes[insertAt] == '-' && runes[insertAt+1] == '-' {
		removeLen := 2
		if insertAt+2 < len(runes) && runes[insertAt+2] == ' ' {
			removeLen = 3
		}
		uncommented := string(runes[:insertAt]) + string(runes[insertAt+removeLen:])
		return uncommented, insertAt, -removeLen
	}
	comment := []rune("-- ")
	commented := string(runes[:insertAt]) + string(comment) + string(runes[insertAt:])
	return commented, insertAt, len(comment)
}

func adjustCommentToggleCol(col, changedAt, delta int) int {
	if delta >= 0 {
		if col >= changedAt {
			return col + delta
		}
		return col
	}
	removedLen := -delta
	if col <= changedAt {
		return col
	}
	if col <= changedAt+removedLen {
		return changedAt
	}
	return col + delta
}

func (m Model) isCommentShortcut(msg tea.KeyMsg) bool {
	if msg.Type == tea.KeyCtrlBackslash {
		return true
	}
	if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && msg.Runes[0] == rune(28) {
		return true
	}
	key := strings.ToLower(msg.String())
	if key == "ctrl+\\" {
		return true
	}
	configured := "ctrl+\\"
	if m.cfg != nil && m.cfg.Keys.ToggleComment != "" {
		configured = strings.ToLower(m.cfg.Keys.ToggleComment)
	}
	if configured == key {
		return true
	}
	if configured == "ctrl+/" || configured == "ctrl+_" {
		return msg.Type == tea.KeyCtrlUnderscore || (msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && msg.Runes[0] == rune(31)) || key == "ctrl+/" || key == "ctrl+_"
	}
	return false
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

// SaveActiveTabCmd returns a Cmd that writes the active tab's content to disk.
// Used by external callers (e.g. MCP handler) after programmatically setting content.
func (m Model) SaveActiveTabCmd() tea.Cmd {
	if m.active < 0 || m.active >= len(m.tabs) {
		return nil
	}
	tab := m.tabs[m.active]
	if tab.Path == "" {
		return nil
	}
	val := tab.ta.Value()
	if m.vimEnabled && tab.vim != nil {
		val = tab.vim.Buf.Value()
	}
	return saveTextToPathCmd(tab.Path, val)
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

	// Overlay goto-line bar at the bottom of the editor.
	if m.gotoOpen {
		bar := m.renderGotoLineBar()
		lines := strings.Split(base, "\n")
		if len(lines) > 0 {
			lines[len(lines)-1] = bar
			base = strings.Join(lines, "\n")
		}
		return base
	}

	// Overlay rename-tab bar at the bottom of the editor.
	if m.renameTabOpen {
		bar := m.renderRenameTabBar()
		lines := strings.Split(base, "\n")
		if len(lines) > 0 {
			lines[len(lines)-1] = bar
			base = strings.Join(lines, "\n")
		}
		return base
	}

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
	hlLines := highlightLinesWithSchema(allText, m.schema)
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
	hlLines := highlightLinesWithSchema(text, m.schema)
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
	tabsStr := label + strings.Join(tabs, "")
	tabsWidth := lipgloss.Width(tabsStr)

	hintStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#555555")).
		Background(lipgloss.Color("#1e1e1e"))
	keyStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#888888")).
		Background(lipgloss.Color("#1e1e1e"))
	hint := hintStyle.Render("Alt+") + keyStyle.Render("h") + hintStyle.Render("/") + keyStyle.Render("l") +
		hintStyle.Render(" switch  ") +
		keyStyle.Render("Ctrl+N") + hintStyle.Render(" new  ") +
		keyStyle.Render("Ctrl+V") + hintStyle.Render(" paste  ") +
		keyStyle.Render("F1") + hintStyle.Render(" help")
	hintWidth := lipgloss.Width(hint)

	gap := m.width - tabsWidth - hintWidth
	if gap < 1 {
		gap = 1
	}
	bar := tabsStr + strings.Repeat(" ", gap) + hint
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
		m.mouseSelecting = false
		return m.clickVimContent(x, y-1)
	}
	m.mouseSelecting = false
	return m.clickTextareaContent(x, y-1)
}

// MouseSelecting reports whether a mouse drag selection is in progress.
func (m Model) MouseSelecting() bool { return m.mouseSelecting }

// Mouse handles an editor-local mouse event.
// x/y are relative to the editor view, including the tab bar at row 0.
func (m Model) Mouse(msg tea.MouseMsg, x, y int) Model {
	if m.active < 0 || m.active >= len(m.tabs) {
		return m
	}
	m.popup.visible = false
	if m.vimEnabled && m.tabs[m.active].vim != nil {
		switch msg.Action {
		case tea.MouseActionPress:
			if msg.Button != tea.MouseButtonLeft {
				return m
			}
			if y == 0 {
				m.mouseSelecting = false
				m.mouseSelectingVim = false
				return m.Click(x, y)
			}
			return m.startVimMouseSelection(x, y)
		case tea.MouseActionMotion:
			if !m.mouseSelecting || !m.mouseSelectingVim {
				return m
			}
			return m.dragVimMouseSelection(x, y)
		case tea.MouseActionRelease:
			if !m.mouseSelecting || !m.mouseSelectingVim {
				return m
			}
			m = m.dragVimMouseSelection(x, y)
			tab := &m.tabs[m.active]
			if tab.vim != nil {
				start, end := tab.vim.SelectionRange()
				if start == end {
					tab.vim.Mode = m.mouseVimPrevMode
				}
			}
			m.mouseSelecting = false
			m.mouseSelectingVim = false
			return m
		default:
			return m
		}
	}

	switch msg.Action {
	case tea.MouseActionPress:
		if msg.Button != tea.MouseButtonLeft {
			return m
		}
		return m.startTextareaMouseSelection(x, y)
	case tea.MouseActionMotion:
		if !m.mouseSelecting {
			return m
		}
		return m.dragTextareaMouseSelection(x, y)
	case tea.MouseActionRelease:
		if !m.mouseSelecting {
			return m
		}
		m = m.dragTextareaMouseSelection(x, y)
		m.mouseSelecting = false
		m.mouseSelectingVim = false
		if _, _, ok := m.textareaSelectionRange(); !ok {
			m.tabs[m.active].selection.Active = false
		}
		return m
	default:
		return m
	}
}

func (m Model) clickVimContent(x, y int) Model {
	tab := &m.tabs[m.active]
	if tab.vim == nil {
		return m
	}
	row, col, ok := m.vimPositionAt(x, y)
	if !ok {
		return m
	}
	tab.vim.Buf.SetCursor(row, col)
	if tab.vim.Mode == vim.ModeVisual || tab.vim.Mode == vim.ModeVisualLine {
		tab.vim.Mode = vim.ModeNormal
	}
	tab.selection.Active = false
	tab.vim.ScrollToReveal(maxInt(1, m.height-1))
	return m
}

func (m Model) startVimMouseSelection(x, y int) Model {
	tab := &m.tabs[m.active]
	if tab.vim == nil {
		return m
	}
	row, col, ok := m.vimPositionAt(x, y-1)
	if !ok {
		return m
	}
	tab.vim.Buf.SetCursor(row, col)
	m.mouseSelecting = true
	m.mouseSelectingVim = true
	m.mouseVimPrevMode = tab.vim.Mode
	tab.vim.Anchor = vim.Pos{Row: row, Col: col}
	tab.vim.Mode = vim.ModeVisual
	tab.selection.Active = false
	tab.vim.ScrollToReveal(maxInt(1, m.height-1))
	return m
}

func (m Model) dragVimMouseSelection(x, y int) Model {
	tab := &m.tabs[m.active]
	if tab.vim == nil {
		return m
	}
	row, col, ok := m.vimPositionAt(x, clampInt(y-1, 0, maxInt(0, m.height-2)))
	if !ok {
		return m
	}
	tab.vim.Buf.SetCursor(row, col)
	tab.vim.Mode = vim.ModeVisual
	tab.vim.ScrollToReveal(maxInt(1, m.height-1))
	return m
}

func (m Model) vimPositionAt(x, y int) (int, int, bool) {
	tab := &m.tabs[m.active]
	if tab.vim == nil || tab.vim.Buf == nil || tab.vim.Buf.LineCount() == 0 {
		return 0, 0, false
	}
	buf := tab.vim.Buf
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
	return row, col, true
}

func (m Model) clickTextareaContent(x, y int) Model {
	tab := &m.tabs[m.active]
	pos := m.textareaPositionAt(x, y)
	setTextareaCursor(&tab.ta, pos.Line, pos.Col)
	tab.selection.Active = false
	return m
}

func (m Model) startTextareaMouseSelection(x, y int) Model {
	tab := &m.tabs[m.active]
	if y <= 0 {
		tab.selection.Active = false
		m.mouseSelecting = false
		return m
	}
	pos := m.textareaPositionAt(x, clampInt(y-1, 0, maxInt(0, m.height-2)))
	setTextareaCursor(&tab.ta, pos.Line, pos.Col)
	tab.selection = textareaSelection{Active: true, Anchor: pos}
	m.mouseSelecting = true
	return m
}

func (m Model) dragTextareaMouseSelection(x, y int) Model {
	tab := &m.tabs[m.active]
	pos := m.textareaPositionAt(x, clampInt(y-1, 0, maxInt(0, m.height-2)))
	setTextareaCursor(&tab.ta, pos.Line, pos.Col)
	tab.selection.Active = true
	return m
}

func (m Model) textareaPositionAt(x, y int) textPos {
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
			return textPos{Line: activeSrcLine, Col: rowOffsets[activeSrcLine] + minInt(targetX, plainWidth)}
		}
		rowOffsets[activeSrcLine] += plainWidth
	}

	lastLine := len(lines) - 1
	return textPos{Line: lastLine, Col: len([]rune(lines[lastLine]))}
}

// Value returns the active tab's text content.
func (m Model) Value() string {
	if m.vimEnabled && m.tabs[m.active].vim != nil {
		return m.tabs[m.active].vim.Buf.Value()
	}
	return m.tabs[m.active].ta.Value()
}

// CurrentBlock returns the logical SQL block at the current cursor position.
func (m Model) CurrentBlock() string {
	return m.blockAtCursor()
}

// WordAtCursor returns the SQL identifier token under the cursor, or "".
func (m Model) WordAtCursor() string {
	if m.active < 0 || m.active >= len(m.tabs) {
		return ""
	}
	var text string
	var cursor textPos
	if m.vimEnabled && m.tabs[m.active].vim != nil {
		text = m.tabs[m.active].vim.Buf.Value()
		row := m.tabs[m.active].vim.Buf.CursorRow()
		col := m.tabs[m.active].vim.Buf.CursorCol()
		cursor = textPos{Line: row, Col: col}
	} else {
		text = m.tabs[m.active].ta.Value()
		cursor = textareaCursorPos(&m.tabs[m.active].ta)
	}
	tokens := scanSQLTokens(text)
	offset := textPosToRuneOffset(text, cursor)
	return currentSQLWordAtCursor(tokens, offset)
}

// InsertText inserts text at the current cursor in the active tab and schedules persistence.
func (m Model) InsertText(text string) (Model, tea.Cmd) {
	if strings.TrimSpace(text) == "" || m.active < 0 || m.active >= len(m.tabs) {
		return m, nil
	}
	tab := &m.tabs[m.active]
	m.popup.visible = false
	if m.vimEnabled && tab.vim != nil {
		buf := tab.vim.Buf
		oldTopRow := tab.vim.TopRow
		buf.PushUndo()
		insertTextIntoVimBuffer(buf, text)
		tab.vim.TopRow = clampInt(oldTopRow, 0, maxInt(0, buf.LineCount()-1))
		if tab.vim.Mode == vim.ModeVisual || tab.vim.Mode == vim.ModeVisualLine {
			tab.vim.Mode = vim.ModeNormal
		}
		tab.Dirty = true
		return m, tea.Batch(saveTextToPathCmd(tab.Path, buf.Value()), m.refreshVimInsertCursorBlink())
	}
	tab.selection.Active = false
	tab.ta.InsertString(text)
	tab.Dirty = true
	return m, m.saveActiveTextareaCmd()
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
	if m.vimEnabled {
		// Vim → textarea: push vim buffer content into textarea.
		for i := range m.tabs {
			tab := &m.tabs[i]
			if tab.vim == nil {
				continue
			}
			currentVim := snapshotVimMode(tab)
			tab.ta.SetValue(currentVim.Content)
			tab.vim = nil

			if tab.pendingTextareaReturn.Valid && currentVim == tab.pendingTextareaReturn.Baseline {
				restoreTextareaMode(tab, tab.pendingTextareaReturn.Target)
			} else {
				restoreTabCursor(tab, currentVim.Snapshot.Cursor.Row, currentVim.Snapshot.Cursor.Col)
				tab.selection = textareaSelection{}
				setTextareaViewportYOffset(&tab.ta, textareaViewportOffsetForSourceLine(&tab.ta, currentVim.Snapshot.TopRow))
			}

			tab.pendingVimReturn = pendingVimReturn{
				Valid:    true,
				Baseline: snapshotTextareaMode(tab),
				Target:   currentVim,
			}
			tab.pendingTextareaReturn = pendingTextareaReturn{}
		}
		m.vimEnabled = false
		m.mouseSelecting = false
		// Re-focus active textarea.
		if m.focused {
			_ = m.tabs[m.active].ta.Focus()
		}
	} else {
		// Textarea → vim: load textarea content into new vim states.
		for i := range m.tabs {
			tab := &m.tabs[i]
			currentTextarea := snapshotTextareaMode(tab)
			vs := vim.NewState()
			vs.Buf.SetValue(currentTextarea.Content)
			vs.SetSize(m.width, m.height-1)
			tab.vim = vs

			if tab.pendingVimReturn.Valid && currentTextarea == tab.pendingVimReturn.Baseline {
				vs.RestoreSnapshot(tab.pendingVimReturn.Target.Snapshot)
			} else {
				restoreTabCursor(tab, currentTextarea.Cursor.Line, currentTextarea.Cursor.Col)
				vs.TopRow = textareaSourceLineForViewportOffset(&tab.ta, currentTextarea.ScrollY)
			}

			tab.pendingTextareaReturn = pendingTextareaReturn{
				Valid:    true,
				Baseline: snapshotVimMode(tab),
				Target:   currentTextarea,
			}
			tab.pendingVimReturn = pendingVimReturn{}
		}
		m.vimEnabled = true
		m.tabs[m.active].ta.Blur()
		m.mouseSelecting = false
	}
	return m
}

func snapshotTextareaMode(tab *Tab) textareaModeSnapshot {
	if tab == nil {
		return textareaModeSnapshot{}
	}
	content := tab.ta.Value()
	cursor := clampTextPosToValue(content, textareaCursorPos(&tab.ta))
	selection := normalizeTextareaSelection(content, tab.selection, cursor)
	return textareaModeSnapshot{
		Content:   content,
		Cursor:    cursor,
		Selection: selection,
		ScrollY:   textareaViewportYOffset(&tab.ta),
	}
}

func restoreTextareaMode(tab *Tab, snapshot textareaModeSnapshot) {
	if tab == nil {
		return
	}
	tab.ta.SetValue(snapshot.Content)
	setTextareaCursor(&tab.ta, snapshot.Cursor.Line, snapshot.Cursor.Col)
	tab.selection = normalizeTextareaSelection(snapshot.Content, snapshot.Selection, snapshot.Cursor)
	setTextareaViewportYOffset(&tab.ta, snapshot.ScrollY)
}

func snapshotVimMode(tab *Tab) vimModeSnapshot {
	if tab == nil || tab.vim == nil {
		return vimModeSnapshot{}
	}
	return vimModeSnapshot{Content: tab.vim.Buf.Value(), Snapshot: tab.vim.Snapshot()}
}

func normalizeTextareaSelection(content string, selection textareaSelection, cursor textPos) textareaSelection {
	if !selection.Active {
		return textareaSelection{}
	}
	selection.Anchor = clampTextPosToValue(content, selection.Anchor)
	cursor = clampTextPosToValue(content, cursor)
	if selection.Anchor == cursor {
		return textareaSelection{}
	}
	return selection
}

func textareaViewportYOffset(ta *textarea.Model) int {
	vp := textareaViewport(ta)
	if vp == nil {
		return 0
	}
	return vp.YOffset
}

func setTextareaViewportYOffset(ta *textarea.Model, yOffset int) {
	vp := textareaViewport(ta)
	if vp == nil {
		return
	}
	maxOffset := maxInt(0, textareaVisualRowCount(ta)-vp.Height)
	if yOffset < 0 {
		yOffset = 0
	}
	if yOffset > maxOffset {
		yOffset = maxOffset
	}
	vp.YOffset = yOffset
}

func textareaViewport(ta *textarea.Model) *viewport.Model {
	if ta == nil {
		return nil
	}
	field := reflect.ValueOf(ta).Elem().FieldByName("viewport")
	if !field.IsValid() || field.IsNil() {
		return nil
	}
	return (*viewport.Model)(unsafe.Pointer(field.Pointer()))
}

func textareaSourceLineForViewportOffset(ta *textarea.Model, yOffset int) int {
	rows, lineCount := renderFullTextareaRows(ta)
	if len(rows) == 0 || lineCount == 0 {
		return 0
	}
	if yOffset < 0 {
		yOffset = 0
	}
	if yOffset >= len(rows) {
		yOffset = len(rows) - 1
	}
	gutterW := textareaGutterWidth(strings.Join(rows, "\n"), lineCount)
	activeSrcLine := 0
	for i, row := range rows {
		_, lineNum, hasLineNum, ok := parseTextareaLineNumberRow(row, gutterW)
		if !ok {
			continue
		}
		if hasLineNum {
			activeSrcLine = lineNum
		}
		if i == yOffset {
			return clampInt(activeSrcLine, 0, lineCount-1)
		}
	}
	return lineCount - 1
}

func textareaViewportOffsetForSourceLine(ta *textarea.Model, line int) int {
	rows, lineCount := renderFullTextareaRows(ta)
	if len(rows) == 0 || lineCount == 0 {
		return 0
	}
	line = clampInt(line, 0, lineCount-1)
	gutterW := textareaGutterWidth(strings.Join(rows, "\n"), lineCount)
	for i, row := range rows {
		_, lineNum, hasLineNum, ok := parseTextareaLineNumberRow(row, gutterW)
		if ok && hasLineNum && lineNum == line {
			return i
		}
	}
	return 0
}

func revealTextareaSourceLine(ta *textarea.Model, line int) {
	vp := textareaViewport(ta)
	if ta == nil || vp == nil || vp.Height <= 0 {
		return
	}
	targetOffset := textareaViewportOffsetForSourceLine(ta, line)
	if targetOffset < vp.YOffset {
		setTextareaViewportYOffset(ta, targetOffset)
		return
	}
	if targetOffset >= vp.YOffset+vp.Height {
		setTextareaViewportYOffset(ta, targetOffset-vp.Height+1)
	}
}

func textareaVisualRowCount(ta *textarea.Model) int {
	rows, _ := renderFullTextareaRows(ta)
	return len(rows)
}

func renderFullTextareaRows(ta *textarea.Model) ([]string, int) {
	if ta == nil {
		return nil, 0
	}
	content := ta.Value()
	lineCount := strings.Count(content, "\n") + 1
	clone := textarea.New()
	clone.SetWidth(ta.Width())
	clone.SetHeight(maxInt(1, len([]rune(content))+lineCount+4))
	clone.SetValue(content)
	return strings.Split(clone.View(), "\n"), lineCount
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
	m.tabs = append([]Tab{t}, m.tabs...)
	m.active = 0
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

// SetActiveTabContent replaces the content of the currently active tab.
func (m Model) SetActiveTabContent(sql string) Model {
	if m.active < 0 || m.active >= len(m.tabs) {
		return m
	}
	m.tabs[m.active].ta.SetValue(sql)
	if m.vimEnabled && m.tabs[m.active].vim != nil {
		m.tabs[m.active].vim.Buf.SetValue(sql)
	}
	return m
}

func cursorForTab(t Tab) (int, int) {
	if t.vim != nil {
		return t.vim.Buf.CursorRow(), t.vim.Buf.CursorCol()
	}
	return t.ta.Line(), t.ta.LineInfo().CharOffset
}

func insertTextIntoVimBuffer(buf *vim.Buffer, text string) {
	if buf == nil || text == "" {
		return
	}
	for _, r := range text {
		if r == '\n' {
			buf.InsertNewline()
			continue
		}
		buf.InsertRune(r)
	}
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

// SetSchema provides the raw introspected schema for context-aware editor actions.
func (m Model) SetSchema(schema *db.Schema) Model {
	m.schema = schema
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
	lineNum := buf.CursorRow()
	line := buf.Line(lineNum)
	col := buf.CursorCol()
	if items, word := joinCompletionItemsForText(buf.Value(), lineNum, col, m.schema); len(items) > 0 {
		m.popup = completionPopup{items: items, selected: 0, visible: true, word: word, mode: popupModeCompletion, title: "Join"}
		return
	}
	word := wordBefore(string(line), col)

	// A word character clears suppression; whitespace keeps it.
	if word != "" {
		m.popup.suppressed = false
	}
	if m.popup.suppressed && word == "" {
		m.popup.visible = false
		return
	}

	// Don't trigger on a new/blank line — only show contextual popup when
	// there is non-whitespace content before the cursor on this line.
	textBeforeCursor := string(line[:col])
	if word == "" && strings.TrimSpace(textBeforeCursor) == "" {
		m.popup.visible = false
		return
	}

	// Context-aware: if the query references specific tables, suggest only their columns.
	if ctxItems := contextualColumnItems(word, buf.Value(), lineNum, m.schema); len(ctxItems) > 0 {
		m.popup = completionPopup{items: ctxItems, selected: 0, visible: true, word: word, mode: popupModeCompletion}
		return
	}

	items := getCompletions(word, m.schemaItems, 8)
	if len(items) == 0 || word == "" {
		m.popup.visible = false
		return
	}
	m.popup = completionPopup{
		items:    popupItemsFromCompletions(items),
		selected: 0,
		visible:  true,
		word:     word,
		mode:     popupModeCompletion,
	}
}

// acceptCompletionVim applies the selected completion into the vim buffer.
func (m Model) acceptCompletionVim() Model {
	if !m.popup.visible || m.popup.mode != popupModeCompletion || m.popup.selected >= len(m.popup.items) {
		return m
	}
	completion := m.popup.items[m.popup.selected]
	word := m.popup.word

	buf := m.tabs[m.active].vim.Buf
	for range []rune(word) {
		buf.DeleteCharBefore()
	}
	spacePrefix := ""
	line := buf.Line(buf.CursorRow())
	if word == "" && buf.CursorCol() < len(line) && unicode.IsSpace(line[buf.CursorCol()]) {
		spacePrefix = string(line[buf.CursorCol()])
		buf.DeleteCharUnder()
	}
	insert := completionInsertText(completion, word)
	if spacePrefix != "" {
		insert = spacePrefix + strings.TrimLeft(insert, " \t")
	}
	// For keywords only: if the typed word is all lowercase, insert lowercase.
	// Schema names (tables, columns) always use their stored casing.
	if completion.InsertText == "" && completion.Kind == CompletionKindKeyword && word == strings.ToLower(word) {
		insert = strings.ToLower(completion.Text)
	}
	for _, r := range insert {
		buf.InsertRune(r)
	}
	m.popup.visible = false
	if suffix, items := autoJoinCompletionFollowup(buf.Value(), buf.CursorRow(), buf.CursorCol(), completion, m.schema); suffix != "" {
		for _, r := range suffix {
			buf.InsertRune(r)
		}
		if len(items) > 0 {
			m.popup = completionPopup{items: items, selected: 0, visible: true, mode: popupModeCompletion, title: "Join"}
		}
	}
	return m
}

// updatePopup recomputes completions based on the word at the current cursor.
func (m *Model) updatePopup() {
	ta := &m.tabs[m.active].ta
	if items, word := joinCompletionItemsForText(ta.Value(), ta.Line(), ta.LineInfo().CharOffset, m.schema); len(items) > 0 {
		m.popup = completionPopup{items: items, selected: 0, visible: true, word: word, mode: popupModeCompletion, title: "Join"}
		return
	}
	lines := strings.Split(ta.Value(), "\n")
	lineNum := ta.Line()
	lineText := ""
	if lineNum < len(lines) {
		lineText = lines[lineNum]
	}
	col := ta.LineInfo().CharOffset
	word := wordBefore(lineText, col)

	// A word character clears suppression; whitespace keeps it.
	if word != "" {
		m.popup.suppressed = false
	}
	if m.popup.suppressed && word == "" {
		m.popup.visible = false
		return
	}

	// Don't trigger on a new/blank line — only show contextual popup when
	// there is non-whitespace content before the cursor on this line.
	textBeforeCursor := lineText[:col]
	if word == "" && strings.TrimSpace(textBeforeCursor) == "" {
		m.popup.visible = false
		return
	}

	// Context-aware: if the query references specific tables, suggest only their columns.
	if ctxItems := contextualColumnItems(word, ta.Value(), lineNum, m.schema); len(ctxItems) > 0 {
		m.popup = completionPopup{items: ctxItems, selected: 0, visible: true, word: word, mode: popupModeCompletion}
		return
	}

	items := getCompletions(word, m.schemaItems, 8)
	if len(items) == 0 || word == "" {
		m.popup.visible = false
		return
	}

	m.popup = completionPopup{
		items:    popupItemsFromCompletions(items),
		selected: 0,
		visible:  true,
		word:     word,
		mode:     popupModeCompletion,
	}
}

// acceptCompletion replaces the typed word with the selected completion.
func (m Model) acceptCompletion() Model {
	if !m.popup.visible || m.popup.mode != popupModeCompletion || m.popup.selected >= len(m.popup.items) {
		return m
	}
	completion := m.popup.items[m.popup.selected]
	word := m.popup.word

	for range []rune(word) {
		m.tabs[m.active].ta, _ = m.tabs[m.active].ta.Update(
			tea.KeyMsg{Type: tea.KeyBackspace},
		)
	}

	insert := completionInsertText(completion, word)
	// For keywords only: if the typed word is all lowercase, insert lowercase.
	// Schema names (tables, columns) always use their stored casing.
	if completion.InsertText == "" && completion.Kind == CompletionKindKeyword && word == strings.ToLower(word) {
		insert = strings.ToLower(completion.Text)
	}
	m.tabs[m.active].ta.InsertString(insert)

	m.popup.visible = false
	if suffix, items := autoJoinCompletionFollowup(m.tabs[m.active].ta.Value(), m.tabs[m.active].ta.Line(), m.tabs[m.active].ta.LineInfo().CharOffset, completion, m.schema); suffix != "" {
		m.tabs[m.active].ta.InsertString(suffix)
		if len(items) > 0 {
			m.popup = completionPopup{items: items, selected: 0, visible: true, mode: popupModeCompletion, title: "Join"}
		}
	}
	return m
}

// renderPopup renders the completion dropdown.
func (m Model) renderPopup() string {
	if m.popup.mode == popupModeRefactor {
		return m.renderRefactorPopup()
	}
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
			row := popupSelectedStyle.Render("  " + label)
			if it.Detail != "" {
				row += popupSelectedDetailStyle.Render("  " + it.Detail)
			}
			row += popupSelectedStyle.Render("  ")
			rows = append(rows, row)
		} else {
			row := popupItemStyle.Render("  " + label)
			if it.Detail != "" {
				row += popupDetailStyle.Render("  " + it.Detail)
			}
			row += popupItemStyle.Render("  ")
			rows = append(rows, row)
		}
	}
	inner := strings.Join(rows, "\n")
	return popupStyle.Render(inner)
}

func (m Model) renderRefactorPopup() string {
	items := m.popup.items
	rows := make([]string, 0, len(items)+1)
	title := m.popup.title
	if title == "" {
		title = "Refactor"
	}
	rows = append(rows, popupItemStyle.Bold(true).Render("  "+title+"  "))
	for i, it := range items {
		label := strings.TrimSpace(it.Shortcut + "  " + it.Text)
		if it.Detail != "" {
			label += " — " + it.Detail
		}
		if i == m.popup.selected {
			rows = append(rows, popupSelectedStyle.Render("  "+label+"  "))
		} else {
			rows = append(rows, popupItemStyle.Render("  "+label+"  "))
		}
	}
	return popupStyle.Render(strings.Join(rows, "\n"))
}

// ─── Goto-line bar ────────────────────────────────────────────────────────────

func (m Model) openGotoLine() (Model, tea.Cmd) {
	m.gotoOpen = true
	m.gotoInput = m.gotoInput[:0]
	return m, nil
}

func (m Model) updateGotoLine(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		input := strings.TrimSpace(string(m.gotoInput))
		m.gotoOpen = false
		m.gotoInput = nil
		if input == "" {
			return m, nil
		}
		n, err := strconv.Atoi(input)
		if err != nil || n < 1 {
			return m, nil
		}
		return m.jumpToLine(n)
	case "esc":
		m.gotoOpen = false
		m.gotoInput = nil
		return m, nil
	case "backspace":
		if len(m.gotoInput) > 0 {
			m.gotoInput = m.gotoInput[:len(m.gotoInput)-1]
		}
		return m, nil
	default:
		if msg.Type == tea.KeyRunes {
			for _, ch := range msg.Runes {
				if ch >= '0' && ch <= '9' {
					m.gotoInput = append(m.gotoInput, ch)
				}
			}
		}
		return m, nil
	}
}

// jumpToLine moves the cursor to the given 1-based line number.
func (m Model) jumpToLine(n int) (Model, tea.Cmd) {
	tab := &m.tabs[m.active]
	if m.vimEnabled && tab.vim != nil {
		tab.vim.Buf.MoveToLine(n)
		tab.vim.ScrollToReveal(m.height - 1)
		return m, nil
	}
	// Non-vim: use textarea — scroll to line n (0-based: n-1).
	ta := &tab.ta
	targetLine := n - 1
	if targetLine < 0 {
		targetLine = 0
	}
	// Move to top first.
	for ta.Line() > 0 {
		ta.CursorUp()
	}
	// Move down to target line.
	for ta.Line() < targetLine {
		cur := ta.Line()
		ta.CursorDown()
		if ta.Line() == cur {
			break
		}
	}
	return m, nil
}

func (m Model) renderGotoLineBar() string {
	gotoStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("#1e1e1e")).
		Foreground(lipgloss.Color("#9cdcfe"))
	cursorSty := lipgloss.NewStyle().
		Background(lipgloss.Color("#569cd6")).
		Foreground(lipgloss.Color("#ffffff"))
	hintSty := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#555555"))

	var sb strings.Builder
	sb.WriteString(gotoStyle.Render(":"))
	for _, ch := range m.gotoInput {
		sb.WriteString(gotoStyle.Render(string(ch)))
	}
	sb.WriteString(cursorSty.Render(" "))
	sb.WriteString(hintSty.Render("  Enter go  Esc cancel"))
	return sb.String()
}

// GotoOpen reports whether the goto-line bar is open.
func (m Model) GotoOpen() bool { return m.gotoOpen }

// RenameTabOpen reports whether the rename-tab bar is open.
func (m Model) RenameTabOpen() bool { return m.renameTabOpen }

// ─── Tab rename bar ───────────────────────────────────────────────────────────

func (m Model) openRenameTab() (Model, tea.Cmd) {
	m.renameTabOpen = true
	m.renameTabErr = ""
	// Pre-fill with current tab title (without extension).
	current := m.tabs[m.active].Title
	m.renameTabInput = []rune(current)
	m.renameTabCursor = len(m.renameTabInput)
	if m.vimEnabled {
		return m, m.refreshVimInsertCursorBlink()
	}
	return m, nil
}

func (m Model) updateRenameTab(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		name := strings.TrimSpace(string(m.renameTabInput))
		if name == "" {
			m.renameTabErr = "name cannot be empty"
			return m, nil
		}
		if strings.ContainsAny(name, `/\:*?"<>|`) {
			m.renameTabErr = "name contains invalid characters"
			return m, nil
		}
		m.renameTabOpen = false
		m.renameTabErr = ""
		return m.applyTabRename(name)
	case "esc":
		m.renameTabOpen = false
		m.renameTabInput = nil
		m.renameTabErr = ""
		return m, nil
	case "backspace":
		if m.renameTabCursor > 0 {
			m.renameTabInput = append(m.renameTabInput[:m.renameTabCursor-1], m.renameTabInput[m.renameTabCursor:]...)
			m.renameTabCursor--
		}
		m.renameTabErr = ""
		return m, nil
	case "delete":
		if m.renameTabCursor < len(m.renameTabInput) {
			m.renameTabInput = append(m.renameTabInput[:m.renameTabCursor], m.renameTabInput[m.renameTabCursor+1:]...)
		}
		m.renameTabErr = ""
		return m, nil
	case "left":
		if m.renameTabCursor > 0 {
			m.renameTabCursor--
		}
		return m, nil
	case "right":
		if m.renameTabCursor < len(m.renameTabInput) {
			m.renameTabCursor++
		}
		return m, nil
	case "home", "ctrl+a":
		m.renameTabCursor = 0
		return m, nil
	case "end", "ctrl+e":
		m.renameTabCursor = len(m.renameTabInput)
		return m, nil
	default:
		if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace {
			ins := []rune(msg.String())
			newInput := make([]rune, 0, len(m.renameTabInput)+len(ins))
			newInput = append(newInput, m.renameTabInput[:m.renameTabCursor]...)
			newInput = append(newInput, ins...)
			newInput = append(newInput, m.renameTabInput[m.renameTabCursor:]...)
			m.renameTabInput = newInput
			m.renameTabCursor += len(ins)
			m.renameTabErr = ""
		}
		return m, nil
	}
}

func (m Model) applyTabRename(name string) (Model, tea.Cmd) {
	tab := &m.tabs[m.active]
	// If the tab has a backing file, rename it on disk.
	if tab.Path != "" {
		dir := filepath.Dir(tab.Path)
		// Use .sql extension.
		newName := name
		if !strings.HasSuffix(strings.ToLower(newName), ".sql") {
			newName += ".sql"
		}
		newPath := filepath.Join(dir, newName)
		if err := os.Rename(tab.Path, newPath); err == nil {
			tab.Path = newPath
		}
		// Regardless of rename success, update display name.
		tab.Title = name
	} else {
		tab.Title = name
	}
	if m.vimEnabled {
		return m, m.refreshVimInsertCursorBlink()
	}
	return m, nil
}

func (m Model) renderRenameTabBar() string {
	promptSty := lipgloss.NewStyle().
		Background(lipgloss.Color("#1e1e1e")).
		Foreground(lipgloss.Color("#c586c0"))
	inputSty := lipgloss.NewStyle().
		Background(lipgloss.Color("#1e1e1e")).
		Foreground(lipgloss.Color("#d4d4d4"))
	cursorSty := lipgloss.NewStyle().
		Background(lipgloss.Color("#569cd6")).
		Foreground(lipgloss.Color("#ffffff"))
	errSty := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#f44747"))
	hintSty := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#555555"))

	var sb strings.Builder
	sb.WriteString(promptSty.Render("Rename tab: "))
	for i, ch := range m.renameTabInput {
		if i == m.renameTabCursor {
			sb.WriteString(cursorSty.Render(string(ch)))
		} else {
			sb.WriteString(inputSty.Render(string(ch)))
		}
	}
	if m.renameTabCursor == len(m.renameTabInput) {
		sb.WriteString(cursorSty.Render(" "))
	}
	if m.renameTabErr != "" {
		sb.WriteString("  " + errSty.Render("✗ "+m.renameTabErr))
	} else {
		sb.WriteString(hintSty.Render("  Enter confirm  Esc cancel"))
	}
	return sb.String()
}

func (m Model) openRefactorPopup() (Model, tea.Cmd) {
	m.popup = completionPopup{
		items: []popupItem{{
			Text:     "Name table alias",
			Detail:   "Alias current table in active query block",
			Shortcut: "N",
			Action:   popupActionNameTableAlias,
		}, {
			Text:     "Expand *",
			Detail:   "Expand SELECT * into explicit columns in active query block",
			Shortcut: "E",
			Action:   popupActionExpandSelectStar,
		}, {
			Text:     "Convert SELECT to UPDATE",
			Detail:   "Rewrite active SELECT block as an UPDATE scaffold, preserving joins/filters",
			Shortcut: "u",
			Action:   popupActionConvertSelectToUpdate,
		}, {
			Text:     "Add UPDATE below",
			Detail:   "Append an UPDATE scaffold beneath the active SELECT block",
			Shortcut: "U",
			Action:   popupActionAppendUpdateBelow,
		}, {
			Text:     "Convert UPDATE to SELECT",
			Detail:   "Rewrite active UPDATE block as a SELECT of the SET columns",
			Shortcut: "s",
			Action:   popupActionConvertUpdateToSelect,
		}, {
			Text:     "Add SELECT below",
			Detail:   "Append a SELECT beneath the active UPDATE block",
			Shortcut: "S",
			Action:   popupActionAppendSelectBelow,
		}, {
			Text:     "Wrap INSERT with IDENTITY_INSERT",
			Detail:   "Wrap the active INSERT block with SQL Server IDENTITY_INSERT ON/OFF",
			Shortcut: "i",
			Action:   popupActionWrapIdentityInsert,
		}, {
			Text:     "Rename tab",
			Detail:   "Rename this query tab and its workspace file",
			Shortcut: "T",
			Action:   popupActionRenameTab,
		}},
		selected: 0,
		visible:  true,
		mode:     popupModeRefactor,
		title:    "Refactor",
	}
	if m.vimEnabled {
		return m, m.refreshVimInsertCursorBlink()
	}
	return m, nil
}

func (m Model) handleRefactorPopupKey(msg tea.KeyMsg, refreshVim bool) (Model, tea.Cmd) {
	finish := func(model Model, cmd tea.Cmd) (Model, tea.Cmd) {
		if refreshVim {
			if cmd == nil {
				return model, model.refreshVimInsertCursorBlink()
			}
			return model, tea.Batch(cmd, model.refreshVimInsertCursorBlink())
		}
		return model, cmd
	}
	switch strings.ToLower(msg.String()) {
	case "esc":
		m.popup.visible = false
		return finish(m, nil)
	case "up", "ctrl+p":
		if m.popup.selected > 0 {
			m.popup.selected--
		}
		return finish(m, nil)
	case "down", "tab":
		if len(m.popup.items) > 0 {
			m.popup.selected = (m.popup.selected + 1) % len(m.popup.items)
		}
		return finish(m, nil)
	case "shift+tab":
		if len(m.popup.items) > 0 {
			m.popup.selected = (m.popup.selected - 1 + len(m.popup.items)) % len(m.popup.items)
		}
		return finish(m, nil)
	case "enter":
		return m.applySelectedRefactor()
	}
	raw := msg.String()
	for i, item := range m.popup.items {
		if item.Shortcut != "" && raw == item.Shortcut {
			m.popup.selected = i
			return m.applySelectedRefactor()
		}
	}
	match := -1
	for i, item := range m.popup.items {
		if item.Shortcut == "" || !strings.EqualFold(raw, item.Shortcut) {
			continue
		}
		if match >= 0 {
			match = -2
			break
		}
		match = i
	}
	if match >= 0 {
		m.popup.selected = match
		return m.applySelectedRefactor()
	}
	m.popup.visible = false
	return finish(m, nil)
}

func (m Model) applySelectedRefactor() (Model, tea.Cmd) {
	if !m.popup.visible || m.popup.selected < 0 || m.popup.selected >= len(m.popup.items) {
		return m, nil
	}
	switch m.popup.items[m.popup.selected].Action {
	case popupActionNameTableAlias:
		if m.vimEnabled {
			return m.nameTableAliasRefactorVim()
		}
		return m.nameTableAliasRefactorTextarea()
	case popupActionExpandSelectStar:
		if m.vimEnabled {
			return m.expandSelectStarRefactorVim()
		}
		return m.expandSelectStarRefactorTextarea()
	case popupActionConvertSelectToUpdate:
		if m.vimEnabled {
			return m.selectToUpdateRefactorVim(false)
		}
		return m.selectToUpdateRefactorTextarea(false)
	case popupActionAppendUpdateBelow:
		if m.vimEnabled {
			return m.selectToUpdateRefactorVim(true)
		}
		return m.selectToUpdateRefactorTextarea(true)
	case popupActionConvertUpdateToSelect:
		if m.vimEnabled {
			return m.updateToSelectRefactorVim(false)
		}
		return m.updateToSelectRefactorTextarea(false)
	case popupActionAppendSelectBelow:
		if m.vimEnabled {
			return m.updateToSelectRefactorVim(true)
		}
		return m.updateToSelectRefactorTextarea(true)
	case popupActionWrapIdentityInsert:
		if m.vimEnabled {
			return m.identityInsertRefactorVim()
		}
		return m.identityInsertRefactorTextarea()
	case popupActionRenameTab:
		m.popup.visible = false
		return m.openRenameTab()
	default:
		m.popup.visible = false
		if m.vimEnabled {
			return m, m.refreshVimInsertCursorBlink()
		}
		return m, nil
	}
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

type sqlTokenKind int

const (
	sqlTokWord sqlTokenKind = iota
	sqlTokDot
	sqlTokWhitespace
	sqlTokString
	sqlTokLineComment
	sqlTokBlockComment
	sqlTokOther
)

type sqlScanToken struct {
	kind       sqlTokenKind
	text       string
	start, end int // rune offsets, [start,end)
}

type sqlTableRef struct {
	keyword    string
	nameStart  int
	nameEnd    int
	aliasStart int
	aliasEnd   int
	alias      string
	segments   []string
	base       string
}

type joinCompletionContext int

const (
	joinContextNone joinCompletionContext = iota
	joinContextOnClause
	joinContextOnExpression
	joinContextAfterEquals
	joinContextAfterEqualsExpression
)

type joinRelation struct {
	leftExpr            string
	rightExpr           string
	detail              string
	exact               bool
	targetHasForeignKey bool
	otherRefIndex       int
	score               int
}

type textEdit struct {
	start       int
	end         int
	replacement string
}

func applyNameTableAliasRefactor(text string, cursorLine, cursorCol int) (string, int, int, bool) {
	start, end, ok := detectBlockRange(text, cursorLine)
	if !ok {
		return text, cursorLine, cursorCol, false
	}
	lines := strings.Split(text, "\n")
	if len(lines) == 0 {
		return text, cursorLine, cursorCol, false
	}
	cursor := clampTextPosToValue(text, textPos{Line: cursorLine, Col: cursorCol})
	block := strings.Join(lines[start:end+1], "\n")
	blockCursor := textPos{Line: cursor.Line - start, Col: cursor.Col}
	updatedBlock, nextBlockCursor, changed := nameTableAliasRefactorInBlock(block, blockCursor)
	if !changed {
		return text, cursor.Line, cursor.Col, false
	}
	updatedLines := append(append([]string{}, lines[:start]...), strings.Split(updatedBlock, "\n")...)
	updatedLines = append(updatedLines, lines[end+1:]...)
	updated := strings.Join(updatedLines, "\n")
	next := clampTextPosToValue(updated, textPos{Line: start + nextBlockCursor.Line, Col: nextBlockCursor.Col})
	return updated, next.Line, next.Col, true
}

func applyExpandSelectStarRefactor(text string, cursorLine, cursorCol int, schema *db.Schema) (string, int, int, bool) {
	start, end, ok := detectBlockRange(text, cursorLine)
	if !ok || schema == nil {
		return text, cursorLine, cursorCol, false
	}
	lines := strings.Split(text, "\n")
	if len(lines) == 0 {
		return text, cursorLine, cursorCol, false
	}
	cursor := clampTextPosToValue(text, textPos{Line: cursorLine, Col: cursorCol})
	block := strings.Join(lines[start:end+1], "\n")
	blockCursor := textPos{Line: cursor.Line - start, Col: cursor.Col}
	updatedBlock, nextBlockCursor, changed := expandSelectStarRefactorInBlock(block, blockCursor, schema)
	if !changed {
		return text, cursor.Line, cursor.Col, false
	}
	updatedLines := append(append([]string{}, lines[:start]...), strings.Split(updatedBlock, "\n")...)
	updatedLines = append(updatedLines, lines[end+1:]...)
	updated := strings.Join(updatedLines, "\n")
	next := clampTextPosToValue(updated, textPos{Line: start + nextBlockCursor.Line, Col: nextBlockCursor.Col})
	return updated, next.Line, next.Col, true
}

func applySelectToUpdateRefactor(text string, cursorLine, cursorCol int, appendBelow bool, schema *db.Schema) (string, int, int, bool) {
	return applyBlockSQLRefactor(text, cursorLine, cursorCol, func(block string, cursor textPos) (string, textPos, bool) {
		return selectToUpdateRefactorInBlock(block, cursor, appendBelow, schema)
	})
}

func applyUpdateToSelectRefactor(text string, cursorLine, cursorCol int, appendBelow bool) (string, int, int, bool) {
	return applyBlockSQLRefactor(text, cursorLine, cursorCol, func(block string, cursor textPos) (string, textPos, bool) {
		return updateToSelectRefactorInBlock(block, cursor, appendBelow)
	})
}

func applyIdentityInsertRefactor(text string, cursorLine, cursorCol int) (string, int, int, bool) {
	return applyBlockSQLRefactor(text, cursorLine, cursorCol, identityInsertRefactorInBlock)
}

func applyBlockSQLRefactor(text string, cursorLine, cursorCol int, transform func(block string, cursor textPos) (string, textPos, bool)) (string, int, int, bool) {
	start, end, ok := detectBlockRange(text, cursorLine)
	if !ok {
		return text, cursorLine, cursorCol, false
	}
	lines := strings.Split(text, "\n")
	if len(lines) == 0 {
		return text, cursorLine, cursorCol, false
	}
	cursor := clampTextPosToValue(text, textPos{Line: cursorLine, Col: cursorCol})
	block := strings.Join(lines[start:end+1], "\n")
	blockCursor := textPos{Line: cursor.Line - start, Col: cursor.Col}
	updatedBlock, nextBlockCursor, changed := transform(block, blockCursor)
	if !changed {
		return text, cursor.Line, cursor.Col, false
	}
	updatedLines := append(append([]string{}, lines[:start]...), strings.Split(updatedBlock, "\n")...)
	updatedLines = append(updatedLines, lines[end+1:]...)
	updated := strings.Join(updatedLines, "\n")
	next := clampTextPosToValue(updated, textPos{Line: start + nextBlockCursor.Line, Col: nextBlockCursor.Col})
	return updated, next.Line, next.Col, true
}

func expandSelectStarRefactorInBlock(block string, cursor textPos, schema *db.Schema) (string, textPos, bool) {
	if schema == nil {
		return block, cursor, false
	}
	cursor = clampTextPosToValue(block, cursor)
	cursorOffset := textPosToRuneOffset(block, cursor)
	tokens := scanSQLTokens(block)
	refs := parseSQLTableRefs(tokens)
	edits := expandSelectStarEdits(tokens, refs, schema)
	if len(edits) == 0 {
		return block, cursor, false
	}
	updated, nextOffset := applyRuneEdits(block, edits, cursorOffset)
	if updated == block {
		return block, cursor, false
	}
	return updated, runeOffsetToTextPos(updated, nextOffset), true
}

func nameTableAliasRefactorInBlock(block string, cursor textPos) (string, textPos, bool) {
	cursor = clampTextPosToValue(block, cursor)
	cursorOffset := textPosToRuneOffset(block, cursor)
	tokens := scanSQLTokens(block)
	refs := parseSQLTableRefs(tokens)
	target, ok := findTargetTableRef(tokens, refs, cursorOffset)
	if !ok || target.alias != "" || len(target.segments) == 0 {
		return block, cursor, false
	}
	alias := deriveTableAlias(target.base, collectUsedAliases(refs))
	edits := []textEdit{{start: target.nameEnd, end: target.nameEnd, replacement: " " + alias}}
	declStarts := make(map[int]bool, len(refs))
	for _, ref := range refs {
		declStarts[ref.nameStart] = true
	}
	sig := significantSQLTokenIndices(tokens)
	for pos := 0; pos < len(sig); pos++ {
		idx := sig[pos]
		tok := tokens[idx]
		if tok.kind != sqlTokWord || declStarts[tok.start] {
			continue
		}
		replaceEnd, nextPos, matched := matchTableReferencePrefix(tokens, sig, pos, target)
		if !matched {
			continue
		}
		if rangesOverlap(tok.start, replaceEnd, target.nameStart, target.nameEnd) {
			pos = nextPos - 1
			continue
		}
		edits = append(edits, textEdit{start: tok.start, end: replaceEnd, replacement: alias})
		pos = nextPos - 1
	}
	updated, nextOffset := applyRuneEdits(block, edits, cursorOffset)
	if updated == block {
		return block, cursor, false
	}
	return updated, runeOffsetToTextPos(updated, nextOffset), true
}

type selectItemSpan struct {
	startSig int
	endSig   int
}

type updateAssignment struct {
	column string
	source string
}

func selectToUpdateRefactorInBlock(block string, cursor textPos, appendBelow bool, schema *db.Schema) (string, textPos, bool) {
	statement, ok := buildUpdateFromSelectBlock(block, schema)
	if !ok {
		return block, clampTextPosToValue(block, cursor), false
	}
	if appendBelow {
		return appendGeneratedStatementBelow(block, statement, "UPDATE")
	}
	return statement, clampTextPosToValue(statement, cursor), true
}

func updateToSelectRefactorInBlock(block string, cursor textPos, appendBelow bool) (string, textPos, bool) {
	statement, ok := buildSelectFromUpdateBlock(block)
	if !ok {
		return block, clampTextPosToValue(block, cursor), false
	}
	if appendBelow {
		return appendGeneratedStatementBelow(block, statement, "SELECT")
	}
	return statement, clampTextPosToValue(statement, cursor), true
}

func identityInsertRefactorInBlock(block string, cursor textPos) (string, textPos, bool) {
	if strings.Contains(strings.ToUpper(block), "IDENTITY_INSERT") {
		return block, clampTextPosToValue(block, cursor), false
	}
	target, ok := identityInsertTargetTable(block)
	if !ok {
		return block, clampTextPosToValue(block, cursor), false
	}
	base := strings.TrimRight(block, "\n")
	prefix := "SET IDENTITY_INSERT " + target + " ON\n"
	suffix := "\nSET IDENTITY_INSERT " + target + " OFF"
	updated := prefix + base + suffix
	clamped := clampTextPosToValue(base, clampTextPosToValue(block, cursor))
	nextOffset := runeCount(prefix) + textPosToRuneOffset(base, clamped)
	return updated, clampTextPosToValue(updated, runeOffsetToTextPos(updated, nextOffset)), true
}

func appendGeneratedStatementBelow(block, statement, keyword string) (string, textPos, bool) {
	base := strings.TrimRight(block, "\n")
	updated := base + "\n\n" + statement
	startLine := strings.Count(base, "\n") + 2
	return updated, clampTextPosToValue(updated, textPos{Line: startLine, Col: len(keyword) + 1}), true
}

func identityInsertTargetTable(block string) (string, bool) {
	tokens := scanSQLTokens(block)
	sig := significantSQLTokenIndices(tokens)
	if len(sig) == 0 {
		return "", false
	}
	pos := 0
	insertTok := tokens[sig[pos]]
	if insertTok.kind != sqlTokWord || !strings.EqualFold(insertTok.text, "INSERT") {
		return "", false
	}
	pos++
	if pos < len(sig) && tokens[sig[pos]].kind == sqlTokWord && strings.EqualFold(tokens[sig[pos]].text, "INTO") {
		pos++
	}
	if pos >= len(sig) || tokens[sig[pos]].kind != sqlTokWord {
		return "", false
	}
	nameTok := tokens[sig[pos]]
	if strings.HasPrefix(normalizeSQLIdentifier(nameTok.text), "@") {
		return "", false
	}
	nextPos, segments, ok := readQualifiedIdentifier(tokens, sig, pos)
	if !ok || len(segments) == 0 {
		return "", false
	}
	return sliceRunes(block, nameTok.start, tokens[sig[nextPos-1]].end), true
}

func buildUpdateFromSelectBlock(block string, schema *db.Schema) (string, bool) {
	tokens := scanSQLTokens(block)
	refs := parseSQLTableRefs(tokens)
	sig, selectPos, fromPos, ok := findTopLevelSelectRange(tokens)
	if !ok || hasDisallowedSelectToUpdateClauses(tokens, sig, fromPos+1) {
		return "", false
	}
	fromTok := tokens[sig[fromPos]]
	target, ok := firstTableRefByKeywordAfter(refs, "FROM", fromTok.end)
	if !ok {
		return "", false
	}
	multiRef := countStatementRefsFrom(refs, fromTok.end) > 1
	assignments := collectSelectUpdateAssignments(tokens, sig, selectPos, fromPos, target, multiRef, schema)
	if len(assignments) == 0 {
		return "", false
	}
	lines := []string{"UPDATE " + qualifierForTableRef(target), "SET"}
	for i, assignment := range assignments {
		suffix := ","
		if i == len(assignments)-1 {
			suffix = ""
		}
		lines = append(lines, "    "+assignment.column+" = "+assignment.source+suffix)
	}
	if selectNeedsUpdateFromClause(target, refs, fromTok.end) {
		lines = append(lines, strings.TrimSpace(sliceRunes(block, fromTok.start, runeCount(block))))
	} else if wherePos := findTopLevelKeywordPos(tokens, sig, fromPos+1, "WHERE"); wherePos >= 0 {
		lines = append(lines, strings.TrimSpace(sliceRunes(block, tokens[sig[wherePos]].start, runeCount(block))))
	}
	return strings.Join(lines, "\n"), true
}

func buildSelectFromUpdateBlock(block string) (string, bool) {
	tokens := scanSQLTokens(block)
	refs := parseSQLTableRefs(tokens)
	sig := significantSQLTokenIndices(tokens)
	updatePos := findTopLevelKeywordPos(tokens, sig, 0, "UPDATE")
	if updatePos < 0 {
		return "", false
	}
	target, ok := firstTableRefByKeywordAfter(refs, "UPDATE", tokens[sig[updatePos]].end)
	if !ok {
		return "", false
	}
	setPos := findTopLevelKeywordPos(tokens, sig, updatePos+1, "SET")
	if setPos < 0 {
		return "", false
	}
	fromPos := findTopLevelKeywordPos(tokens, sig, setPos+1, "FROM")
	wherePos := findTopLevelKeywordPos(tokens, sig, setPos+1, "WHERE")
	setEndPos := len(sig) - 1
	if fromPos >= 0 {
		setEndPos = fromPos - 1
	} else if wherePos >= 0 {
		setEndPos = wherePos - 1
	}
	if setEndPos < setPos+1 {
		return "", false
	}
	qualify := target.alias != "" || fromPos >= 0
	columns := collectUpdateSelectColumns(tokens, sig, setPos, setEndPos, target, qualify)
	if len(columns) == 0 {
		return "", false
	}
	lines := []string{"SELECT " + strings.Join(columns, ", ")}
	if fromPos >= 0 {
		lines = append(lines, strings.TrimSpace(sliceRunes(block, tokens[sig[fromPos]].start, runeCount(block))))
		return strings.Join(lines, "\n"), true
	}
	fromLine := "FROM " + strings.Join(target.segments, ".")
	if target.alias != "" {
		fromLine += " " + target.alias
	}
	lines = append(lines, fromLine)
	if wherePos >= 0 {
		lines = append(lines, strings.TrimSpace(sliceRunes(block, tokens[sig[wherePos]].start, runeCount(block))))
	}
	return strings.Join(lines, "\n"), true
}

func hasDisallowedSelectToUpdateClauses(tokens []sqlScanToken, sig []int, startPos int) bool {
	return findTopLevelKeywordPos(tokens, sig, startPos, "GROUP", "ORDER", "HAVING", "LIMIT", "UNION", "EXCEPT", "INTERSECT") >= 0
}

func findTopLevelKeywordPos(tokens []sqlScanToken, sig []int, startPos int, keywords ...string) int {
	if startPos < 0 {
		startPos = 0
	}
	lookup := make(map[string]bool, len(keywords))
	for _, keyword := range keywords {
		lookup[strings.ToUpper(keyword)] = true
	}
	depth := 0
	for pos := startPos; pos < len(sig); pos++ {
		tok := tokens[sig[pos]]
		if tok.kind == sqlTokOther {
			switch tok.text {
			case "(":
				depth++
				continue
			case ")":
				if depth > 0 {
					depth--
				}
				continue
			}
		}
		if depth == 0 && tok.kind == sqlTokWord && lookup[strings.ToUpper(tok.text)] {
			return pos
		}
	}
	return -1
}

func firstTableRefByKeywordAfter(refs []sqlTableRef, keyword string, minStart int) (sqlTableRef, bool) {
	for _, ref := range refs {
		if ref.nameStart < minStart || !strings.EqualFold(ref.keyword, keyword) {
			continue
		}
		return ref, true
	}
	return sqlTableRef{}, false
}

func countStatementRefsFrom(refs []sqlTableRef, minStart int) int {
	count := 0
	for _, ref := range refs {
		if ref.nameStart >= minStart && (strings.EqualFold(ref.keyword, "FROM") || strings.EqualFold(ref.keyword, "JOIN")) {
			count++
		}
	}
	return count
}

func selectNeedsUpdateFromClause(target sqlTableRef, refs []sqlTableRef, fromStart int) bool {
	if target.alias != "" {
		return true
	}
	for _, ref := range refs {
		if ref.nameStart >= fromStart && strings.EqualFold(ref.keyword, "JOIN") {
			return true
		}
	}
	return false
}

func collectSelectUpdateAssignments(tokens []sqlScanToken, sig []int, selectPos, fromPos int, target sqlTableRef, multiRef bool, schema *db.Schema) []updateAssignment {
	items := collectTopLevelSelectItems(tokens, sig, selectPos, fromPos)
	seen := map[string]bool{}
	out := make([]updateAssignment, 0, len(items))
	for _, item := range items {
		assignments := selectItemToUpdateAssignments(tokens, sig, item, target, multiRef, schema)
		for _, assignment := range assignments {
			if seen[strings.ToLower(assignment.column)] {
				continue
			}
			seen[strings.ToLower(assignment.column)] = true
			out = append(out, assignment)
		}
	}
	return out
}

func selectItemToUpdateAssignments(tokens []sqlScanToken, sig []int, item selectItemSpan, target sqlTableRef, multiRef bool, schema *db.Schema) []updateAssignment {
	startPos := item.startSig
	if startPos > item.endSig {
		return nil
	}
	if tokens[sig[startPos]].kind == sqlTokWord && strings.EqualFold(tokens[sig[startPos]].text, "DISTINCT") {
		startPos++
	}
	if startPos > item.endSig {
		return nil
	}
	if assignments, ok := selectStarToUpdateAssignments(tokens, sig, startPos, item.endSig, target, multiRef, schema); ok {
		return assignments
	}
	nextPos, segments, ok := readQualifiedIdentifier(tokens, sig, startPos)
	if !ok || !selectItemHasOnlyAliasRemainder(tokens, sig, nextPos, item.endSig) {
		return nil
	}
	column := segments[len(segments)-1]
	qualifier := qualifierForTableRef(target)
	source := column
	if qualifier != "" && (target.alias != "" || multiRef || len(segments) > 1) {
		source = qualifier + "." + column
	}
	switch {
	case len(segments) == 1:
		return []updateAssignment{{column: column, source: source}}
	case sqlTableRefMatchesQualifier(target, segments[:len(segments)-1]):
		return []updateAssignment{{column: column, source: qualifier + "." + column}}
	default:
		return nil
	}
}

func selectStarToUpdateAssignments(tokens []sqlScanToken, sig []int, startPos, endSig int, target sqlTableRef, multiRef bool, schema *db.Schema) ([]updateAssignment, bool) {
	if endSig < startPos {
		return nil, false
	}
	starTok := tokens[sig[endSig]]
	if starTok.kind != sqlTokOther || starTok.text != "*" {
		return nil, false
	}
	if startPos == endSig {
		return updateAssignmentsForTargetColumns(schema, target, multiRef), true
	}
	if endSig <= startPos || tokens[sig[endSig-1]].kind != sqlTokDot {
		return nil, false
	}
	prefixEnd, segments, ok := readQualifiedIdentifier(tokens, sig, startPos)
	if !ok || prefixEnd != endSig-1 || !sqlTableRefMatchesQualifier(target, segments) {
		return nil, false
	}
	return updateAssignmentsForTargetColumns(schema, target, true), true
}

func updateAssignmentsForTargetColumns(schema *db.Schema, target sqlTableRef, multiRef bool) []updateAssignment {
	info, ok := lookupSchemaTableInfo(schema, target)
	if !ok || len(info.Columns) == 0 {
		return nil
	}
	qualifier := qualifierForTableRef(target)
	qualifySource := target.alias != "" || multiRef
	out := make([]updateAssignment, 0, len(info.Columns))
	for _, col := range info.Columns {
		source := col.Name
		if qualifySource {
			source = qualifier + "." + col.Name
		}
		out = append(out, updateAssignment{column: col.Name, source: source})
	}
	return out
}

func selectItemHasOnlyAliasRemainder(tokens []sqlScanToken, sig []int, nextPos, endSig int) bool {
	if nextPos > endSig {
		return true
	}
	remaining := endSig - nextPos + 1
	if remaining == 1 {
		return tokens[sig[nextPos]].kind == sqlTokWord
	}
	if remaining == 2 {
		return tokens[sig[nextPos]].kind == sqlTokWord && strings.EqualFold(tokens[sig[nextPos]].text, "AS") && tokens[sig[nextPos+1]].kind == sqlTokWord
	}
	return false
}

func collectUpdateSelectColumns(tokens []sqlScanToken, sig []int, setPos, setEndPos int, target sqlTableRef, qualify bool) []string {
	items := collectTopLevelDelimitedItems(tokens, sig, setPos+1, setEndPos)
	seen := map[string]bool{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		column, ok := updateSetItemToSelectColumn(tokens, sig, item, target, qualify)
		if !ok || seen[strings.ToLower(column)] {
			continue
		}
		seen[strings.ToLower(column)] = true
		out = append(out, column)
	}
	return out
}

func updateSetItemToSelectColumn(tokens []sqlScanToken, sig []int, item selectItemSpan, target sqlTableRef, qualify bool) (string, bool) {
	eqPos := findTopLevelOtherPos(tokens, sig, item.startSig, item.endSig, "=")
	if eqPos <= item.startSig {
		return "", false
	}
	nextPos, segments, ok := readQualifiedIdentifier(tokens, sig, item.startSig)
	if !ok || nextPos != eqPos {
		return "", false
	}
	column := segments[len(segments)-1]
	switch {
	case len(segments) == 1:
		if qualify {
			return qualifierForTableRef(target) + "." + column, true
		}
		return column, true
	case sqlTableRefMatchesQualifier(target, segments[:len(segments)-1]):
		return qualifierForTableRef(target) + "." + column, true
	default:
		return "", false
	}
}

func collectTopLevelDelimitedItems(tokens []sqlScanToken, sig []int, startPos, endPos int) []selectItemSpan {
	items := make([]selectItemSpan, 0, 4)
	depth := 0
	itemStart := -1
	for pos := startPos; pos <= endPos && pos < len(sig); pos++ {
		tok := tokens[sig[pos]]
		if tok.kind == sqlTokOther {
			switch tok.text {
			case "(":
				if itemStart < 0 {
					itemStart = pos
				}
				depth++
				continue
			case ")":
				if itemStart < 0 {
					itemStart = pos
				}
				if depth > 0 {
					depth--
				}
				continue
			case ",":
				if depth == 0 {
					if itemStart >= 0 && pos-1 >= itemStart {
						items = append(items, selectItemSpan{startSig: itemStart, endSig: pos - 1})
					}
					itemStart = -1
					continue
				}
			}
		}
		if itemStart < 0 {
			itemStart = pos
		}
	}
	if itemStart >= 0 && endPos >= itemStart {
		items = append(items, selectItemSpan{startSig: itemStart, endSig: endPos})
	}
	return items
}

func findTopLevelOtherPos(tokens []sqlScanToken, sig []int, startPos, endPos int, text string) int {
	depth := 0
	for pos := startPos; pos <= endPos && pos < len(sig); pos++ {
		tok := tokens[sig[pos]]
		if tok.kind == sqlTokOther {
			switch tok.text {
			case "(":
				depth++
				continue
			case ")":
				if depth > 0 {
					depth--
				}
				continue
			}
			if depth == 0 && tok.text == text {
				return pos
			}
		}
	}
	return -1
}

func sliceRunes(text string, start, end int) string {
	runes := []rune(text)
	start = clampInt(start, 0, len(runes))
	end = clampInt(end, start, len(runes))
	return string(runes[start:end])
}

func runeCount(text string) int {
	return len([]rune(text))
}

func expandSelectStarEdits(tokens []sqlScanToken, refs []sqlTableRef, schema *db.Schema) []textEdit {
	sig, selectPos, fromPos, ok := findTopLevelSelectRange(tokens)
	if !ok {
		return nil
	}
	fromTok := tokens[sig[fromPos]]
	selectRefs := make([]sqlTableRef, 0, len(refs))
	for _, ref := range refs {
		if ref.nameStart >= fromTok.end && (strings.EqualFold(ref.keyword, "FROM") || strings.EqualFold(ref.keyword, "JOIN")) {
			selectRefs = append(selectRefs, ref)
		}
	}
	if len(selectRefs) == 0 {
		return nil
	}
	items := collectTopLevelSelectItems(tokens, sig, selectPos, fromPos)
	edits := make([]textEdit, 0, len(items))
	for _, item := range items {
		edit, ok := expandSelectStarItem(tokens, sig, item, selectRefs, schema)
		if ok {
			edits = append(edits, edit)
		}
	}
	return edits
}

func findTopLevelSelectRange(tokens []sqlScanToken) ([]int, int, int, bool) {
	sig := significantSQLTokenIndices(tokens)
	selectPos := -1
	depth := 0
	for pos, idx := range sig {
		tok := tokens[idx]
		if tok.kind == sqlTokOther {
			switch tok.text {
			case "(":
				depth++
				continue
			case ")":
				if depth > 0 {
					depth--
				}
				continue
			}
		}
		if depth == 0 && tok.kind == sqlTokWord && strings.EqualFold(tok.text, "SELECT") {
			selectPos = pos
			break
		}
	}
	if selectPos < 0 {
		return nil, 0, 0, false
	}
	depth = 0
	for pos := selectPos + 1; pos < len(sig); pos++ {
		tok := tokens[sig[pos]]
		if tok.kind == sqlTokOther {
			switch tok.text {
			case "(":
				depth++
				continue
			case ")":
				if depth > 0 {
					depth--
				}
				continue
			}
		}
		if depth == 0 && tok.kind == sqlTokWord && strings.EqualFold(tok.text, "FROM") {
			return sig, selectPos, pos, true
		}
	}
	return nil, 0, 0, false
}

func collectTopLevelSelectItems(tokens []sqlScanToken, sig []int, selectPos, fromPos int) []selectItemSpan {
	items := make([]selectItemSpan, 0, 4)
	depth := 0
	itemStart := -1
	for pos := selectPos + 1; pos < fromPos; pos++ {
		tok := tokens[sig[pos]]
		if tok.kind == sqlTokOther {
			switch tok.text {
			case "(":
				if itemStart < 0 {
					itemStart = pos
				}
				depth++
				continue
			case ")":
				if itemStart < 0 {
					itemStart = pos
				}
				if depth > 0 {
					depth--
				}
				continue
			case ",":
				if depth == 0 {
					if itemStart >= 0 && pos-1 >= itemStart {
						items = append(items, selectItemSpan{startSig: itemStart, endSig: pos - 1})
					}
					itemStart = -1
					continue
				}
			}
		}
		if itemStart < 0 {
			itemStart = pos
		}
	}
	if itemStart >= 0 && fromPos-1 >= itemStart {
		items = append(items, selectItemSpan{startSig: itemStart, endSig: fromPos - 1})
	}
	return items
}

func expandSelectStarItem(tokens []sqlScanToken, sig []int, item selectItemSpan, refs []sqlTableRef, schema *db.Schema) (textEdit, bool) {
	if item.startSig < 0 || item.endSig < item.startSig {
		return textEdit{}, false
	}
	starTok := tokens[sig[item.endSig]]
	if starTok.kind != sqlTokOther || starTok.text != "*" {
		return textEdit{}, false
	}
	if item.endSig > item.startSig && tokens[sig[item.endSig-1]].kind == sqlTokDot {
		startPos := item.endSig - 2
		if startPos < item.startSig || tokens[sig[startPos]].kind != sqlTokWord {
			return textEdit{}, false
		}
		for startPos-2 >= item.startSig && tokens[sig[startPos-1]].kind == sqlTokDot && tokens[sig[startPos-2]].kind == sqlTokWord {
			startPos -= 2
		}
		segments := make([]string, 0, 3)
		for pos := startPos; pos <= item.endSig-2; pos += 2 {
			if tokens[sig[pos]].kind != sqlTokWord {
				return textEdit{}, false
			}
			segments = append(segments, normalizeSQLIdentifier(tokens[sig[pos]].text))
			if pos < item.endSig-2 && tokens[sig[pos+1]].kind != sqlTokDot {
				return textEdit{}, false
			}
		}
		ref, ok := findRefForQualifiedStar(refs, segments)
		if !ok {
			return textEdit{}, false
		}
		expanded, ok := explicitColumnsForRef(schema, ref, true)
		if !ok {
			return textEdit{}, false
		}
		return textEdit{start: tokens[sig[startPos]].start, end: starTok.end, replacement: strings.Join(expanded, ", ")}, true
	}
	expanded, ok := explicitColumnsForAllRefs(schema, refs)
	if !ok {
		return textEdit{}, false
	}
	return textEdit{start: starTok.start, end: starTok.end, replacement: strings.Join(expanded, ", ")}, true
}

func findRefForQualifiedStar(refs []sqlTableRef, segments []string) (sqlTableRef, bool) {
	if len(segments) == 0 {
		return sqlTableRef{}, false
	}
	match := -1
	for i, ref := range refs {
		if sqlTableRefMatchesQualifier(ref, segments) {
			if match >= 0 {
				return sqlTableRef{}, false
			}
			match = i
		}
	}
	if match < 0 {
		return sqlTableRef{}, false
	}
	return refs[match], true
}

func sqlTableRefMatchesQualifier(ref sqlTableRef, segments []string) bool {
	if len(segments) == 1 {
		seg := segments[0]
		return strings.EqualFold(ref.alias, seg) || strings.EqualFold(ref.base, seg)
	}
	if len(segments) != len(ref.segments) {
		return false
	}
	for i := range segments {
		if !strings.EqualFold(ref.segments[i], segments[i]) {
			return false
		}
	}
	return true
}

func explicitColumnsForAllRefs(schema *db.Schema, refs []sqlTableRef) ([]string, bool) {
	qualify := len(refs) > 1
	if len(refs) == 1 && refs[0].alias != "" {
		qualify = true
	}
	out := make([]string, 0, 16)
	for _, ref := range refs {
		cols, ok := explicitColumnsForRef(schema, ref, qualify)
		if !ok {
			return nil, false
		}
		out = append(out, cols...)
	}
	return out, len(out) > 0
}

func explicitColumnsForRef(schema *db.Schema, ref sqlTableRef, qualify bool) ([]string, bool) {
	info, ok := lookupSchemaTableInfo(schema, ref)
	if !ok || len(info.Columns) == 0 {
		return nil, false
	}
	prefix := ""
	if qualify {
		prefix = qualifierForTableRef(ref) + "."
	}
	out := make([]string, 0, len(info.Columns))
	for _, col := range info.Columns {
		out = append(out, prefix+col.Name)
	}
	return out, true
}

func completionInsertText(item popupItem, _ string) string {
	if item.InsertText != "" {
		return item.InsertText
	}
	return item.Text
}

func joinCompletionItemsForText(text string, cursorLine, cursorCol int, schema *db.Schema) ([]popupItem, string) {
	if schema == nil {
		return nil, ""
	}
	start, end, ok := detectBlockRange(text, cursorLine)
	if !ok {
		return nil, ""
	}
	lines := strings.Split(text, "\n")
	if len(lines) == 0 {
		return nil, ""
	}
	cursor := clampTextPosToValue(text, textPos{Line: cursorLine, Col: cursorCol})
	block := strings.Join(lines[start:end+1], "\n")
	blockCursor := textPos{Line: cursor.Line - start, Col: cursor.Col}
	return joinCompletionItemsForBlock(block, blockCursor, schema)
}

func joinCompletionItemsForBlock(block string, cursor textPos, schema *db.Schema) ([]popupItem, string) {
	cursor = clampTextPosToValue(block, cursor)
	cursorOffset := textPosToRuneOffset(block, cursor)
	tokens := scanSQLTokens(block)
	refs := parseSQLTableRefs(tokens)
	if len(refs) < 2 {
		return nil, ""
	}
	target := refs[len(refs)-1]
	if !strings.EqualFold(target.keyword, "JOIN") {
		return nil, ""
	}
	ctx, lhsExpr, exprPrefix := detectJoinCompletionContext(block, cursorOffset, tokens)
	if ctx == joinContextNone {
		return nil, ""
	}
	relations := inferJoinRelationsForTarget(schema, refs, target)
	if len(relations) == 0 {
		return nil, ""
	}
	switch ctx {
	case joinContextOnClause:
		return joinPredicatePopupItems(block, cursorOffset, relations), ""
	case joinContextOnExpression:
		return joinExpressionPopupItems(lhsExpr, relations), ""
	case joinContextAfterEquals:
		return joinRHSCompletionItems(block, cursorOffset, lhsExpr, relations), ""
	case joinContextAfterEqualsExpression:
		return joinRHSExpressionPopupItems(exprPrefix, lhsExpr, relations), ""
	default:
		return nil, ""
	}
}

func autoJoinCompletionFollowup(text string, cursorLine, cursorCol int, completion popupItem, schema *db.Schema) (string, []popupItem) {
	if schema == nil || completion.InsertText != "" || (completion.Kind != CompletionKindTable && completion.Kind != CompletionKindView) {
		return "", nil
	}
	start, end, ok := detectBlockRange(text, cursorLine)
	if !ok {
		return "", nil
	}
	lines := strings.Split(text, "\n")
	if len(lines) == 0 {
		return "", nil
	}
	cursor := clampTextPosToValue(text, textPos{Line: cursorLine, Col: cursorCol})
	block := strings.Join(lines[start:end+1], "\n")
	blockCursor := textPos{Line: cursor.Line - start, Col: cursor.Col}
	cursorOffset := textPosToRuneOffset(block, blockCursor)
	tokens := scanSQLTokens(block)
	refs := parseSQLTableRefs(tokens)
	if len(refs) < 2 {
		return "", nil
	}
	target := refs[len(refs)-1]
	if !strings.EqualFold(target.keyword, "JOIN") || cursorOffset < target.nameEnd {
		return "", nil
	}
	relations := inferJoinRelationsForTarget(schema, refs, target)
	if len(relations) == 0 {
		return "", nil
	}
	exact := filterExactJoinRelations(relations)
	if len(exact) == 1 {
		rel := exact[0]
		return " ON " + rel.leftExpr + " = " + rel.rightExpr, nil
	}
	return " ON ", joinPredicatePopupItems(block, cursorOffset+len([]rune(" ON ")), relations)
}

func detectJoinCompletionContext(block string, cursorOffset int, tokens []sqlScanToken) (joinCompletionContext, string, string) {
	sig := significantSQLTokenIndices(tokens)
	last := -1
	for i, idx := range sig {
		if tokens[idx].end <= cursorOffset {
			last = i
			continue
		}
		break
	}
	if last < 0 {
		return joinContextNone, "", ""
	}
	tok := tokens[sig[last]]
	if tok.kind == sqlTokWord && strings.EqualFold(tok.text, "ON") {
		return joinContextOnClause, "", ""
	}
	if tok.kind == sqlTokOther && tok.text == "=" {
		if lhs := extractQualifiedExprBeforeEquals(block, cursorOffset); lhs != "" {
			return joinContextAfterEquals, lhs, ""
		}
	}
	if expr, exprStart := extractQualifiedExprAtCursor(block, cursorOffset); expr != "" && insideJoinOnClause(tokens, exprStart) {
		if lhs := extractQualifiedExprBeforeOffset(block, exprStart); lhs != "" {
			return joinContextAfterEqualsExpression, lhs, expr
		}
		return joinContextOnExpression, expr, ""
	}
	return joinContextNone, "", ""
}

func extractQualifiedExprAtCursor(block string, cursorOffset int) (string, int) {
	runes := []rune(block)
	cursorOffset = clampInt(cursorOffset, 0, len(runes))
	if cursorOffset == 0 || !isQualifiedExprRune(runes[cursorOffset-1]) {
		return "", cursorOffset
	}
	start := cursorOffset - 1
	for start >= 0 && isQualifiedExprRune(runes[start]) {
		start--
	}
	start++
	expr := strings.TrimSpace(string(runes[start:cursorOffset]))
	if expr == "" {
		return "", cursorOffset
	}
	return expr, start
}

func insideJoinOnClause(tokens []sqlScanToken, offset int) bool {
	for i := len(tokens) - 1; i >= 0; i-- {
		tok := tokens[i]
		if tok.end > offset {
			continue
		}
		if tok.kind != sqlTokWord {
			continue
		}
		switch strings.ToUpper(tok.text) {
		case "ON":
			return true
		case "AND", "OR":
			continue
		case "WHERE", "GROUP", "ORDER", "HAVING", "LIMIT", "UNION", "SELECT", "FROM", "JOIN":
			return false
		default:
			continue
		}
	}
	return false
}

func extractQualifiedExprBeforeEquals(block string, cursorOffset int) string {
	runes := []rune(block)
	cursorOffset = clampInt(cursorOffset, 0, len(runes))
	i := cursorOffset - 1
	for i >= 0 && unicode.IsSpace(runes[i]) {
		i--
	}
	if i < 0 || runes[i] != '=' {
		return ""
	}
	i--
	for i >= 0 && unicode.IsSpace(runes[i]) {
		i--
	}
	end := i + 1
	for i >= 0 && isQualifiedExprRune(runes[i]) {
		i--
	}
	expr := strings.TrimSpace(string(runes[i+1 : end]))
	if expr == "" {
		return ""
	}
	return expr
}

func extractQualifiedExprBeforeOffset(block string, offset int) string {
	runes := []rune(block)
	offset = clampInt(offset, 0, len(runes))
	i := offset - 1
	for i >= 0 && unicode.IsSpace(runes[i]) {
		i--
	}
	if i < 0 || runes[i] != '=' {
		return ""
	}
	return extractQualifiedExprBeforeEquals(block, i+1)
}

func isQualifiedExprRune(r rune) bool {
	return isSQLIdentifierContinue(r) || r == '.' || r == '[' || r == ']'
}

func inferJoinRelationsForTarget(schema *db.Schema, refs []sqlTableRef, target sqlTableRef) []joinRelation {
	if schema == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []joinRelation
	for i, other := range refs[:len(refs)-1] {
		for _, rel := range inferJoinRelationsBetween(schema, other, target) {
			rel.otherRefIndex = i
			rel.score = scoreJoinRelation(rel)
			key := strings.ToUpper(rel.leftExpr + "=" + rel.rightExpr)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, rel)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].score != out[j].score {
			return out[i].score > out[j].score
		}
		if out[i].exact != out[j].exact {
			return out[i].exact
		}
		if out[i].targetHasForeignKey != out[j].targetHasForeignKey {
			return out[i].targetHasForeignKey
		}
		if out[i].otherRefIndex != out[j].otherRefIndex {
			return out[i].otherRefIndex > out[j].otherRefIndex
		}
		return out[i].leftExpr+out[i].rightExpr < out[j].leftExpr+out[j].rightExpr
	})
	return out
}

func scoreJoinRelation(rel joinRelation) int {
	score := 0
	if rel.exact {
		score += 1000
	} else {
		score += 100
	}
	if rel.targetHasForeignKey {
		score += 150
	}
	score += rel.otherRefIndex * 10
	return score
}

func inferJoinRelationsBetween(schema *db.Schema, leftRef, rightRef sqlTableRef) []joinRelation {
	leftInfo, leftOK := lookupSchemaTableInfo(schema, leftRef)
	rightInfo, rightOK := lookupSchemaTableInfo(schema, rightRef)
	if !leftOK || !rightOK {
		return nil
	}
	seen := map[string]bool{}
	var out []joinRelation
	add := func(rel joinRelation) {
		key := strings.ToUpper(rel.leftExpr + "=" + rel.rightExpr)
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, rel)
	}
	for _, col := range leftInfo.Columns {
		if col.ForeignKey != nil && foreignKeyTargetsTable(col.ForeignKey, rightRef, rightInfo) {
			add(joinRelation{
				leftExpr:            qualifierForTableRef(leftRef) + "." + col.Name,
				rightExpr:           qualifierForTableRef(rightRef) + "." + col.ForeignKey.RefColumn,
				detail:              "fk",
				exact:               true,
				targetHasForeignKey: false,
			})
		}
	}
	for _, col := range rightInfo.Columns {
		if col.ForeignKey != nil && foreignKeyTargetsTable(col.ForeignKey, leftRef, leftInfo) {
			add(joinRelation{
				leftExpr:            qualifierForTableRef(leftRef) + "." + col.ForeignKey.RefColumn,
				rightExpr:           qualifierForTableRef(rightRef) + "." + col.Name,
				detail:              "fk",
				exact:               true,
				targetHasForeignKey: true,
			})
		}
	}
	leftPK := choosePrimaryKeyColumn(leftInfo, leftRef)
	rightPK := choosePrimaryKeyColumn(rightInfo, rightRef)
	sameTable := strings.EqualFold(leftInfo.SchemaName, rightInfo.SchemaName) && strings.EqualFold(leftInfo.Name, rightInfo.Name)
	if sameTable {
		if rightPK != "" {
			if fkCol := findSelfReferentialForeignKeyColumn(leftInfo.Columns); fkCol != "" {
				add(joinRelation{
					leftExpr:  qualifierForTableRef(leftRef) + "." + fkCol,
					rightExpr: qualifierForTableRef(rightRef) + "." + rightPK,
					detail:    "self",
				})
			}
		}
		if leftPK != "" {
			if fkCol := findSelfReferentialForeignKeyColumn(rightInfo.Columns); fkCol != "" {
				add(joinRelation{
					leftExpr:            qualifierForTableRef(leftRef) + "." + leftPK,
					rightExpr:           qualifierForTableRef(rightRef) + "." + fkCol,
					detail:              "self",
					targetHasForeignKey: true,
				})
			}
		}
	}
	if leftPK != "" {
		if fkCol := findHeuristicForeignKeyColumn(rightInfo.Columns, leftRef.base); fkCol != "" {
			add(joinRelation{
				leftExpr:            qualifierForTableRef(leftRef) + "." + leftPK,
				rightExpr:           qualifierForTableRef(rightRef) + "." + fkCol,
				detail:              "heuristic",
				targetHasForeignKey: true,
			})
		}
	}
	if rightPK != "" {
		if fkCol := findHeuristicForeignKeyColumn(leftInfo.Columns, rightRef.base); fkCol != "" {
			add(joinRelation{
				leftExpr:            qualifierForTableRef(leftRef) + "." + fkCol,
				rightExpr:           qualifierForTableRef(rightRef) + "." + rightPK,
				detail:              "heuristic",
				targetHasForeignKey: false,
			})
		}
	}
	return out
}

type schemaTableInfo struct {
	SchemaName string
	Name       string
	Columns    []db.ColumnDef
}

func lookupSchemaTableInfo(schema *db.Schema, ref sqlTableRef) (schemaTableInfo, bool) {
	if schema == nil {
		return schemaTableInfo{}, false
	}
	var fallback schemaTableInfo
	fallbackOK := false
	for _, database := range schema.Databases {
		for _, schemaNode := range database.Schemas {
			for _, table := range schemaNode.Tables {
				info := schemaTableInfo{SchemaName: schemaNode.Name, Name: table.Name, Columns: table.Columns}
				if schemaTableMatchesRef(info, ref) {
					return info, true
				}
				if !fallbackOK && strings.EqualFold(table.Name, ref.base) {
					fallback = info
					fallbackOK = true
				}
			}
			for _, view := range schemaNode.Views {
				info := schemaTableInfo{SchemaName: schemaNode.Name, Name: view.Name, Columns: view.Columns}
				if schemaTableMatchesRef(info, ref) {
					return info, true
				}
				if !fallbackOK && strings.EqualFold(view.Name, ref.base) {
					fallback = info
					fallbackOK = true
				}
			}
		}
	}
	return fallback, fallbackOK
}

func schemaTableMatchesRef(info schemaTableInfo, ref sqlTableRef) bool {
	if strings.EqualFold(info.Name, ref.base) {
		if len(ref.segments) < 2 {
			return true
		}
		return strings.EqualFold(info.SchemaName, ref.segments[len(ref.segments)-2])
	}
	return false
}

func foreignKeyTargetsTable(fk *db.ForeignKey, ref sqlTableRef, info schemaTableInfo) bool {
	if fk == nil {
		return false
	}
	target := strings.TrimSpace(fk.RefTable)
	if target == "" {
		return false
	}
	if strings.EqualFold(target, ref.base) || strings.EqualFold(target, info.Name) {
		return true
	}
	if strings.Contains(target, ".") {
		parts := strings.Split(target, ".")
		last := parts[len(parts)-1]
		if strings.EqualFold(last, ref.base) || strings.EqualFold(last, info.Name) {
			return true
		}
	}
	return false
}

func qualifierForTableRef(ref sqlTableRef) string {
	if ref.alias != "" {
		return ref.alias
	}
	return ref.base
}

func choosePrimaryKeyColumn(info schemaTableInfo, ref sqlTableRef) string {
	for _, col := range info.Columns {
		if col.PrimaryKey {
			return col.Name
		}
	}
	for _, col := range info.Columns {
		norm := normalizeLookupName(col.Name)
		if norm == "id" || norm == normalizeLookupName(stripCommonTablePrefix(ref.base))+"id" {
			return col.Name
		}
	}
	return ""
}

func findHeuristicForeignKeyColumn(columns []db.ColumnDef, base string) string {
	candidates := possibleForeignKeyNames(base)
	for _, col := range columns {
		if col.PrimaryKey {
			continue
		}
		if candidates[normalizeLookupName(col.Name)] {
			return col.Name
		}
	}
	return ""
}

func findSelfReferentialForeignKeyColumn(columns []db.ColumnDef) string {
	for _, col := range columns {
		if col.PrimaryKey {
			continue
		}
		norm := normalizeLookupName(col.Name)
		if norm == "parentid" || strings.HasSuffix(norm, "parentid") {
			return col.Name
		}
	}
	return ""
}

func possibleForeignKeyNames(base string) map[string]bool {
	core := stripCommonTablePrefix(base)
	joined := strings.Join(splitAliasParts(core), "")
	full := strings.Join(splitAliasParts(normalizeSQLIdentifier(base)), "")
	out := map[string]bool{}
	for _, name := range []string{joined, singularizeSQLName(joined), full, singularizeSQLName(full)} {
		name = normalizeLookupName(name)
		if name == "" {
			continue
		}
		out[name+"id"] = true
	}
	return out
}

func singularizeSQLName(name string) string {
	name = strings.TrimSpace(name)
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, "ies") && len(name) > 3:
		return name[:len(name)-3] + "y"
	case strings.HasSuffix(lower, "ses") && len(name) > 3:
		return name[:len(name)-2]
	case strings.HasSuffix(lower, "s") && !strings.HasSuffix(lower, "ss") && len(name) > 1:
		return name[:len(name)-1]
	default:
		return name
	}
}

func normalizeLookupName(name string) string {
	name = strings.ToLower(trimCommonColumnPrefix(normalizeSQLIdentifier(name)))
	var b strings.Builder
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func trimCommonColumnPrefix(name string) string {
	trimmed := strings.TrimSpace(name)
	for _, prefix := range []string{"lng", "str", "dte", "dtm", "bln", "dbl", "dec", "flt", "num", "txt", "int", "guid"} {
		if hasCommonColumnPrefix(trimmed, prefix) {
			return strings.TrimLeft(trimmed[len(prefix):], "_ ")
		}
	}
	return trimmed
}

func hasCommonColumnPrefix(name, prefix string) bool {
	if len(name) <= len(prefix) || !strings.EqualFold(name[:len(prefix)], prefix) {
		return false
	}
	next := rune(name[len(prefix)])
	return next == '_' || unicode.IsUpper(next)
}

func joinPredicatePopupItems(block string, cursorOffset int, relations []joinRelation) []popupItem {
	items := make([]popupItem, 0, len(relations))
	insertPrefix := joinCompletionInsertPrefix(block, cursorOffset)
	for _, rel := range relations {
		text := rel.leftExpr + " = " + rel.rightExpr
		items = append(items, popupItem{Text: text, InsertText: insertPrefix + text, Kind: CompletionKindName, Detail: joinPopupDetail("predicate", rel.detail)})
	}
	return items
}

func joinExpressionPopupItems(prefix string, relations []joinRelation) []popupItem {
	normalizedPrefix := normalizeJoinExpression(prefix)
	if normalizedPrefix == "" {
		return nil
	}
	seen := map[string]bool{}
	items := make([]popupItem, 0, len(relations))
	for _, rel := range relations {
		for _, pair := range [][2]string{{rel.leftExpr, rel.rightExpr}, {rel.rightExpr, rel.leftExpr}} {
			fullExpr := pair[0]
			if !strings.HasPrefix(normalizeJoinExpression(fullExpr), normalizedPrefix) {
				continue
			}
			predicate := fullExpr + " = " + pair[1]
			key := strings.ToUpper(predicate)
			if seen[key] {
				continue
			}
			seen[key] = true
			role := "lhs"
			if pair[0] == rel.rightExpr {
				role = "rhs"
			}
			items = append(items, popupItem{Text: predicate, InsertText: joinExpressionInsertSuffix(prefix, predicate), Kind: CompletionKindName, Detail: joinPopupDetail(role, rel.detail)})
		}
	}
	return items
}

func joinExpressionInsertSuffix(prefix, predicate string) string {
	prefixRunes := []rune(prefix)
	predicateRunes := []rune(predicate)
	if len(prefixRunes) > len(predicateRunes) {
		return predicate
	}
	if !strings.EqualFold(string(predicateRunes[:len(prefixRunes)]), prefix) {
		return predicate
	}
	return string(predicateRunes[len(prefixRunes):])
}

func joinCompletionInsertPrefix(block string, cursorOffset int) string {
	runes := []rune(block)
	if cursorOffset > 0 && cursorOffset <= len(runes) && unicode.IsSpace(runes[cursorOffset-1]) {
		return ""
	}
	return " "
}

func joinRHSCompletionItems(block string, cursorOffset int, lhsExpr string, relations []joinRelation) []popupItem {
	normalizedLHS := normalizeJoinExpression(lhsExpr)
	seen := map[string]bool{}
	items := make([]popupItem, 0, len(relations))
	insertPrefix := joinCompletionInsertPrefix(block, cursorOffset)
	for _, rel := range relations {
		candidate := ""
		switch normalizedLHS {
		case normalizeJoinExpression(rel.leftExpr):
			candidate = rel.rightExpr
		case normalizeJoinExpression(rel.rightExpr):
			candidate = rel.leftExpr
		default:
			continue
		}
		key := strings.ToUpper(candidate)
		if seen[key] {
			continue
		}
		seen[key] = true
		items = append(items, popupItem{Text: candidate, InsertText: insertPrefix + candidate, Kind: CompletionKindName, Detail: joinPopupDetail("rhs", rel.detail)})
	}
	return items
}

func joinRHSExpressionPopupItems(prefix, lhsExpr string, relations []joinRelation) []popupItem {
	normalizedLHS := normalizeJoinExpression(lhsExpr)
	normalizedPrefix := normalizeJoinExpression(prefix)
	if normalizedPrefix == "" {
		return nil
	}
	seen := map[string]bool{}
	items := make([]popupItem, 0, len(relations))
	for _, rel := range relations {
		candidate := ""
		switch normalizedLHS {
		case normalizeJoinExpression(rel.leftExpr):
			candidate = rel.rightExpr
		case normalizeJoinExpression(rel.rightExpr):
			candidate = rel.leftExpr
		default:
			continue
		}
		if !strings.HasPrefix(normalizeJoinExpression(candidate), normalizedPrefix) {
			continue
		}
		key := strings.ToUpper(candidate)
		if seen[key] {
			continue
		}
		seen[key] = true
		items = append(items, popupItem{Text: candidate, InsertText: joinExpressionInsertSuffix(prefix, candidate), Kind: CompletionKindName, Detail: joinPopupDetail("rhs", rel.detail)})
	}
	return items
}

func joinPopupDetail(role, source string) string {
	role = strings.TrimSpace(role)
	source = strings.TrimSpace(source)
	if role == "predicate" {
		role = "pred"
	}
	if source == "heuristic" {
		source = "heur"
	}
	switch {
	case role != "" && source != "":
		return role + " · " + source
	case role != "":
		return role
	default:
		return source
	}
}

func normalizeJoinExpression(expr string) string {
	parts := strings.Split(expr, ".")
	for i, part := range parts {
		parts[i] = strings.ToUpper(normalizeSQLIdentifier(strings.TrimSpace(part)))
	}
	return strings.Join(parts, ".")
}

func filterExactJoinRelations(relations []joinRelation) []joinRelation {
	out := make([]joinRelation, 0, len(relations))
	for _, rel := range relations {
		if rel.exact {
			out = append(out, rel)
		}
	}
	return out
}

func scanSQLTokens(text string) []sqlScanToken {
	runes := []rune(text)
	tokens := make([]sqlScanToken, 0, len(runes))
	for i := 0; i < len(runes); {
		start := i
		switch r := runes[i]; {
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			i++
			for i < len(runes) && (runes[i] == ' ' || runes[i] == '\t' || runes[i] == '\n' || runes[i] == '\r') {
				i++
			}
			tokens = append(tokens, sqlScanToken{kind: sqlTokWhitespace, text: string(runes[start:i]), start: start, end: i})
		case r == '-' && i+1 < len(runes) && runes[i+1] == '-':
			i += 2
			for i < len(runes) && runes[i] != '\n' && runes[i] != '\r' {
				i++
			}
			tokens = append(tokens, sqlScanToken{kind: sqlTokLineComment, text: string(runes[start:i]), start: start, end: i})
		case r == '/' && i+1 < len(runes) && runes[i+1] == '*':
			i += 2
			for i+1 < len(runes) && !(runes[i] == '*' && runes[i+1] == '/') {
				i++
			}
			if i+1 < len(runes) {
				i += 2
			}
			tokens = append(tokens, sqlScanToken{kind: sqlTokBlockComment, text: string(runes[start:i]), start: start, end: i})
		case r == '\'':
			i++
			for i < len(runes) {
				if runes[i] == '\'' {
					i++
					if i < len(runes) && runes[i] == '\'' {
						i++
						continue
					}
					break
				}
				i++
			}
			tokens = append(tokens, sqlScanToken{kind: sqlTokString, text: string(runes[start:i]), start: start, end: i})
		case r == '.':
			i++
			tokens = append(tokens, sqlScanToken{kind: sqlTokDot, text: ".", start: start, end: i})
		case r == '[':
			i++
			for i < len(runes) && runes[i] != ']' {
				i++
			}
			if i < len(runes) {
				i++
			}
			tokens = append(tokens, sqlScanToken{kind: sqlTokWord, text: string(runes[start:i]), start: start, end: i})
		case isSQLIdentifierStart(r):
			i++
			for i < len(runes) && isSQLIdentifierContinue(runes[i]) {
				i++
			}
			tokens = append(tokens, sqlScanToken{kind: sqlTokWord, text: string(runes[start:i]), start: start, end: i})
		default:
			i++
			tokens = append(tokens, sqlScanToken{kind: sqlTokOther, text: string(r), start: start, end: i})
		}
	}
	return tokens
}

func isSQLIdentifierStart(r rune) bool {
	return r == '_' || r == '@' || r == '#' || r == '$' || unicode.IsLetter(r)
}

func isSQLIdentifierContinue(r rune) bool {
	return isSQLIdentifierStart(r) || unicode.IsDigit(r)
}

func significantSQLTokenIndices(tokens []sqlScanToken) []int {
	indices := make([]int, 0, len(tokens))
	for i, tok := range tokens {
		if tok.kind == sqlTokWhitespace || tok.kind == sqlTokLineComment || tok.kind == sqlTokBlockComment {
			continue
		}
		indices = append(indices, i)
	}
	return indices
}

func parseSQLTableRefs(tokens []sqlScanToken) []sqlTableRef {
	sig := significantSQLTokenIndices(tokens)
	refs := make([]sqlTableRef, 0, 4)
	for pos := 0; pos < len(sig); pos++ {
		idx := sig[pos]
		tok := tokens[idx]
		if tok.kind != sqlTokWord || !isSQLTableStartKeyword(tok.text) {
			continue
		}
		nameEndPos, segments, ok := readQualifiedIdentifier(tokens, sig, pos+1)
		if !ok || len(segments) == 0 {
			continue
		}
		nameStartTok := tokens[sig[pos+1]]
		nameEndTok := tokens[sig[nameEndPos-1]]
		ref := sqlTableRef{
			keyword:   strings.ToUpper(tok.text),
			nameStart: nameStartTok.start,
			nameEnd:   nameEndTok.end,
			segments:  segments,
			base:      segments[len(segments)-1],
		}
		aliasPos := nameEndPos
		if aliasPos < len(sig) {
			aliasTok := tokens[sig[aliasPos]]
			if aliasTok.kind == sqlTokWord && strings.EqualFold(aliasTok.text, "AS") {
				aliasPos++
				if aliasPos < len(sig) {
					aliasTok = tokens[sig[aliasPos]]
				} else {
					aliasTok = sqlScanToken{}
				}
			}
			if aliasTok.kind == sqlTokWord && !isSQLKeywordWord(aliasTok.text) {
				ref.aliasStart = aliasTok.start
				ref.aliasEnd = aliasTok.end
				ref.alias = normalizeSQLIdentifier(aliasTok.text)
			}
		}
		refs = append(refs, ref)
	}
	return refs
}

func isSQLTableStartKeyword(word string) bool {
	switch strings.ToUpper(word) {
	case "FROM", "JOIN", "UPDATE", "INTO":
		return true
	default:
		return false
	}
}

func isSQLKeywordWord(word string) bool {
	for _, kw := range sqlKeywords {
		if strings.EqualFold(kw, word) {
			return true
		}
	}
	return false
}

func readQualifiedIdentifier(tokens []sqlScanToken, sig []int, sigPos int) (int, []string, bool) {
	if sigPos >= len(sig) || tokens[sig[sigPos]].kind != sqlTokWord {
		return sigPos, nil, false
	}
	segments := []string{normalizeSQLIdentifier(tokens[sig[sigPos]].text)}
	pos := sigPos + 1
	for pos+1 <= len(sig)-1 && tokens[sig[pos]].kind == sqlTokDot && tokens[sig[pos+1]].kind == sqlTokWord {
		segments = append(segments, normalizeSQLIdentifier(tokens[sig[pos+1]].text))
		pos += 2
	}
	return pos, segments, true
}

func normalizeSQLIdentifier(name string) string {
	if len(name) >= 2 && strings.HasPrefix(name, "[") && strings.HasSuffix(name, "]") {
		return strings.TrimSpace(name[1 : len(name)-1])
	}
	return name
}

func findTargetTableRef(tokens []sqlScanToken, refs []sqlTableRef, cursorOffset int) (sqlTableRef, bool) {
	for _, ref := range refs {
		if cursorOffset >= ref.nameStart && cursorOffset <= ref.nameEnd {
			return ref, true
		}
	}
	word := currentSQLWordAtCursor(tokens, cursorOffset)
	if word == "" {
		return sqlTableRef{}, false
	}
	matchIdx := -1
	for i, ref := range refs {
		if strings.EqualFold(ref.base, word) {
			if matchIdx >= 0 {
				return sqlTableRef{}, false
			}
			matchIdx = i
		}
	}
	if matchIdx < 0 {
		return sqlTableRef{}, false
	}
	return refs[matchIdx], true
}

func currentSQLWordAtCursor(tokens []sqlScanToken, cursorOffset int) string {
	for _, tok := range tokens {
		if tok.kind != sqlTokWord {
			continue
		}
		if cursorOffset >= tok.start && cursorOffset <= tok.end {
			return normalizeSQLIdentifier(tok.text)
		}
	}
	return ""
}

func collectUsedAliases(refs []sqlTableRef) map[string]bool {
	used := make(map[string]bool, len(refs))
	for _, ref := range refs {
		if ref.alias != "" {
			used[strings.ToUpper(ref.alias)] = true
		}
	}
	return used
}

func deriveTableAlias(base string, used map[string]bool) string {
	core := stripCommonTablePrefix(base)
	parts := splitAliasParts(core)
	alias := ""
	for _, part := range parts {
		if part == "" {
			continue
		}
		r := []rune(part)
		if len(r) == 0 {
			continue
		}
		alias += strings.ToUpper(string(r[0]))
	}
	if alias == "" {
		alias = "T"
	}
	baseAlias := alias
	for i := 2; used[strings.ToUpper(alias)]; i++ {
		alias = baseAlias + strconv.Itoa(i)
	}
	return alias
}

func stripCommonTablePrefix(name string) string {
	trimmed := normalizeSQLIdentifier(name)
	lower := strings.ToLower(trimmed)
	for _, prefix := range []string{"tbl", "dim", "fact", "vw"} {
		if strings.HasPrefix(lower, prefix) && len(trimmed) > len(prefix) {
			trimmed = trimmed[len(prefix):]
			trimmed = strings.TrimLeft(trimmed, "_ ")
			break
		}
	}
	if trimmed == "" {
		return normalizeSQLIdentifier(name)
	}
	return trimmed
}

func splitAliasParts(name string) []string {
	if name == "" {
		return nil
	}
	runes := []rune(name)
	parts := make([]string, 0, 4)
	start := 0
	flush := func(end int) {
		if end > start {
			parts = append(parts, string(runes[start:end]))
		}
	}
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if r == '_' || r == ' ' || r == '-' {
			flush(i)
			start = i + 1
			continue
		}
		if i > start && unicode.IsUpper(r) && unicode.IsLower(runes[i-1]) {
			flush(i)
			start = i
		}
	}
	flush(len(runes))
	if len(parts) == 0 {
		return []string{name}
	}
	return parts
}

func matchTableReferencePrefix(tokens []sqlScanToken, sig []int, sigPos int, target sqlTableRef) (int, int, bool) {
	if sigPos >= len(sig) || tokens[sig[sigPos]].kind != sqlTokWord {
		return 0, sigPos, false
	}
	if sigPos > 0 && tokens[sig[sigPos-1]].kind == sqlTokDot {
		return 0, sigPos, false
	}
	pos := sigPos
	matchedEnd := 0
	for segIdx, seg := range target.segments {
		if pos >= len(sig) {
			return 0, sigPos, false
		}
		wordTok := tokens[sig[pos]]
		if wordTok.kind != sqlTokWord || !strings.EqualFold(normalizeSQLIdentifier(wordTok.text), seg) {
			return 0, sigPos, false
		}
		matchedEnd = wordTok.end
		pos++
		if segIdx == len(target.segments)-1 {
			break
		}
		if pos >= len(sig) || tokens[sig[pos]].kind != sqlTokDot {
			return 0, sigPos, false
		}
		pos++
	}
	return matchedEnd, pos, true
}

func applyRuneEdits(text string, edits []textEdit, cursorOffset int) (string, int) {
	if len(edits) == 0 {
		return text, cursorOffset
	}
	sort.SliceStable(edits, func(i, j int) bool {
		if edits[i].start == edits[j].start {
			return edits[i].end < edits[j].end
		}
		return edits[i].start < edits[j].start
	})
	runes := []rune(text)
	var out strings.Builder
	last := 0
	newCursor := cursorOffset
	for _, edit := range edits {
		if edit.start < last {
			continue
		}
		if edit.start > len(runes) {
			edit.start = len(runes)
		}
		if edit.end > len(runes) {
			edit.end = len(runes)
		}
		out.WriteString(string(runes[last:edit.start]))
		out.WriteString(edit.replacement)
		replLen := len([]rune(edit.replacement))
		if cursorOffset > edit.end {
			newCursor += replLen - (edit.end - edit.start)
		} else if cursorOffset >= edit.start {
			newCursor = edit.start + replLen
		}
		last = edit.end
	}
	out.WriteString(string(runes[last:]))
	updated := out.String()
	return updated, clampInt(newCursor, 0, len([]rune(updated)))
}

func rangesOverlap(startA, endA, startB, endB int) bool {
	return startA < endB && startB < endA
}

func textPosToRuneOffset(text string, pos textPos) int {
	pos = clampTextPosToValue(text, pos)
	lines := strings.Split(text, "\n")
	offset := 0
	for i := 0; i < pos.Line && i < len(lines); i++ {
		offset += len([]rune(lines[i])) + 1
	}
	return offset + pos.Col
}

func runeOffsetToTextPos(text string, offset int) textPos {
	runes := []rune(text)
	offset = clampInt(offset, 0, len(runes))
	line, col := 0, 0
	for i := 0; i < offset; i++ {
		if runes[i] == '\n' {
			line++
			col = 0
			continue
		}
		col++
	}
	return textPos{Line: line, Col: col}
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

func adjacentBlockLine(text string, cursorLine, direction int) (int, bool) {
	lines := strings.Split(text, "\n")
	if len(lines) == 0 || direction == 0 {
		return 0, false
	}
	cursorLine = clampInt(cursorLine, 0, len(lines)-1)
	currentStart, currentEnd, currentOK := detectBlockRange(text, cursorLine)
	if direction < 0 {
		searchLine := cursorLine - 1
		if currentOK {
			searchLine = currentStart - 1
		}
		for searchLine >= 0 && strings.TrimSpace(lines[searchLine]) == "" {
			searchLine--
		}
		if searchLine < 0 {
			return 0, false
		}
		start, _, ok := detectBlockRange(text, searchLine)
		return start, ok
	}

	searchLine := cursorLine + 1
	if currentOK {
		searchLine = currentEnd + 1
	}
	for searchLine < len(lines) && strings.TrimSpace(lines[searchLine]) == "" {
		searchLine++
	}
	if searchLine >= len(lines) {
		return 0, false
	}
	start, _, ok := detectBlockRange(text, searchLine)
	return start, ok
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

	// isSetOp returns true if line is a bare set-operator keyword (UNION [ALL],
	// INTERSECT [ALL], EXCEPT [ALL]).  These lines bridge blank-line gaps between
	// query parts so that UNION ALL queries aren't split into separate blocks.
	isSetOp := func(line string) bool {
		upper := strings.ToUpper(strings.TrimSpace(line))
		switch upper {
		case "UNION", "UNION ALL", "INTERSECT", "INTERSECT ALL", "EXCEPT", "EXCEPT ALL":
			return true
		}
		return false
	}

	// shouldCrossBlank returns true when blank line i sits between a set operator
	// and the rest of the query, meaning the blank should not terminate the block.
	shouldCrossBlank := func(i int) bool {
		// nearest non-blank above i
		for j := i - 1; j >= 0; j-- {
			if strings.TrimSpace(lines[j]) != "" {
				if isSetOp(lines[j]) {
					return true
				}
				break
			}
		}
		// nearest non-blank below i
		for j := i + 1; j < len(lines); j++ {
			if strings.TrimSpace(lines[j]) != "" {
				if isSetOp(lines[j]) {
					return true
				}
				break
			}
		}
		return false
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
			if shouldCrossBlank(i) {
				continue
			}
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
			if shouldCrossBlank(i) {
				continue
			}
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
