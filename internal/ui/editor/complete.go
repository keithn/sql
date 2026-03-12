package editor

import (
	"strings"

	"github.com/sahilm/fuzzy"
	"github.com/sqltui/sql/internal/db"
)

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
		out = append(out, popupItem{Text: item.Text, Kind: item.Kind})
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
	if pattern == "" {
		return nil
	}
	up := strings.ToUpper(pattern)

	// Build a deduplicated candidate list: keywords first, then schema items.
	seen := map[string]bool{}
	candidates := make([]CompletionItem, 0, len(sqlKeywordItems)+len(extra))
	for _, item := range sqlKeywordItems {
		key := strings.ToUpper(item.Text)
		candidates = append(candidates, item)
		seen[key] = true
	}
	for _, item := range extra {
		key := strings.ToUpper(item.Text)
		if item.Text != "" && !seen[key] {
			candidates = append(candidates, item)
			seen[key] = true
		}
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
func contextualColumnItems(word string, fullText string, cursorLine int, schema *db.Schema) []popupItem {
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
				Detail: qualifier,
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
		candidates = append(candidates, popupItem{Text: kw, Kind: CompletionKindKeyword})
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
