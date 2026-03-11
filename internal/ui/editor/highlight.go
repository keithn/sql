package editor

import (
	"bytes"
	"strings"
	"unicode/utf8"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/lipgloss"
	xansi "github.com/charmbracelet/x/ansi"
)

// highlightLines tokenizes sql with chroma and returns one ANSI-colored string
// per source line. Length matches strings.Count(sql, "\n") + 1.
func highlightLines(sql string) []string {
	lexer := chroma.Coalesce(lexers.Get("sql"))
	style := styles.Get("monokai")
	if style == nil {
		style = styles.Fallback
	}
	// Prefer true-color; fall back to 256-color.
	formatter := formatters.Get("terminal16m")
	if formatter == nil {
		formatter = formatters.Get("terminal256")
	}
	if formatter == nil {
		return strings.Split(sql, "\n")
	}

	var buf bytes.Buffer
	iter, err := lexer.Tokenise(nil, sql)
	if err != nil {
		return strings.Split(sql, "\n")
	}
	if err := formatter.Format(&buf, style, iter); err != nil {
		return strings.Split(sql, "\n")
	}

	out := strings.TrimRight(buf.String(), "\n")
	return strings.Split(out, "\n")
}

// injectCursorStyled applies a custom lipgloss style as the cursor character.
func injectCursorStyled(hlText string, col int, style lipgloss.Style) string {
	stripped := xansi.Strip(hlText)
	runes := []rune(stripped)

	if len(runes) == 0 {
		return style.Render(" ")
	}

	cursorChar := " "
	if col < len(runes) {
		cursorChar = string(runes[col])
	}

	left := xansi.Truncate(hlText, col, "")
	right := skipVisualCols(hlText, col+1)
	return left + style.Render(cursorChar) + right
}

// skipVisualCols returns s with the first n visual columns removed.
// Any ANSI escape sequences encountered while skipping are prepended to the
// result so that the terminal color state is correctly restored.
func skipVisualCols(s string, n int) string {
	if n <= 0 {
		return s
	}
	var ansiCtx strings.Builder
	i := 0
	skipped := 0
	for i < len(s) && skipped < n {
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			// CSI sequence: ESC [ ... <final byte 0x40–0x7e>
			j := i + 2
			for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
				j++
			}
			if j < len(s) {
				j++ // include final byte
			}
			ansiCtx.WriteString(s[i:j])
			i = j
			continue
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
		skipped++
	}
	if i >= len(s) {
		return ""
	}
	return ansiCtx.String() + s[i:]
}
