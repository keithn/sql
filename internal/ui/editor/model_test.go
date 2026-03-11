package editor

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/sqltui/sql/internal/config"
	"github.com/sqltui/sql/internal/ui/editor/vim"
)

func TestDetectBlockRangeMatchesCtrlEBlock(t *testing.T) {
	text := "select 1;\n\nselect\n  2\nfrom dual;\n\nselect 3;"
	start, end, ok := detectBlockRange(text, 3)
	if !ok {
		t.Fatalf("detectBlockRange() returned ok=false")
	}
	if start != 2 || end != 4 {
		t.Fatalf("detectBlockRange() = (%d,%d,%v), want (2,4,true)", start, end, ok)
	}
	if got := detectBlock(text, 3); got != "select\n  2\nfrom dual;" {
		t.Fatalf("detectBlock() = %q, want middle statement", got)
	}
}

func TestDetectBlockRangeSeparatesSingleBlankLineStatements(t *testing.T) {
	text := "select * from tblAlarm\n\n\n\nselect * from tblFreqUpload\nwhere  Id > 2\n\nselect * from tblAQ"
	start, end, ok := detectBlockRange(text, 7)
	if !ok || start != 7 || end != 7 {
		t.Fatalf("detectBlockRange() = (%d,%d,%v), want (7,7,true)", start, end, ok)
	}
	if got := detectBlock(text, 7); got != "select * from tblAQ" {
		t.Fatalf("detectBlock() = %q, want final query only", got)
	}

	start, end, ok = detectBlockRange(text, 4)
	if !ok || start != 4 || end != 5 {
		t.Fatalf("middle detectBlockRange() = (%d,%d,%v), want (4,5,true)", start, end, ok)
	}
}

func TestDetectBlockRangeOnBlankLineHasNoActiveBlock(t *testing.T) {
	text := "select 1;\n\n\nselect 2;"
	start, end, ok := detectBlockRange(text, 1)
	if !ok || start != 0 || end != 0 {
		t.Fatalf("detectBlockRange() = (%d,%d,%v), want first blank line to attach to statement above", start, end, ok)
	}
	if got := detectBlock(text, 1); got != "select 1;" {
		t.Fatalf("detectBlock() = %q, want previous statement on first blank separator line", got)
	}

	start, end, ok = detectBlockRange(text, 2)
	if ok {
		t.Fatalf("detectBlockRange() = (%d,%d,%v), want no active block on second blank separator line", start, end, ok)
	}
	if got := detectBlock(text, 2); got != "" {
		t.Fatalf("detectBlock() = %q, want empty on second blank separator line", got)
	}
}

func TestSetTabsRestoresTextareaCursorPosition(t *testing.T) {
	m := New(&config.Config{})
	m = m.SetSize(80, 20)
	m = m.SetTabs([]TabState{{
		Path:       "query1.sql",
		Content:    "select 1;\nselect 2;\nselect 3;",
		CursorLine: 2,
		CursorCol:  4,
	}})

	info := m.TabsInfo()
	if len(info) != 1 {
		t.Fatalf("len(TabsInfo()) = %d, want 1", len(info))
	}
	if info[0].CursorLine != 2 || info[0].CursorCol != 4 {
		t.Fatalf("cursor = (%d,%d), want (2,4)", info[0].CursorLine, info[0].CursorCol)
	}
}

func TestSetTabsRestoresVimCursorPosition(t *testing.T) {
	m := New(&config.Config{Editor: config.EditorConfig{VimMode: true}})
	m = m.SetSize(80, 20)
	m = m.SetTabs([]TabState{{
		Path:       "query1.sql",
		Content:    "select 1;\nselect 2;\nselect 3;",
		CursorLine: 2,
		CursorCol:  4,
	}})

	info := m.TabsInfo()
	if len(info) != 1 {
		t.Fatalf("len(TabsInfo()) = %d, want 1", len(info))
	}
	if info[0].CursorLine != 2 || info[0].CursorCol != 4 {
		t.Fatalf("vim cursor = (%d,%d), want (2,4)", info[0].CursorLine, info[0].CursorCol)
	}
}

func TestToggleVimKeepsLineNumberGutterAligned(t *testing.T) {
	m := New(&config.Config{})
	m = m.SetSize(80, 8)
	m = m.SetTabs([]TabState{{
		Path:    "query1.sql",
		Content: "select 1;\nselect 2;\nselect 3;",
	}})

	nonVimLine := strings.Split(ansi.Strip(m.renderContent()), "\n")[0]
	nonVimCol := strings.Index(nonVimLine, "select")
	if nonVimCol < 0 {
		t.Fatalf("non-vim render missing SQL text: %q", nonVimLine)
	}

	m = m.ToggleVim()
	vimLine := strings.Split(ansi.Strip(m.renderVimContent()), "\n")[0]
	vimCol := strings.Index(vimLine, "select")
	if vimCol < 0 {
		t.Fatalf("vim render missing SQL text: %q", vimLine)
	}

	if vimCol != nonVimCol {
		t.Fatalf("SQL column changed across mode toggle: non-vim=%d vim=%d (%q vs %q)", nonVimCol, vimCol, nonVimLine, vimLine)
	}
	if nonVimLine[:nonVimCol] != vimLine[:vimCol] {
		t.Fatalf("gutter prefix changed across mode toggle: non-vim=%q vim=%q", nonVimLine[:nonVimCol], vimLine[:vimCol])
	}
}

func TestToggleVimPreservesTextareaCursorPosition(t *testing.T) {
	m := New(testConfig())
	m = m.SetSize(80, 8)
	m = m.SetTabs([]TabState{{
		Path:    "query1.sql",
		Content: "select 1;\nselect 22;\nselect 333;",
	}})
	setTextareaCursor(&m.tabs[0].ta, 1, 5)

	m = m.ToggleVim()
	if !m.VimEnabled() {
		t.Fatalf("expected vim mode after toggle")
	}
	if gotLine, gotCol := m.tabs[0].vim.Buf.CursorRow(), m.tabs[0].vim.Buf.CursorCol(); gotLine != 1 || gotCol != 5 {
		t.Fatalf("vim cursor after toggle = (%d,%d), want (1,5)", gotLine, gotCol)
	}
}

func TestToggleVimPreservesVimCursorPosition(t *testing.T) {
	cfg := testConfig()
	cfg.Editor.VimMode = true
	m := New(cfg)
	m = m.SetSize(80, 8)
	m = m.SetTabs([]TabState{{
		Path:    "query1.sql",
		Content: "select 1;\nselect 22;\nselect 333;",
	}})
	m.tabs[0].vim.Buf.SetCursor(2, 6)

	m = m.ToggleVim()
	if m.VimEnabled() {
		t.Fatalf("expected non-vim mode after toggle")
	}
	if gotLine, gotCol := m.tabs[0].ta.Line(), m.tabs[0].ta.LineInfo().CharOffset; gotLine != 2 || gotCol != 6 {
		t.Fatalf("textarea cursor after toggle = (%d,%d), want (2,6)", gotLine, gotCol)
	}
}

func TestToggleVimPreservesInactiveTabCursorPositions(t *testing.T) {
	m := New(testConfig())
	m = m.SetSize(80, 8)
	m = m.SetTabs([]TabState{
		{Path: "query1.sql", Content: "first\nsecond"},
		{Path: "query2.sql", Content: "alpha\nbravo\ncharlie"},
	})
	setTextareaCursor(&m.tabs[0].ta, 0, 3)
	setTextareaCursor(&m.tabs[1].ta, 2, 4)

	m = m.ToggleVim()
	if gotLine, gotCol := m.tabs[0].vim.Buf.CursorRow(), m.tabs[0].vim.Buf.CursorCol(); gotLine != 0 || gotCol != 3 {
		t.Fatalf("tab0 vim cursor after toggle = (%d,%d), want (0,3)", gotLine, gotCol)
	}
	if gotLine, gotCol := m.tabs[1].vim.Buf.CursorRow(), m.tabs[1].vim.Buf.CursorCol(); gotLine != 2 || gotCol != 4 {
		t.Fatalf("tab1 vim cursor after toggle = (%d,%d), want (2,4)", gotLine, gotCol)
	}
}

func TestBlockRangeAtCursorUsesTextareaCursor(t *testing.T) {
	m := New(testConfig())
	m = m.SetSize(80, 10)
	m = m.SetTabs([]TabState{{
		Path:    "query1.sql",
		Content: "select 1;\n\nselect\n  2\nfrom dual;\n\nselect 3;",
	}})
	setTextareaCursor(&m.tabs[0].ta, 3, 1)

	start, end, ok := m.blockRangeAtCursor()
	if !ok || start != 2 || end != 4 {
		t.Fatalf("blockRangeAtCursor() = (%d,%d,%v), want (2,4,true)", start, end, ok)
	}
}

func TestBlockRangeAtCursorUsesVimCursor(t *testing.T) {
	cfg := testConfig()
	cfg.Editor.VimMode = true
	m := New(cfg)
	m = m.SetSize(80, 10)
	m = m.SetTabs([]TabState{{
		Path:    "query1.sql",
		Content: "select 1;\n\nselect\n  2\nfrom dual;\n\nselect 3;",
	}})
	m.tabs[0].vim.Buf.SetCursor(3, 1)

	start, end, ok := m.blockRangeAtCursor()
	if !ok || start != 2 || end != 4 {
		t.Fatalf("blockRangeAtCursor() = (%d,%d,%v), want (2,4,true)", start, end, ok)
	}
}

func TestRenderContentPreservesSyntaxHighlighting(t *testing.T) {
	m := New(testConfig())
	m = m.SetSize(80, 8)
	m = m.SetTabs([]TabState{{
		Path:    "query1.sql",
		Content: "select 1 from dual;",
	}})

	rendered := m.renderContent()
	if !strings.Contains(rendered, "\x1b[") {
		t.Fatalf("renderContent() should include ANSI syntax highlighting, got %q", rendered)
	}
}

func TestRenderContentWrapsWithoutRepeatingFullLine(t *testing.T) {
	m := New(testConfig())
	m = m.SetSize(24, 8)
	m = m.SetTabs([]TabState{{
		Path:    "query1.sql",
		Content: "select 1234567890, 1234567890 from dual;",
	}})

	lines := strings.Split(ansi.Strip(m.renderContent()), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected wrapped output, got %q", lines)
	}
	first := strings.TrimSpace(lines[0])
	second := strings.TrimSpace(lines[1])
	if first == second {
		t.Fatalf("wrapped rows should show different slices, got duplicate rows %q and %q", first, second)
	}
	if strings.Count(first+second, "select") > 1 {
		t.Fatalf("wrapped rows should not repeat the full source line, got %q and %q", first, second)
	}
}

func TestActiveBlockLineSkipsBlankLines(t *testing.T) {
	lines := strings.Split("select 1;\n\nselect 2;", "\n")
	if !isActiveBlockLine(lines, 0, 0, 2, true, 0) {
		t.Fatalf("expected non-blank line to be active")
	}
	if isActiveBlockLine(lines, 1, 0, 2, true, 0) {
		t.Fatalf("blank line should not be marked active")
	}
	if !isActiveBlockLine(lines, 2, 0, 2, true, 2) {
		t.Fatalf("expected trailing non-blank line to be active")
	}
}

func TestActiveBlockLineIncludesOnlyCursorBlankLineAttachedToPrevious(t *testing.T) {
	lines := strings.Split("select 1;\n\n\nselect 2;", "\n")
	if !isActiveBlockLine(lines, 1, 0, 0, true, 1) {
		t.Fatalf("expected cursor blank line to count as active for previous statement")
	}
	if isActiveBlockLine(lines, 2, 0, 0, true, 1) {
		t.Fatalf("did not expect second blank separator line to count as active")
	}
}

func TestParseTextareaLineNumberRowDistinguishesWrappedContinuation(t *testing.T) {
	m := New(testConfig())
	m = m.SetSize(24, 8)
	m = m.SetTabs([]TabState{{
		Path:    "query1.sql",
		Content: "select 1234567890, 1234567890 from dual;\nselect 2;",
	}})
	gutterW := textareaGutterWidth(m.tabs[0].ta.View(), 2)
	lines := strings.Split(m.tabs[0].ta.View(), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected wrapped textarea output, got %q", lines)
	}

	_, lineNum, hasLineNum, ok := parseTextareaLineNumberRow(lines[0], gutterW)
	if !ok || !hasLineNum || lineNum != 0 {
		t.Fatalf("first row parse = (%d,%v,%v), want (0,true,true)", lineNum, hasLineNum, ok)
	}

	_, lineNum, hasLineNum, ok = parseTextareaLineNumberRow(lines[1], gutterW)
	if !ok || hasLineNum {
		t.Fatalf("wrapped continuation parse = (%d,%v,%v), want no line number", lineNum, hasLineNum, ok)
	}
}

func TestRenderContentStillShowsActiveBlockOnNumberedRows(t *testing.T) {
	m := New(testConfig())
	m = m.SetSize(80, 10)
	m = m.SetTabs([]TabState{{
		Path:    "query1.sql",
		Content: "select 1;\n\nselect 2;\nselect 3;",
	}})
	setTextareaCursor(&m.tabs[0].ta, 2, 1)
	start, end, ok := m.blockRangeAtCursor()
	if !ok || start != 2 || end != 2 {
		t.Fatalf("blockRangeAtCursor() = (%d,%d,%v), want (2,2,true)", start, end, ok)
	}
	if !isActiveBlockLine(strings.Split(m.tabs[0].ta.Value(), "\n"), 2, start, end, ok, 2) {
		t.Fatalf("expected current SQL line to be active in gutter mapping")
	}
}

func TestNonVimCtrlRightMovesByWord(t *testing.T) {
	m := New(testConfig())
	m = m.SetSize(80, 8)
	m = m.SetTabs([]TabState{{Path: "query1.sql", Content: "hello world"}})
	setTextareaCursor(&m.tabs[0].ta, 0, 0)

	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlRight})
	m = nm
	if got := m.tabs[0].ta.LineInfo().CharOffset; got != 5 {
		t.Fatalf("ctrl+right cursor col = %d, want 5", got)
	}
}

func TestNonVimShiftRightCreatesSelection(t *testing.T) {
	m := New(testConfig())
	m = m.SetSize(80, 8)
	m = m.SetTabs([]TabState{{Path: "query1.sql", Content: "hello"}})
	setTextareaCursor(&m.tabs[0].ta, 0, 0)

	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyShiftRight})
	m = nm
	start, end, ok := m.textareaSelectionRange()
	if !ok {
		t.Fatalf("expected active textarea selection after shift+right")
	}
	if start != (textPos{Line: 0, Col: 0}) || end != (textPos{Line: 0, Col: 1}) {
		t.Fatalf("selection = (%+v,%+v), want [(0,0),(0,1)]", start, end)
	}
}

func TestNonVimBackspaceDeletesSelection(t *testing.T) {
	m := New(testConfig())
	m = m.SetSize(80, 8)
	m = m.SetTabs([]TabState{{Path: "query1.sql", Content: "hello"}})
	setTextareaCursor(&m.tabs[0].ta, 0, 0)

	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyShiftRight})
	m = nm
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyShiftRight})
	m = nm
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = nm

	if got := m.tabs[0].ta.Value(); got != "llo" {
		t.Fatalf("value after deleting selection = %q, want %q", got, "llo")
	}
	if _, _, ok := m.textareaSelectionRange(); ok {
		t.Fatalf("selection should clear after backspace delete")
	}
	if got := m.tabs[0].ta.LineInfo().CharOffset; got != 0 {
		t.Fatalf("cursor col after deleting selection = %d, want 0", got)
	}
}

func TestEditorClickPlacesTextareaCursor(t *testing.T) {
	m := New(testConfig())
	m = m.SetSize(80, 8)
	m = m.SetTabs([]TabState{{Path: "query1.sql", Content: "alpha\ncharlie"}})
	gutterW := textareaGutterWidth(m.tabs[0].ta.View(), 2)

	m = m.Click(gutterW+4, 2)
	if got := m.tabs[0].ta.Line(); got != 1 {
		t.Fatalf("clicked cursor line = %d, want 1", got)
	}
	if got := m.tabs[0].ta.LineInfo().CharOffset; got != 4 {
		t.Fatalf("clicked cursor col = %d, want 4", got)
	}
}

func TestNonVimShiftDownPreservesMultilineSelectionColumn(t *testing.T) {
	m := New(testConfig())
	m = m.SetSize(80, 8)
	m = m.SetTabs([]TabState{{Path: "query1.sql", Content: "alpha bravo\ncharlie delta\necho foxtrot"}})
	setTextareaCursor(&m.tabs[0].ta, 0, 8)

	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyShiftDown})
	m = nm
	start, end, ok := m.textareaSelectionRange()
	if !ok {
		t.Fatalf("expected active textarea selection after shift+down")
	}
	if start != (textPos{Line: 0, Col: 8}) || end != (textPos{Line: 1, Col: 8}) {
		t.Fatalf("selection after shift+down = (%+v,%+v), want [(0,8),(1,8)]", start, end)
	}
}

func TestApplyTextareaSelectionSliceKeepsWholePlainSelectedSpan(t *testing.T) {
	source := "select foo from bar"
	hl := highlightLines(source)[0]
	got := applyTextareaSelectionSlice(
		hl,
		0,
		0,
		len([]rune(source)),
		textPos{Line: 0, Col: 0},
		textPos{Line: 0, Col: len([]rune(source))},
		[]string{source},
	)
	want := selectionStyle.Render(source)
	if !strings.Contains(got, want) {
		t.Fatalf("selected slice should contain plain full-span selection; got %q want substring %q", got, want)
	}
}

func TestCommentSQLLinePreservesIndentation(t *testing.T) {
	got, insertAt, insertedLen := commentSQLLine("    select 1")
	if got != "    -- select 1" {
		t.Fatalf("commentSQLLine() = %q, want indented SQL comment", got)
	}
	if insertAt != 4 || insertedLen != 3 {
		t.Fatalf("commentSQLLine() metadata = (%d,%d), want (4,3)", insertAt, insertedLen)
	}
}

func TestIsCommentShortcut(t *testing.T) {
	if !isCommentShortcut(tea.KeyMsg{Type: tea.KeyCtrlUnderscore}) {
		t.Fatalf("expected KeyCtrlUnderscore to be recognized as a comment shortcut")
	}
	if !isCommentShortcut(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{31}}) {
		t.Fatalf("expected raw unit-separator rune to be recognized as a comment shortcut")
	}
	if isCommentShortcut(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}}) {
		t.Fatalf("plain slash should not be recognized as a comment shortcut")
	}
}

func TestUpdateCtrlUnderscoreCommentsCurrentLine(t *testing.T) {
	m := New(testConfig())
	m = m.SetSize(80, 8)
	m = m.SetTabs([]TabState{{Path: "query1.sql", Content: "select 1\nselect 2"}})
	setTextareaCursor(&m.tabs[0].ta, 0, 0)

	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlUnderscore})
	m = nm
	if got := m.tabs[0].ta.Value(); got != "-- select 1\nselect 2" {
		t.Fatalf("value after KeyCtrlUnderscore = %q, want first line commented", got)
	}
}

func TestCommentCurrentLineTextareaCommentsLineAndMovesDown(t *testing.T) {
	m := New(testConfig())
	m = m.SetSize(80, 8)
	m = m.SetTabs([]TabState{{Path: "query1.sql", Content: "select 1\nselect 2"}})
	setTextareaCursor(&m.tabs[0].ta, 0, 0)

	m, _ = m.commentCurrentLineTextarea()
	if got := m.tabs[0].ta.Value(); got != "-- select 1\nselect 2" {
		t.Fatalf("value after commentCurrentLineTextarea() = %q, want first line commented", got)
	}
	if got := m.tabs[0].ta.Line(); got != 1 {
		t.Fatalf("cursor line after commentCurrentLineTextarea() = %d, want 1", got)
	}
	if got := m.tabs[0].ta.LineInfo().CharOffset; got != 0 {
		t.Fatalf("cursor col after commentCurrentLineTextarea() = %d, want 0", got)
	}
}

func TestCommentCurrentLineTextareaOnLastLineStaysPut(t *testing.T) {
	m := New(testConfig())
	m = m.SetSize(80, 8)
	m = m.SetTabs([]TabState{{Path: "query1.sql", Content: "select 1"}})
	setTextareaCursor(&m.tabs[0].ta, 0, 0)

	m, _ = m.commentCurrentLineTextarea()
	if got := m.tabs[0].ta.Value(); got != "-- select 1" {
		t.Fatalf("value after commentCurrentLineTextarea() = %q, want commented line", got)
	}
	if got := m.tabs[0].ta.Line(); got != 0 {
		t.Fatalf("cursor line after commentCurrentLineTextarea() = %d, want 0", got)
	}
}

func TestCommentCurrentLineVimCommentsLineAndMovesDown(t *testing.T) {
	cfg := testConfig()
	cfg.Editor.VimMode = true
	m := New(cfg)
	m = m.SetSize(80, 8)
	m = m.SetTabs([]TabState{{Path: "query1.sql", Content: "select 1\nselect 2"}})
	m.tabs[0].vim.Buf.SetCursor(0, 0)

	m, _ = m.commentCurrentLineVim()
	if got := m.tabs[0].vim.Buf.Value(); got != "-- select 1\nselect 2" {
		t.Fatalf("vim buffer after commentCurrentLineVim() = %q, want first line commented", got)
	}
	if got := m.tabs[0].vim.Buf.CursorRow(); got != 1 {
		t.Fatalf("vim cursor row after commentCurrentLineVim() = %d, want 1", got)
	}
}

func TestVimInsertCursorBlinkTickTogglesVisibility(t *testing.T) {
	cfg := testConfig()
	cfg.Editor.VimMode = true
	m := New(cfg)
	m = m.SetSize(80, 8)
	m.tabs[0].vim.Mode = vim.ModeInsert

	cmd := m.refreshVimInsertCursorBlink()
	if cmd == nil {
		t.Fatalf("refreshVimInsertCursorBlink() should schedule a tick in vim insert mode")
	}
	if !m.blinkOn {
		t.Fatalf("blink should restart in visible state")
	}

	blinkID := m.blinkID
	m, next := m.Update(vimInsertCursorBlinkMsg{id: blinkID})
	if m.blinkOn {
		t.Fatalf("blink tick should toggle cursor visibility off")
	}
	if next == nil {
		t.Fatalf("blink tick should schedule the next frame")
	}
	if m.blinkID != blinkID {
		t.Fatalf("blink tick should preserve blink id, got %d want %d", m.blinkID, blinkID)
	}
}

func TestRenderVimContentBlinkChangesCursorStyleOnly(t *testing.T) {
	cfg := testConfig()
	cfg.Editor.VimMode = true
	m := New(cfg)
	m = m.SetSize(80, 8)
	m = m.SetTabs([]TabState{{
		Path:    "query1.sql",
		Content: "select 1;",
	}})
	m.tabs[0].vim.Mode = vim.ModeInsert
	m.tabs[0].vim.Buf.SetCursor(0, 0)
	m.focused = true
	m.blinkOn = true
	on := m.renderVimContent()
	m.blinkOn = false
	off := m.renderVimContent()

	if on == off {
		t.Fatalf("blink on/off should change rendered vim cursor styling")
	}
	if ansi.Strip(on) != ansi.Strip(off) {
		t.Fatalf("blink on/off should not change rendered text content")
	}
}

func TestUpdateVimVisualYankCopiesToClipboard(t *testing.T) {
	cfg := testConfig()
	cfg.Editor.VimMode = true
	m := New(cfg)
	m = m.SetSize(80, 8)
	m = m.SetTabs([]TabState{{Path: "query1.sql", Content: "hello world"}})
	m.tabs[0].vim.Mode = vim.ModeVisual
	m.tabs[0].vim.Anchor = vim.Pos{Row: 0, Col: 0}
	m.tabs[0].vim.Buf.SetCursor(0, 4)

	var copied string
	oldWriter := writeClipboard
	writeClipboard = func(text string) error {
		copied = text
		return nil
	}
	defer func() { writeClipboard = oldWriter }()

	nm, cmd := m.updateVim(keyMsg("y"))
	m = nm
	if m.tabs[0].vim.Mode != vim.ModeNormal {
		t.Fatalf("expected visual yank to exit to normal mode")
	}
	if cmd == nil {
		t.Fatalf("expected clipboard command after visual yank")
	}
	if msg := cmd(); msg != nil {
		t.Fatalf("clipboard command should not emit a message, got %#v", msg)
	}
	if copied != "hello" {
		t.Fatalf("clipboard copy = %q, want %q", copied, "hello")
	}
	if reg, regLine := m.tabs[0].vim.Buf.Register(); reg != "hello" || regLine {
		t.Fatalf("register = %q linewise=%v, want charwise hello", reg, regLine)
	}
}

func TestUpdateVimYYCopiesLineToClipboard(t *testing.T) {
	cfg := testConfig()
	cfg.Editor.VimMode = true
	m := New(cfg)
	m = m.SetSize(80, 8)
	m = m.SetTabs([]TabState{{Path: "query1.sql", Content: "hello world\nselect 1;"}})

	var copied string
	oldWriter := writeClipboard
	writeClipboard = func(text string) error {
		copied = text
		return nil
	}
	defer func() { writeClipboard = oldWriter }()

	nm, cmd := m.updateVim(keyMsg("y"))
	m = nm
	if cmd != nil {
		t.Fatalf("first y of yy should not copy yet")
	}
	nm, cmd = m.updateVim(keyMsg("y"))
	m = nm
	if cmd == nil {
		t.Fatalf("second y of yy should copy to clipboard")
	}
	_ = cmd()
	if copied != "hello world\n" {
		t.Fatalf("clipboard copy = %q, want linewise %q", copied, "hello world\n")
	}
	if reg, regLine := m.tabs[0].vim.Buf.Register(); reg != "hello world\n" || !regLine {
		t.Fatalf("register = %q linewise=%v, want linewise hello world", reg, regLine)
	}
}

func TestUpdateVimYWCopiesWordToClipboard(t *testing.T) {
	cfg := testConfig()
	cfg.Editor.VimMode = true
	m := New(cfg)
	m = m.SetSize(80, 8)
	m = m.SetTabs([]TabState{{Path: "query1.sql", Content: "hello world"}})

	var copied string
	oldWriter := writeClipboard
	writeClipboard = func(text string) error {
		copied = text
		return nil
	}
	defer func() { writeClipboard = oldWriter }()

	nm, cmd := m.updateVim(keyMsg("y"))
	m = nm
	if cmd != nil {
		t.Fatalf("first y of yw should not copy yet")
	}
	nm, cmd = m.updateVim(keyMsg("w"))
	m = nm
	if cmd == nil {
		t.Fatalf("yw should copy to clipboard")
	}
	_ = cmd()
	if copied != "hello " {
		t.Fatalf("clipboard copy = %q, want %q", copied, "hello ")
	}
	if reg, regLine := m.tabs[0].vim.Buf.Register(); reg != "hello " || regLine {
		t.Fatalf("register = %q linewise=%v, want charwise hello-space", reg, regLine)
	}
}

func keyMsg(key string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
}

func testConfig() *config.Config {
	return &config.Config{Theme: config.ThemeConfig{
		LineNumber:        "#4a4a4a",
		CursorLineNumber:  "#858585",
		Selection:         "#264f78",
		ActiveQueryGutter: "#c86f93",
		Cursor:            "#a6e3a1",
		InsertCursor:      "#a6e3a1",
	}}
}
