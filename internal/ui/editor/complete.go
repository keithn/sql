package editor

import (
	"strings"

	"github.com/sahilm/fuzzy"
)

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

// completionPopup holds the state for the autocomplete dropdown.
type completionPopup struct {
	items    []string
	selected int
	visible  bool
	word     string // partial word that triggered the popup
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
// keywords first then extra schema names. Results are ranked by match score.
// Returns at most maxItems results.
func getCompletions(pattern string, extra []string, maxItems int) []string {
	if pattern == "" {
		return nil
	}
	up := strings.ToUpper(pattern)

	// Build a deduplicated candidate list: keywords first, then schema names.
	seen := map[string]bool{}
	candidates := make([]string, 0, len(sqlKeywords)+len(extra))
	for _, kw := range sqlKeywords {
		candidates = append(candidates, kw)
		seen[kw] = true
	}
	for _, name := range extra {
		if !seen[strings.ToUpper(name)] {
			candidates = append(candidates, name)
		}
	}

	// Use a case-insensitive source so "sel" matches "SELECT" and "sales_table".
	matches := fuzzy.FindFrom(up, upperSource(candidates))

	out := make([]string, 0, maxItems)
	for _, m := range matches {
		out = append(out, candidates[m.Index])
		if len(out) >= maxItems {
			break
		}
	}
	return out
}

// upperSource wraps a string slice so fuzzy matching is case-insensitive.
type upperSource []string

func (u upperSource) String(i int) string { return strings.ToUpper(u[i]) }
func (u upperSource) Len() int            { return len(u) }

