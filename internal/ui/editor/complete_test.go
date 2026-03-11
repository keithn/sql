package editor

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
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

func TestRenderPopupShowsKindLabels(t *testing.T) {
	m := New(testConfig())
	m.popup = completionPopup{
		visible: true,
		items: []CompletionItem{
			{Text: "SELECT", Kind: CompletionKindKeyword},
			{Text: "users", Kind: CompletionKindTable},
			{Text: "email", Kind: CompletionKindColumn},
		},
	}
	view := ansi.Strip(m.renderPopup())
	for _, want := range []string{"SELECT", "[keyword]", "users", "[table]", "email", "[column]"} {
		if !strings.Contains(view, want) {
			t.Fatalf("popup view %q should contain %q", view, want)
		}
	}
}
