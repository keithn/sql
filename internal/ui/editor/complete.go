package editor

import (
	"strings"

	"github.com/sahilm/fuzzy"
	"github.com/sqltui/sql/internal/db"
)

// keywordPhraseTable maps keywords to their standard multi-word expansion.
// When a keyword in this table is completed, the full phrase is inserted instead
// of just the single keyword, reducing keystrokes for common SQL patterns. (AC9)
var keywordPhraseTable = map[string]string{
	"ORDER": "ORDER BY",
	"GROUP": "GROUP BY",
	"LEFT":  "LEFT JOIN",
	"RIGHT": "RIGHT JOIN",
	"INNER": "INNER JOIN",
	"CROSS": "CROSS JOIN",
	"FULL":  "FULL OUTER JOIN",
}

// CompletionKind identifies what a popup item represents.
type CompletionKind string

const (
	CompletionKindKeyword   CompletionKind = "keyword"
	CompletionKindTable     CompletionKind = "table"
	CompletionKindView      CompletionKind = "view"
	CompletionKindColumn    CompletionKind = "column"
	CompletionKindFunction  CompletionKind = "function"
	CompletionKindProcedure CompletionKind = "procedure"
	CompletionKindName      CompletionKind = "name"
)

// CompletionItem is one autocomplete candidate plus its UI kind.
type CompletionItem struct {
	Text string
	Kind CompletionKind
}

func (c CompletionItem) kindLabel() string {
	switch c.Kind {
	case CompletionKindKeyword:
		return "keyword"
	case CompletionKindTable:
		return "table"
	case CompletionKindView:
		return "view"
	case CompletionKindColumn:
		return "column"
	case CompletionKindFunction:
		return "function"
	case CompletionKindProcedure:
		return "procedure"
	default:
		return "name"
	}
}

type popupMode int

const (
	popupModeCompletion popupMode = iota
	popupModeRefactor
)

type popupAction string

const popupActionNameTableAlias popupAction = "name_table_alias"

const popupActionExpandSelectStar popupAction = "expand_select_star"

const popupActionConvertSelectToUpdate popupAction = "convert_select_to_update"

const popupActionAppendUpdateBelow popupAction = "append_update_below"

const popupActionConvertUpdateToSelect popupAction = "convert_update_to_select"

const popupActionAppendSelectBelow popupAction = "append_select_below"

const popupActionWrapIdentityInsert popupAction = "wrap_identity_insert"

const popupActionRenameTab popupAction = "rename_tab"

type popupItem struct {
	Text       string
	InsertText string
	Kind       CompletionKind
	Detail     string
	Shortcut   string
	Action     popupAction
}

func (p popupItem) kindLabel() string {
	switch p.Kind {
	case CompletionKindKeyword:
		return "keyword"
	case CompletionKindTable:
		return "table"
	case CompletionKindView:
		return "view"
	case CompletionKindColumn:
		return "column"
	case CompletionKindFunction:
		return "function"
	case CompletionKindProcedure:
		return "procedure"
	default:
		return "name"
	}
}

// sqlKeywords is the static completion list for SQL keywords.
var sqlKeywords = []string{
	"ADD", "ALL", "ALTER", "AND", "ANY", "AS", "ASC",
	"BACKUP", "BEGIN", "BETWEEN", "BY",
	"CASE", "CAST", "CHECK", "COALESCE", "COLUMN", "COMMIT", "CONSTRAINT",
	"CONVERT", "COUNT", "CREATE", "CROSS",
	"DATABASE", "DEFAULT", "DELETE", "DESC", "DISTINCT", "DROP",
	"ELSE", "END", "EXEC", "EXECUTE", "EXISTS",
	"FETCH", "FIRST", "FOREIGN", "FROM", "FULL",
	"GO", "GROUP",
	"HAVING",
	"IF", "IN", "INDEX", "INNER", "INSERT", "INTO", "IS", "ISNULL",
	"JOIN",
	"KEY",
	"LEFT", "LIKE", "LIMIT",
	"MAX", "MIN", "MERGE",
	"NEXT", "NOT", "NULL", "NULLIF",
	"OF", "OFFSET", "ON", "OR", "ORDER", "OUTER", "OVER",
	"PARTITION", "PRIMARY", "PROCEDURE",
	"REFERENCES", "RETURN", "RIGHT", "ROLLBACK", "ROW", "ROWS",
	"SELECT", "SET", "SUM",
	"TABLE", "TOP", "TRANSACTION", "TRIGGER", "TRUNCATE",
	"UNION", "UNIQUE", "UPDATE",
	"VALUES", "VIEW",
	"WHEN", "WHERE", "WITH",
}

var sqlKeywordItems = makeKeywordCompletionItems(sqlKeywords)

// completionPopup holds the state for the autocomplete dropdown.
type completionPopup struct {
	items      []popupItem
	selected   int
	visible    bool
	word       string // partial word that triggered the popup
	mode       popupMode
	title      string
	suppressed bool // true after Esc; cleared when user types a word character
}

func popupItemsFromCompletions(items []CompletionItem) []popupItem {
	out := make([]popupItem, 0, len(items))
	for _, item := range items {
		pi := popupItem{Text: item.Text, Kind: item.Kind}
		if item.Kind == CompletionKindKeyword {
			if phrase, ok := keywordPhraseTable[item.Text]; ok {
				pi.Text = phrase
				pi.InsertText = phrase
			}
		}
		out = append(out, pi)
	}
	return out
}

func makeKeywordCompletionItems(keywords []string) []CompletionItem {
	items := make([]CompletionItem, 0, len(keywords))
	for _, kw := range keywords {
		items = append(items, CompletionItem{Text: kw, Kind: CompletionKindKeyword})
	}
	return items
}

// wordBefore returns the SQL identifier being typed immediately before col
// in line. A word character is a letter, digit, or underscore.
func wordBefore(line string, col int) string {
	if col > len(line) {
		col = len(line)
	}
	end := col
	start := end
	for start > 0 && isWordRune(rune(line[start-1])) {
		start--
	}
	return line[start:end]
}

func isWordRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9') || r == '_'
}

// getCompletions returns fuzzy-matched completions for pattern, checking
// keywords first then extra schema items. Results are ranked by match score.
// Returns at most maxItems results.
func getCompletions(pattern string, extra []CompletionItem, maxItems int) []CompletionItem {
	return getCompletionsCtx(pattern, extra, maxItems, false)
}

// getCompletionsCtx is like getCompletions but when schemaFirst is true it
// puts schema/column items before keywords so they rank higher in fuzzy
// tie-breaking — useful when the cursor is in a column or table name position. (AC6)
func getCompletionsCtx(pattern string, extra []CompletionItem, maxItems int, schemaFirst bool) []CompletionItem {
	if pattern == "" {
		return nil
	}
	up := strings.ToUpper(pattern)

	seen := map[string]bool{}
	candidates := make([]CompletionItem, 0, len(sqlKeywordItems)+len(extra))

	addKeywords := func() {
		for _, item := range sqlKeywordItems {
			key := strings.ToUpper(item.Text)
			if !seen[key] {
				candidates = append(candidates, item)
				seen[key] = true
			}
		}
	}
	addExtra := func() {
		for _, item := range extra {
			key := strings.ToUpper(item.Text)
			if item.Text != "" && !seen[key] {
				candidates = append(candidates, item)
				seen[key] = true
			}
		}
	}

	if schemaFirst {
		addExtra()
		addKeywords()
	} else {
		addKeywords()
		addExtra()
	}

	// Use a case-insensitive source so "sel" matches "SELECT" and "sales_table".
	matches := fuzzy.FindFrom(up, completionSource(candidates))

	out := make([]CompletionItem, 0, maxItems)
	for _, m := range matches {
		out = append(out, candidates[m.Index])
		if len(out) >= maxItems {
			break
		}
	}
	return out
}

// completionSource wraps completion items so fuzzy matching is case-insensitive.
type completionSource []CompletionItem

func (u completionSource) String(i int) string { return strings.ToUpper(u[i].Text) }
func (u completionSource) Len() int            { return len(u) }

// popupItemSource wraps []popupItem for fuzzy.FindFrom.
type popupItemSource []popupItem

func (s popupItemSource) String(i int) string { return strings.ToUpper(s[i].Text) }
func (s popupItemSource) Len() int            { return len(s) }

// contextualColumnItems builds completions when the query references specific tables.
// It returns columns only from those referenced tables (each with a Detail field showing
// alias or table name as the source), plus SQL keywords — all fuzzy-matched against word.
// Returns nil if no table refs are detected or the schema is unavailable.
// cursorCol is the column offset in the current line; it is used to detect dot-qualified
// prefixes such as "u." (AC1) so only that table's columns are returned.
func contextualColumnItems(word string, fullText string, cursorLine int, cursorCol int, schema *db.Schema) []popupItem {
	if schema == nil {
		return nil
	}
	// Restrict to the current SQL block so refs from other statements are ignored.
	blockText := fullText
	if start, end, ok := detectBlockRange(fullText, cursorLine); ok {
		lines := strings.Split(fullText, "\n")
		if end < len(lines) {
			blockText = strings.Join(lines[start:end+1], "\n")
		}
	}
	tokens := scanSQLTokens(blockText)
	refs := parseSQLTableRefs(tokens)
	if len(refs) == 0 {
		return nil
	}

	// AC1: detect dot qualifier (e.g. "u." before the typed word) and restrict
	// completions to only that table/alias's columns.
	fullLines := strings.Split(fullText, "\n")
	lineText := ""
	if cursorLine < len(fullLines) {
		lineText = fullLines[cursorLine]
	}
	if qual := dotQualifier(lineText, cursorCol); qual != "" {
		return columnItemsForQualifier(qual, word, refs, schema)
	}

	// Build column candidates from each referenced table.
	seen := map[string]bool{}
	var colCandidates []popupItem
	for _, ref := range refs {
		info, ok := lookupSchemaTableInfo(schema, ref)
		if !ok {
			continue
		}
		qualifier := qualifierForTableRef(ref)
		tableKey := strings.Join(ref.segments, ".") + "|" + qualifier
		if seen[tableKey] {
			continue
		}
		seen[tableKey] = true
		for _, col := range info.Columns {
			colCandidates = append(colCandidates, popupItem{
				Text:   col.Name,
				Kind:   CompletionKindColumn,
				Detail: columnDetail(qualifier, col), // AC2: include type + PK flag
			})
		}
	}

	if len(colCandidates) == 0 {
		return nil
	}

	// Empty word (e.g. after "ORDER BY "): list all columns without keyword noise.
	if word == "" {
		const maxBrowse = 15
		if len(colCandidates) > maxBrowse {
			return colCandidates[:maxBrowse]
		}
		return colCandidates
	}

	// Non-empty word: fuzzy-match keywords + columns together.
	up := strings.ToUpper(word)
	candidates := make([]popupItem, 0, len(sqlKeywords)+len(colCandidates))
	for _, kw := range sqlKeywords {
		pi := popupItem{Text: kw, Kind: CompletionKindKeyword}
		if phrase, ok := keywordPhraseTable[kw]; ok {
			pi.Text = phrase
			pi.InsertText = phrase
		}
		candidates = append(candidates, pi)
	}
	candidates = append(candidates, colCandidates...)

	matches := fuzzy.FindFrom(up, popupItemSource(candidates))
	const maxItems = 10
	out := make([]popupItem, 0, maxItems)
	for _, m := range matches {
		out = append(out, candidates[m.Index])
		if len(out) >= maxItems {
			break
		}
	}
	return out
}

// columnDetail builds the Detail string for a column popup item, including the
// table qualifier, column type, and PK flag when available. (AC2)
func columnDetail(qualifier string, col db.ColumnDef) string {
	typeStr := col.Type
	if col.PrimaryKey {
		if typeStr != "" {
			typeStr += " PK"
		} else {
			typeStr = "PK"
		}
	}
	if typeStr == "" {
		return qualifier
	}
	if qualifier == "" {
		return typeStr
	}
	return qualifier + "  " + typeStr
}

// dotQualifier returns the identifier that immediately precedes a "." before the
// word at col in lineText, or "" if no such dot-qualifier exists.
// Example: in "SELECT u.na|" (cursor at |), dotQualifier returns "u". (AC1)
func dotQualifier(lineText string, col int) string {
	if col > len(lineText) {
		col = len(lineText)
	}
	// Skip back past the current word.
	wordStart := col
	for wordStart > 0 && isWordRune(rune(lineText[wordStart-1])) {
		wordStart--
	}
	// Check for a dot immediately before the word.
	if wordStart == 0 || lineText[wordStart-1] != '.' {
		return ""
	}
	// Walk back past the qualifier identifier.
	end := wordStart - 1 // position of the dot
	start := end
	for start > 0 && isWordRune(rune(lineText[start-1])) {
		start--
	}
	if start == end {
		return ""
	}
	return lineText[start:end]
}

// columnItemsForQualifier returns completion items for columns belonging to the
// table/alias identified by qual in the given refs, fuzzy-matched against word. (AC1)
func columnItemsForQualifier(qual, word string, refs []sqlTableRef, schema *db.Schema) []popupItem {
	for _, ref := range refs {
		if !strings.EqualFold(qualifierForTableRef(ref), qual) {
			continue
		}
		info, ok := lookupSchemaTableInfo(schema, ref)
		if !ok {
			continue
		}
		var candidates []popupItem
		for _, col := range info.Columns {
			candidates = append(candidates, popupItem{
				Text:   col.Name,
				Kind:   CompletionKindColumn,
				Detail: columnDetail(qual, col),
			})
		}
		if len(candidates) == 0 {
			return nil
		}
		if word == "" {
			const maxBrowse = 15
			if len(candidates) > maxBrowse {
				return candidates[:maxBrowse]
			}
			return candidates
		}
		up := strings.ToUpper(word)
		matches := fuzzy.FindFrom(up, popupItemSource(candidates))
		const maxItems = 10
		out := make([]popupItem, 0, maxItems)
		for _, m := range matches {
			out = append(out, candidates[m.Index])
			if len(out) >= maxItems {
				break
			}
		}
		return out
	}
	return nil
}

// cursorInsideComment reports whether the cursor at (cursorLine, cursorCol)
// falls inside a -- line comment or a /* */ block comment in text. (AC4)
func cursorInsideComment(text string, cursorLine, cursorCol int) bool {
	// Compute the rune offset of the cursor within the full text.
	lines := strings.Split(text, "\n")
	offset := 0
	for i := 0; i < cursorLine && i < len(lines); i++ {
		offset += len([]rune(lines[i])) + 1 // +1 for the newline
	}
	if cursorLine < len(lines) {
		lr := []rune(lines[cursorLine])
		c := cursorCol
		if c > len(lr) {
			c = len(lr)
		}
		offset += c
	}
	for _, tok := range scanSQLTokens(text) {
		if tok.kind != sqlTokLineComment && tok.kind != sqlTokBlockComment {
			continue
		}
		if tok.start < offset && offset <= tok.end {
			return true
		}
	}
	return false
}

// cursorAfterComparisonOp returns true when the non-whitespace token immediately
// before the typed word is a comparison operator (=, <, >, !=, <>, <=, >=) or
// the keyword LIKE. Autocomplete is suppressed in this context. (AC5)
func cursorAfterComparisonOp(lineText string, col int) bool {
	if col > len(lineText) {
		col = len(lineText)
	}
	// Skip past the current word to find its start.
	wordStart := col
	for wordStart > 0 && isWordRune(rune(lineText[wordStart-1])) {
		wordStart--
	}
	// Skip whitespace before the word.
	i := wordStart - 1
	for i >= 0 && (lineText[i] == ' ' || lineText[i] == '\t') {
		i--
	}
	if i < 0 {
		return false
	}
	switch lineText[i] {
	case '=', '<', '>':
		return true
	}
	// Check for word-based comparison keyword (LIKE).
	end := i + 1
	for i >= 0 && isWordRune(rune(lineText[i])) {
		i--
	}
	kw := strings.ToUpper(lineText[i+1 : end])
	return kw == "LIKE"
}

// detectLastSQLClause scans tokens up to the cursor and returns the last
// significant SQL clause keyword seen. Used to detect column vs table position
// for schema-first ordering. (AC3/AC6)
func detectLastSQLClause(fullText string, cursorLine, cursorCol int) string {
	// Compute rune offset of cursor.
	lines := strings.Split(fullText, "\n")
	offset := 0
	for i := 0; i < cursorLine && i < len(lines); i++ {
		offset += len([]rune(lines[i])) + 1
	}
	if cursorLine < len(lines) {
		lr := []rune(lines[cursorLine])
		c := cursorCol
		if c > len(lr) {
			c = len(lr)
		}
		offset += c
	}
	clauseKW := map[string]bool{
		"SELECT": true, "FROM": true, "WHERE": true,
		"JOIN": true, "INNER": true, "LEFT": true, "RIGHT": true,
		"FULL": true, "CROSS": true, "ON": true,
		"SET": true, "AND": true, "OR": true, "HAVING": true,
	}
	last := ""
	for _, tok := range scanSQLTokens(fullText) {
		if tok.end > offset {
			break
		}
		if tok.kind == sqlTokWord {
			up := strings.ToUpper(tok.text)
			if clauseKW[up] {
				last = up
			}
		}
	}
	return last
}

// schemaFirstClause returns true when the SQL clause context suggests that
// schema items (tables, columns) are more relevant than keywords. (AC6)
func schemaFirstClause(clause string) bool {
	switch clause {
	case "SELECT", "FROM", "WHERE", "JOIN", "INNER", "LEFT", "RIGHT",
		"FULL", "CROSS", "ON", "SET", "AND", "OR", "HAVING":
		return true
	}
	return false
}
