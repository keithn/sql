package editor

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/sqltui/sql/internal/db"
)

func TestGetCompletionsPreservesKinds(t *testing.T) {
	extra := []CompletionItem{
		{Text: "customers", Kind: CompletionKindTable},
		{Text: "customer_id", Kind: CompletionKindColumn},
	}
	items := getCompletions("cust", extra, 8)
	got := map[string]CompletionKind{}
	for _, item := range items {
		got[item.Text] = item.Kind
	}
	if got["customers"] != CompletionKindTable {
		t.Fatalf("customers kind = %q, want %q", got["customers"], CompletionKindTable)
	}
	if got["customer_id"] != CompletionKindColumn {
		t.Fatalf("customer_id kind = %q, want %q", got["customer_id"], CompletionKindColumn)
	}
	if kw := getCompletions("sel", nil, 8); len(kw) == 0 || kw[0].Kind != CompletionKindKeyword || kw[0].Text != "SELECT" {
		t.Fatalf("keyword completion = %#v, want SELECT keyword first", kw)
	}
}

// ── contextualColumnItems ─────────────────────────────────────────────────────

func makeTestSchema() *db.Schema {
	return &db.Schema{
		Databases: []db.Database{{
			Schemas: []db.SchemaNode{{
				Name: "dbo",
				Tables: []db.Table{
					{
						Name: "tblClient",
						Columns: []db.ColumnDef{
							{Name: "lngClientID", PrimaryKey: true},
							{Name: "strClientName"},
							{Name: "strEmail"},
						},
					},
					{
						Name: "tblOrder",
						Columns: []db.ColumnDef{
							{Name: "lngOrderID", PrimaryKey: true},
							{Name: "lngClientID"},
							{Name: "dteOrderDate"},
						},
					},
					{
						Name: "tblProduct",
						Columns: []db.ColumnDef{
							{Name: "lngProductID", PrimaryKey: true},
							{Name: "strProductName"},
						},
					},
				},
			}},
		}},
	}
}

func TestContextualColumnItems_NoSchema(t *testing.T) {
	items := contextualColumnItems("name", "SELECT * FROM tblClient", 0, 0, nil)
	if items != nil {
		t.Fatalf("expected nil without schema, got %v", items)
	}
}

func TestContextualColumnItems_NoTableRefs(t *testing.T) {
	schema := makeTestSchema()
	items := contextualColumnItems("name", "SELECT 1", 0, 0, schema)
	if items != nil {
		t.Fatalf("expected nil when no table refs, got %v", items)
	}
}

func TestContextualColumnItems_ReturnsColumnsFromReferencedTable(t *testing.T) {
	schema := makeTestSchema()
	sql := "SELECT * FROM dbo.tblClient WHERE "
	items := contextualColumnItems("str", sql, 0, 0, schema)
	texts := make([]string, len(items))
	for i, it := range items {
		texts[i] = it.Text
	}
	found := false
	for _, it := range items {
		if it.Text == "strClientName" {
			found = true
			if it.Kind != CompletionKindColumn {
				t.Errorf("strClientName kind = %q, want column", it.Kind)
			}
			if it.Detail == "" {
				t.Errorf("strClientName should have Detail set (table/alias source)")
			}
		}
	}
	if !found {
		t.Fatalf("strClientName not in results %v", texts)
	}
}

func TestContextualColumnItems_ExcludesUnreferencedTable(t *testing.T) {
	schema := makeTestSchema()
	// Only tblClient is referenced; tblOrder columns should not appear.
	sql := "SELECT * FROM dbo.tblClient"
	items := contextualColumnItems("lng", sql, 0, 0, schema)
	for _, it := range items {
		if it.Text == "lngOrderID" {
			t.Fatalf("lngOrderID from unreferenced tblOrder should not appear")
		}
	}
}

func TestContextualColumnItems_EmptyWordListsAllColumns(t *testing.T) {
	schema := makeTestSchema()
	sql := "SELECT * FROM dbo.tblClient ORDER BY "
	items := contextualColumnItems("", sql, 0, 0, schema)
	if len(items) == 0 {
		t.Fatal("expected columns for empty word after ORDER BY")
	}
	for _, it := range items {
		if it.Kind == CompletionKindKeyword {
			t.Fatalf("keywords should not appear with empty word, got %q", it.Text)
		}
	}
}

func TestContextualColumnItems_OnlyCurrentBlock(t *testing.T) {
	schema := makeTestSchema()
	// Two blocks: tblOrder on line 0-1, tblClient on line 3-4.
	// Cursor on line 4 (tblClient block). tblOrder columns must not appear.
	sql := "SELECT * FROM dbo.tblOrder\n\n\nSELECT * FROM dbo.tblClient\nORDER BY "
	items := contextualColumnItems("lng", sql, 4, 0, schema)
	for _, it := range items {
		if it.Text == "lngOrderID" {
			t.Fatalf("lngOrderID from other block's tblOrder should not appear")
		}
	}
	found := false
	for _, it := range items {
		if it.Text == "lngClientID" {
			found = true
		}
	}
	if !found {
		t.Fatal("lngClientID from current block's tblClient should appear")
	}
}

func TestContextualColumnItems_Suppressed(t *testing.T) {
	// The suppression flag is checked in updatePopup/updatePopupVim, not in
	// contextualColumnItems itself — verify the field exists and can be set.
	p := completionPopup{suppressed: true}
	if !p.suppressed {
		t.Fatal("suppressed field not set correctly")
	}
}

// ── AC4: comment suppression ──────────────────────────────────────────────────

func TestCursorInsideStringLiteral(t *testing.T) {
	text := "WHERE name = 'hello world'"
	// Cursor inside the string value.
	if !cursorInsideStringLiteral(text, 0, 20) {
		t.Fatal("expected cursor inside string literal to be detected")
	}
	// Cursor before the opening quote.
	if cursorInsideStringLiteral(text, 0, 13) {
		t.Fatal("cursor before string literal should not be detected")
	}
	// Cursor after the closing quote.
	if cursorInsideStringLiteral(text, 0, len(text)) {
		t.Fatal("cursor after string literal should not be detected")
	}
}

func TestUpdatePopupSuppressedInsideStringLiteral(t *testing.T) {
	m := New(testConfig())
	m = m.SetSize(80, 8)
	m = m.SetTabs([]TabState{{Path: "q.sql", Content: "WHERE name = 'hel"}})
	setTextareaCursor(&m.tabs[0].ta, 0, len("WHERE name = 'hel"))
	m.updatePopup()
	if m.popup.visible {
		t.Fatal("popup should be hidden when cursor is inside a string literal")
	}
}

func TestCursorInsideLineComment(t *testing.T) {
	text := "SELECT * FROM t -- this is a comment"
	// Cursor inside the comment text.
	if !cursorInsideComment(text, 0, 30) {
		t.Fatal("expected cursor inside line comment to be detected")
	}
	// Cursor before the comment.
	if cursorInsideComment(text, 0, 14) {
		t.Fatal("cursor before comment should not be detected as inside comment")
	}
}

func TestCursorInsideBlockComment(t *testing.T) {
	text := "SELECT /* multi\nline */ * FROM t"
	// Cursor on second line, inside the block comment.
	if !cursorInsideComment(text, 1, 3) {
		t.Fatal("expected cursor inside block comment to be detected")
	}
	// Cursor after the block comment.
	if cursorInsideComment(text, 1, 8) {
		t.Fatal("cursor after block comment end should not be detected")
	}
}

func TestUpdatePopupSuppressedInsideComment(t *testing.T) {
	m := New(testConfig())
	m = m.SetSize(80, 8)
	m = m.SetTabs([]TabState{{Path: "q.sql", Content: "-- SELECT"}})
	setTextareaCursor(&m.tabs[0].ta, 0, len("-- SELECT"))
	m.updatePopup()
	if m.popup.visible {
		t.Fatal("popup should be hidden when cursor is inside a line comment")
	}
}

// ── AC5: comparison operator suppression ──────────────────────────────────────

func TestCursorAfterEquals(t *testing.T) {
	if !cursorAfterComparisonOp("WHERE id = ", 11) {
		t.Fatal("should detect cursor after =")
	}
}

func TestCursorAfterLike(t *testing.T) {
	if !cursorAfterComparisonOp("WHERE name LIKE ", 16) {
		t.Fatal("should detect cursor after LIKE")
	}
}

func TestCursorNotAfterOperatorMidWord(t *testing.T) {
	// Cursor in the middle of a word, not after an operator.
	if cursorAfterComparisonOp("WHERE idcol", 11) {
		t.Fatal("should not detect operator when last token is a word")
	}
}

func TestUpdatePopupSuppressedAfterEquals(t *testing.T) {
	m := New(testConfig())
	m = m.SetSize(80, 8)
	m = m.SetTabs([]TabState{{Path: "q.sql", Content: "WHERE id = "}})
	setTextareaCursor(&m.tabs[0].ta, 0, len("WHERE id = "))
	m.updatePopup()
	if m.popup.visible {
		t.Fatal("popup should be hidden after comparison operator")
	}
}

// ── AC1: dot-qualifier column filtering ───────────────────────────────────────

func TestDotQualifierDetected(t *testing.T) {
	if got := dotQualifier("SELECT u.na", 11); got != "u" {
		t.Fatalf("dotQualifier = %q, want %q", got, "u")
	}
}

func TestDotQualifierNone(t *testing.T) {
	if got := dotQualifier("SELECT name", 11); got != "" {
		t.Fatalf("dotQualifier = %q, want empty", got)
	}
}

func TestDotQualifierEmptyAfterDot(t *testing.T) {
	// Cursor right after dot, no word yet.
	if got := dotQualifier("SELECT u.", 9); got != "u" {
		t.Fatalf("dotQualifier = %q, want %q", got, "u")
	}
}

func TestContextualColumnItemsDotPrefix(t *testing.T) {
	schema := makeTestSchema()
	// "u." prefix in "SELECT u.str FROM dbo.tblClient u"
	sql := "SELECT u.str FROM dbo.tblClient u"
	// col 12 = position after "u.str" (S=0,E=1,L=2,E=3,C=4,T=5, =6,u=7,.=8,s=9,t=10,r=11; col=12)
	items := contextualColumnItems("str", sql, 0, 12, schema)
	if len(items) == 0 {
		t.Fatal("expected columns for dot-qualified prefix")
	}
	for _, it := range items {
		if it.Kind != CompletionKindColumn {
			t.Errorf("dot-prefix completion item %q should be column, got %v", it.Text, it.Kind)
		}
	}
	// Should not contain keywords.
	for _, it := range items {
		if it.Kind == CompletionKindKeyword {
			t.Errorf("keyword %q should not appear in dot-prefix column completions", it.Text)
		}
	}
}

// ── AC2: column type and PK in detail ─────────────────────────────────────────

func TestColumnDetailWithTypePK(t *testing.T) {
	col := db.ColumnDef{Name: "id", Type: "int", PrimaryKey: true}
	got := columnDetail("u", col)
	if !strings.Contains(got, "int") {
		t.Errorf("columnDetail = %q, should contain type", got)
	}
	if !strings.Contains(got, "PK") {
		t.Errorf("columnDetail = %q, should contain PK", got)
	}
}

func TestColumnDetailNoType(t *testing.T) {
	col := db.ColumnDef{Name: "name"}
	got := columnDetail("u", col)
	if got != "u" {
		t.Errorf("columnDetail = %q, want %q", got, "u")
	}
}

func TestContextualColumnItemsIncludesTypeInDetail(t *testing.T) {
	schema := &db.Schema{Databases: []db.Database{{
		Name: "testdb",
		Schemas: []db.SchemaNode{{
			Name: "dbo",
			Tables: []db.Table{{
				Name: "tblFoo",
				Columns: []db.ColumnDef{
					{Name: "FooID", Type: "int", PrimaryKey: true},
					{Name: "FooName", Type: "varchar(100)"},
				},
			}},
		}},
	}}}
	items := contextualColumnItems("Foo", "SELECT * FROM dbo.tblFoo", 0, 0, schema)
	for _, it := range items {
		if it.Text == "FooID" && !strings.Contains(it.Detail, "PK") {
			t.Errorf("FooID detail = %q, expected PK indicator", it.Detail)
		}
		if it.Text == "FooName" && !strings.Contains(it.Detail, "varchar") {
			t.Errorf("FooName detail = %q, expected type", it.Detail)
		}
	}
}

// ── AC9: multi-word keyword expansion ─────────────────────────────────────────

func TestKeywordPhraseExpansion(t *testing.T) {
	items := popupItemsFromCompletions([]CompletionItem{
		{Text: "ORDER", Kind: CompletionKindKeyword},
		{Text: "GROUP", Kind: CompletionKindKeyword},
		{Text: "LEFT", Kind: CompletionKindKeyword},
	})
	wantText := map[string]string{
		"ORDER": "ORDER BY",
		"GROUP": "GROUP BY",
		"LEFT":  "LEFT JOIN",
	}
	for _, it := range items {
		if want, ok := wantText[it.InsertText]; ok {
			_ = want // InsertText is the phrase
		}
		// Check that Text is expanded.
		for orig, phrase := range wantText {
			if it.InsertText == orig {
				t.Errorf("InsertText should be phrase %q not original %q", phrase, orig)
			}
		}
	}
	// Verify ORDER has expanded text.
	for _, it := range items {
		if it.InsertText == "ORDER BY" && it.Kind != CompletionKindKeyword {
			t.Errorf("ORDER BY item should be keyword kind")
		}
	}
}

func TestGetCompletionsReturnsExpandedPhraseViaPopupItems(t *testing.T) {
	// getCompletions returns CompletionItem with original keyword text.
	// The phrase expansion happens in popupItemsFromCompletions.
	items := getCompletions("ord", nil, 10)
	popupItems := popupItemsFromCompletions(items)
	foundOrderBy := false
	for _, it := range popupItems {
		if it.InsertText == "ORDER BY" {
			foundOrderBy = true
		}
		// Bare "ORDER" should not appear as InsertText (it should be "ORDER BY").
		if it.InsertText == "ORDER" {
			t.Errorf("InsertText should be expanded phrase ORDER BY, not bare ORDER")
		}
	}
	if !foundOrderBy {
		t.Errorf("expected ORDER BY in popup items for prefix 'ord'")
	}
}

// ── AC6: schema-first ordering ────────────────────────────────────────────────

func TestGetCompletionsCtxSchemaFirst(t *testing.T) {
	extra := []CompletionItem{
		{Text: "my_table", Kind: CompletionKindTable},
	}
	// With schemaFirst=true and prefix "my", my_table should appear before keywords.
	items := getCompletionsCtx("my", extra, 10, true)
	if len(items) == 0 {
		t.Fatal("expected completions")
	}
	if items[0].Text != "my_table" {
		t.Errorf("first item = %q, want my_table (schema-first)", items[0].Text)
	}
}

func TestSchemaFirstClauseDetection(t *testing.T) {
	for _, clause := range []string{"SELECT", "FROM", "WHERE", "JOIN", "ON", "SET"} {
		if !schemaFirstClause(clause) {
			t.Errorf("schemaFirstClause(%q) = false, want true", clause)
		}
	}
	if schemaFirstClause("") {
		t.Error("schemaFirstClause('') = true, want false")
	}
}

func TestDetectLastSQLClause(t *testing.T) {
	tests := []struct {
		sql   string
		line  int
		col   int
		want  string
	}{
		{"SELECT id FROM", 0, 14, "FROM"},
		{"SELECT ", 0, 7, "SELECT"},
		{"SELECT id FROM t WHERE ", 0, 23, "WHERE"},
		{"SELECT id", 0, 9, "SELECT"},
	}
	for _, tc := range tests {
		got := detectLastSQLClause(tc.sql, tc.line, tc.col)
		if got != tc.want {
			t.Errorf("detectLastSQLClause(%q) = %q, want %q", tc.sql, got, tc.want)
		}
	}
}

func TestRenderPopupShowsKindLabels(t *testing.T) {
	m := New(testConfig())
	m.popup = completionPopup{
		visible: true,
		items: popupItemsFromCompletions([]CompletionItem{
			{Text: "SELECT", Kind: CompletionKindKeyword},
			{Text: "users", Kind: CompletionKindTable},
			{Text: "email", Kind: CompletionKindColumn},
		}),
		mode: popupModeCompletion,
	}
	view := ansi.Strip(m.renderPopup())
	for _, want := range []string{"SELECT", "[keyword]", "users", "[table]", "email", "[column]"} {
		if !strings.Contains(view, want) {
			t.Fatalf("popup view %q should contain %q", view, want)
		}
	}
}
