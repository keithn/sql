package results

import (
	"strings"
	"testing"
)

func TestFormatCell_Nil(t *testing.T) {
	if got := formatCell(nil); got != "∅" {
		t.Fatalf("nil → %q, want ∅", got)
	}
}

func TestFormatCell_PlainString(t *testing.T) {
	if got := formatCell("hello"); got != "hello" {
		t.Fatalf("plain string → %q, want hello", got)
	}
}

func TestFormatCell_Int(t *testing.T) {
	if got := formatCell(42); got != "42" {
		t.Fatalf("int 42 → %q, want 42", got)
	}
}

func TestFormatCell_NewlinesReplacedWithSymbol(t *testing.T) {
	cases := []struct {
		input any
		desc  string
	}{
		{"line1\nline2", "LF"},
		{"line1\r\nline2", "CRLF"},
		{"line1\rline2", "CR"},
	}
	for _, c := range cases {
		got := formatCell(c.input)
		if strings.Contains(got, "\n") || strings.Contains(got, "\r") {
			t.Errorf("%s: formatCell still contains newline: %q", c.desc, got)
		}
		if !strings.Contains(got, "↵") {
			t.Errorf("%s: formatCell should contain ↵ symbol, got %q", c.desc, got)
		}
	}
}

func TestFormatCell_EmbeddedNewlineInJSON(t *testing.T) {
	json := "{\n  \"key\": \"value\"\n}"
	got := formatCell(json)
	if strings.Contains(got, "\n") {
		t.Fatalf("JSON with newlines should be flattened, got: %q", got)
	}
	if !strings.Contains(got, "↵") {
		t.Fatalf("expected ↵ in flattened JSON, got: %q", got)
	}
}

func TestFormatCell_BinaryBytes(t *testing.T) {
	// Pure printable bytes should render as string.
	got := formatCell([]byte("hello"))
	if got != "hello" {
		t.Fatalf("printable bytes → %q, want hello", got)
	}
	// Binary bytes should render as <binary N bytes>.
	got = formatCell([]byte{0x00, 0x01, 0x02})
	if !strings.HasPrefix(got, "<binary") {
		t.Fatalf("binary bytes → %q, want <binary ...>", got)
	}
}
