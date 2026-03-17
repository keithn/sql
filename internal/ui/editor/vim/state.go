package vim

// Mode is the current vim editing mode.
type Mode int

const (
	ModeNormal Mode = iota
	ModeInsert
	ModeVisual
	ModeVisualLine
)

func (m Mode) String() string {
	switch m {
	case ModeNormal:
		return "NORMAL"
	case ModeInsert:
		return "INSERT"
	case ModeVisual:
		return "VISUAL"
	case ModeVisualLine:
		return "V-LINE"
	default:
		return ""
	}
}

// Snapshot captures enough vim state to restore a tab after a temporary mode
// toggle away from vim.
type Snapshot struct {
	Cursor        Pos
	Mode          Mode
	Anchor        Pos
	TopRow        int
	LeftCol       int
	CountStr      string
	Operator      rune
	OperatorCount int
	PendingG      bool
	PendingR      bool
}

// State is the per-tab vim key-input state machine.
type State struct {
	Mode Mode
	Buf  *Buffer

	// Count prefix (accumulated digit keys).
	countStr string

	// Operator-pending state.
	operator      rune // 'd', 'c', 'y', or 0
	operatorCount int  // count saved before the operator key

	// Pending 'g' prefix.
	pendingG bool

	// Pending 'r' replace-char.
	pendingR bool

	// Visual mode anchor.
	Anchor Pos

	// Viewport: first visible row and first visible column.
	TopRow  int
	LeftCol int

	// Display dimensions (set by editor via SetSize).
	width  int
	height int
}

// NewState creates a State with an empty buffer.
func NewState() *State {
	return &State{
		Mode: ModeNormal,
		Buf:  NewBuffer(),
	}
}

// SetSize records display dimensions for viewport scrolling.
func (s *State) SetSize(w, h int) {
	s.width = w
	s.height = h
}

// Snapshot returns the current vim state for round-trip restoration.
func (s *State) Snapshot() Snapshot {
	if s == nil || s.Buf == nil {
		return Snapshot{}
	}
	return Snapshot{
		Cursor:        Pos{Row: s.Buf.row, Col: s.Buf.col},
		Mode:          s.Mode,
		Anchor:        s.Anchor,
		TopRow:        s.TopRow,
		LeftCol:       s.LeftCol,
		CountStr:      s.countStr,
		Operator:      s.operator,
		OperatorCount: s.operatorCount,
		PendingG:      s.pendingG,
		PendingR:      s.pendingR,
	}
}

// RestoreSnapshot restores previously captured vim state.
func (s *State) RestoreSnapshot(snapshot Snapshot) {
	if s == nil || s.Buf == nil {
		return
	}
	s.Buf.SetCursor(snapshot.Cursor.Row, snapshot.Cursor.Col)
	s.Mode = snapshot.Mode
	s.Anchor = clampPosToBuffer(s.Buf, snapshot.Anchor)
	s.TopRow = snapshot.TopRow
	if s.TopRow < 0 {
		s.TopRow = 0
	}
	maxTop := max(0, s.Buf.LineCount()-1)
	if s.TopRow > maxTop {
		s.TopRow = maxTop
	}
	s.LeftCol = snapshot.LeftCol
	if s.LeftCol < 0 {
		s.LeftCol = 0
	}
	s.countStr = snapshot.CountStr
	s.operator = snapshot.Operator
	s.operatorCount = snapshot.OperatorCount
	s.pendingG = snapshot.PendingG
	s.pendingR = snapshot.PendingR
}

// ModeString returns the mode label for the status bar.
func (s *State) ModeString() string { return s.Mode.String() }

// SelectionRange returns the normalised (start, end) of the visual selection.
// For V-LINE, start.Col and end.Col are the full line bounds.
func (s *State) SelectionRange() (start, end Pos) {
	cur := Pos{s.Buf.row, s.Buf.col}
	anchor := s.Anchor
	if s.Mode == ModeVisualLine {
		r1, r2 := anchor.Row, cur.Row
		if r1 > r2 {
			r1, r2 = r2, r1
		}
		return Pos{Row: r1, Col: 0},
			Pos{Row: r2, Col: max(0, len(s.Buf.Line(r2))-1)}
	}
	if anchor.Row > cur.Row || (anchor.Row == cur.Row && anchor.Col > cur.Col) {
		return cur, anchor
	}
	return anchor, cur
}

// ScrollToReveal adjusts TopRow so the cursor is within the visible area.
func (s *State) ScrollToReveal(height int) {
	if height <= 0 {
		return
	}
	row := s.Buf.row
	if row < s.TopRow {
		s.TopRow = row
	}
	if row >= s.TopRow+height {
		s.TopRow = row - height + 1
	}
	if s.TopRow < 0 {
		s.TopRow = 0
	}
}

// ScrollToRevealHoriz adjusts LeftCol so the cursor column is within the
// visible content width.
func (s *State) ScrollToRevealHoriz(contentWidth int) {
	if contentWidth <= 0 {
		return
	}
	col := s.Buf.col
	if col < s.LeftCol {
		s.LeftCol = col
	}
	if col >= s.LeftCol+contentWidth {
		s.LeftCol = col - contentWidth + 1
	}
	if s.LeftCol < 0 {
		s.LeftCol = 0
	}
}

// HandleKey processes a single key in the current mode.
// Returns true if the key was consumed (should not be forwarded elsewhere).
func (s *State) HandleKey(key string) bool {
	switch s.Mode {
	case ModeNormal:
		return s.handleNormal(key)
	case ModeInsert:
		return s.handleInsert(key)
	case ModeVisual, ModeVisualLine:
		return s.handleVisual(key)
	}
	return false
}

// ─── Count helpers ────────────────────────────────────────────────────────────

func (s *State) count() int {
	if s.countStr == "" {
		return 1
	}
	n := 0
	for _, c := range s.countStr {
		n = n*10 + int(c-'0')
	}
	if n == 0 {
		n = 1
	}
	return n
}

// totalCount multiplies the pre-operator count by the post-operator motion count.
func (s *State) totalCount() int {
	oc := s.operatorCount
	if oc == 0 {
		oc = 1
	}
	return oc * s.count()
}

func (s *State) resetMotion() {
	s.countStr = ""
	s.operatorCount = 0
	s.operator = 0
	s.pendingG = false
	s.pendingR = false
}

// ─── Normal mode ──────────────────────────────────────────────────────────────

func (s *State) handleNormal(key string) bool {
	// Count accumulation: non-zero digits always; '0' only when count already started.
	if len(key) == 1 && key[0] >= '1' && key[0] <= '9' {
		s.countStr += key
		return true
	}
	if key == "0" && s.countStr != "" {
		s.countStr += "0"
		return true
	}

	// Pending 'g' prefix: wait for second key.
	if s.pendingG {
		s.pendingG = false
		if key == "g" {
			s.Buf.MoveFirstLine()
		}
		s.resetMotion()
		return true
	}

	// Pending 'r' replace-char: next key is the replacement character.
	if s.pendingR {
		n := s.count()
		s.resetMotion()
		if key != "esc" && len([]rune(key)) == 1 {
			r := []rune(key)[0]
			if !isCtrl(r) {
				s.Buf.PushUndo()
				s.Buf.ReplaceChars(r, n)
			}
		}
		return true
	}

	// Operator-pending: next key must be a motion or second operator.
	if s.operator != 0 {
		s.applyOperatorMotion(key)
		return true
	}

	n := s.count()

	switch key {
	// ─── Movement ───────────────────────────────────────────────────────────
	case "h", "left":
		s.Buf.MoveLeft(n)
		s.resetMotion()
	case "l", "right":
		s.Buf.MoveRight(n)
		s.resetMotion()
	case "j", "down":
		s.Buf.MoveDown(n)
		s.resetMotion()
	case "k", "up":
		s.Buf.MoveUp(n)
		s.resetMotion()
	case "w", "W":
		s.Buf.WordForward(n)
		s.resetMotion()
	case "b", "B":
		s.Buf.WordBackward(n)
		s.resetMotion()
	case "e", "E":
		s.Buf.WordEnd(n)
		s.resetMotion()
	case "0":
		s.Buf.MoveLineStart()
		s.resetMotion()
	case "^":
		s.Buf.MoveFirstNonBlank()
		s.resetMotion()
	case "$":
		s.Buf.MoveLineEnd()
		s.resetMotion()
	case "g":
		s.pendingG = true
	case "G":
		if s.countStr != "" {
			s.Buf.MoveToLine(n)
		} else {
			s.Buf.MoveLastLine()
		}
		s.resetMotion()
	case "}":
		s.Buf.ParagraphForward(n)
		s.resetMotion()
	case "{":
		s.Buf.ParagraphBackward(n)
		s.resetMotion()
	case "ctrl+d":
		s.Buf.MoveDown(max(1, s.height/2) * n)
		s.resetMotion()
	case "ctrl+u":
		s.Buf.MoveUp(max(1, s.height/2) * n)
		s.resetMotion()

	// ─── Enter insert mode ──────────────────────────────────────────────────
	case "i":
		s.Buf.PushUndo()
		s.Mode = ModeInsert
		s.resetMotion()
	case "I":
		s.Buf.PushUndo()
		s.Buf.MoveFirstNonBlank()
		s.Mode = ModeInsert
		s.resetMotion()
	case "a":
		s.Buf.PushUndo()
		// Move one past cursor for "after".
		line := s.Buf.Line(s.Buf.row)
		if s.Buf.col < len(line) {
			s.Buf.col++
		}
		s.Buf.clampInsert()
		s.Mode = ModeInsert
		s.resetMotion()
	case "A":
		s.Buf.PushUndo()
		line := s.Buf.Line(s.Buf.row)
		s.Buf.col = len(line) // one past last char = after last
		s.Buf.clampInsert()
		s.Mode = ModeInsert
		s.resetMotion()
	case "o":
		s.Buf.PushUndo()
		s.Buf.OpenLineBelow()
		s.Mode = ModeInsert
		s.resetMotion()
	case "O":
		s.Buf.PushUndo()
		s.Buf.OpenLineAbove()
		s.Mode = ModeInsert
		s.resetMotion()

	// ─── Editing ────────────────────────────────────────────────────────────
	case "r":
		s.pendingR = true
		// Keep countStr so ReplaceChars uses it; don't resetMotion here.

	case "x":
		s.Buf.PushUndo()
		for i := 0; i < n; i++ {
			s.Buf.DeleteCharUnder()
		}
		s.resetMotion()
	case "X":
		s.Buf.PushUndo()
		for i := 0; i < n; i++ {
			s.Buf.DeleteCharBefore()
		}
		s.resetMotion()
	case "J": // join current line with next (count: join N lines)
		s.Buf.PushUndo()
		s.Buf.JoinLines(n)
		s.resetMotion()

	case "D": // delete to end of line (= d$)
		s.Buf.PushUndo()
		s.Buf.DeleteToEndOfLine()
		s.resetMotion()
	case "C": // change to end of line (= c$)
		s.Buf.PushUndo()
		s.Buf.DeleteToEndOfLine()
		s.Mode = ModeInsert
	case "d":
		s.operatorCount = n
		s.operator = 'd'
		s.countStr = "" // reset for motion count
	case "c":
		s.operatorCount = n
		s.operator = 'c'
		s.countStr = ""
	case "y":
		s.operatorCount = n
		s.operator = 'y'
		s.countStr = ""
	case "p":
		s.Buf.PushUndo()
		for i := 0; i < n; i++ {
			s.Buf.PasteAfter()
		}
		s.resetMotion()
	case "P":
		s.Buf.PushUndo()
		for i := 0; i < n; i++ {
			s.Buf.PasteBefore()
		}
		s.resetMotion()
	case "u":
		s.Buf.Undo()
		s.resetMotion()
	case "ctrl+r":
		s.Buf.Redo()
		s.resetMotion()

	// ─── Visual mode ────────────────────────────────────────────────────────
	case "v":
		s.Mode = ModeVisual
		s.Anchor = Pos{s.Buf.row, s.Buf.col}
		s.resetMotion()
	case "V":
		s.Mode = ModeVisualLine
		s.Anchor = Pos{s.Buf.row, s.Buf.col}
		s.resetMotion()

	case "esc":
		s.resetMotion()
	}

	return true // Normal mode always consumes the key.
}

// applyOperatorMotion applies the pending operator with a motion key.
func (s *State) applyOperatorMotion(key string) {
	op := s.operator
	tc := s.totalCount()

	switch {
	// Double-operator: dd, cc, yy.
	case op == 'd' && key == "d":
		s.resetMotion()
		s.Buf.PushUndo()
		s.Buf.DeleteLines(tc)

	case op == 'c' && key == "c":
		s.resetMotion()
		s.Buf.PushUndo()
		s.Buf.ChangeLines(tc)
		s.Mode = ModeInsert

	case op == 'y' && key == "y":
		s.resetMotion()
		s.Buf.YankLines(tc)

	// End of line.
	case key == "$":
		s.resetMotion()
		cur := Pos{s.Buf.row, s.Buf.col}
		switch op {
		case 'd':
			s.Buf.PushUndo()
			s.Buf.DeleteToEndOfLine()
		case 'c':
			s.Buf.PushUndo()
			s.Buf.DeleteToEndOfLine()
			s.Mode = ModeInsert
		case 'y':
			s.Buf.YankToEndOfLine()
			s.Buf.row, s.Buf.col = cur.Row, cur.Col
		}

	// Word forward (w/W): delete/change/yank to start of next word.
	case key == "w" || key == "W":
		s.resetMotion()
		for i := 0; i < tc; i++ {
			cur := Pos{s.Buf.row, s.Buf.col}
			end := s.Buf.WordForwardPos()
			// dw deletes up to (not including) the next word start.
			// Adjust end back by one char on same line.
			if end.Row == cur.Row && end.Col > 0 {
				end.Col--
			} else if end.Row > cur.Row {
				// Cross-line: delete to end of current line.
				end = Pos{Row: cur.Row, Col: max(0, len(s.Buf.Line(cur.Row))-1)}
			}
			switch op {
			case 'd':
				if i == 0 {
					s.Buf.PushUndo()
				}
				s.Buf.DeleteRange(cur, end)
			case 'c':
				if i == 0 {
					s.Buf.PushUndo()
				}
				s.Buf.DeleteRange(cur, end)
			case 'y':
				s.Buf.YankRange(cur, end)
				s.Buf.row, s.Buf.col = cur.Row, cur.Col
			}
		}
		if op == 'c' {
			s.Mode = ModeInsert
		}

	// Word backward (b/B).
	case key == "b" || key == "B":
		s.resetMotion()
		cur := Pos{s.Buf.row, s.Buf.col}
		startPos := s.Buf.WordBackwardPos()
		switch op {
		case 'd':
			s.Buf.PushUndo()
			s.Buf.DeleteRange(startPos, Pos{Row: cur.Row, Col: cur.Col - 1})
		case 'c':
			s.Buf.PushUndo()
			s.Buf.DeleteRange(startPos, Pos{Row: cur.Row, Col: cur.Col - 1})
			s.Mode = ModeInsert
		case 'y':
			s.Buf.YankRange(startPos, Pos{Row: cur.Row, Col: cur.Col - 1})
			s.Buf.row, s.Buf.col = cur.Row, cur.Col
		}

	// Word end (e/E).
	case key == "e" || key == "E":
		s.resetMotion()
		cur := Pos{s.Buf.row, s.Buf.col}
		endPos := s.Buf.WordEndPos()
		switch op {
		case 'd':
			s.Buf.PushUndo()
			s.Buf.DeleteRange(cur, endPos)
		case 'c':
			s.Buf.PushUndo()
			s.Buf.DeleteRange(cur, endPos)
			s.Mode = ModeInsert
		case 'y':
			s.Buf.YankRange(cur, endPos)
			s.Buf.row, s.Buf.col = cur.Row, cur.Col
		}

	// Motion '0' (line start).
	case key == "0":
		s.resetMotion()
		cur := Pos{s.Buf.row, s.Buf.col}
		lineStart := Pos{Row: cur.Row, Col: 0}
		switch op {
		case 'd':
			s.Buf.PushUndo()
			s.Buf.DeleteRange(lineStart, Pos{Row: cur.Row, Col: cur.Col - 1})
		case 'c':
			s.Buf.PushUndo()
			s.Buf.DeleteRange(lineStart, Pos{Row: cur.Row, Col: cur.Col - 1})
			s.Mode = ModeInsert
		case 'y':
			s.Buf.YankRange(lineStart, Pos{Row: cur.Row, Col: cur.Col - 1})
			s.Buf.row, s.Buf.col = cur.Row, cur.Col
		}

	default:
		// Unrecognised motion — cancel.
		s.resetMotion()
	}
}

// ─── Insert mode ─────────────────────────────────────────────────────────────

func (s *State) handleInsert(key string) bool {
	switch key {
	case "esc":
		s.Mode = ModeNormal
		// Clamp col to normal-mode bounds (one past end → last char).
		line := s.Buf.Line(s.Buf.row)
		if s.Buf.col > 0 && s.Buf.col >= len(line) {
			s.Buf.col = len(line) - 1
			if s.Buf.col < 0 {
				s.Buf.col = 0
			}
		}
	case "backspace", "ctrl+h":
		s.Buf.DeleteCharBefore()
	case "delete":
		s.Buf.DeleteCharAtCursor()
	case "enter":
		s.Buf.InsertNewline()
	case "left":
		s.Buf.MoveLeft(1)
	case "right":
		s.Buf.MoveRight(1)
	case "up":
		s.Buf.MoveUp(1)
	case "down":
		s.Buf.MoveDown(1)
	case "tab":
		for i := 0; i < 4; i++ {
			s.Buf.InsertRune(' ')
		}
	default:
		runes := []rune(key)
		if len(runes) == 1 && !isCtrl(runes[0]) {
			s.Buf.InsertRune(runes[0])
		} else {
			return false // don't consume unknown insert-mode keys
		}
	}
	return true
}

// ─── Visual mode ─────────────────────────────────────────────────────────────

func (s *State) handleVisual(key string) bool {
	// Count accumulation.
	if len(key) == 1 && key[0] >= '1' && key[0] <= '9' {
		s.countStr += key
		return true
	}
	if key == "0" && s.countStr != "" {
		s.countStr += "0"
		return true
	}

	if s.pendingG {
		s.pendingG = false
		if key == "g" {
			s.Buf.MoveFirstLine()
		}
		s.resetMotion()
		return true
	}

	n := s.count()

	switch key {
	case "esc":
		s.Mode = ModeNormal
		s.resetMotion()

	// Movement — same as normal.
	case "h", "left":
		s.Buf.MoveLeft(n)
		s.resetMotion()
	case "l", "right":
		s.Buf.MoveRight(n)
		s.resetMotion()
	case "j", "down":
		s.Buf.MoveDown(n)
		s.resetMotion()
	case "k", "up":
		s.Buf.MoveUp(n)
		s.resetMotion()
	case "w", "W":
		s.Buf.WordForward(n)
		s.resetMotion()
	case "b", "B":
		s.Buf.WordBackward(n)
		s.resetMotion()
	case "e", "E":
		s.Buf.WordEnd(n)
		s.resetMotion()
	case "0":
		s.Buf.MoveLineStart()
		s.resetMotion()
	case "^":
		s.Buf.MoveFirstNonBlank()
		s.resetMotion()
	case "$":
		s.Buf.MoveLineEnd()
		s.resetMotion()
	case "g":
		s.pendingG = true
	case "G":
		s.Buf.MoveLastLine()
		s.resetMotion()
	case "}":
		s.Buf.ParagraphForward(n)
		s.resetMotion()
	case "{":
		s.Buf.ParagraphBackward(n)
		s.resetMotion()

	// Visual operators.
	case "y":
		selStart, selEnd := s.SelectionRange()
		if s.Mode == ModeVisualLine {
			s.Buf.row = selStart.Row
			s.Buf.YankLines(selEnd.Row - selStart.Row + 1)
		} else {
			s.Buf.YankRange(selStart, selEnd)
		}
		s.Buf.row, s.Buf.col = selStart.Row, selStart.Col
		s.Buf.clamp()
		s.Mode = ModeNormal
		s.resetMotion()

	case "d":
		selStart, selEnd := s.SelectionRange()
		s.Buf.PushUndo()
		if s.Mode == ModeVisualLine {
			s.Buf.row = selStart.Row
			s.Buf.DeleteLines(selEnd.Row - selStart.Row + 1)
		} else {
			s.Buf.DeleteRange(selStart, selEnd)
		}
		s.Mode = ModeNormal
		s.resetMotion()

	case ">":
		selStart, selEnd := s.SelectionRange()
		s.Buf.PushUndo()
		s.Buf.IndentLines(selStart.Row, selEnd.Row)
		s.Mode = ModeNormal
		s.resetMotion()

	case "<":
		selStart, selEnd := s.SelectionRange()
		s.Buf.PushUndo()
		s.Buf.DedentLines(selStart.Row, selEnd.Row)
		s.Mode = ModeNormal
		s.resetMotion()
	}

	return true // Visual mode consumes all keys.
}

func isCtrl(r rune) bool {
	return r < 32 || r == 127
}

func clampPosToBuffer(buf *Buffer, pos Pos) Pos {
	if buf == nil || buf.LineCount() == 0 {
		return Pos{}
	}
	if pos.Row < 0 {
		pos.Row = 0
	}
	if pos.Row >= buf.LineCount() {
		pos.Row = buf.LineCount() - 1
	}
	lineLen := len(buf.Line(pos.Row))
	if pos.Col < 0 {
		pos.Col = 0
	}
	if pos.Col > lineLen {
		pos.Col = lineLen
	}
	return pos
}
