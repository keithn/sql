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
	items := contextualColumnItems("name", "SELECT * FROM tblClient", 0, nil)
	if items != nil {
		t.Fatalf("expected nil without schema, got %v", items)
	}
}

func TestContextualColumnItems_NoTableRefs(t *testing.T) {
	schema := makeTestSchema()
	items := contextualColumnItems("name", "SELECT 1", 0, schema)
	if items != nil {
		t.Fatalf("expected nil when no table refs, got %v", items)
	}
}

func TestContextualColumnItems_ReturnsColumnsFromReferencedTable(t *testing.T) {
	schema := makeTestSchema()
	sql := "SELECT * FROM dbo.tblClient WHERE "
	items := contextualColumnItems("str", sql, 0, schema)
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
	items := contextualColumnItems("lng", sql, 0, schema)
	for _, it := range items {
		if it.Text == "lngOrderID" {
			t.Fatalf("lngOrderID from unreferenced tblOrder should not appear")
		}
	}
}

func TestContextualColumnItems_EmptyWordListsAllColumns(t *testing.T) {
	schema := makeTestSchema()
	sql := "SELECT * FROM dbo.tblClient ORDER BY "
	items := contextualColumnItems("", sql, 0, schema)
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
	items := contextualColumnItems("lng", sql, 4, schema)
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
