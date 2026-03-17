package vim

import (
	"strings"
	"unicode"
)

// Pos is a cursor position (0-indexed).
type Pos struct{ Row, Col int }

// Buffer stores text as a slice of rune slices and manages the cursor and undo history.
type Buffer struct {
	lines     [][]rune
	row, col  int // cursor position
	undoStack []bufSnap
	redoStack []bufSnap
	undoLimit int
	register  string // unnamed yank register
	regLine   bool   // true = register holds line-wise content
}

type bufSnap struct {
	lines [][]rune
	row   int
	col   int
}

// NewBuffer creates an empty buffer (one blank line).
func NewBuffer() *Buffer {
	return &Buffer{
		lines:     [][]rune{{}},
		undoLimit: 100,
	}
}

// SetValue loads a string into the buffer and resets the cursor.
func (b *Buffer) SetValue(s string) {
	b.lines = splitLines(s)
	b.row, b.col = 0, 0
}

// Value returns the buffer content as a newline-joined string.
func (b *Buffer) Value() string {
	parts := make([]string, len(b.lines))
	for i, l := range b.lines {
		parts[i] = string(l)
	}
	return strings.Join(parts, "\n")
}

// LineCount returns the number of lines.
func (b *Buffer) LineCount() int { return len(b.lines) }

// CursorRow returns the current cursor row.
func (b *Buffer) CursorRow() int { return b.row }

// CursorCol returns the current cursor column.
func (b *Buffer) CursorCol() int { return b.col }

// SetCursor moves the cursor to the requested 0-based row/column, clamped to
// valid bounds for the current buffer.
func (b *Buffer) SetCursor(row, col int) {
	b.row = row
	b.col = col
	b.clamp()
}

// Line returns the rune slice for the given row (nil if out of range).
func (b *Buffer) Line(row int) []rune {
	if row < 0 || row >= len(b.lines) {
		return nil
	}
	return b.lines[row]
}

// Register returns the unnamed register content and its line-wise flag.
func (b *Buffer) Register() (string, bool) { return b.register, b.regLine }

// SetSize is a no-op on the buffer; viewport is managed by State.
func (b *Buffer) SetSize(_, _ int) {}

// ─── Snapshot / undo ──────────────────────────────────────────────────────────

// PushUndo snapshots the current state onto the undo stack and clears redo.
func (b *Buffer) PushUndo() {
	b.undoStack = append(b.undoStack, b.snap())
	if len(b.undoStack) > b.undoLimit {
		b.undoStack = b.undoStack[1:]
	}
	b.redoStack = nil
}

// Undo restores the previous snapshot.
func (b *Buffer) Undo() {
	if len(b.undoStack) == 0 {
		return
	}
	b.redoStack = append(b.redoStack, b.snap())
	s := b.undoStack[len(b.undoStack)-1]
	b.undoStack = b.undoStack[:len(b.undoStack)-1]
	b.restore(s)
}

// Redo re-applies the last undone change.
func (b *Buffer) Redo() {
	if len(b.redoStack) == 0 {
		return
	}
	b.undoStack = append(b.undoStack, b.snap())
	s := b.redoStack[len(b.redoStack)-1]
	b.redoStack = b.redoStack[:len(b.redoStack)-1]
	b.restore(s)
}

func (b *Buffer) snap() bufSnap {
	lines := make([][]rune, len(b.lines))
	for i, l := range b.lines {
		c := make([]rune, len(l))
		copy(c, l)
		lines[i] = c
	}
	return bufSnap{lines: lines, row: b.row, col: b.col}
}

func (b *Buffer) restore(s bufSnap) {
	b.lines = s.lines
	b.row, b.col = s.row, s.col
}

// ─── Cursor clamping ──────────────────────────────────────────────────────────

// clamp clamps the cursor to valid Normal-mode bounds.
func (b *Buffer) clamp() {
	if len(b.lines) == 0 {
		b.lines = [][]rune{{}}
	}
	if b.row < 0 {
		b.row = 0
	}
	if b.row >= len(b.lines) {
		b.row = len(b.lines) - 1
	}
	b.clampCol(false)
}

// clampInsert clamps the cursor to valid Insert-mode bounds (col may be one past end).
func (b *Buffer) clampInsert() {
	if b.row < 0 {
		b.row = 0
	}
	if b.row >= len(b.lines) {
		b.row = len(b.lines) - 1
	}
	b.clampCol(true)
}

func (b *Buffer) clampCol(insertMode bool) {
	line := b.lines[b.row]
	if b.col < 0 {
		b.col = 0
	}
	maxCol := len(line) - 1
	if insertMode {
		maxCol = len(line)
	}
	if maxCol < 0 {
		maxCol = 0
	}
	if b.col > maxCol {
		b.col = maxCol
	}
}

// ─── Insert-mode operations ───────────────────────────────────────────────────

// InsertText inserts an arbitrary string at the cursor, handling newlines.
// Intended for paste operations — does not interact with the yank register.
func (b *Buffer) InsertText(s string) {
	for _, r := range s {
		if r == '\r' {
			continue // strip CR from CRLF
		}
		if r == '\n' {
			b.InsertNewline()
		} else {
			b.InsertRune(r)
		}
	}
}

// InsertRune inserts a rune at the cursor in insert mode.
func (b *Buffer) InsertRune(r rune) {
	row, col := b.row, b.col
	line := b.lines[row]
	newLine := make([]rune, len(line)+1)
	copy(newLine, line[:col])
	newLine[col] = r
	copy(newLine[col+1:], line[col:])
	b.lines[row] = newLine
	b.col++
}

// InsertNewline splits the current line at the cursor.
func (b *Buffer) InsertNewline() {
	row, col := b.row, b.col
	line := b.lines[row]
	before := make([]rune, col)
	copy(before, line[:col])
	after := make([]rune, len(line)-col)
	copy(after, line[col:])
	b.lines[row] = before
	newLines := make([][]rune, len(b.lines)+1)
	copy(newLines, b.lines[:row+1])
	newLines[row+1] = after
	copy(newLines[row+2:], b.lines[row+1:])
	b.lines = newLines
	b.row = row + 1
	b.col = 0
}

// DeleteCharBefore deletes the character before the cursor (Backspace).
func (b *Buffer) DeleteCharBefore() {
	row, col := b.row, b.col
	if col > 0 {
		line := b.lines[row]
		newLine := make([]rune, len(line)-1)
		copy(newLine, line[:col-1])
		copy(newLine[col-1:], line[col:])
		b.lines[row] = newLine
		b.col--
	} else if row > 0 {
		prevLine := b.lines[row-1]
		curLine := b.lines[row]
		newCol := len(prevLine)
		joined := append(append([]rune{}, prevLine...), curLine...)
		newLines := make([][]rune, len(b.lines)-1)
		copy(newLines, b.lines[:row])
		copy(newLines[row:], b.lines[row+1:])
		newLines[row-1] = joined
		b.lines = newLines
		b.row = row - 1
		b.col = newCol
	}
}

// DeleteCharAtCursor deletes the character at the insert-mode cursor.
// If the cursor is at end-of-line, it joins with the next line.
func (b *Buffer) DeleteCharAtCursor() {
	row, col := b.row, b.col
	line := b.lines[row]
	if col < len(line) {
		newLine := make([]rune, len(line)-1)
		copy(newLine, line[:col])
		copy(newLine[col:], line[col+1:])
		b.lines[row] = newLine
		return
	}
	if row >= len(b.lines)-1 {
		return
	}
	nextLine := b.lines[row+1]
	joined := append(append([]rune{}, line...), nextLine...)
	newLines := make([][]rune, len(b.lines)-1)
	copy(newLines, b.lines[:row])
	newLines[row] = joined
	copy(newLines[row+1:], b.lines[row+2:])
	b.lines = newLines
	b.clampInsert()
}

// ─── Normal-mode operations ───────────────────────────────────────────────────

// OpenLineBelow inserts a blank line after the cursor row and moves there.
func (b *Buffer) OpenLineBelow() {
	row := b.row
	newLines := make([][]rune, len(b.lines)+1)
	copy(newLines, b.lines[:row+1])
	newLines[row+1] = []rune{}
	copy(newLines[row+2:], b.lines[row+1:])
	b.lines = newLines
	b.row = row + 1
	b.col = 0
}

// OpenLineAbove inserts a blank line before the cursor row and moves there.
func (b *Buffer) OpenLineAbove() {
	row := b.row
	newLines := make([][]rune, len(b.lines)+1)
	copy(newLines, b.lines[:row])
	newLines[row] = []rune{}
	copy(newLines[row+1:], b.lines[row:])
	b.lines = newLines
	b.row = row
	b.col = 0
}

// ReplaceChars replaces count characters starting at the cursor with r.
// If there are fewer than count characters remaining on the line, only the
// available characters are replaced. Cursor stays at the last replaced char.
func (b *Buffer) ReplaceChars(r rune, count int) {
	row, col := b.row, b.col
	line := b.lines[row]
	if len(line) == 0 || col >= len(line) {
		return
	}
	end := col + count
	if end > len(line) {
		end = len(line)
	}
	newLine := make([]rune, len(line))
	copy(newLine, line)
	for i := col; i < end; i++ {
		newLine[i] = r
	}
	b.lines[row] = newLine
	b.col = end - 1
	b.clamp()
}

// DeleteCharUnder deletes the character under the cursor (x).
func (b *Buffer) DeleteCharUnder() {
	row, col := b.row, b.col
	line := b.lines[row]
	if len(line) == 0 {
		return
	}
	if col >= len(line) {
		col = len(line) - 1
	}
	newLine := make([]rune, len(line)-1)
	copy(newLine, line[:col])
	copy(newLine[col:], line[col+1:])
	b.lines[row] = newLine
	b.clamp()
}

// DeleteLines deletes count lines at the cursor row, saving to the register (linewise).
// JoinLines joins count lines starting at the cursor row into one line,
// separating each joined pair with a single space (trailing/leading whitespace
// is trimmed from the appended line, matching vim's J behaviour).
// The cursor is left at the position of the first inserted space.
func (b *Buffer) JoinLines(count int) {
	if count < 2 {
		count = 2
	}
	for i := 1; i < count; i++ {
		if b.row+1 >= len(b.lines) {
			break
		}
		cur := b.lines[b.row]
		next := b.lines[b.row+1]

		// Determine the join point: end of current line.
		joinCol := len(cur)

		// Trim leading whitespace from next line (vim trims it).
		trimmed := []rune(strings.TrimLeft(string(next), " \t"))

		// Build the joined line.
		var joined []rune
		if len(cur) > 0 && joinCol > 0 {
			joined = append(joined, cur...)
			joined = append(joined, ' ')
		} else {
			joined = append(joined, cur...)
		}
		joined = append(joined, trimmed...)

		// Replace current line and remove next.
		newLines := make([][]rune, len(b.lines)-1)
		copy(newLines, b.lines[:b.row])
		newLines[b.row] = joined
		copy(newLines[b.row+1:], b.lines[b.row+2:])
		b.lines = newLines

		// Leave cursor at the space that was inserted (or end of original line).
		if joinCol > 0 {
			b.col = joinCol // position of the space
		}
	}
	b.clamp()
}

func (b *Buffer) DeleteLines(count int) {
	row := b.row
	end := row + count
	if end > len(b.lines) {
		end = len(b.lines)
	}
	var sb strings.Builder
	for i := row; i < end; i++ {
		sb.WriteString(string(b.lines[i]))
		sb.WriteByte('\n')
	}
	b.register = sb.String()
	b.regLine = true
	newLines := make([][]rune, len(b.lines)-(end-row))
	copy(newLines, b.lines[:row])
	copy(newLines[row:], b.lines[end:])
	if len(newLines) == 0 {
		newLines = [][]rune{{}}
	}
	b.lines = newLines
	b.clamp()
}

// ChangeLines clears count lines at the cursor row (like cc: empties content, stays on line).
func (b *Buffer) ChangeLines(count int) {
	row := b.row
	end := row + count
	if end > len(b.lines) {
		end = len(b.lines)
	}
	// Save to register.
	var sb strings.Builder
	for i := row; i < end; i++ {
		sb.WriteString(string(b.lines[i]))
		sb.WriteByte('\n')
	}
	b.register = sb.String()
	b.regLine = true
	// Clear first line; remove extra lines.
	b.lines[row] = []rune{}
	if end > row+1 {
		newLines := make([][]rune, len(b.lines)-(end-row-1))
		copy(newLines, b.lines[:row+1])
		copy(newLines[row+1:], b.lines[end:])
		b.lines = newLines
	}
	b.row = row
	b.col = 0
}

// YankLines yanks count lines at the cursor row to the register (linewise).
func (b *Buffer) YankLines(count int) {
	row := b.row
	end := row + count
	if end > len(b.lines) {
		end = len(b.lines)
	}
	var sb strings.Builder
	for i := row; i < end; i++ {
		sb.WriteString(string(b.lines[i]))
		sb.WriteByte('\n')
	}
	b.register = sb.String()
	b.regLine = true
}

// DeleteRange deletes text from start to end inclusive, saves to register (charwise).
func (b *Buffer) DeleteRange(start, end Pos) {
	if start.Row > end.Row || (start.Row == end.Row && start.Col > end.Col) {
		start, end = end, start
	}
	b.register = b.extractRange(start, end)
	b.regLine = false
	if start.Row == end.Row {
		line := b.lines[start.Row]
		sc := clampInt(start.Col, 0, len(line))
		ec := clampInt(end.Col+1, 0, len(line))
		newLine := make([]rune, sc+len(line)-ec)
		copy(newLine, line[:sc])
		copy(newLine[sc:], line[ec:])
		b.lines[start.Row] = newLine
	} else {
		startLine := b.lines[start.Row]
		endLine := b.lines[end.Row]
		sc := clampInt(start.Col, 0, len(startLine))
		ec := clampInt(end.Col+1, 0, len(endLine))
		merged := append(append([]rune{}, startLine[:sc]...), endLine[ec:]...)
		newLines := make([][]rune, 0, len(b.lines)-(end.Row-start.Row))
		newLines = append(newLines, b.lines[:start.Row]...)
		newLines = append(newLines, merged)
		newLines = append(newLines, b.lines[end.Row+1:]...)
		if len(newLines) == 0 {
			newLines = [][]rune{{}}
		}
		b.lines = newLines
	}
	b.row, b.col = start.Row, start.Col
	b.clamp()
}

// YankRange saves text from start to end inclusive to the register (charwise).
func (b *Buffer) YankRange(start, end Pos) {
	if start.Row > end.Row || (start.Row == end.Row && start.Col > end.Col) {
		start, end = end, start
	}
	b.register = b.extractRange(start, end)
	b.regLine = false
}

// DeleteToEndOfLine deletes from cursor to end of line.
func (b *Buffer) DeleteToEndOfLine() {
	row, col := b.row, b.col
	line := b.lines[row]
	if col >= len(line) {
		return
	}
	b.register = string(line[col:])
	b.regLine = false
	b.lines[row] = line[:col]
	b.clamp()
}

// YankToEndOfLine saves from cursor to end of line in the register (charwise).
func (b *Buffer) YankToEndOfLine() {
	row, col := b.row, b.col
	line := b.lines[row]
	if col >= len(line) {
		b.register = ""
		b.regLine = false
		return
	}
	b.register = string(line[col:])
	b.regLine = false
}

// PasteAfter pastes the register after the cursor.
func (b *Buffer) PasteAfter() {
	if b.register == "" {
		return
	}
	if b.regLine {
		row := b.row
		pasteLines := splitLines(strings.TrimRight(b.register, "\n"))
		newLines := make([][]rune, len(b.lines)+len(pasteLines))
		copy(newLines, b.lines[:row+1])
		for i, l := range pasteLines {
			newLines[row+1+i] = l
		}
		copy(newLines[row+1+len(pasteLines):], b.lines[row+1:])
		b.lines = newLines
		b.row = row + 1
		b.col = 0
	} else {
		row, col := b.row, b.col
		line := b.lines[row]
		insertAt := col + 1
		if insertAt > len(line) {
			insertAt = len(line)
		}
		b.charPasteAt(row, insertAt, line)
	}
}

// charPasteAt inserts the (charwise) register at insertAt on row,
// handling multi-line register content by splitting into new lines.
func (b *Buffer) charPasteAt(row, insertAt int, line []rune) {
	parts := splitLines(b.register)
	if len(parts) == 1 {
		ins := parts[0]
		newLine := make([]rune, len(line)+len(ins))
		copy(newLine, line[:insertAt])
		copy(newLine[insertAt:], ins)
		copy(newLine[insertAt+len(ins):], line[insertAt:])
		b.lines[row] = newLine
		if len(ins) > 0 {
			b.col = insertAt + len(ins) - 1
		}
	} else {
		tail := append([]rune(nil), line[insertAt:]...)
		first := append(append([]rune(nil), line[:insertAt]...), parts[0]...)
		last := append(append([]rune(nil), parts[len(parts)-1]...), tail...)
		newLines := make([][]rune, len(b.lines)+len(parts)-1)
		copy(newLines, b.lines[:row])
		newLines[row] = first
		for i := 1; i < len(parts)-1; i++ {
			newLines[row+i] = append([]rune(nil), parts[i]...)
		}
		newLines[row+len(parts)-1] = last
		copy(newLines[row+len(parts):], b.lines[row+1:])
		b.lines = newLines
		b.row = row + len(parts) - 1
		b.col = len(parts[len(parts)-1])
		if b.col > 0 {
			b.col--
		}
	}
	b.clamp()
}

// PasteBefore pastes the register before the cursor.
func (b *Buffer) PasteBefore() {
	if b.register == "" {
		return
	}
	if b.regLine {
		row := b.row
		pasteLines := splitLines(strings.TrimRight(b.register, "\n"))
		newLines := make([][]rune, len(b.lines)+len(pasteLines))
		copy(newLines, b.lines[:row])
		for i, l := range pasteLines {
			newLines[row+i] = l
		}
		copy(newLines[row+len(pasteLines):], b.lines[row:])
		b.lines = newLines
		b.row = row
		b.col = 0
	} else {
		row, col := b.row, b.col
		line := b.lines[row]
		b.charPasteAt(row, col, line)
	}
}

// IndentLines adds 4 spaces to the start of each line in [startRow, endRow].
func (b *Buffer) IndentLines(startRow, endRow int) {
	for row := startRow; row <= endRow && row < len(b.lines); row++ {
		b.lines[row] = append([]rune("    "), b.lines[row]...)
	}
}

// DedentLines removes up to 4 leading spaces from each line in [startRow, endRow].
func (b *Buffer) DedentLines(startRow, endRow int) {
	for row := startRow; row <= endRow && row < len(b.lines); row++ {
		n := 0
		for n < 4 && n < len(b.lines[row]) && b.lines[row][n] == ' ' {
			n++
		}
		b.lines[row] = b.lines[row][n:]
	}
}

// ─── Motions ──────────────────────────────────────────────────────────────────

func (b *Buffer) MoveLeft(n int) {
	b.col -= n
	if b.col < 0 {
		b.col = 0
	}
}

func (b *Buffer) MoveRight(n int) {
	line := b.lines[b.row]
	maxCol := len(line) - 1
	if maxCol < 0 {
		maxCol = 0
	}
	b.col += n
	if b.col > maxCol {
		b.col = maxCol
	}
}

func (b *Buffer) MoveUp(n int) {
	b.row -= n
	if b.row < 0 {
		b.row = 0
	}
	b.clamp()
}

func (b *Buffer) MoveDown(n int) {
	b.row += n
	if b.row >= len(b.lines) {
		b.row = len(b.lines) - 1
	}
	b.clamp()
}

func (b *Buffer) MoveLineStart() { b.col = 0 }

func (b *Buffer) MoveLineEnd() {
	line := b.lines[b.row]
	if len(line) == 0 {
		b.col = 0
	} else {
		b.col = len(line) - 1
	}
}

func (b *Buffer) MoveFirstNonBlank() {
	for i, r := range b.lines[b.row] {
		if !unicode.IsSpace(r) {
			b.col = i
			return
		}
	}
	b.col = 0
}

func (b *Buffer) MoveFirstLine() { b.row, b.col = 0, 0 }

func (b *Buffer) MoveLastLine() {
	b.row = len(b.lines) - 1
	b.clamp()
}

func (b *Buffer) MoveToLine(n int) {
	b.row = n - 1
	if b.row < 0 {
		b.row = 0
	}
	if b.row >= len(b.lines) {
		b.row = len(b.lines) - 1
	}
	b.clamp()
}

// WordForward moves n words forward (to start of next word).
func (b *Buffer) WordForward(n int) {
	for i := 0; i < n; i++ {
		b.wordForwardOnce()
	}
}

func (b *Buffer) wordForwardOnce() {
	row, col := b.row, b.col
	line := b.lines[row]
	if col < len(line) {
		if isWord(line[col]) {
			for col < len(line) && isWord(line[col]) {
				col++
			}
		} else if !unicode.IsSpace(line[col]) {
			for col < len(line) && !isWord(line[col]) && !unicode.IsSpace(line[col]) {
				col++
			}
		}
	}
	for {
		for col < len(line) && unicode.IsSpace(line[col]) {
			col++
		}
		if col < len(line) {
			b.row, b.col = row, col
			return
		}
		row++
		if row >= len(b.lines) {
			last := len(b.lines) - 1
			b.row = last
			b.col = max(0, len(b.lines[last])-1)
			return
		}
		line = b.lines[row]
		col = 0
	}
}

// WordForwardPos returns where the 'w' motion would land without moving.
func (b *Buffer) WordForwardPos() Pos {
	sr, sc := b.row, b.col
	b.wordForwardOnce()
	p := Pos{b.row, b.col}
	b.row, b.col = sr, sc
	return p
}

// WordBackward moves n words backward (to start of previous word).
func (b *Buffer) WordBackward(n int) {
	for i := 0; i < n; i++ {
		b.wordBackwardOnce()
	}
}

func (b *Buffer) wordBackwardOnce() {
	row, col := b.row, b.col
	if col == 0 && row == 0 {
		return
	}
	if col == 0 {
		row--
		col = len(b.lines[row])
	}
	line := b.lines[row]
	col-- // step back one
	// Skip whitespace backward (possibly across lines).
	for {
		for col >= 0 && unicode.IsSpace(line[col]) {
			col--
		}
		if col >= 0 {
			break
		}
		if row == 0 {
			b.row, b.col = 0, 0
			return
		}
		row--
		line = b.lines[row]
		col = len(line) - 1
	}
	// Skip word chars backward.
	if isWord(line[col]) {
		for col > 0 && isWord(line[col-1]) {
			col--
		}
	} else {
		for col > 0 && !isWord(line[col-1]) && !unicode.IsSpace(line[col-1]) {
			col--
		}
	}
	b.row, b.col = row, col
}

// WordBackwardPos returns where the 'b' motion would land without moving.
func (b *Buffer) WordBackwardPos() Pos {
	sr, sc := b.row, b.col
	b.wordBackwardOnce()
	p := Pos{b.row, b.col}
	b.row, b.col = sr, sc
	return p
}

// WordEnd moves to end of word n times.
func (b *Buffer) WordEnd(n int) {
	for i := 0; i < n; i++ {
		b.wordEndOnce()
	}
}

func (b *Buffer) wordEndOnce() {
	row, col := b.row, b.col
	line := b.lines[row]
	col++
	// Cross lines if needed.
	for col >= len(line) {
		row++
		if row >= len(b.lines) {
			b.row = len(b.lines) - 1
			b.clamp()
			return
		}
		line = b.lines[row]
		col = 0
	}
	// Skip whitespace.
	for col < len(line) && unicode.IsSpace(line[col]) {
		col++
		if col >= len(line) && row+1 < len(b.lines) {
			row++
			line = b.lines[row]
			col = 0
		}
	}
	// Move to end of word group.
	if col < len(line) {
		if isWord(line[col]) {
			for col+1 < len(line) && isWord(line[col+1]) {
				col++
			}
		} else {
			for col+1 < len(line) && !isWord(line[col+1]) && !unicode.IsSpace(line[col+1]) {
				col++
			}
		}
	}
	b.row, b.col = row, col
}

// WordEndPos returns where the 'e' motion would land without moving.
func (b *Buffer) WordEndPos() Pos {
	sr, sc := b.row, b.col
	b.wordEndOnce()
	p := Pos{b.row, b.col}
	b.row, b.col = sr, sc
	return p
}

func (b *Buffer) ParagraphForward(n int) {
	for i := 0; i < n; i++ {
		b.paragraphForwardOnce()
	}
}

func (b *Buffer) paragraphForwardOnce() {
	for b.row < len(b.lines)-1 {
		b.row++
		if len(strings.TrimSpace(string(b.lines[b.row]))) == 0 {
			b.col = 0
			return
		}
	}
	b.row = len(b.lines) - 1
	b.col = 0
}

func (b *Buffer) ParagraphBackward(n int) {
	for i := 0; i < n; i++ {
		b.paragraphBackwardOnce()
	}
}

func (b *Buffer) paragraphBackwardOnce() {
	for b.row > 0 {
		b.row--
		if len(strings.TrimSpace(string(b.lines[b.row]))) == 0 {
			b.col = 0
			return
		}
	}
	b.row, b.col = 0, 0
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func (b *Buffer) extractRange(start, end Pos) string {
	if start.Row == end.Row {
		line := b.lines[start.Row]
		sc := clampInt(start.Col, 0, len(line))
		ec := clampInt(end.Col+1, 0, len(line))
		return string(line[sc:ec])
	}
	var sb strings.Builder
	sl := b.lines[start.Row]
	sc := clampInt(start.Col, 0, len(sl))
	sb.WriteString(string(sl[sc:]))
	for i := start.Row + 1; i < end.Row; i++ {
		sb.WriteByte('\n')
		sb.WriteString(string(b.lines[i]))
	}
	sb.WriteByte('\n')
	el := b.lines[end.Row]
	ec := clampInt(end.Col+1, 0, len(el))
	sb.WriteString(string(el[:ec]))
	return sb.String()
}

func splitLines(s string) [][]rune {
	raw := strings.Split(s, "\n")
	lines := make([][]rune, len(raw))
	for i, l := range raw {
		lines[i] = []rune(l)
	}
	if len(lines) == 0 {
		lines = [][]rune{{}}
	}
	return lines
}

func isWord(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
