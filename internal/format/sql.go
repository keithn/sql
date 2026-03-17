package format

import (
	"strings"
)

// Format pretty-prints a SQL string. Idempotent and error-tolerant.
func Format(input string) string {
	tokens := tokenise(input)
	return render(tokens)
}

// ── Tokeniser ────────────────────────────────────────────────────────────────

type tokKind int

const (
	tokWord    tokKind = iota // keyword or identifier
	tokNumber                 // numeric literal
	tokString                 // single-quoted string
	tokLineCmt                // -- comment
	tokBlkCmt                 // /* */ comment
	tokOp                     // =  <>  <=  >=  !=  <  >  +  -  /  %
	tokStar                   // * (select wildcard or multiply)
	tokComma                  // ,
	tokLParen                 // (
	tokRParen                 // )
	tokSemi                   // ;
	tokWS                     // whitespace / newline (skipped by renderer)
)

type token struct {
	kind tokKind
	val  string
}

func tokenise(s string) []token {
	var out []token
	i := 0
	for i < len(s) {
		c := s[i]

		// Whitespace / newline
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			j := i + 1
			for j < len(s) && (s[j] == ' ' || s[j] == '\t' || s[j] == '\n' || s[j] == '\r') {
				j++
			}
			out = append(out, token{tokWS, " "})
			i = j
			continue
		}

		// Line comment
		if c == '-' && i+1 < len(s) && s[i+1] == '-' {
			j := i + 2
			for j < len(s) && s[j] != '\n' && s[j] != '\r' {
				j++
			}
			out = append(out, token{tokLineCmt, s[i:j]})
			i = j
			continue
		}

		// Block comment
		if c == '/' && i+1 < len(s) && s[i+1] == '*' {
			j := i + 2
			for j+1 < len(s) && !(s[j] == '*' && s[j+1] == '/') {
				j++
			}
			if j+1 < len(s) {
				j += 2
			}
			out = append(out, token{tokBlkCmt, s[i:j]})
			i = j
			continue
		}

		// Single-quoted string
		if c == '\'' {
			j := i + 1
			for j < len(s) {
				if s[j] == '\'' {
					j++
					if j < len(s) && s[j] == '\'' { // escaped ''
						j++
						continue
					}
					break
				}
				j++
			}
			out = append(out, token{tokString, s[i:j]})
			i = j
			continue
		}

		// Multi-char operators: <>, <=, >=, !=
		if i+1 < len(s) {
			two := s[i : i+2]
			if two == "<>" || two == "<=" || two == ">=" || two == "!=" {
				out = append(out, token{tokOp, two})
				i += 2
				continue
			}
		}

		// Single-char operators
		if c == '=' || c == '<' || c == '>' || c == '+' || c == '-' || c == '/' || c == '%' {
			out = append(out, token{tokOp, string(c)})
			i++
			continue
		}

		// Star (separate from operators)
		if c == '*' {
			out = append(out, token{tokStar, "*"})
			i++
			continue
		}

		// Punctuation
		switch c {
		case ',':
			out = append(out, token{tokComma, ","})
			i++
			continue
		case '(':
			out = append(out, token{tokLParen, "("})
			i++
			continue
		case ')':
			out = append(out, token{tokRParen, ")"})
			i++
			continue
		case ';':
			out = append(out, token{tokSemi, ";"})
			i++
			continue
		}

		// Number
		if c >= '0' && c <= '9' {
			j := i + 1
			for j < len(s) && (s[j] >= '0' && s[j] <= '9' || s[j] == '.') {
				j++
			}
			out = append(out, token{tokNumber, s[i:j]})
			i = j
			continue
		}

		// Word (identifier or keyword)
		if isWordByte(c) {
			j := i + 1
			for j < len(s) && (isWordByte(s[j]) || s[j] >= '0' && s[j] <= '9') {
				j++
			}
			// Handle qualified names: word.word.word... (e.g. schema.dbo.Table)
			for j < len(s) && s[j] == '.' && j+1 < len(s) && (isWordByte(s[j+1]) || s[j+1] >= '0' && s[j+1] <= '9') {
				j++ // include the dot
				for j < len(s) && (isWordByte(s[j]) || s[j] >= '0' && s[j] <= '9') {
					j++
				}
			}
			out = append(out, token{tokWord, s[i:j]})
			i = j
			continue
		}

		// Anything else: skip
		i++
	}
	return out
}

func isWordByte(c byte) bool {
	return c == '_' || c == '@' || c == '#' || c == '[' || c == ']' ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// ── Keyword sets ─────────────────────────────────────────────────────────────

// sqlKeywords are uppercased. Function names (count, sum…) are intentionally
// excluded so they preserve the user's original casing.
var sqlKeywords = setOf(
	"SELECT", "FROM", "WHERE", "AND", "OR", "NOT", "IN", "IS", "NULL",
	"LIKE", "BETWEEN", "EXISTS", "CASE", "WHEN", "THEN", "ELSE", "END",
	"AS", "ON", "JOIN", "INNER", "LEFT", "RIGHT", "FULL", "CROSS", "OUTER",
	"GROUP", "BY", "ORDER", "HAVING", "LIMIT", "OFFSET", "DISTINCT", "TOP",
	"ALL", "UNION", "INTERSECT", "EXCEPT",
	"INSERT", "INTO", "VALUES", "UPDATE", "SET", "DELETE",
	"CREATE", "TABLE", "VIEW", "INDEX", "DROP", "ALTER", "ADD", "COLUMN",
	"PRIMARY", "KEY", "FOREIGN", "REFERENCES", "UNIQUE", "DEFAULT",
	"CONSTRAINT", "CHECK", "WITH", "RETURNING",
	"BEGIN", "COMMIT", "ROLLBACK", "TRANSACTION",
	"EXEC", "EXECUTE", "PROCEDURE", "FUNCTION",
	"IF", "WHILE", "DECLARE", "PRINT",
	"NOCOUNT", "NVARCHAR", "VARCHAR", "INT", "BIGINT", "DECIMAL", "BIT",
	"DATETIME", "DATE", "TIME", "FLOAT", "REAL",
	"ASC", "DESC", "NULLS", "FIRST", "LAST",
	"GO",
)

// clauseBreak starts a new line at column 0.
var clauseBreak = setOf(
	"FROM", "WHERE", "HAVING", "LIMIT", "OFFSET", "VALUES", "RETURNING",
)

// compoundFirst are set operators that take an optional second word (ALL).
var compoundFirst = setOf("UNION", "INTERSECT", "EXCEPT")

// statementStart begins a new top-level statement.
var statementStart = setOf(
	"SELECT", "INSERT", "UPDATE", "DELETE", "WITH",
	"CREATE", "DROP", "ALTER", "EXEC", "EXECUTE",
)

// joinFirst are the first word of a JOIN clause.
var joinFirst = setOf("INNER", "LEFT", "RIGHT", "FULL", "CROSS")

func setOf(words ...string) map[string]bool {
	m := make(map[string]bool, len(words))
	for _, w := range words {
		m[w] = true
	}
	return m
}

// ── Renderer ─────────────────────────────────────────────────────────────────

// stmtFrame saves renderer state when entering a subquery so it can be
// restored when the subquery's closing ')' is reached.
type stmtFrame struct {
	inSelect    bool
	inWhere     bool
	inSet       bool
	afterJoin   bool
	afterDelete bool
	stmtIdx     int
	selectCols  int
	caseLevel   int // CASE nesting depth at stmtDepth
	stmtDepth   int // paren depth at which this statement started
	baseIndent  int // indent level for this statement
}

type rend struct {
	tokens []token
	pos    int

	lines []string        // completed lines (trailing space already trimmed)
	cur   strings.Builder // current line in progress
	ps    bool            // pendingSpace: write a space before the next non-empty emit

	depth       int    // parenthesis depth
	inSelect    bool   // inside SELECT column list at stmtDepth
	inWhere     bool   // inside WHERE/HAVING clause at stmtDepth
	inSet       bool   // inside SET clause
	afterJoin   bool   // just emitted a JOIN; next ON should indent
	afterDelete bool   // just emitted DELETE; next FROM should not clause-break
	stmtIdx     int    // top-level statement counter
	prevWord    string // most-recent word token (uppercased)
	selectCols  int    // pre-counted columns for current SELECT

	// Subquery formatting state.
	stmtDepth  int         // paren depth at which current statement formatting started (0 = top-level)
	baseIndent int         // extra indent levels added by subquery nesting
	subStack   []stmtFrame // saved frames for restoring after subquery closes

	// CASE expression formatting state.
	caseLevel int // number of CASE expressions open at stmtDepth
}

func render(tokens []token) string {
	r := &rend{tokens: tokens}
	r.selectCols = preCountCols(tokens)
	r.run()

	// Flush last line.
	r.breakLine()

	// Join, removing consecutive blank lines and trailing blank lines.
	var out []string
	prevBlank := false
	for _, l := range r.lines {
		blank := strings.TrimSpace(l) == ""
		if blank && prevBlank {
			continue // collapse consecutive blanks
		}
		out = append(out, l)
		prevBlank = blank
	}
	// Remove trailing blank lines.
	for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
		out = out[:len(out)-1]
	}
	return strings.Join(out, "\n")
}

// preCountCols counts SELECT columns in the first SELECT at depth 0.
func preCountCols(tokens []token) int {
	return preCountColsFrom(tokens, 0)
}

// preCountColsFrom counts SELECT columns starting at pos, treating depth 0
// relative to the starting position (used for subquery SELECT column counts).
func preCountColsFrom(tokens []token, startPos int) int {
	depth := 0
	inSel := false
	count := 0
	for i, t := range tokens {
		if i < startPos {
			continue
		}
		if t.kind == tokWS {
			continue
		}
		up := strings.ToUpper(t.val)
		if !inSel {
			if t.kind == tokWord && up == "SELECT" && depth == 0 {
				inSel = true
				count = 1
			}
			if t.kind == tokLParen {
				depth++
			} else if t.kind == tokRParen {
				depth--
				if depth < 0 {
					return 0
				}
			}
			continue
		}
		switch t.kind {
		case tokLParen:
			depth++
		case tokRParen:
			depth--
			if depth < 0 {
				return count
			}
		case tokComma:
			if depth == 0 {
				count++
			}
		case tokWord:
			if depth == 0 && (clauseBreak[up] || joinFirst[up] || up == "JOIN" ||
				statementStart[up] || compoundFirst[up]) {
				return count
			}
		}
	}
	return count
}

func (r *rend) next() (token, bool) {
	for r.pos < len(r.tokens) {
		t := r.tokens[r.pos]
		r.pos++
		if t.kind == tokWS {
			continue
		}
		return t, true
	}
	return token{}, false
}

func (r *rend) peekWord() string {
	for i := r.pos; i < len(r.tokens); i++ {
		t := r.tokens[i]
		if t.kind == tokWS {
			continue
		}
		if t.kind == tokWord {
			return strings.ToUpper(t.val)
		}
		return ""
	}
	return ""
}

// emit writes s to the current line, prepending a space if ps is set.
func (r *rend) emit(s string) {
	if s == "" {
		return
	}
	if r.ps {
		r.cur.WriteByte(' ')
		r.ps = false
	}
	r.cur.WriteString(s)
	r.ps = true
}

// emitNoTrail writes s without setting the pending space after.
func (r *rend) emitNoTrail(s string) {
	if s == "" {
		return
	}
	if r.ps {
		r.cur.WriteByte(' ')
		r.ps = false
	}
	r.cur.WriteString(s)
}

// emitOp writes an operator with exactly one space on each side.
func (r *rend) emitOp(op string) {
	r.cur.WriteByte(' ')
	r.ps = false
	r.cur.WriteString(op)
	r.ps = true // trailing space via next emit
}

// breakLine trims and saves the current line, resets the builder.
// A leading empty line (before anything has been written) is suppressed.
func (r *rend) breakLine() {
	line := strings.TrimRight(r.cur.String(), " \t")
	if line != "" || len(r.lines) > 0 {
		r.lines = append(r.lines, line)
	}
	r.cur.Reset()
	r.ps = false
}

// clauseLine starts a new line, applies base indent, then emits kw.
func (r *rend) clauseLine(kw string) {
	r.breakLine()
	for i := 0; i < r.baseIndent; i++ {
		r.cur.WriteString("    ")
	}
	r.cur.WriteString(kw)
	r.ps = true
}

// subClauseLine starts a new line, applies base indent + prefix, then kw.
func (r *rend) subClauseLine(indent, kw string) {
	r.breakLine()
	for i := 0; i < r.baseIndent; i++ {
		r.cur.WriteString("    ")
	}
	r.cur.WriteString(indent)
	r.cur.WriteString(kw)
	r.ps = true
}

// indentLine starts a new line at (baseIndent + n) * 4 spaces.
func (r *rend) indentLine(n int) {
	r.breakLine()
	for i := 0; i < r.baseIndent+n; i++ {
		r.cur.WriteString("    ")
	}
	r.ps = false
}

func (r *rend) run() {
	for {
		t, ok := r.next()
		if !ok {
			break
		}

		switch t.kind {
		case tokLineCmt:
			// Attach to current line.
			r.cur.WriteByte(' ')
			r.cur.WriteString(t.val)
			r.ps = false

		case tokBlkCmt:
			r.clauseLine(t.val)

		case tokString:
			r.emit(t.val)

		case tokNumber:
			r.emit(t.val)

		case tokStar:
			if r.depth > r.stmtDepth {
				// Inside parens deeper than stmt level: *, no spaces (e.g. count(*))
				r.ps = false
				r.cur.WriteByte('*')
			} else {
				// SELECT *
				r.emit("*")
			}

		case tokOp:
			r.emitOp(t.val)

		case tokComma:
			r.ps = false
			r.cur.WriteByte(',')
			if r.depth == r.stmtDepth {
				switch {
				case r.inSelect:
					r.indentLine(1)
				case r.inSet:
					r.indentLine(1)
				default:
					r.ps = true // ", " via next emit
				}
			} else {
				r.ps = true
			}

		case tokLParen:
			// No space before ( when it's a function call
			// (preceding non-keyword word → treat as function).
			if sqlKeywords[r.prevWord] {
				r.emit("(")
			} else {
				r.ps = false
				r.cur.WriteByte('(')
			}
			r.ps = false
			r.depth++

			// If the next significant token is SELECT or WITH, this paren opens
			// a subquery. Switch to subquery formatting mode.
			nextW := r.peekWord()
			if nextW == "SELECT" || nextW == "WITH" {
				r.subStack = append(r.subStack, stmtFrame{
					inSelect:    r.inSelect,
					inWhere:     r.inWhere,
					inSet:       r.inSet,
					afterJoin:   r.afterJoin,
					afterDelete: r.afterDelete,
					stmtIdx:     r.stmtIdx,
					selectCols:  r.selectCols,
					caseLevel:   r.caseLevel,
					stmtDepth:   r.stmtDepth,
					baseIndent:  r.baseIndent,
				})
				r.stmtDepth = r.depth
				r.baseIndent++
				r.inSelect = false
				r.inWhere = false
				r.inSet = false
				r.afterJoin = false
				r.afterDelete = false
				r.stmtIdx = 0
				r.caseLevel = 0
				r.selectCols = preCountColsFrom(r.tokens, r.pos)
				// The opening paren is already on the current line; the next
				// clauseLine call (for SELECT/WITH) will break and indent.
			}

		case tokRParen:
			// If this closes a subquery paren, break the line and restore state.
			if len(r.subStack) > 0 && r.depth == r.stmtDepth {
				r.breakLine()
				f := r.subStack[len(r.subStack)-1]
				r.subStack = r.subStack[:len(r.subStack)-1]
				r.inSelect = f.inSelect
				r.inWhere = f.inWhere
				r.inSet = f.inSet
				r.afterJoin = f.afterJoin
				r.afterDelete = f.afterDelete
				// Reset stmtIdx so the next statement (e.g. the SELECT after a CTE)
			// does not get a blank-line separator — it's part of the same outer statement.
				r.stmtIdx = 0
				r.selectCols = f.selectCols
				r.caseLevel = f.caseLevel
				r.stmtDepth = f.stmtDepth
				r.baseIndent = f.baseIndent
				// Indent the closing paren at the restored base indent level.
				for i := 0; i < r.baseIndent; i++ {
					r.cur.WriteString("    ")
				}
			}
			r.ps = false
			r.cur.WriteByte(')')
			r.ps = true
			r.depth--

		case tokSemi:
			r.ps = false
			r.cur.WriteByte(';')
			// Blank line between semicolon-terminated statements.
			r.breakLine()
			r.lines = append(r.lines, "")
			r.inSelect = false
			r.inWhere = false
			r.inSet = false
			r.afterJoin = false
			r.stmtIdx = 0

		case tokWord:
			up := strings.ToUpper(t.val)
			display := t.val
			if sqlKeywords[up] {
				display = up
			}

			// GO batch separator (only at top level).
			if up == "GO" && r.depth == 0 {
				r.clauseLine("GO")
				r.inSelect = false
				r.inWhere = false
				r.inSet = false
				r.stmtIdx = 0 // next SELECT won't add a blank line
				r.prevWord = up
				continue
			}

			// Statement starters.
			if statementStart[up] && r.depth == r.stmtDepth {
				if r.stmtIdx > 0 {
					r.breakLine()
					if r.baseIndent == 0 {
						r.lines = append(r.lines, "") // blank line between top-level statements
					}
				}
				r.stmtIdx++
				r.inSelect = up == "SELECT"
				r.inWhere = false
				r.inSet = false
				r.afterJoin = false
				r.afterDelete = up == "DELETE"

				r.clauseLine(display)

				if up == "SELECT" && r.selectCols > 1 {
					r.indentLine(1)
				}
				r.prevWord = up
				continue
			}

			// UNION / INTERSECT / EXCEPT — own line; next SELECT follows immediately.
			if compoundFirst[up] && r.depth == r.stmtDepth {
				compound := display
				if r.peekWord() == "ALL" {
					r.next() // consume ALL
					compound = display + " ALL"
				}
				r.clauseLine(compound)
				// Reset stmtIdx so the next SELECT doesn't add a blank line.
				r.stmtIdx = 0
				r.inSelect = false
				r.prevWord = up
				continue
			}

			// GROUP BY / ORDER BY — clause line with compound keyword.
			if (up == "GROUP" || up == "ORDER") && r.depth == r.stmtDepth {
				compound := display
				if r.peekWord() == "BY" {
					r.next() // consume BY
					compound = display + " BY"
				}
				r.clauseLine(compound)
				r.inSelect = false
				r.inWhere = false
				r.prevWord = up
				continue
			}

			// JOIN keywords (INNER, LEFT, RIGHT, FULL, CROSS).
			if joinFirst[up] && r.depth == r.stmtDepth {
				// Consume JOIN.
				joinType := display
				if r.peekWord() == "JOIN" {
					r.next()
					joinType = display + " JOIN"
				}
				r.clauseLine(joinType)
				r.afterJoin = true
				r.inSelect = false
				r.prevWord = up
				continue
			}

			// Plain JOIN keyword.
			if up == "JOIN" && r.depth == r.stmtDepth {
				r.clauseLine("JOIN")
				r.afterJoin = true
				r.inSelect = false
				r.prevWord = up
				continue
			}

			// ON after JOIN — stays on the same line as the JOIN.
			if up == "ON" && r.afterJoin && r.depth == r.stmtDepth {
				r.afterJoin = false
				r.emit(display)
				r.prevWord = up
				continue
			}

			// SET keyword.
			if up == "SET" && r.depth == r.stmtDepth {
				r.clauseLine("SET")
				r.inSet = true
				r.inWhere = false
				r.prevWord = up
				continue
			}

			// FROM: special-case after DELETE (stays on same line).
			if up == "FROM" && r.afterDelete && r.depth == r.stmtDepth {
				r.emit("FROM")
				r.afterDelete = false
				r.prevWord = up
				continue
			}

			// Clause-break keywords.
			if clauseBreak[up] && r.depth == r.stmtDepth {
				r.clauseLine(display)
				wasWhere := up == "WHERE" || up == "HAVING"
				r.inWhere = wasWhere
				r.inSelect = false
				r.inSet = false
				r.afterJoin = false
				r.prevWord = up
				continue
			}

			// CASE expression handling (at statement depth only).
			if up == "CASE" && r.depth == r.stmtDepth {
				// Break to a new indented line when inside a SELECT list or SET clause.
				if r.inSelect || r.inSet {
					r.indentLine(1)
				}
				r.emit(display)
				r.caseLevel++
				r.prevWord = up
				continue
			}

			if (up == "WHEN" || up == "ELSE") && r.caseLevel > 0 && r.depth == r.stmtDepth {
				r.indentLine(2)
				r.emit(display)
				r.prevWord = up
				continue
			}

			if up == "END" && r.caseLevel > 0 && r.depth == r.stmtDepth {
				r.indentLine(1)
				r.emit(display)
				r.caseLevel--
				r.prevWord = up
				continue
			}

			// Default: emit as word.
			r.emit(display)
			r.prevWord = up
		}
	}
}
