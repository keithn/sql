package export

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sqltui/sql/internal/db"
)

// CSV exports a QueryResult as comma-separated values (RFC 4180).
func CSV(rs db.QueryResult) string {
	var sb strings.Builder
	cols := make([]string, len(rs.Columns))
	for i, c := range rs.Columns {
		cols[i] = csvQuote(c.Name)
	}
	sb.WriteString(strings.Join(cols, ","))
	sb.WriteByte('\n')
	for _, row := range rs.Rows {
		fields := make([]string, len(rs.Columns))
		for i := range rs.Columns {
			if i < len(row) {
				fields[i] = csvQuote(cellStr(row[i]))
			}
		}
		sb.WriteString(strings.Join(fields, ","))
		sb.WriteByte('\n')
	}
	return sb.String()
}

// Markdown exports a QueryResult as a GitHub-flavored Markdown table.
func Markdown(rs db.QueryResult) string {
	if len(rs.Columns) == 0 {
		return ""
	}
	widths := make([]int, len(rs.Columns))
	for i, c := range rs.Columns {
		widths[i] = len(c.Name)
	}
	strRows := make([][]string, len(rs.Rows))
	for r, row := range rs.Rows {
		strRows[r] = make([]string, len(rs.Columns))
		for i := range rs.Columns {
			if i < len(row) {
				s := cellStr(row[i])
				strRows[r][i] = s
				if len(s) > widths[i] {
					widths[i] = len(s)
				}
			}
		}
	}
	var sb strings.Builder
	sb.WriteByte('|')
	for i, c := range rs.Columns {
		_ = i
		sb.WriteString(" " + padRight(c.Name, widths[i]) + " |")
	}
	sb.WriteByte('\n')
	sb.WriteByte('|')
	for _, w := range widths {
		sb.WriteString(" " + strings.Repeat("-", w) + " |")
	}
	sb.WriteByte('\n')
	for _, row := range strRows {
		sb.WriteByte('|')
		for i, w := range widths {
			val := ""
			if i < len(row) {
				val = row[i]
			}
			sb.WriteString(" " + padRight(val, w) + " |")
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

// JSON exports a QueryResult as a JSON array of objects.
func JSON(rs db.QueryResult) (string, error) {
	rows := make([]map[string]any, 0, len(rs.Rows))
	for _, row := range rs.Rows {
		obj := make(map[string]any, len(rs.Columns))
		for i, c := range rs.Columns {
			if i < len(row) {
				obj[c.Name] = row[i]
			}
		}
		rows = append(rows, obj)
	}
	b, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b) + "\n", nil
}

// SQLInsert exports a QueryResult as SQL INSERT statements.
// tableName is used as-is in the INSERT INTO clause.
func SQLInsert(rs db.QueryResult, tableName string) string {
	if len(rs.Columns) == 0 || len(rs.Rows) == 0 {
		return ""
	}
	cols := make([]string, len(rs.Columns))
	for i, c := range rs.Columns {
		cols[i] = c.Name
	}
	colList := strings.Join(cols, ", ")
	var sb strings.Builder
	for ri, row := range rs.Rows {
		vals := make([]string, len(rs.Columns))
		for i := range rs.Columns {
			if i < len(row) {
				vals[i] = sqlLiteral(row[i])
			} else {
				vals[i] = "NULL"
			}
		}
		if ri == 0 {
			sb.WriteString(fmt.Sprintf("INSERT INTO %s (%s) VALUES\n", tableName, colList))
		}
		sb.WriteString("    (" + strings.Join(vals, ", ") + ")")
		if ri < len(rs.Rows)-1 {
			sb.WriteString(",\n")
		} else {
			sb.WriteString(";\n")
		}
	}
	return sb.String()
}

// ExtractTableName attempts to infer the primary table name from a SQL statement.
// It handles SELECT … FROM <table>, UPDATE <table> SET …, INSERT INTO <table>,
// and DELETE FROM <table>. Returns "table_name" if no table can be reliably extracted.
func ExtractTableName(sql string) string {
	// Normalize all whitespace to single spaces so newlines between tokens don't break matching.
	normalized := strings.Join(strings.Fields(sql), " ")
	upper := strings.ToUpper(normalized)

	// UPDATE <table> SET — most common case for generated UPDATEs.
	if strings.HasPrefix(upper, "UPDATE ") {
		if name := extractIdentAfterKeyword(normalized[7:], "SET"); name != "" {
			return name
		}
	}

	// INSERT INTO <table>
	if strings.HasPrefix(upper, "INSERT INTO ") {
		if name := extractFirstIdent(strings.TrimSpace(normalized[12:])); name != "" {
			return name
		}
	}

	// SELECT / DELETE: find first top-level FROM.
	depth := 0
	for i := 0; i < len(upper)-5; i++ {
		switch upper[i] {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		}
		if depth == 0 && upper[i:i+5] == " FROM" {
			if name := extractFirstIdent(strings.TrimSpace(normalized[i+5:])); name != "" {
				return name
			}
		}
	}
	return "table_name"
}

// extractIdentAfterKeyword extracts the identifier from s that appears before
// the given keyword (e.g. extracts table from "dbo.tbl SET col=1").
func extractIdentAfterKeyword(s, keyword string) string {
	// Normalize whitespace so newlines between tokens don't break matching.
	s = strings.Join(strings.Fields(s), " ")
	upper := strings.ToUpper(s)
	idx := -1
	// Find the keyword at a word boundary.
	search := " " + keyword
	i := strings.Index(upper, search)
	if i >= 0 {
		idx = i
	} else if strings.HasPrefix(upper, keyword+" ") || upper == keyword {
		return "" // keyword is first token, no table before it
	}
	var portion string
	if idx >= 0 {
		portion = strings.TrimSpace(s[:idx])
	} else {
		portion = strings.TrimSpace(s)
	}
	return extractFirstIdent(portion)
}

// extractFirstIdent returns the first SQL identifier (possibly schema-qualified)
// from s, handling bracket-quoted, backtick-quoted, and bare identifiers.
func extractFirstIdent(s string) string {
	if s == "" {
		return ""
	}
	switch s[0] {
	case '[':
		end := strings.Index(s, "]")
		if end < 0 {
			return ""
		}
		inner := s[1:end]
		rest := s[end+1:]
		// May be schema-qualified: [schema].[table]
		if strings.HasPrefix(rest, ".[") {
			end2 := strings.Index(rest[2:], "]")
			if end2 >= 0 {
				return inner + "." + rest[2:end2+2]
			}
		}
		return inner
	case '`':
		end := strings.Index(s[1:], "`")
		if end < 0 {
			return ""
		}
		return s[1 : end+1]
	case '(':
		return "" // subquery
	default:
		end := strings.IndexAny(s, " \t\n\r,([")
		if end < 0 {
			return s
		}
		return s[:end]
	}
}

func csvQuote(s string) string {
	if strings.ContainsAny(s, ",\"\r\n") {
		return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}
	return s
}

// fmtGUID formats a 16-byte slice as an uppercase GUID string.
func fmtGUID(b []byte) string {
	return fmt.Sprintf("%X-%X-%X-%X-%X", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func cellStr(v any) string {
	if v == nil {
		return ""
	}
	if b, ok := v.([]byte); ok {
		if len(b) == 16 {
			return fmtGUID(b)
		}
		return string(b)
	}
	return fmt.Sprintf("%v", v)
}

func sqlLiteral(v any) string {
	if v == nil {
		return "NULL"
	}
	switch val := v.(type) {
	case int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64:
		return fmt.Sprintf("%v", val)
	case bool:
		if val {
			return "1"
		}
		return "0"
	case []byte:
		if len(val) == 16 {
			return "'" + fmtGUID(val) + "'"
		}
		return "'" + strings.ReplaceAll(string(val), "'", "''") + "'"
	default:
		s := fmt.Sprintf("%v", v)
		return "'" + strings.ReplaceAll(s, "'", "''") + "'"
	}
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}
