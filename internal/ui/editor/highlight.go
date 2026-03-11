package editor

import (
	"bytes"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/lipgloss"
	xansi "github.com/charmbracelet/x/ansi"

	"github.com/sqltui/sql/internal/db"
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

func highlightLinesWithSchema(sql string, schema *db.Schema) []string {
	lines := highlightLines(sql)
	if schema == nil || strings.TrimSpace(sql) == "" {
		return lines
	}
	spans := invalidSchemaHighlightSpans(sql, schema)
	if len(spans) == 0 {
		return lines
	}
	out := append([]string(nil), lines...)
	byLine := make(map[int][]highlightSpan)
	for _, span := range spans {
		start := runeOffsetToTextPos(sql, span.start)
		end := runeOffsetToTextPos(sql, span.end)
		if start.Line != end.Line || start.Line < 0 || start.Line >= len(out) {
			continue
		}
		byLine[start.Line] = append(byLine[start.Line], highlightSpan{start: start.Col, end: end.Col})
	}
	for line, lineSpans := range byLine {
		sort.SliceStable(lineSpans, func(i, j int) bool {
			if lineSpans[i].start == lineSpans[j].start {
				return lineSpans[i].end > lineSpans[j].end
			}
			return lineSpans[i].start > lineSpans[j].start
		})
		for _, span := range lineSpans {
			out[line] = applyInlineStyleSpan(out[line], span.start, span.end, missingSchemaRefStyle)
		}
	}
	return out
}

type highlightSpan struct {
	start int
	end   int
}

func applyInlineStyleSpan(hlText string, start, end int, style lipgloss.Style) string {
	if end <= start {
		return hlText
	}
	left := xansi.Truncate(hlText, start, "")
	mid := xansi.Truncate(skipVisualCols(hlText, start), end-start, "")
	right := skipVisualCols(hlText, end)
	return left + style.Render(xansi.Strip(mid)) + right
}

func invalidSchemaHighlightSpans(sql string, schema *db.Schema) []highlightSpan {
	blocks := sqlHighlightBlocks(sql)
	seen := map[string]bool{}
	out := make([]highlightSpan, 0, 8)
	for _, block := range blocks {
		for _, span := range invalidSchemaHighlightSpansInBlock(block.text, schema) {
			global := highlightSpan{start: block.start + span.start, end: block.start + span.end}
			key := strings.Join([]string{itoa(global.start), itoa(global.end)}, ":")
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, global)
		}
	}
	return out
}

type sqlHighlightBlock struct {
	start int
	text  string
}

func sqlHighlightBlocks(sql string) []sqlHighlightBlock {
	lines := strings.Split(sql, "\n")
	lineOffsets := make([]int, len(lines))
	offset := 0
	for i, line := range lines {
		lineOffsets[i] = offset
		offset += len([]rune(line))
		if i < len(lines)-1 {
			offset++
		}
	}
	blocks := make([]sqlHighlightBlock, 0, 4)
	for i := 0; i < len(lines); {
		if isSQLHighlightSeparatorLine(lines[i]) {
			i++
			continue
		}
		start := i
		for i+1 < len(lines) && !isSQLHighlightSeparatorLine(lines[i+1]) {
			i++
		}
		blocks = append(blocks, sqlHighlightBlock{start: lineOffsets[start], text: strings.Join(lines[start:i+1], "\n")})
		i++
	}
	return blocks
}

func isSQLHighlightSeparatorLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	return trimmed == "" || strings.EqualFold(trimmed, "GO")
}

func invalidSchemaHighlightSpansInBlock(block string, schema *db.Schema) []highlightSpan {
	if schema == nil || strings.TrimSpace(block) == "" {
		return nil
	}
	tokens := scanSQLTokens(block)
	refs := parseSQLTableRefs(tokens)
	if len(tokens) == 0 {
		return nil
	}
	spans := make([]highlightSpan, 0, len(refs))
	for _, ref := range refs {
		if _, ok := lookupSchemaTableInfo(schema, ref); !ok {
			spans = append(spans, highlightSpan{start: ref.nameStart, end: ref.nameEnd})
		}
	}
	sig := significantSQLTokenIndices(tokens)
	for pos := 0; pos < len(sig); pos++ {
		idx := sig[pos]
		tok := tokens[idx]
		if tok.kind != sqlTokWord || tokenWithinTableDecl(tok, refs) || pos > 0 && tokens[sig[pos-1]].kind == sqlTokDot {
			continue
		}
		endPos, segments, ok := readQualifiedIdentifier(tokens, sig, pos)
		if !ok || len(segments) < 2 {
			continue
		}
		ref, ok := findRefForQualifiedStar(refs, segments[:len(segments)-1])
		if !ok {
			pos = endPos - 1
			continue
		}
		info, ok := lookupSchemaTableInfo(schema, ref)
		if ok && !schemaColumnExists(info.Columns, segments[len(segments)-1]) {
			colTok := tokens[sig[endPos-1]]
			spans = append(spans, highlightSpan{start: colTok.start, end: colTok.end})
		}
		pos = endPos - 1
	}
	return spans
}

func tokenWithinTableDecl(tok sqlScanToken, refs []sqlTableRef) bool {
	for _, ref := range refs {
		if rangesOverlap(tok.start, tok.end, ref.nameStart, ref.nameEnd) {
			return true
		}
		if ref.alias != "" && rangesOverlap(tok.start, tok.end, ref.aliasStart, ref.aliasEnd) {
			return true
		}
	}
	return false
}

func schemaColumnExists(columns []db.ColumnDef, name string) bool {
	name = normalizeSQLIdentifier(name)
	for _, col := range columns {
		if strings.EqualFold(normalizeSQLIdentifier(col.Name), name) {
			return true
		}
	}
	return false
}

func itoa(v int) string {
	return strconv.Itoa(v)
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
