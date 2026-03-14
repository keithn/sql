package editor

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/sqltui/sql/internal/config"
	"github.com/sqltui/sql/internal/db"
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

func TestDetectBlockRangeUnionAll(t *testing.T) {
	// UNION ALL query written with blank lines around the operator — the whole
	// thing should be treated as one block regardless of cursor position.
	text := "SELECT a, b\nFROM t1\nWHERE a = 1\n\nUNION ALL\n\nSELECT a, b\nFROM t2\nWHERE a = 2"
	// line indices: 0=SELECT, 1=FROM, 2=WHERE, 3=blank, 4=UNION ALL, 5=blank, 6=SELECT, 7=FROM, 8=WHERE

	for _, cursorLine := range []int{0, 1, 2, 4, 6, 7, 8} {
		start, end, ok := detectBlockRange(text, cursorLine)
		if !ok || start != 0 || end != 8 {
			t.Errorf("cursor=%d: detectBlockRange()=(%d,%d,%v), want (0,8,true)", cursorLine, start, end, ok)
		}
	}

	// Cursor on blank lines should also pick up the whole block (snapping to adjacent content).
	// Line 3 (blank between WHERE and UNION ALL) snaps to line 2, then forward scan crosses UNION ALL.
	start, end, ok := detectBlockRange(text, 3)
	if !ok || start != 0 || end != 8 {
		t.Errorf("cursor=3 (blank before UNION ALL): detectBlockRange()=(%d,%d,%v), want (0,8,true)", start, end, ok)
	}
}

func TestDetectBlockRangeUnionSeparateFromOtherQueries(t *testing.T) {
	// Two independent queries; the second is a UNION ALL.  Cursor in first query
	// should not extend into the UNION ALL block.
	text := "SELECT 1\n\nSELECT a\nFROM t1\n\nUNION ALL\n\nSELECT b\nFROM t2"
	// line 0: SELECT 1  (standalone)
	// line 1: blank
	// lines 2-8: UNION ALL query
	start, end, ok := detectBlockRange(text, 0)
	if !ok || start != 0 || end != 0 {
		t.Errorf("cursor=0 (standalone query): detectBlockRange()=(%d,%d,%v), want (0,0,true)", start, end, ok)
	}
}

func TestUpdateCtrlROpensRefactorPopup(t *testing.T) {
	m := New(testConfig())
	m = m.SetSize(80, 8)
	m = m.SetTabs([]TabState{{Path: "query1.sql", Content: "select * from tblUser"}})
	setTextareaCursor(&m.tabs[0].ta, 0, 16)

	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	m = nm

	if !m.popup.visible || m.popup.mode != popupModeRefactor {
		t.Fatalf("expected visible refactor popup after ctrl+r")
	}
	if got := ansi.Strip(m.renderPopup()); !strings.Contains(got, "Name table alias") || !strings.Contains(got, "Expand *") || !strings.Contains(got, "Convert SELECT to UPDATE") || !strings.Contains(got, "Add UPDATE below") || !strings.Contains(got, "Convert UPDATE to SELECT") || !strings.Contains(got, "Add SELECT below") || !strings.Contains(got, "Wrap INSERT with IDENTITY_INSERT") || !strings.Contains(got, "u") || !strings.Contains(got, "U") || !strings.Contains(got, "s") || !strings.Contains(got, "S") || !strings.Contains(got, "i") {
		t.Fatalf("refactor popup render = %q, want alias/expand/select-update/identity-insert actions with shortcuts", got)
	}
}

func TestRenderPopupStylesCompletionDetailSeparately(t *testing.T) {
	m := New(testConfig())
	m.popup = completionPopup{
		visible:  true,
		mode:     popupModeCompletion,
		selected: 0,
		items: []popupItem{{
			Text:   "O.lngClientID = C.lngClientID",
			Kind:   CompletionKindName,
			Detail: "pred · heur",
		}},
	}

	raw := m.renderPopup()
	if got := ansi.Strip(raw); !strings.Contains(got, "pred · heur") {
		t.Fatalf("stripped popup render = %q, want detail text present", got)
	}
	if !strings.Contains(raw, popupSelectedDetailStyle.Render("  pred · heur")) {
		t.Fatalf("popup render should style detail with selected detail style; raw = %q", raw)
	}
}

func TestUpdateCtrlRThenNAliasesCurrentBlockOnly(t *testing.T) {
	m := New(testConfig())
	m = m.SetSize(80, 10)
	m = m.SetTabs([]TabState{{
		Path:    "query1.sql",
		Content: "select tblUser.Name\nfrom tblUser\nwhere tblUser.Id = 1\n\nselect tblUser.Name\nfrom tblUser\nwhere tblUser.Id = 2",
	}})
	setTextareaCursor(&m.tabs[0].ta, 1, 7)

	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	m = nm
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = nm

	want := "select U.Name\nfrom tblUser U\nwhere U.Id = 1\n\nselect tblUser.Name\nfrom tblUser\nwhere tblUser.Id = 2"
	if got := m.tabs[0].ta.Value(); got != want {
		t.Fatalf("textarea value after refactor = %q, want %q", got, want)
	}
	if m.popup.visible {
		t.Fatalf("refactor popup should close after applying action")
	}
}

func TestUpdateCtrlRThenNVimAliasesCurrentBlockOnly(t *testing.T) {
	cfg := testConfig()
	cfg.Editor.VimMode = true
	m := New(cfg)
	m = m.SetSize(80, 10)
	m = m.SetTabs([]TabState{{
		Path:    "query1.sql",
		Content: "select tblUser.Name\nfrom tblUser\nwhere tblUser.Id = 1\n\nselect tblUser.Name\nfrom tblUser\nwhere tblUser.Id = 2",
	}})
	m.tabs[0].vim.Buf.SetCursor(1, 7)

	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	m = nm
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = nm

	want := "select U.Name\nfrom tblUser U\nwhere U.Id = 1\n\nselect tblUser.Name\nfrom tblUser\nwhere tblUser.Id = 2"
	if got := m.tabs[0].vim.Buf.Value(); got != want {
		t.Fatalf("vim value after refactor = %q, want %q", got, want)
	}
	if m.popup.visible {
		t.Fatalf("refactor popup should close after applying action in vim mode")
	}
}

func TestUpdateCtrlRThenEExpandsSelectStarCurrentBlockOnly(t *testing.T) {
	m := New(testConfig()).SetSchema(joinInferenceTestSchema())
	m = m.SetSize(80, 10)
	m = m.SetTabs([]TabState{{
		Path:    "query1.sql",
		Content: "select *\nfrom tblUser\nwhere Id = 1\n\nselect *\nfrom tblUser",
	}})
	setTextareaCursor(&m.tabs[0].ta, 1, 6)

	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	m = nm
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m = nm

	want := "select Id, Name\nfrom tblUser\nwhere Id = 1\n\nselect *\nfrom tblUser"
	if got := m.tabs[0].ta.Value(); got != want {
		t.Fatalf("textarea value after expand-star refactor = %q, want %q", got, want)
	}
	if m.popup.visible {
		t.Fatalf("refactor popup should close after expand-star action")
	}
}

func TestUpdateCtrlRThenEExpandsJoinStarUsingAliases(t *testing.T) {
	m := New(testConfig()).SetSchema(joinInferenceTestSchema())
	m = m.SetSize(100, 10)
	m = m.SetTabs([]TabState{{
		Path:    "query1.sql",
		Content: "select *\nfrom tblUser U\njoin tblOrder O on U.Id = O.UserId",
	}})
	setTextareaCursor(&m.tabs[0].ta, 0, 8)

	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	m = nm
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m = nm

	want := "select U.Id, U.Name, O.Id, O.UserId\nfrom tblUser U\njoin tblOrder O on U.Id = O.UserId"
	if got := m.tabs[0].ta.Value(); got != want {
		t.Fatalf("textarea value after join expand-star refactor = %q, want %q", got, want)
	}
}

func TestUpdateCtrlRThenEVimExpandsSelectStarCurrentBlockOnly(t *testing.T) {
	cfg := testConfig()
	cfg.Editor.VimMode = true
	m := New(cfg).SetSchema(joinInferenceTestSchema())
	m = m.SetSize(80, 10)
	m = m.SetTabs([]TabState{{
		Path:    "query1.sql",
		Content: "select *\nfrom tblUser\nwhere Id = 1\n\nselect *\nfrom tblUser",
	}})
	m.tabs[0].vim.Buf.SetCursor(1, 6)

	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	m = nm
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m = nm

	want := "select Id, Name\nfrom tblUser\nwhere Id = 1\n\nselect *\nfrom tblUser"
	if got := m.tabs[0].vim.Buf.Value(); got != want {
		t.Fatalf("vim value after expand-star refactor = %q, want %q", got, want)
	}
	if m.popup.visible {
		t.Fatalf("refactor popup should close after expand-star action in vim mode")
	}
}

func TestUpdateCtrlRThenuConvertsJoinedSelectToUpdate(t *testing.T) {
	m := New(testConfig())
	m = m.SetSize(100, 10)
	m = m.SetTabs([]TabState{{
		Path:    "query1.sql",
		Content: "select U.Id, U.Name, O.UserId\nfrom tblUser U\njoin tblOrder O on U.Id = O.UserId\nwhere O.Id = 1",
	}})
	setTextareaCursor(&m.tabs[0].ta, 0, 9)

	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	m = nm
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	m = nm

	want := "UPDATE U\nSET\n    Id = U.Id,\n    Name = U.Name\nfrom tblUser U\njoin tblOrder O on U.Id = O.UserId\nwhere O.Id = 1"
	if got := m.tabs[0].ta.Value(); got != want {
		t.Fatalf("textarea value after select->update refactor = %q, want %q", got, want)
	}
}

func TestUpdateCtrlRThenuConvertsSelectStarToUpdateUsingSchema(t *testing.T) {
	m := New(testConfig()).SetSchema(joinInferenceTestSchema())
	m = m.SetSize(100, 10)
	m = m.SetTabs([]TabState{{
		Path:    "query1.sql",
		Content: "select *\nfrom tblUser\nwhere Name like '%keith%'",
	}})
	setTextareaCursor(&m.tabs[0].ta, 0, 8)

	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	m = nm
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	m = nm

	want := "UPDATE tblUser\nSET\n    Id = Id,\n    Name = Name\nwhere Name like '%keith%'"
	if got := m.tabs[0].ta.Value(); got != want {
		t.Fatalf("textarea value after select-star -> update refactor = %q, want %q", got, want)
	}
}

func TestUpdateCtrlRThenuConvertsQualifiedTargetStarToJoinedUpdateUsingSchema(t *testing.T) {
	m := New(testConfig()).SetSchema(joinInferenceTestSchema())
	m = m.SetSize(100, 10)
	m = m.SetTabs([]TabState{{
		Path:    "query1.sql",
		Content: "select U.*\nfrom tblUser U\njoin tblOrder O on U.Id = O.UserId\nwhere O.Id = 1",
	}})
	setTextareaCursor(&m.tabs[0].ta, 0, 9)

	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	m = nm
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	m = nm

	want := "UPDATE U\nSET\n    Id = U.Id,\n    Name = U.Name\nfrom tblUser U\njoin tblOrder O on U.Id = O.UserId\nwhere O.Id = 1"
	if got := m.tabs[0].ta.Value(); got != want {
		t.Fatalf("textarea value after qualified-star select->update refactor = %q, want %q", got, want)
	}
}

func TestUpdateCtrlRThenUAppendsUpdateBelowSelect(t *testing.T) {
	m := New(testConfig())
	m = m.SetSize(80, 10)
	m = m.SetTabs([]TabState{{
		Path:    "query1.sql",
		Content: "select Id, Name\nfrom tblUser\nwhere Id = 1",
	}})
	setTextareaCursor(&m.tabs[0].ta, 1, 4)

	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	m = nm
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'U'}})
	m = nm

	want := "select Id, Name\nfrom tblUser\nwhere Id = 1\n\nUPDATE tblUser\nSET\n    Id = Id,\n    Name = Name\nwhere Id = 1"
	if got := m.tabs[0].ta.Value(); got != want {
		t.Fatalf("textarea value after append-update refactor = %q, want %q", got, want)
	}
}

func TestUpdateCtrlRThensConvertsJoinedUpdateToSelect(t *testing.T) {
	m := New(testConfig())
	m = m.SetSize(100, 10)
	m = m.SetTabs([]TabState{{
		Path:    "query1.sql",
		Content: "update U\nset U.Name = 'alice', U.Id = U.Id\nfrom tblUser U\njoin tblOrder O on U.Id = O.UserId\nwhere O.Id = 1",
	}})
	setTextareaCursor(&m.tabs[0].ta, 1, 6)

	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	m = nm
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = nm

	want := "SELECT U.Name, U.Id\nfrom tblUser U\njoin tblOrder O on U.Id = O.UserId\nwhere O.Id = 1"
	if got := m.tabs[0].ta.Value(); got != want {
		t.Fatalf("textarea value after update->select refactor = %q, want %q", got, want)
	}
}

func TestUpdateCtrlRThenSAppendsSelectBelowUpdate(t *testing.T) {
	m := New(testConfig())
	m = m.SetSize(80, 10)
	m = m.SetTabs([]TabState{{
		Path:    "query1.sql",
		Content: "update tblUser\nset Name = 'alice'\nwhere Id = 1",
	}})
	setTextareaCursor(&m.tabs[0].ta, 1, 4)

	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	m = nm
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})
	m = nm

	want := "update tblUser\nset Name = 'alice'\nwhere Id = 1\n\nSELECT Name\nFROM tblUser\nwhere Id = 1"
	if got := m.tabs[0].ta.Value(); got != want {
		t.Fatalf("textarea value after append-select refactor = %q, want %q", got, want)
	}
}

func TestUpdateCtrlRThenuVimConvertsJoinedSelectToUpdate(t *testing.T) {
	cfg := testConfig()
	cfg.Editor.VimMode = true
	m := New(cfg)
	m = m.SetSize(100, 10)
	m = m.SetTabs([]TabState{{
		Path:    "query1.sql",
		Content: "select U.Id, U.Name, O.UserId\nfrom tblUser U\njoin tblOrder O on U.Id = O.UserId\nwhere O.Id = 1",
	}})
	m.tabs[0].vim.Buf.SetCursor(0, 9)

	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	m = nm
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	m = nm

	want := "UPDATE U\nSET\n    Id = U.Id,\n    Name = U.Name\nfrom tblUser U\njoin tblOrder O on U.Id = O.UserId\nwhere O.Id = 1"
	if got := m.tabs[0].vim.Buf.Value(); got != want {
		t.Fatalf("vim value after select->update refactor = %q, want %q", got, want)
	}
}

func TestUpdateCtrlRThenuVimConvertsSelectStarToUpdateUsingSchema(t *testing.T) {
	cfg := testConfig()
	cfg.Editor.VimMode = true
	m := New(cfg).SetSchema(joinInferenceTestSchema())
	m = m.SetSize(100, 10)
	m = m.SetTabs([]TabState{{
		Path:    "query1.sql",
		Content: "select *\nfrom tblUser\nwhere Name like '%keith%'",
	}})
	m.tabs[0].vim.Buf.SetCursor(0, 8)

	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	m = nm
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	m = nm

	want := "UPDATE tblUser\nSET\n    Id = Id,\n    Name = Name\nwhere Name like '%keith%'"
	if got := m.tabs[0].vim.Buf.Value(); got != want {
		t.Fatalf("vim value after select-star -> update refactor = %q, want %q", got, want)
	}
}

func TestUpdateCtrlRTheniWrapsInsertWithIdentityInsertCurrentBlockOnly(t *testing.T) {
	m := New(testConfig())
	m = m.SetSize(100, 10)
	m = m.SetTabs([]TabState{{
		Path:    "query1.sql",
		Content: "insert into dbo.tblUser (Id, Name)\nvalues (1, 'alice')\n\nselect 2",
	}})
	setTextareaCursor(&m.tabs[0].ta, 1, 8)

	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	m = nm
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	m = nm

	want := "SET IDENTITY_INSERT dbo.tblUser ON\ninsert into dbo.tblUser (Id, Name)\nvalues (1, 'alice')\nSET IDENTITY_INSERT dbo.tblUser OFF\n\nselect 2"
	if got := m.tabs[0].ta.Value(); got != want {
		t.Fatalf("textarea value after identity-insert refactor = %q, want %q", got, want)
	}
	if m.popup.visible {
		t.Fatalf("refactor popup should close after identity-insert action")
	}
}

func TestUpdateCtrlRTheniVimWrapsBracketedInsertWithIdentityInsert(t *testing.T) {
	cfg := testConfig()
	cfg.Editor.VimMode = true
	m := New(cfg)
	m = m.SetSize(100, 10)
	m = m.SetTabs([]TabState{{
		Path:    "query1.sql",
		Content: "INSERT INTO [dbo].[tblUser] ([Id], [Name])\nVALUES (1, 'alice')",
	}})
	m.tabs[0].vim.Buf.SetCursor(1, 4)

	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	m = nm
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	m = nm

	want := "SET IDENTITY_INSERT [dbo].[tblUser] ON\nINSERT INTO [dbo].[tblUser] ([Id], [Name])\nVALUES (1, 'alice')\nSET IDENTITY_INSERT [dbo].[tblUser] OFF"
	if got := m.tabs[0].vim.Buf.Value(); got != want {
		t.Fatalf("vim value after identity-insert refactor = %q, want %q", got, want)
	}
	if m.popup.visible {
		t.Fatalf("refactor popup should close after identity-insert action in vim mode")
	}
}

func TestApplyIdentityInsertRefactorLeavesNonInsertUnchanged(t *testing.T) {
	text := "select 1"
	updated, nextLine, nextCol, changed := applyIdentityInsertRefactor(text, 0, 4)
	if changed {
		t.Fatalf("applyIdentityInsertRefactor() changed non-insert block: %q", updated)
	}
	if updated != text || nextLine != 0 || nextCol != 4 {
		t.Fatalf("applyIdentityInsertRefactor() = (%q,%d,%d,%v), want unchanged text/cursor", updated, nextLine, nextCol, changed)
	}
}

func TestUpdatePopupShowsJoinPredicateSuggestionsAfterOn(t *testing.T) {
	m := New(testConfig()).SetSchema(joinInferenceTestSchema())
	m = m.SetSize(80, 8)
	m = m.SetTabs([]TabState{{Path: "query1.sql", Content: "select * from tblUser U join tblOrder O on "}})
	setTextareaCursor(&m.tabs[0].ta, 0, len("select * from tblUser U join tblOrder O on "))

	m.updatePopup()

	if !m.popup.visible {
		t.Fatalf("expected join popup after ON clause")
	}
	if len(m.popup.items) == 0 || m.popup.items[0].InsertText != "U.Id = O.UserId" {
		t.Fatalf("join popup first item = %#v, want inferred FK predicate", m.popup.items)
	}
}

func TestUpdatePopupShowsJoinPredicateSuggestionsForMSSQLForeignKeyNames(t *testing.T) {
	m := New(testConfig()).SetSchema(joinMSSQLForeignKeySchema())
	m = m.SetSize(100, 8)
	m = m.SetTabs([]TabState{{Path: "query1.sql", Content: "select * from tblOutpost O inner join tblLogger L on "}})
	setTextareaCursor(&m.tabs[0].ta, 0, len("select * from tblOutpost O inner join tblLogger L on "))

	m.updatePopup()

	if !m.popup.visible {
		t.Fatalf("expected join popup after ON clause")
	}
	if len(m.popup.items) == 0 || m.popup.items[0].InsertText != "O.lngLoggerID = L.lngLoggerID" {
		t.Fatalf("join popup first item = %#v, want lngLoggerID FK predicate", m.popup.items)
	}
	if got := m.popup.items[0].Detail; got != "pred · fk" {
		t.Fatalf("join popup detail = %q, want pred · fk", got)
	}
}

func TestUpdatePopupShowsJoinPredicateSuggestionsForHungarianHeuristicNames(t *testing.T) {
	m := New(testConfig()).SetSchema(joinClientHungarianHeuristicSchema())
	m = m.SetSize(100, 8)
	m = m.SetTabs([]TabState{{Path: "query1.sql", Content: "select * from tblOutpost O inner join tblClient C on "}})
	setTextareaCursor(&m.tabs[0].ta, 0, len("select * from tblOutpost O inner join tblClient C on "))

	m.updatePopup()

	if !m.popup.visible {
		t.Fatalf("expected join popup after ON clause")
	}
	if len(m.popup.items) == 0 || m.popup.items[0].InsertText != "O.lngClientID = C.lngClientID" {
		t.Fatalf("join popup first item = %#v, want lngClientID heuristic predicate", m.popup.items)
	}
	if got := m.popup.items[0].Detail; got != "pred · heur" {
		t.Fatalf("join popup detail = %q, want pred · heur", got)
	}
}

func TestUpdatePopupShowsJoinPredicateSuggestionsForUnderscoreHeuristicNames(t *testing.T) {
	m := New(testConfig()).SetSchema(joinClientUnderscoreHeuristicSchema())
	m = m.SetSize(100, 8)
	m = m.SetTabs([]TabState{{Path: "query1.sql", Content: "select * from tblOrderSummary S inner join tblClient C on "}})
	setTextareaCursor(&m.tabs[0].ta, 0, len("select * from tblOrderSummary S inner join tblClient C on "))

	m.updatePopup()

	if !m.popup.visible {
		t.Fatalf("expected join popup after ON clause")
	}
	if len(m.popup.items) == 0 || m.popup.items[0].InsertText != "S.Client_id = C.lngClientID" {
		t.Fatalf("join popup first item = %#v, want Client_id heuristic predicate", m.popup.items)
	}
}

func TestUpdatePopupShowsJoinPredicateSuggestionsAfterOnQualifiedPrefix(t *testing.T) {
	m := New(testConfig()).SetSchema(joinMSSQLForeignKeySchema())
	m = m.SetSize(100, 8)
	m = m.SetTabs([]TabState{{Path: "query1.sql", Content: "select * from tblOutpost O inner join tblLogger L on O."}})
	setTextareaCursor(&m.tabs[0].ta, 0, len("select * from tblOutpost O inner join tblLogger L on O."))

	m.updatePopup()

	if !m.popup.visible {
		t.Fatalf("expected join popup after ON qualified prefix")
	}
	if len(m.popup.items) == 0 || m.popup.items[0].InsertText != "lngLoggerID = L.lngLoggerID" {
		t.Fatalf("join popup first item = %#v, want suffix completion after O.", m.popup.items)
	}
	if got := m.popup.items[0].Detail; got != "lhs · fk" {
		t.Fatalf("join popup detail = %q, want lhs · fk", got)
	}
}

func TestJoinPredicateRankingPrefersMostRecentMatchingTable(t *testing.T) {
	m := New(testConfig()).SetSchema(joinRankingTestSchema())
	m = m.SetSize(100, 8)
	m = m.SetTabs([]TabState{{Path: "query1.sql", Content: "select * from tblAccount A join tblZoo Z on 1 = 1 join tblTask T on "}})
	setTextareaCursor(&m.tabs[0].ta, 0, len("select * from tblAccount A join tblZoo Z on 1 = 1 join tblTask T on "))

	m.updatePopup()

	if !m.popup.visible || len(m.popup.items) < 2 {
		t.Fatalf("expected multiple ranked join suggestions, got %#v", m.popup.items)
	}
	if got := m.popup.items[0].InsertText; got != "Z.Id = T.ZooId" {
		t.Fatalf("top-ranked join predicate = %q, want most recent-table relation first", got)
	}
	if got := m.popup.items[1].InsertText; got != "A.Id = T.AccountId" {
		t.Fatalf("second-ranked join predicate = %q, want earlier-table relation second", got)
	}
}

func TestAcceptJoinCompletionAfterOnInsertsPredicateTextarea(t *testing.T) {
	m := New(testConfig()).SetSchema(joinInferenceTestSchema())
	m = m.SetSize(80, 8)
	m = m.SetTabs([]TabState{{Path: "query1.sql", Content: "select * from tblUser U join tblOrder O on "}})
	setTextareaCursor(&m.tabs[0].ta, 0, len("select * from tblUser U join tblOrder O on "))
	m.updatePopup()

	m = m.acceptCompletion()

	want := "select * from tblUser U join tblOrder O on U.Id = O.UserId"
	if got := m.tabs[0].ta.Value(); got != want {
		t.Fatalf("textarea value after join completion = %q, want %q", got, want)
	}
}

func TestAcceptJoinCompletionAfterOnWithoutTrailingSpaceAddsSpaceTextarea(t *testing.T) {
	m := New(testConfig()).SetSchema(joinInferenceTestSchema())
	m = m.SetSize(80, 8)
	m = m.SetTabs([]TabState{{Path: "query1.sql", Content: "select * from tblUser U join tblOrder O on"}})
	setTextareaCursor(&m.tabs[0].ta, 0, len("select * from tblUser U join tblOrder O on"))
	m.updatePopup()

	m = m.acceptCompletion()

	want := "select * from tblUser U join tblOrder O on U.Id = O.UserId"
	if got := m.tabs[0].ta.Value(); got != want {
		t.Fatalf("textarea value after join completion without trailing ON space = %q, want %q", got, want)
	}
}

func TestAcceptJoinCompletionAfterOnQualifiedPrefixCompletesPredicateTextarea(t *testing.T) {
	m := New(testConfig()).SetSchema(joinMSSQLForeignKeySchema())
	m = m.SetSize(100, 8)
	m = m.SetTabs([]TabState{{Path: "query1.sql", Content: "select * from tblOutpost O inner join tblLogger L on O."}})
	setTextareaCursor(&m.tabs[0].ta, 0, len("select * from tblOutpost O inner join tblLogger L on O."))
	m.updatePopup()

	m = m.acceptCompletion()

	want := "select * from tblOutpost O inner join tblLogger L on O.lngLoggerID = L.lngLoggerID"
	if got := m.tabs[0].ta.Value(); got != want {
		t.Fatalf("textarea value after join completion from O. prefix = %q, want %q", got, want)
	}
}

func TestAcceptJoinCompletionForSelfReferentialParentHeuristicTextarea(t *testing.T) {
	m := New(testConfig()).SetSchema(joinClientSelfReferenceSchema())
	m = m.SetSize(100, 8)
	m = m.SetTabs([]TabState{{Path: "query1.sql", Content: "select * from tblClient Parent inner join tblClient Child on Child."}})
	setTextareaCursor(&m.tabs[0].ta, 0, len("select * from tblClient Parent inner join tblClient Child on Child."))
	m.updatePopup()
	if !m.popup.visible || len(m.popup.items) == 0 {
		t.Fatalf("expected self-referential join popup")
	}
	if got := m.popup.items[0].Detail; got != "rhs · self" {
		t.Fatalf("join popup detail = %q, want rhs · self", got)
	}

	m = m.acceptCompletion()

	want := "select * from tblClient Parent inner join tblClient Child on Child.lngParentID = Parent.lngClientID"
	if got := m.tabs[0].ta.Value(); got != want {
		t.Fatalf("textarea value after self-referential join completion = %q, want %q", got, want)
	}
}

func TestAcceptJoinCompletionAfterOnPartialColumnPrefixKeepsTypedPrefixTextarea(t *testing.T) {
	m := New(testConfig()).SetSchema(joinMSSQLForeignKeySchema())
	m = m.SetSize(100, 8)
	m = m.SetTabs([]TabState{{Path: "query1.sql", Content: "select * from tblOutpost O inner join tblLogger L on O.lng"}})
	setTextareaCursor(&m.tabs[0].ta, 0, len("select * from tblOutpost O inner join tblLogger L on O.lng"))
	m.updatePopup()

	m = m.acceptCompletion()

	want := "select * from tblOutpost O inner join tblLogger L on O.lngLoggerID = L.lngLoggerID"
	if got := m.tabs[0].ta.Value(); got != want {
		t.Fatalf("textarea value after join completion from O.lng prefix = %q, want %q", got, want)
	}
}

func TestUpdatePopupShowsJoinRHSSuggestionsAfterEquals(t *testing.T) {
	m := New(testConfig()).SetSchema(joinInferenceTestSchema())
	m = m.SetSize(80, 8)
	m = m.SetTabs([]TabState{{Path: "query1.sql", Content: "select * from tblUser U join tblOrder O on U.Id ="}})
	setTextareaCursor(&m.tabs[0].ta, 0, len("select * from tblUser U join tblOrder O on U.Id ="))

	m.updatePopup()

	if !m.popup.visible {
		t.Fatalf("expected join RHS popup after equals")
	}
	if len(m.popup.items) == 0 || m.popup.items[0].Text != "O.UserId" {
		t.Fatalf("join RHS popup first item = %#v, want O.UserId", m.popup.items)
	}
	if m.popup.items[0].InsertText != " O.UserId" {
		t.Fatalf("join RHS popup insert text = %q, want leading-space RHS insertion", m.popup.items[0].InsertText)
	}
	if got := m.popup.items[0].Detail; got != "rhs · fk" {
		t.Fatalf("join RHS popup detail = %q, want rhs · fk", got)
	}
}

func TestAcceptJoinCompletionAfterPartialRHSPrefixKeepsCurrentPredicateTextarea(t *testing.T) {
	m := New(testConfig()).SetSchema(joinMSSQLForeignKeySchema())
	m = m.SetSize(100, 8)
	m = m.SetTabs([]TabState{{Path: "query1.sql", Content: "select * from tblOutpost O inner join tblLogger L on O.lngLoggerID = L.lngLogger"}})
	setTextareaCursor(&m.tabs[0].ta, 0, len("select * from tblOutpost O inner join tblLogger L on O.lngLoggerID = L.lngLogger"))
	m.updatePopup()

	m = m.acceptCompletion()

	want := "select * from tblOutpost O inner join tblLogger L on O.lngLoggerID = L.lngLoggerID"
	if got := m.tabs[0].ta.Value(); got != want {
		t.Fatalf("textarea value after join completion from RHS prefix = %q, want %q", got, want)
	}
}

func TestJoinPredicateRankingPrefersExplicitFKOverRecentHeuristic(t *testing.T) {
	m := New(testConfig()).SetSchema(joinMixedRankingTestSchema())
	m = m.SetSize(100, 8)
	m = m.SetTabs([]TabState{{Path: "query1.sql", Content: "select * from tblAccount A join tblZoo Z on 1 = 1 join tblTask T on "}})
	setTextareaCursor(&m.tabs[0].ta, 0, len("select * from tblAccount A join tblZoo Z on 1 = 1 join tblTask T on "))

	m.updatePopup()

	if !m.popup.visible || len(m.popup.items) < 2 {
		t.Fatalf("expected multiple ranked join suggestions, got %#v", m.popup.items)
	}
	if got := m.popup.items[0].InsertText; got != "A.Id = T.AccountId" {
		t.Fatalf("top-ranked join predicate = %q, want explicit FK before heuristic", got)
	}
	if got := m.popup.items[1].InsertText; got != "Z.Id = T.ZooId" {
		t.Fatalf("second-ranked join predicate = %q, want heuristic suggestion after explicit FK", got)
	}
}

func TestAcceptTableCompletionAutoAppendsJoinOnUniqueFK(t *testing.T) {
	m := New(testConfig()).SetSchema(joinInferenceTestSchema())
	m = m.SetSchemaCompletions([]CompletionItem{{Text: "tblOrder", Kind: CompletionKindTable}})
	m = m.SetSize(80, 8)
	m = m.SetTabs([]TabState{{Path: "query1.sql", Content: "select * from tblUser U join tblOrd"}})
	setTextareaCursor(&m.tabs[0].ta, 0, len("select * from tblUser U join tblOrd"))
	m.updatePopup()

	m = m.acceptCompletion()

	want := "select * from tblUser U join tblOrder ON U.Id = tblOrder.UserId"
	if got := m.tabs[0].ta.Value(); got != want {
		t.Fatalf("textarea value after table completion join follow-up = %q, want %q", got, want)
	}
}

func TestAcceptJoinCompletionAfterOnInsertsPredicateVim(t *testing.T) {
	cfg := testConfig()
	cfg.Editor.VimMode = true
	m := New(cfg).SetSchema(joinInferenceTestSchema())
	m = m.SetSize(80, 8)
	m = m.SetTabs([]TabState{{Path: "query1.sql", Content: "select * from tblUser U join tblOrder O on "}})
	m.tabs[0].vim.HandleKey("A")
	m.updatePopupVim()

	m = m.acceptCompletionVim()

	want := "select * from tblUser U join tblOrder O on U.Id = O.UserId"
	if got := m.tabs[0].vim.Buf.Value(); got != want {
		t.Fatalf("vim value after join completion = %q, want %q", got, want)
	}
}

func TestVimInsertDeleteDeletesCharUnderCursorAndClosesPopup(t *testing.T) {
	cfg := testConfig()
	cfg.Editor.VimMode = true
	m := New(cfg)
	m = m.SetSchemaCompletions([]CompletionItem{{Text: "tblUser", Kind: CompletionKindTable}})
	m = m.SetSize(80, 8)
	m = m.SetTabs([]TabState{{Path: "query1.sql", Content: "select * from tblUseX"}})
	m.tabs[0].vim.Buf.SetCursor(0, len("select * from tblUse"))
	m.tabs[0].vim.HandleKey("i")
	m.updatePopupVim()
	if !m.popup.visible {
		t.Fatalf("expected popup before delete in vim insert mode")
	}

	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyDelete})
	m = nm

	if got := m.tabs[0].vim.Buf.Value(); got != "select * from tblUse" {
		t.Fatalf("vim value after delete = %q, want deleted char under insert cursor", got)
	}
	if m.popup.visible {
		t.Fatalf("popup should close on vim insert delete")
	}
	if got := m.tabs[0].vim.Mode; got != vim.ModeInsert {
		t.Fatalf("vim mode after delete = %v, want %v", got, vim.ModeInsert)
	}
}

func joinInferenceTestSchema() *db.Schema {
	return &db.Schema{Databases: []db.Database{{
		Name: "testdb",
		Schemas: []db.SchemaNode{{
			Name: "dbo",
			Tables: []db.Table{
				{Name: "tblUser", Columns: []db.ColumnDef{{Name: "Id", PrimaryKey: true}, {Name: "Name"}}},
				{Name: "tblOrder", Columns: []db.ColumnDef{{Name: "Id", PrimaryKey: true}, {Name: "UserId", ForeignKey: &db.ForeignKey{RefTable: "tblUser", RefColumn: "Id"}}}},
			},
		}},
	}}}
}

func joinMSSQLForeignKeySchema() *db.Schema {
	return &db.Schema{Databases: []db.Database{{
		Name: "ActivePLC",
		Schemas: []db.SchemaNode{{
			Name: "dbo",
			Tables: []db.Table{
				{Name: "tblLogger", Columns: []db.ColumnDef{{Name: "lngLoggerID", PrimaryKey: true}, {Name: "strName"}}},
				{Name: "tblOutpost", Columns: []db.ColumnDef{{Name: "lngOutpostID", PrimaryKey: true}, {Name: "lngLoggerID", ForeignKey: &db.ForeignKey{RefTable: "dbo.tblLogger", RefColumn: "lngLoggerID"}}}},
			},
		}},
	}}}
}

func joinClientHungarianHeuristicSchema() *db.Schema {
	return &db.Schema{Databases: []db.Database{{
		Name: "ActivePLC",
		Schemas: []db.SchemaNode{{
			Name: "dbo",
			Tables: []db.Table{
				{Name: "tblClient", Columns: []db.ColumnDef{{Name: "lngClientID", PrimaryKey: true}, {Name: "strClientName"}}},
				{Name: "tblOutpost", Columns: []db.ColumnDef{{Name: "lngOutpostID", PrimaryKey: true}, {Name: "lngClientID"}}},
			},
		}},
	}}}
}

func joinClientUnderscoreHeuristicSchema() *db.Schema {
	return &db.Schema{Databases: []db.Database{{
		Name: "ActivePLC",
		Schemas: []db.SchemaNode{{
			Name: "dbo",
			Tables: []db.Table{
				{Name: "tblClient", Columns: []db.ColumnDef{{Name: "lngClientID", PrimaryKey: true}, {Name: "strClientName"}}},
				{Name: "tblOrderSummary", Columns: []db.ColumnDef{{Name: "lngOrderSummaryID", PrimaryKey: true}, {Name: "Client_id"}}},
			},
		}},
	}}}
}

func joinClientSelfReferenceSchema() *db.Schema {
	return &db.Schema{Databases: []db.Database{{
		Name: "ActivePLC",
		Schemas: []db.SchemaNode{{
			Name: "dbo",
			Tables: []db.Table{
				{Name: "tblClient", Columns: []db.ColumnDef{{Name: "lngClientID", PrimaryKey: true}, {Name: "lngParentID"}, {Name: "strClientName"}}},
			},
		}},
	}}}
}

func joinRankingTestSchema() *db.Schema {
	return &db.Schema{Databases: []db.Database{{
		Name: "testdb",
		Schemas: []db.SchemaNode{{
			Name: "dbo",
			Tables: []db.Table{
				{Name: "tblAccount", Columns: []db.ColumnDef{{Name: "Id", PrimaryKey: true}}},
				{Name: "tblZoo", Columns: []db.ColumnDef{{Name: "Id", PrimaryKey: true}}},
				{Name: "tblTask", Columns: []db.ColumnDef{{Name: "Id", PrimaryKey: true}, {Name: "AccountId", ForeignKey: &db.ForeignKey{RefTable: "tblAccount", RefColumn: "Id"}}, {Name: "ZooId", ForeignKey: &db.ForeignKey{RefTable: "tblZoo", RefColumn: "Id"}}}},
			},
		}},
	}}}
}

func joinMixedRankingTestSchema() *db.Schema {
	return &db.Schema{Databases: []db.Database{{
		Name: "testdb",
		Schemas: []db.SchemaNode{{
			Name: "dbo",
			Tables: []db.Table{
				{Name: "tblAccount", Columns: []db.ColumnDef{{Name: "Id", PrimaryKey: true}}},
				{Name: "tblZoo", Columns: []db.ColumnDef{{Name: "Id", PrimaryKey: true}}},
				{Name: "tblTask", Columns: []db.ColumnDef{{Name: "Id", PrimaryKey: true}, {Name: "AccountId", ForeignKey: &db.ForeignKey{RefTable: "tblAccount", RefColumn: "Id"}}, {Name: "ZooId"}}},
			},
		}},
	}}}
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

func TestIsFormatShortcutNormalizesCtrlShiftLetter(t *testing.T) {
	m := New(testConfig())
	if !m.isFormatShortcut(tea.KeyMsg{Type: tea.KeyCtrlF}) {
		t.Fatalf("expected ctrl+f to match configured ctrl+shift+f shortcut")
	}
}

func TestFormatActiveBlockTextareaShortcutFormatsOnlyCurrentBlock(t *testing.T) {
	m := New(testConfig())
	m = m.SetSize(80, 8)
	m = m.SetTabs([]TabState{{
		Path:    "query1.sql",
		Content: "select 1\n\nselect id, name from users where active = 1 and role = 'admin'",
	}})
	setTextareaCursor(&m.tabs[0].ta, 2, 0)

	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlF})
	m = nm

	want := "select 1\n\nSELECT\n    id,\n    name\nFROM users\nWHERE active = 1\n  AND role = 'admin'"
	if got := m.tabs[0].ta.Value(); got != want {
		t.Fatalf("textarea value after format = %q, want %q", got, want)
	}
}

func TestFormatActiveBlockVimShortcutFormatsOnlyCurrentBlock(t *testing.T) {
	cfg := testConfig()
	cfg.Editor.VimMode = true
	m := New(cfg)
	m = m.SetSize(80, 8)
	m = m.SetTabs([]TabState{{
		Path:    "query1.sql",
		Content: "select 1\n\nselect id, name from users where active = 1 and role = 'admin'",
	}})
	m.tabs[0].vim.Buf.SetCursor(2, 0)

	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlF})
	m = nm

	want := "select 1\n\nSELECT\n    id,\n    name\nFROM users\nWHERE active = 1\n  AND role = 'admin'"
	if got := m.tabs[0].vim.Buf.Value(); got != want {
		t.Fatalf("vim value after format = %q, want %q", got, want)
	}
}

func TestFormatActiveBlockOnBlankLineFormatsPreviousBlock(t *testing.T) {
	m := New(testConfig())
	m = m.SetSize(80, 8)
	m = m.SetTabs([]TabState{{
		Path:    "query1.sql",
		Content: "select 1\n\nselect 2",
	}})
	setTextareaCursor(&m.tabs[0].ta, 1, 0)

	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlF})
	m = nm

	if got := m.tabs[0].ta.Value(); got != "SELECT 1\n\nselect 2" {
		t.Fatalf("blank-line format should affect previous block, got %q", got)
	}
}

func TestFormatActiveBlockOnLeadingBlankLineIsNoOp(t *testing.T) {
	m := New(testConfig())
	m = m.SetSize(80, 8)
	m = m.SetTabs([]TabState{{
		Path:    "query1.sql",
		Content: "\nselect 1",
	}})
	setTextareaCursor(&m.tabs[0].ta, 0, 0)
	before := m.tabs[0].ta.Value()

	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlF})
	m = nm

	if got := m.tabs[0].ta.Value(); got != before {
		t.Fatalf("leading blank-line format should be no-op, got %q want %q", got, before)
	}
}

func TestToggleVimRestoresTextareaSelectionOnRoundTrip(t *testing.T) {
	m := New(testConfig())
	m = m.SetSize(80, 8)
	m = m.SetTabs([]TabState{{Path: "query1.sql", Content: "alpha bravo"}})
	m.tabs[0].selection = textareaSelection{Active: true, Anchor: textPos{Line: 0, Col: 0}}
	setTextareaCursor(&m.tabs[0].ta, 0, 5)

	m = m.ToggleVim()
	m = m.ToggleVim()

	start, end, ok := m.textareaSelectionRange()
	if !ok {
		t.Fatalf("expected textarea selection after vim round trip")
	}
	if start != (textPos{Line: 0, Col: 0}) || end != (textPos{Line: 0, Col: 5}) {
		t.Fatalf("selection after round trip = (%+v,%+v), want [(0,0),(0,5)]", start, end)
	}
}

func TestToggleVimRestoresTextareaScrollOnRoundTrip(t *testing.T) {
	lines := make([]string, 20)
	for i := range lines {
		lines[i] = fmt.Sprintf("select %d;", i)
	}
	m := New(testConfig())
	m = m.SetSize(80, 6)
	m = m.SetTabs([]TabState{{Path: "query1.sql", Content: strings.Join(lines, "\n")}})

	wantOffset := textareaViewportOffsetForSourceLine(&m.tabs[0].ta, 10)
	setTextareaViewportYOffset(&m.tabs[0].ta, wantOffset)

	m = m.ToggleVim()
	if got := m.tabs[0].vim.TopRow; got != 10 {
		t.Fatalf("vim top row after toggle = %d, want 10", got)
	}
	m = m.ToggleVim()

	if got := textareaViewportYOffset(&m.tabs[0].ta); got != wantOffset {
		t.Fatalf("textarea viewport offset after round trip = %d, want %d", got, wantOffset)
	}
}

func TestToggleVimCarriesCurrentVimScrollIntoTextarea(t *testing.T) {
	lines := make([]string, 20)
	for i := range lines {
		lines[i] = fmt.Sprintf("select %d;", i)
	}
	cfg := testConfig()
	cfg.Editor.VimMode = true
	m := New(cfg)
	m = m.SetSize(80, 6)
	m = m.SetTabs([]TabState{{Path: "query1.sql", Content: strings.Join(lines, "\n")}})
	m.tabs[0].vim.TopRow = 12
	m.tabs[0].vim.Buf.SetCursor(12, 0)

	m = m.ToggleVim()
	if got := textareaSourceLineForViewportOffset(&m.tabs[0].ta, textareaViewportYOffset(&m.tabs[0].ta)); got != 12 {
		t.Fatalf("textarea top source line after vim toggle = %d, want 12", got)
	}
}

func TestToggleVimRestoresVimModeStateOnRoundTrip(t *testing.T) {
	cfg := testConfig()
	cfg.Editor.VimMode = true
	m := New(cfg)
	m = m.SetSize(80, 8)
	m = m.SetTabs([]TabState{{Path: "query1.sql", Content: "select 1;\nselect 2;\nselect 3;\nselect 4;"}})
	m.tabs[0].vim.Mode = vim.ModeInsert
	m.tabs[0].vim.TopRow = 2
	m.tabs[0].vim.Buf.SetCursor(2, 4)

	m = m.ToggleVim()
	m = m.ToggleVim()

	if got := m.tabs[0].vim.Mode; got != vim.ModeInsert {
		t.Fatalf("vim mode after round trip = %v, want %v", got, vim.ModeInsert)
	}
	if got := m.tabs[0].vim.TopRow; got != 2 {
		t.Fatalf("vim top row after round trip = %d, want 2", got)
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

func TestAdjacentBlockLineFindsPreviousAndNextBlocks(t *testing.T) {
	text := "select 1;\n\nselect\n  2\nfrom dual;\n\nselect 3;"
	if got, ok := adjacentBlockLine(text, 3, -1); !ok || got != 0 {
		t.Fatalf("adjacentBlockLine(..., up) = (%d,%v), want (0,true)", got, ok)
	}
	if got, ok := adjacentBlockLine(text, 3, 1); !ok || got != 6 {
		t.Fatalf("adjacentBlockLine(..., down) = (%d,%v), want (6,true)", got, ok)
	}
	if _, ok := adjacentBlockLine(text, 6, 1); ok {
		t.Fatalf("adjacentBlockLine() from last block should have no next block")
	}
	if got, ok := adjacentBlockLine(text, 1, 1); !ok || got != 2 {
		t.Fatalf("adjacentBlockLine() from separator line down = (%d,%v), want (2,true)", got, ok)
	}
}

func TestAltDownMovesTextareaCursorToNextQueryBlock(t *testing.T) {
	m := New(testConfig())
	m = m.SetSize(80, 8)
	m = m.SetTabs([]TabState{{Path: "query1.sql", Content: "select 1;\n\nselect\n  22\nfrom dual;\n\nselect 333;"}})
	setTextareaCursor(&m.tabs[0].ta, 0, 5)
	m.popup.visible = true

	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown, Alt: true})
	m = nm

	if gotLine, gotCol := m.tabs[0].ta.Line(), m.tabs[0].ta.LineInfo().CharOffset; gotLine != 2 || gotCol != 5 {
		t.Fatalf("textarea cursor after alt+down = (%d,%d), want (2,5)", gotLine, gotCol)
	}
	if m.popup.visible {
		t.Fatalf("popup should close after alt+down block jump")
	}
	if m.tabs[0].selection.Active {
		t.Fatalf("textarea selection should clear after alt+down block jump")
	}
}

func TestAltUpAtFirstTextareaQueryBlockIsNoOp(t *testing.T) {
	m := New(testConfig())
	m = m.SetSize(80, 8)
	m = m.SetTabs([]TabState{{Path: "query1.sql", Content: "select 1;\n\nselect 22;"}})
	setTextareaCursor(&m.tabs[0].ta, 0, 4)
	m.popup.visible = true

	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp, Alt: true})
	m = nm

	if gotLine, gotCol := m.tabs[0].ta.Line(), m.tabs[0].ta.LineInfo().CharOffset; gotLine != 0 || gotCol != 4 {
		t.Fatalf("textarea cursor after top alt+up = (%d,%d), want (0,4)", gotLine, gotCol)
	}
	if m.popup.visible {
		t.Fatalf("popup should close on top alt+up no-op")
	}
}

func TestAltUpMovesVimCursorToPreviousQueryBlock(t *testing.T) {
	cfg := testConfig()
	cfg.Editor.VimMode = true
	m := New(cfg)
	m = m.SetSize(80, 4)
	m = m.SetTabs([]TabState{{Path: "query1.sql", Content: "select 1;\n\nselect 22;\n\nselect 333;"}})
	m.tabs[0].vim.Buf.SetCursor(4, 6)
	m.tabs[0].vim.TopRow = 4
	m.tabs[0].vim.Mode = vim.ModeVisual
	m.popup.visible = true

	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp, Alt: true})
	m = nm

	if gotRow, gotCol := m.tabs[0].vim.Buf.CursorRow(), m.tabs[0].vim.Buf.CursorCol(); gotRow != 2 || gotCol != 6 {
		t.Fatalf("vim cursor after alt+up = (%d,%d), want (2,6)", gotRow, gotCol)
	}
	if got := m.tabs[0].vim.TopRow; got > 2 {
		t.Fatalf("vim top row after alt+up = %d, want target row revealed", got)
	}
	if got := m.tabs[0].vim.Mode; got != vim.ModeNormal {
		t.Fatalf("vim mode after alt+up = %v, want %v", got, vim.ModeNormal)
	}
	if m.popup.visible {
		t.Fatalf("popup should close after vim alt+up block jump")
	}
}

func TestAltUpAtFirstVimQueryBlockIsNoOp(t *testing.T) {
	cfg := testConfig()
	cfg.Editor.VimMode = true
	m := New(cfg)
	m = m.SetSize(80, 4)
	m = m.SetTabs([]TabState{{Path: "query1.sql", Content: "select 1;\n\nselect 22;"}})
	m.tabs[0].vim.Buf.SetCursor(0, 4)
	m.tabs[0].vim.Mode = vim.ModeVisual
	m.popup.visible = true

	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp, Alt: true})
	m = nm

	if gotRow, gotCol := m.tabs[0].vim.Buf.CursorRow(), m.tabs[0].vim.Buf.CursorCol(); gotRow != 0 || gotCol != 4 {
		t.Fatalf("vim cursor after top alt+up = (%d,%d), want (0,4)", gotRow, gotCol)
	}
	if got := m.tabs[0].vim.Mode; got != vim.ModeVisual {
		t.Fatalf("vim mode after top alt+up no-op = %v, want %v", got, vim.ModeVisual)
	}
	if m.popup.visible {
		t.Fatalf("popup should close on top vim alt+up no-op")
	}
}

func TestAltDownAtLastTextareaQueryBlockIsNoOp(t *testing.T) {
	m := New(testConfig())
	m = m.SetSize(80, 8)
	m = m.SetTabs([]TabState{{Path: "query1.sql", Content: "select 1;\n\nselect 22;"}})
	setTextareaCursor(&m.tabs[0].ta, 2, 4)
	m.popup.visible = true

	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown, Alt: true})
	m = nm

	if gotLine, gotCol := m.tabs[0].ta.Line(), m.tabs[0].ta.LineInfo().CharOffset; gotLine != 2 || gotCol != 4 {
		t.Fatalf("textarea cursor after bottom alt+down = (%d,%d), want (2,4)", gotLine, gotCol)
	}
	if m.popup.visible {
		t.Fatalf("popup should close on bottom alt+down no-op")
	}
}

func TestAltDownAtLastVimQueryBlockIsNoOp(t *testing.T) {
	cfg := testConfig()
	cfg.Editor.VimMode = true
	m := New(cfg)
	m = m.SetSize(80, 4)
	m = m.SetTabs([]TabState{{Path: "query1.sql", Content: "select 1;\n\nselect 22;"}})
	m.tabs[0].vim.Buf.SetCursor(2, 4)
	m.tabs[0].vim.Mode = vim.ModeVisual
	m.popup.visible = true

	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown, Alt: true})
	m = nm

	if gotRow, gotCol := m.tabs[0].vim.Buf.CursorRow(), m.tabs[0].vim.Buf.CursorCol(); gotRow != 2 || gotCol != 4 {
		t.Fatalf("vim cursor after bottom alt+down = (%d,%d), want (2,4)", gotRow, gotCol)
	}
	if got := m.tabs[0].vim.Mode; got != vim.ModeVisual {
		t.Fatalf("vim mode after bottom alt+down no-op = %v, want %v", got, vim.ModeVisual)
	}
	if m.popup.visible {
		t.Fatalf("popup should close on bottom vim alt+down no-op")
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

func TestRenderContentHighlightsMissingSchemaTableAndQualifiedColumn(t *testing.T) {
	m := New(testConfig()).SetSchema(joinInferenceTestSchema())
	m = m.SetSize(100, 8)
	m = m.SetTabs([]TabState{{
		Path:    "query1.sql",
		Content: "select U.MissingField from tblUser U join tblGhost G on U.Id = G.UserId",
	}})

	rendered := m.renderContent()
	if !strings.Contains(rendered, missingSchemaRefStyle.Render("MissingField")) {
		t.Fatalf("renderContent() should mark missing qualified column red; got %q", rendered)
	}
	if !strings.Contains(rendered, missingSchemaRefStyle.Render("tblGhost")) {
		t.Fatalf("renderContent() should mark missing table red; got %q", rendered)
	}
}

func TestRenderVimContentHighlightsMissingSchemaTableAndQualifiedColumn(t *testing.T) {
	cfg := testConfig()
	cfg.Editor.VimMode = true
	m := New(cfg).SetSchema(joinInferenceTestSchema())
	m = m.SetSize(100, 8)
	m = m.SetTabs([]TabState{{
		Path:    "query1.sql",
		Content: "select U.MissingField from tblUser U join tblGhost G on U.Id = G.UserId",
	}})

	rendered := m.renderVimContent()
	if !strings.Contains(rendered, missingSchemaRefStyle.Render("MissingField")) {
		t.Fatalf("renderVimContent() should mark missing qualified column red; got %q", rendered)
	}
	if !strings.Contains(rendered, missingSchemaRefStyle.Render("tblGhost")) {
		t.Fatalf("renderVimContent() should mark missing table red; got %q", rendered)
	}
}

func TestInvalidSchemaHighlightSpansInBlockMarksOnlyMissingResolvedNames(t *testing.T) {
	block := "select U.MissingField from tblUser U join tblGhost G on U.Id = G.UserId"
	spans := invalidSchemaHighlightSpansInBlock(block, joinInferenceTestSchema())
	if len(spans) != 2 {
		t.Fatalf("invalidSchemaHighlightSpansInBlock() len = %d, want 2 (%#v)", len(spans), spans)
	}
	var got []string
	for _, span := range spans {
		got = append(got, string([]rune(block)[span.start:span.end]))
	}
	sort.Strings(got)
	want := []string{"MissingField", "tblGhost"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("invalidSchemaHighlightSpansInBlock() = %#v, want %#v", got, want)
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

func TestDetectBlockRangeAndHighlightUnionAll(t *testing.T) {
	// UNION ALL query with blank lines around the operator.
	// All cursor positions inside the query should resolve to the full range (0..8),
	// and isActiveBlockLine should mark every non-blank line as active.
	text := "SELECT a, b\nFROM t1\nWHERE a = 1\n\nUNION ALL\n\nSELECT a, b\nFROM t2\nWHERE a = 2"
	lines := strings.Split(text, "\n")
	// line 0: SELECT a, b
	// line 1: FROM t1
	// line 2: WHERE a = 1
	// line 3: (blank)
	// line 4: UNION ALL
	// line 5: (blank)
	// line 6: SELECT a, b
	// line 7: FROM t2
	// line 8: WHERE a = 2

	nonBlankLines := []int{0, 1, 2, 4, 6, 7, 8}
	blankLines := []int{3, 5}

	for _, cursorLine := range nonBlankLines {
		start, end, ok := detectBlockRange(text, cursorLine)
		if !ok || start != 0 || end != 8 {
			t.Errorf("cursor=%d: detectBlockRange()=(%d,%d,%v), want (0,8,true)", cursorLine, start, end, ok)
			continue
		}
		// Every non-blank line in range should be highlighted.
		for _, row := range nonBlankLines {
			if !isActiveBlockLine(lines, row, start, end, ok, cursorLine) {
				t.Errorf("cursor=%d row=%d: expected active block line", cursorLine, row)
			}
		}
		// Blank separator lines within the range should NOT be highlighted.
		for _, row := range blankLines {
			if isActiveBlockLine(lines, row, start, end, ok, cursorLine) {
				t.Errorf("cursor=%d row=%d: blank line should not be active", cursorLine, row)
			}
		}
	}
}

func TestDetectBlockRangeUnionIntersectExcept(t *testing.T) {
	// All three set operators should bridge blank-line gaps.
	operators := []string{"UNION", "UNION ALL", "INTERSECT", "INTERSECT ALL", "EXCEPT", "EXCEPT ALL"}
	for _, op := range operators {
		text := "SELECT 1\n\n" + op + "\n\nSELECT 2"
		// line 0: SELECT 1, line 1: blank, line 2: op, line 3: blank, line 4: SELECT 2
		for _, cursorLine := range []int{0, 4} {
			start, end, ok := detectBlockRange(text, cursorLine)
			if !ok || start != 0 || end != 4 {
				t.Errorf("op=%q cursor=%d: detectBlockRange()=(%d,%d,%v), want (0,4,true)", op, cursorLine, start, end, ok)
			}
		}
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

func TestEditorMouseDragSelectsText(t *testing.T) {
	m := New(testConfig())
	m = m.SetSize(80, 8)
	m = m.SetTabs([]TabState{{Path: "query1.sql", Content: "alpha bravo"}})
	gutterW := textareaGutterWidth(m.tabs[0].ta.View(), 1)

	m = m.Mouse(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}, gutterW, 1)
	m = m.Mouse(tea.MouseMsg{Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft}, gutterW+5, 1)
	m = m.Mouse(tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft}, gutterW+5, 1)

	start, end, ok := m.textareaSelectionRange()
	if !ok {
		t.Fatalf("expected active mouse selection after drag")
	}
	if start != (textPos{Line: 0, Col: 0}) || end != (textPos{Line: 0, Col: 5}) {
		t.Fatalf("mouse selection = (%+v,%+v), want [(0,0),(0,5)]", start, end)
	}
	if m.MouseSelecting() {
		t.Fatalf("mouseSelecting should be false after release")
	}
}

func TestEditorMouseClickDoesNotLeaveSelection(t *testing.T) {
	m := New(testConfig())
	m = m.SetSize(80, 8)
	m = m.SetTabs([]TabState{{Path: "query1.sql", Content: "alpha bravo"}})
	gutterW := textareaGutterWidth(m.tabs[0].ta.View(), 1)

	m = m.Mouse(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}, gutterW+3, 1)
	m = m.Mouse(tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft}, gutterW+3, 1)

	if _, _, ok := m.textareaSelectionRange(); ok {
		t.Fatalf("plain mouse click should not leave an active selection")
	}
	if got := m.tabs[0].ta.LineInfo().CharOffset; got != 3 {
		t.Fatalf("clicked cursor col = %d, want 3", got)
	}
}

func TestEditorMouseDragSelectsTextInVimMode(t *testing.T) {
	cfg := testConfig()
	cfg.Editor.VimMode = true
	m := New(cfg)
	m = m.SetSize(80, 8)
	m = m.SetTabs([]TabState{{Path: "query1.sql", Content: "alpha bravo"}})
	gutterW := lineNumberGutterWidth(m.tabs[0].vim.Buf.LineCount())

	m = m.Mouse(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}, gutterW, 1)
	m = m.Mouse(tea.MouseMsg{Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft}, gutterW+5, 1)
	m = m.Mouse(tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft}, gutterW+5, 1)

	if got := m.tabs[0].vim.Mode; got != vim.ModeVisual {
		t.Fatalf("vim mode after mouse drag = %v, want %v", got, vim.ModeVisual)
	}
	start, end := m.tabs[0].vim.SelectionRange()
	if start != (vim.Pos{Row: 0, Col: 0}) || end != (vim.Pos{Row: 0, Col: 5}) {
		t.Fatalf("vim selection after drag = (%+v,%+v), want [(0,0),(0,5)]", start, end)
	}
	if m.MouseSelecting() {
		t.Fatalf("mouseSelecting should be false after vim release")
	}
}

func TestEditorMouseClickDoesNotLeaveVimSelection(t *testing.T) {
	cfg := testConfig()
	cfg.Editor.VimMode = true
	m := New(cfg)
	m = m.SetSize(80, 8)
	m = m.SetTabs([]TabState{{Path: "query1.sql", Content: "alpha bravo"}})
	gutterW := lineNumberGutterWidth(m.tabs[0].vim.Buf.LineCount())

	m = m.Mouse(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}, gutterW+3, 1)
	m = m.Mouse(tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft}, gutterW+3, 1)

	if got := m.tabs[0].vim.Mode; got != vim.ModeNormal {
		t.Fatalf("vim mode after plain click = %v, want %v", got, vim.ModeNormal)
	}
	if gotRow, gotCol := m.tabs[0].vim.Buf.CursorRow(), m.tabs[0].vim.Buf.CursorCol(); gotRow != 0 || gotCol != 3 {
		t.Fatalf("vim cursor after plain click = (%d,%d), want (0,3)", gotRow, gotCol)
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

func TestCommentSQLLineUncommentsIndentedLine(t *testing.T) {
	got, changedAt, delta := commentSQLLine("    -- select 1")
	if got != "    select 1" {
		t.Fatalf("commentSQLLine() = %q, want indented SQL uncomment", got)
	}
	if changedAt != 4 || delta != -3 {
		t.Fatalf("commentSQLLine() metadata = (%d,%d), want (4,-3)", changedAt, delta)
	}
}

func TestIsCommentShortcut(t *testing.T) {
	m := New(testConfig())
	if !m.isCommentShortcut(tea.KeyMsg{Type: tea.KeyCtrlBackslash}) {
		t.Fatalf("expected KeyCtrlBackslash to be recognized as a comment shortcut")
	}
	if !m.isCommentShortcut(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{28}}) {
		t.Fatalf("expected raw file-separator rune to be recognized as a comment shortcut")
	}
	if m.isCommentShortcut(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'\\'}}) {
		t.Fatalf("plain backslash should not be recognized as a comment shortcut")
	}
}

func TestLegacyCtrlSlashConfigStillMatchesCtrlUnderscore(t *testing.T) {
	cfg := testConfig()
	cfg.Keys.ToggleComment = "ctrl+/"
	m := New(cfg)
	if !m.isCommentShortcut(tea.KeyMsg{Type: tea.KeyCtrlUnderscore}) {
		t.Fatalf("expected legacy ctrl+/ config to still match ctrl+_")
	}
}

func TestUpdateCtrlBackslashCommentsCurrentLine(t *testing.T) {
	m := New(testConfig())
	m = m.SetSize(80, 8)
	m = m.SetTabs([]TabState{{Path: "query1.sql", Content: "select 1\nselect 2"}})
	setTextareaCursor(&m.tabs[0].ta, 0, 0)

	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlBackslash})
	m = nm
	if got := m.tabs[0].ta.Value(); got != "-- select 1\nselect 2" {
		t.Fatalf("value after KeyCtrlBackslash = %q, want first line commented", got)
	}
}

func TestUpdateCtrlBackslashUncommentsCurrentLine(t *testing.T) {
	m := New(testConfig())
	m = m.SetSize(80, 8)
	m = m.SetTabs([]TabState{{Path: "query1.sql", Content: "-- select 1\nselect 2"}})
	setTextareaCursor(&m.tabs[0].ta, 0, 0)

	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlBackslash})
	m = nm
	if got := m.tabs[0].ta.Value(); got != "select 1\nselect 2" {
		t.Fatalf("value after KeyCtrlBackslash toggle = %q, want first line uncommented", got)
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

func TestCommentCurrentLineTextareaUncommentsLastLineAndAdjustsCursor(t *testing.T) {
	m := New(testConfig())
	m = m.SetSize(80, 8)
	m = m.SetTabs([]TabState{{Path: "query1.sql", Content: "-- select 1"}})
	setTextareaCursor(&m.tabs[0].ta, 0, 3)

	m, _ = m.commentCurrentLineTextarea()
	if got := m.tabs[0].ta.Value(); got != "select 1" {
		t.Fatalf("value after commentCurrentLineTextarea() toggle = %q, want uncommented line", got)
	}
	if got := m.tabs[0].ta.LineInfo().CharOffset; got != 0 {
		t.Fatalf("cursor col after uncomment toggle = %d, want 0", got)
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

func TestCommentCurrentLineVimUncommentsLineAndMovesDown(t *testing.T) {
	cfg := testConfig()
	cfg.Editor.VimMode = true
	m := New(cfg)
	m = m.SetSize(80, 8)
	m = m.SetTabs([]TabState{{Path: "query1.sql", Content: "-- select 1\nselect 2"}})
	m.tabs[0].vim.Buf.SetCursor(0, 0)

	m, _ = m.commentCurrentLineVim()
	if got := m.tabs[0].vim.Buf.Value(); got != "select 1\nselect 2" {
		t.Fatalf("vim buffer after commentCurrentLineVim() toggle = %q, want first line uncommented", got)
	}
	if got := m.tabs[0].vim.Buf.CursorRow(); got != 1 {
		t.Fatalf("vim cursor row after uncomment toggle = %d, want 1", got)
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
		ActiveQueryGutter: "#a64d73",
		Cursor:            "#a6e3a1",
		InsertCursor:      "#a6e3a1",
	}}
}
