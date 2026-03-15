package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sqltui/sql/internal/config"
	"github.com/sqltui/sql/internal/testdb"
	"github.com/sqltui/sql/internal/ui/results"
	"github.com/sqltui/sql/internal/ui/schema"
	"github.com/sqltui/sql/internal/workspace"
)

func newTestWorkspace(t *testing.T) *workspace.Workspace {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "ws")
	return workspace.New(dir)
}

// --- Result diff / pin (#11) ---

func TestResultPinAndDiff(t *testing.T) {
	_, sess := testdb.SQLiteDB(t)

	ctx := context.Background()
	qrs, err := sess.Execute(ctx, "SELECT ProductID, Name, Price FROM Products ORDER BY ProductID")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	rm := results.New()
	rm = rm.SetSize(120, 20)
	rm = rm.SetResults(qrs)
	rm = rm.Focus()

	// Initially not pinned.
	if rm.Pinned() {
		t.Fatal("should not be pinned initially")
	}
	if rm.DiffMode() {
		t.Fatal("should not be in diff mode initially")
	}

	// Press 'p' to pin.
	rm, _ = rm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	if !rm.Pinned() {
		t.Fatal("should be pinned after 'p'")
	}

	// Set new results with same columns — diff mode should activate.
	qrs2, err := sess.Execute(ctx, "SELECT ProductID, Name, Price FROM Products ORDER BY ProductID")
	if err != nil {
		t.Fatalf("Execute 2: %v", err)
	}
	rm = rm.SetResults(qrs2)
	if !rm.DiffMode() {
		t.Fatal("diff mode should be active after SetResults with same columns while pinned")
	}

	// Press 'p' again to unpin.
	rm, _ = rm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	if rm.Pinned() {
		t.Fatal("should not be pinned after second 'p'")
	}
	if rm.DiffMode() {
		t.Fatal("diff mode should be off after unpinning")
	}
}

func TestResultPinDiffColumnMismatch(t *testing.T) {
	_, sess := testdb.SQLiteDB(t)
	ctx := context.Background()

	qrs, _ := sess.Execute(ctx, "SELECT ProductID, Name FROM Products")
	rm := results.New()
	rm = rm.SetSize(120, 20)
	rm = rm.SetResults(qrs).Focus()
	rm, _ = rm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})

	// New results with different columns — diff should NOT activate.
	qrs2, _ := sess.Execute(ctx, "SELECT OrderID, CustomerName FROM Orders")
	rm = rm.SetResults(qrs2)
	if rm.DiffMode() {
		t.Fatal("diff mode should not activate when columns differ")
	}
}

// --- Stacked filters (#7) ---

func TestStackedFilters(t *testing.T) {
	_, sess := testdb.SQLiteDB(t)
	ctx := context.Background()

	qrs, err := sess.Execute(ctx, "SELECT ProductID, Name, Price FROM Products")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	rm := results.New()
	rm = rm.SetSize(120, 20)
	rm = rm.SetResults(qrs).Focus()

	// Open filter on column 0.
	rm = rm.OpenFilter()
	if !rm.FilterOpen() {
		t.Fatal("filter should be open")
	}

	// Type a pattern and confirm with Enter.
	for _, r := range "Widget" {
		rm, _ = rm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	rm, _ = rm.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Filter should be confirmed (not open) and meta line should show the filter.
	if rm.FilterOpen() {
		t.Fatal("filter should be closed after Enter")
	}
	view := rm.View()
	if !strings.Contains(view, "Widget") {
		t.Error("view should mention the active filter pattern")
	}

	// Press F to clear all filters.
	rm, _ = rm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'F'}})
	view = rm.View()
	if strings.Contains(view, "⊞") {
		t.Error("view should not show filter indicator after F clears all")
	}
}

// --- Row numbers (#6) ---

func TestRowNumbersToggle(t *testing.T) {
	_, sess := testdb.SQLiteDB(t)
	ctx := context.Background()
	qrs, _ := sess.Execute(ctx, "SELECT * FROM Products")
	rm := results.New().SetSize(120, 20).SetResults(qrs).Focus()

	viewBefore := rm.View()
	rm, _ = rm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'#'}})
	viewAfter := rm.View()
	if viewBefore == viewAfter {
		t.Error("view should change after toggling row numbers")
	}
}

// --- Row detail view (#2) ---

func TestRowDetailView(t *testing.T) {
	_, sess := testdb.SQLiteDB(t)
	ctx := context.Background()
	qrs, _ := sess.Execute(ctx, "SELECT ProductID, Name, Price FROM Products")
	rm := results.New().SetSize(120, 20).SetResults(qrs).Focus()

	if rm.RowDetailOpen() {
		t.Fatal("row detail should not be open initially")
	}
	rm, _ = rm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !rm.RowDetailOpen() {
		t.Fatal("row detail should open on Enter")
	}
	// Esc closes it.
	rm, _ = rm.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if rm.RowDetailOpen() {
		t.Fatal("row detail should close on Esc")
	}
}

// --- Row tagging (#20) ---

func TestRowTagging(t *testing.T) {
	_, sess := testdb.SQLiteDB(t)
	ctx := context.Background()
	qrs, _ := sess.Execute(ctx, "SELECT ProductID, Name FROM Products ORDER BY ProductID")
	rm := results.New().SetSize(120, 20).SetResults(qrs).Focus()

	if rm.TagCount() != 0 {
		t.Fatalf("initial tag count = %d, want 0", rm.TagCount())
	}
	// Space tags row 0 and advances cursor.
	rm, _ = rm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	if rm.TagCount() != 1 {
		t.Fatalf("tag count after Space = %d, want 1", rm.TagCount())
	}
	tagged := rm.TaggedResult()
	if tagged == nil || len(tagged.Rows) != 1 {
		t.Fatalf("TaggedResult returned %v rows, want 1", func() int {
			if tagged == nil {
				return -1
			}
			return len(tagged.Rows)
		}())
	}
}

// --- Column type in status bar (#5) ---

func TestColumnTypeInStatusBar(t *testing.T) {
	_, sess := testdb.SQLiteDB(t)
	ctx := context.Background()
	qrs, _ := sess.Execute(ctx, "SELECT ProductID, Name, Price FROM Products")

	rm := results.New().SetSize(120, 20).SetResults(qrs).Focus()
	colType := rm.CurrentColumnType()
	if colType == "" {
		t.Error("CurrentColumnType should return a non-empty type for first column")
	}
}

// --- Schema row count (#13) ---

func TestSchemaRowCountRequest(t *testing.T) {
	_, sess := testdb.SQLiteDB(t)
	ctx := context.Background()
	s, err := sess.Introspect(ctx)
	if err != nil {
		t.Fatalf("Introspect: %v", err)
	}

	sm := schema.New().SetSchema(s, "test", "sqlite").SetSize(120, 40)
	sm, _ = sm.Open("")

	// Move to list mode with down arrow.
	sm, _ = sm.Update(tea.KeyMsg{Type: tea.KeyDown})

	// Press 'r' to request row count.
	sm, cmd := sm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	_ = sm
	if cmd == nil {
		t.Fatal("'r' in schema list mode should return a command for row count")
	}
	msg := cmd()
	_, ok := msg.(schema.RowCountRequestMsg)
	if !ok {
		t.Fatalf("expected RowCountRequestMsg, got %T", msg)
	}
}

// --- Schema search / list panel (#bug1) ---

func TestSchemaBrowserPanelSwitching(t *testing.T) {
	_, sess := testdb.SQLiteDB(t)
	ctx := context.Background()
	s, err := sess.Introspect(ctx)
	if err != nil {
		t.Fatalf("Introspect: %v", err)
	}

	sm := schema.New().SetSchema(s, "test", "sqlite").SetSize(120, 40)
	sm, _ = sm.Open("")

	// Initially in search mode — typing 'r' should filter, not request row count.
	sm, cmd := sm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if cmd != nil {
		msg := cmd()
		if _, ok := msg.(schema.RowCountRequestMsg); ok {
			t.Fatal("'r' in search mode should not trigger RowCountRequestMsg")
		}
	}

	// Press Down to switch to list mode.
	sm, _ = sm.Update(tea.KeyMsg{Type: tea.KeyDown})

	// Now 'r' should trigger row count.
	sm, cmd = sm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	_ = sm
	if cmd == nil {
		t.Fatal("'r' in list mode should return a command")
	}
	msg := cmd()
	if _, ok := msg.(schema.RowCountRequestMsg); !ok {
		t.Fatalf("expected RowCountRequestMsg in list mode, got %T", msg)
	}
}

// --- Goto-line (#10) ---

func TestGotoLineInEditor(t *testing.T) {
	m := New(&config.Config{}, "")
	// Ctrl+G should open goto-line without error (no session needed).
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
	m = nm.(Model)
	if !m.editor.GotoOpen() {
		t.Error("Ctrl+G should open goto-line bar in editor")
	}
}

// --- Results fullscreen (#bug3) ---

func TestResultsFullscreen(t *testing.T) {
	m := New(&config.Config{}, "")
	m.width = 200
	m.height = 50
	m2, _ := m.applySize()
	m = m2.(Model)

	if m.resultsFullscreen {
		t.Fatal("should not be in fullscreen initially")
	}
	// Ctrl+L toggles fullscreen.
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlL})
	m = nm.(Model)
	if !m.resultsFullscreen {
		t.Fatal("should be in fullscreen after Ctrl+L")
	}
	// Alt+1 should clear fullscreen.
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1"), Alt: true})
	m = nm.(Model)
	if m.resultsFullscreen {
		t.Fatal("fullscreen should be cleared after Alt+1")
	}
}

// --- Named snippets (#9) ---

func TestSnippetSaveAndList(t *testing.T) {
	_, sess := testdb.SQLiteDB(t)
	t.TempDir() // ensure temp env exists
	ws := newTestWorkspace(t)

	m := New(&config.Config{}, "")
	m.session = sess
	m.ws = ws

	// Trigger save via command (simulate state directly).
	m.snippetSaveOpen = true
	m.snippetSaveSQL = "SELECT 1"
	m.snippetSaveInput = []rune("my snippet")

	// Press Enter to confirm.
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nm.(Model)

	if m.snippetSaveOpen {
		t.Fatal("snippet save should be closed after Enter")
	}

	// List snippets.
	snippets, err := ws.ListSnippets()
	if err != nil {
		t.Fatalf("ListSnippets: %v", err)
	}
	if len(snippets) != 1 {
		t.Fatalf("expected 1 snippet, got %d", len(snippets))
	}
	if snippets[0].Name != "my snippet" || snippets[0].SQL != "SELECT 1" {
		t.Errorf("unexpected snippet: %+v", snippets[0])
	}
}

// --- Cell edit → UPDATE (#14) ---

func TestCellEditGeneratesUPDATE(t *testing.T) {
	_, sess := testdb.SQLiteDB(t)
	ctx := context.Background()
	qrs, _ := sess.Execute(ctx, "SELECT ProductID, Name FROM Products ORDER BY ProductID LIMIT 1")

	rm := results.New().SetSize(120, 20).SetResults(qrs).Focus()

	// Move cursor to column 1 (Name).
	rm, _ = rm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})

	ctx2, ok := rm.CurrentCellContext()
	if !ok {
		t.Fatal("no cell context available")
	}
	if ctx2.ColName != "Name" {
		t.Fatalf("expected cursor on Name, got %s", ctx2.ColName)
	}

	sql := generateUpdateSQL(ctx2, "Widget Pro", "SELECT ProductID, Name FROM Products")
	if sql == "" {
		t.Fatal("generateUpdateSQL returned empty string")
	}
	if !strings.Contains(sql, "UPDATE") || !strings.Contains(sql, "Widget Pro") {
		t.Errorf("unexpected UPDATE SQL: %s", sql)
	}
	if !strings.Contains(sql, "WHERE") {
		t.Error("UPDATE SQL should contain WHERE clause")
	}
}

// --- Find in results (/) ---

func TestFindBasicMatching(t *testing.T) {
	_, sess := testdb.SQLiteDB(t)
	ctx := context.Background()
	qrs, _ := sess.Execute(ctx, "SELECT ProductID, Name, Price FROM Products ORDER BY ProductID")

	rm := results.New().SetSize(120, 20).SetResults(qrs).Focus()

	// Open find bar with /.
	rm, _ = rm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	if !rm.FindOpen() {
		t.Fatal("find bar should be open after /")
	}

	// Type "et" — matches "Widget" (row 0) and "Gadget" (row 1).
	for _, r := range "et" {
		rm, _ = rm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}

	matches := rm.FindMatches()
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches for 'et', got %d", len(matches))
	}
	// Both matches should be in the Name column (col 1).
	for _, m := range matches {
		if m[1] != 1 {
			t.Errorf("expected match in col 1 (Name), got col %d", m[1])
		}
	}

	view := rm.View()
	if !strings.Contains(view, "2/2") && !strings.Contains(view, "1/2") {
		t.Errorf("view should show match count; got: %s", view)
	}
}

func TestFindNavigation(t *testing.T) {
	_, sess := testdb.SQLiteDB(t)
	ctx := context.Background()
	// "9.99" matches Widget (row 0, Price col 2) and Doohickey (row 2, Price col 2).
	qrs, _ := sess.Execute(ctx, "SELECT ProductID, Name, Price FROM Products ORDER BY ProductID")

	rm := results.New().SetSize(120, 20).SetResults(qrs).Focus()

	rm, _ = rm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	for _, r := range "9.99" {
		rm, _ = rm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	// Press Enter to close bar.
	rm, _ = rm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if rm.FindOpen() {
		t.Fatal("find bar should be closed after Enter")
	}
	// Find should remain active.
	if len(rm.FindMatches()) == 0 {
		t.Fatal("find matches should persist after Enter closes the bar")
	}

	cur0 := rm.FindCurrent()

	// Press n — advance to next match.
	rm, _ = rm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	cur1 := rm.FindCurrent()
	if cur1 == cur0 && len(rm.FindMatches()) > 1 {
		t.Error("n should advance findCurrent")
	}

	// Press N — go back.
	rm, _ = rm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'N'}})
	cur2 := rm.FindCurrent()
	if cur2 != cur0 {
		t.Errorf("N should return to original match: got %d want %d", cur2, cur0)
	}
}

func TestFindWraparound(t *testing.T) {
	_, sess := testdb.SQLiteDB(t)
	ctx := context.Background()
	qrs, _ := sess.Execute(ctx, "SELECT ProductID, Name FROM Products ORDER BY ProductID")

	rm := results.New().SetSize(120, 20).SetResults(qrs).Focus()

	// "et" matches "Widget" (row 0, Name col) and "Gadget" (row 1, Name col).
	rm, _ = rm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	for _, r := range "et" {
		rm, _ = rm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	rm, _ = rm.Update(tea.KeyMsg{Type: tea.KeyEnter})

	total := len(rm.FindMatches())
	if total < 2 {
		t.Fatalf("expected at least 2 matches for 'it', got %d", total)
	}

	// Navigate to the last match.
	rm, _ = rm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'N'}})
	last := rm.FindCurrent()
	if last != total-1 {
		t.Fatalf("N from first match should wrap to last, got %d want %d", last, total-1)
	}

	// n from last should wrap to first.
	rm, _ = rm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if rm.FindCurrent() != 0 {
		t.Errorf("n from last match should wrap to 0, got %d", rm.FindCurrent())
	}
}

func TestFindEscClearsState(t *testing.T) {
	_, sess := testdb.SQLiteDB(t)
	ctx := context.Background()
	qrs, _ := sess.Execute(ctx, "SELECT Name FROM Products")

	rm := results.New().SetSize(120, 20).SetResults(qrs).Focus()

	rm, _ = rm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	for _, r := range "Widget" {
		rm, _ = rm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if len(rm.FindMatches()) == 0 {
		t.Fatal("should have matches before Esc")
	}

	rm, _ = rm.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if rm.FindOpen() {
		t.Error("find bar should be closed after Esc")
	}
	if len(rm.FindMatches()) != 0 {
		t.Error("find matches should be cleared after Esc")
	}
	view := rm.View()
	if strings.Contains(view, "🔍") {
		t.Error("view should not show find indicator after Esc")
	}
}

func TestFindNoMatches(t *testing.T) {
	_, sess := testdb.SQLiteDB(t)
	ctx := context.Background()
	qrs, _ := sess.Execute(ctx, "SELECT Name FROM Products")

	rm := results.New().SetSize(120, 20).SetResults(qrs).Focus()

	rm, _ = rm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	for _, r := range "zzznomatch" {
		rm, _ = rm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}

	if len(rm.FindMatches()) != 0 {
		t.Errorf("expected 0 matches, got %d", len(rm.FindMatches()))
	}

	// n/N with no matches should not panic.
	rm, _ = rm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	rm, _ = rm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	rm, _ = rm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'N'}})
}

func TestFindCaseInsensitive(t *testing.T) {
	_, sess := testdb.SQLiteDB(t)
	ctx := context.Background()
	qrs, _ := sess.Execute(ctx, "SELECT Name FROM Products ORDER BY ProductID")

	rm := results.New().SetSize(120, 20).SetResults(qrs).Focus()

	rm, _ = rm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	// "WIDGET" upper-case should match "Widget".
	for _, r := range "WIDGET" {
		rm, _ = rm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}

	if len(rm.FindMatches()) == 0 {
		t.Error("find should be case-insensitive: WIDGET should match Widget")
	}
}

func TestFindStateResetOnSetResults(t *testing.T) {
	_, sess := testdb.SQLiteDB(t)
	ctx := context.Background()
	qrs, _ := sess.Execute(ctx, "SELECT Name FROM Products")

	rm := results.New().SetSize(120, 20).SetResults(qrs).Focus()

	// Open find and type a pattern.
	rm, _ = rm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	for _, r := range "Widget" {
		rm, _ = rm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if len(rm.FindMatches()) == 0 {
		t.Fatal("should have matches before SetResults")
	}

	// Load new results — find state must be fully cleared.
	qrs2, _ := sess.Execute(ctx, "SELECT OrderID, CustomerID FROM Orders")
	rm = rm.SetResults(qrs2)

	if rm.FindOpen() {
		t.Error("findOpen should be false after SetResults")
	}
	if len(rm.FindMatches()) != 0 {
		t.Error("findMatches should be nil after SetResults")
	}
	if rm.FindCurrent() != -1 {
		t.Errorf("findCurrent should be -1 after SetResults, got %d", rm.FindCurrent())
	}

	// Pressing n should be a no-op (not panic, not use stale indices).
	rm, _ = rm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
}

func TestFindInputEditing(t *testing.T) {
	_, sess := testdb.SQLiteDB(t)
	ctx := context.Background()
	qrs, _ := sess.Execute(ctx, "SELECT Name FROM Products")

	rm := results.New().SetSize(120, 20).SetResults(qrs).Focus()
	rm, _ = rm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})

	// Type "Widgxt".
	for _, r := range "Widgxt" {
		rm, _ = rm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	// Backspace twice to remove "xt".
	rm, _ = rm.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	rm, _ = rm.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	// Type "et" to get "Widget".
	for _, r := range "et" {
		rm, _ = rm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}

	if len(rm.FindMatches()) == 0 {
		t.Error("after editing to 'Widget', should have matches")
	}

	// Ctrl+U should clear.
	rm, _ = rm.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	if len(rm.FindMatches()) != 0 {
		t.Error("Ctrl+U should clear the find input and matches")
	}
}

func TestFindHighlightInView(t *testing.T) {
	_, sess := testdb.SQLiteDB(t)
	ctx := context.Background()
	qrs, _ := sess.Execute(ctx, "SELECT Name FROM Products ORDER BY ProductID")

	rm := results.New().SetSize(120, 20).SetResults(qrs).Focus()

	viewBefore := rm.View()

	// Activate find for "Widget".
	rm, _ = rm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	for _, r := range "Widget" {
		rm, _ = rm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}

	viewAfter := rm.View()
	if viewBefore == viewAfter {
		t.Error("view should change when find is active")
	}
	// Find indicator should appear.
	if !strings.Contains(viewAfter, "🔍") {
		t.Error("view should contain find indicator 🔍")
	}
}

// --- MSSQL: TOP N generation, FK introspection ---

func TestMSSQLTopNGeneration(t *testing.T) {
	_, sess, schemaName := testdb.MSSQLDB(t)
	ctx := context.Background()
	s, err := sess.Introspect(ctx)
	if err != nil {
		t.Fatalf("Introspect: %v", err)
	}

	sm := schema.New().SetSchema(s, "test", "mssql").SetSize(160, 40)
	sm, _ = sm.Open("")

	// Navigate to list mode.
	sm, _ = sm.Update(tea.KeyMsg{Type: tea.KeyDown})

	// Press Enter to generate SELECT.
	sm, cmd := sm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	_ = sm
	if cmd == nil {
		t.Fatal("Enter should return a command with generated SQL")
	}
	msg := cmd()
	sel, ok := msg.(schema.TableSelectedMsg)
	if !ok {
		t.Fatalf("expected TableSelectedMsg, got %T", msg)
	}
	if !strings.Contains(sel.SQL, "TOP") {
		t.Errorf("MSSQL SELECT should use TOP N, got: %s", sel.SQL)
	}
	_ = schemaName
	_ = fmt.Sprintf // avoid unused import
}
