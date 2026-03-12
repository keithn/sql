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

// ExtractTableName attempts to infer the primary table name from a SQL statement
// by locating the first identifier after a FROM keyword. Returns "table_name" if
// no table can be reliably extracted.
func ExtractTableName(sql string) string {
	upper := strings.ToUpper(sql)
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
			rest := strings.TrimSpace(sql[i+5:])
			if rest == "" {
				return "table_name"
			}
			// Bracket-quoted identifier: [name with spaces]
			if rest[0] == '[' {
				end := strings.Index(rest, "]")
				if end < 0 {
					return "table_name"
				}
				name := rest[1:end]
				if name == "" {
					return "table_name"
				}
				return name
			}
			// Backtick-quoted identifier
			if rest[0] == '`' {
				end := strings.Index(rest[1:], "`")
				if end < 0 {
					return "table_name"
				}
				name := rest[1 : end+1]
				if name == "" {
					return "table_name"
				}
				return name
			}
			// Subquery
			if rest[0] == '(' {
				return "table_name"
			}
			// Unquoted identifier — take until whitespace or punctuation (keep dots for schema.table)
			end := strings.IndexAny(rest, " \t\n\r,([")
			var name string
			if end < 0 {
				name = rest
			} else {
				name = rest[:end]
			}
			if name == "" {
				return "table_name"
			}
			return name
		}
	}
	return "table_name"
}

func csvQuote(s string) string {
	if strings.ContainsAny(s, ",\"\r\n") {
		return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}
	return s
}

func cellStr(v any) string {
	if v == nil {
		return ""
	}
	if b, ok := v.([]byte); ok {
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
