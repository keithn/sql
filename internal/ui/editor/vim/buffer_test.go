package vim

import (
	"testing"
)

func TestBufferSetGetValue(t *testing.T) {
	b := NewBuffer()
	b.SetValue("SELECT *\nFROM foo\nWHERE id = 1")
	if b.LineCount() != 3 {
		t.Fatalf("want 3 lines, got %d", b.LineCount())
	}
	if b.Value() != "SELECT *\nFROM foo\nWHERE id = 1" {
		t.Fatalf("unexpected value: %q", b.Value())
	}
}

func TestBufferMoveLeftRight(t *testing.T) {
	b := NewBuffer()
	b.SetValue("hello")
	b.MoveRight(3)
	if b.CursorCol() != 3 {
		t.Errorf("want col 3, got %d", b.CursorCol())
	}
	b.MoveLeft(2)
	if b.CursorCol() != 1 {
		t.Errorf("want col 1, got %d", b.CursorCol())
	}
	b.MoveLeft(10) // clamp to 0
	if b.CursorCol() != 0 {
		t.Errorf("want col 0, got %d", b.CursorCol())
	}
}

func TestBufferMoveUpDown(t *testing.T) {
	b := NewBuffer()
	b.SetValue("a\nbb\nccc")
	b.MoveDown(2)
	if b.CursorRow() != 2 {
		t.Errorf("want row 2, got %d", b.CursorRow())
	}
	b.MoveUp(1)
	if b.CursorRow() != 1 {
		t.Errorf("want row 1, got %d", b.CursorRow())
	}
	b.MoveUp(100) // clamp to 0
	if b.CursorRow() != 0 {
		t.Errorf("want row 0, got %d", b.CursorRow())
	}
}

func TestBufferWordForward(t *testing.T) {
	b := NewBuffer()
	b.SetValue("hello world foo")
	b.WordForward(1)
	if b.CursorCol() != 6 {
		t.Errorf("want col 6 (start of 'world'), got %d", b.CursorCol())
	}
	b.WordForward(1)
	if b.CursorCol() != 12 {
		t.Errorf("want col 12 (start of 'foo'), got %d", b.CursorCol())
	}
}

func TestBufferWordBackward(t *testing.T) {
	b := NewBuffer()
	b.SetValue("hello world foo")
	b.MoveRight(14) // end of "foo"
	b.WordBackward(1)
	if b.CursorCol() != 12 {
		t.Errorf("want col 12 (start of 'foo'), got %d", b.CursorCol())
	}
	b.WordBackward(1)
	if b.CursorCol() != 6 {
		t.Errorf("want col 6 (start of 'world'), got %d", b.CursorCol())
	}
}

func TestBufferInsertRune(t *testing.T) {
	b := NewBuffer()
	b.SetValue("ac")
	b.MoveRight(1) // cursor at col 1, before 'c' → insert after 'a'
	// Actually in insert mode col can be 1
	b.col = 1 // force insert position
	b.InsertRune('b')
	if b.Value() != "abc" {
		t.Errorf("want 'abc', got %q", b.Value())
	}
}

func TestBufferInsertNewline(t *testing.T) {
	b := NewBuffer()
	b.SetValue("hello world")
	b.col = 5 // after 'hello'
	b.InsertNewline()
	if b.Value() != "hello\n world" {
		t.Errorf("want 'hello\\n world', got %q", b.Value())
	}
	if b.CursorRow() != 1 || b.CursorCol() != 0 {
		t.Errorf("cursor should be at (1,0), got (%d,%d)", b.CursorRow(), b.CursorCol())
	}
}

func TestBufferDeleteCharBefore(t *testing.T) {
	b := NewBuffer()
	b.SetValue("abc")
	b.col = 2
	b.DeleteCharBefore()
	if b.Value() != "ac" {
		t.Errorf("want 'ac', got %q", b.Value())
	}
}

func TestBufferDeleteCharBeforeJoin(t *testing.T) {
	b := NewBuffer()
	b.SetValue("foo\nbar")
	b.row = 1
	b.col = 0
	b.DeleteCharBefore() // join lines
	if b.Value() != "foobar" {
		t.Errorf("want 'foobar', got %q", b.Value())
	}
	if b.CursorRow() != 0 || b.CursorCol() != 3 {
		t.Errorf("cursor should be at (0,3), got (%d,%d)", b.CursorRow(), b.CursorCol())
	}
}

func TestBufferDeleteCharAtCursor(t *testing.T) {
	b := NewBuffer()
	b.SetValue("abc")
	b.col = 1
	b.DeleteCharAtCursor()
	if b.Value() != "ac" {
		t.Errorf("want 'ac', got %q", b.Value())
	}
	if b.CursorCol() != 1 {
		t.Errorf("cursor should stay at col 1, got %d", b.CursorCol())
	}
}

func TestBufferDeleteCharAtCursorJoin(t *testing.T) {
	b := NewBuffer()
	b.SetValue("foo\nbar")
	b.col = 3
	b.DeleteCharAtCursor()
	if b.Value() != "foobar" {
		t.Errorf("want 'foobar', got %q", b.Value())
	}
	if b.CursorRow() != 0 || b.CursorCol() != 3 {
		t.Errorf("cursor should stay at (0,3), got (%d,%d)", b.CursorRow(), b.CursorCol())
	}
}

func TestBufferDeleteLines(t *testing.T) {
	b := NewBuffer()
	b.SetValue("a\nb\nc")
	b.MoveDown(1)
	b.DeleteLines(1)
	if b.Value() != "a\nc" {
		t.Errorf("want 'a\\nc', got %q", b.Value())
	}
	reg, regLine := b.Register()
	if reg != "b\n" || !regLine {
		t.Errorf("register should be 'b\\n' (linewise), got %q linewise=%v", reg, regLine)
	}
}

func TestBufferDeleteRange(t *testing.T) {
	b := NewBuffer()
	b.SetValue("hello world")
	b.DeleteRange(Pos{0, 6}, Pos{0, 10})
	if b.Value() != "hello " {
		t.Errorf("want 'hello ', got %q", b.Value())
	}
}

func TestBufferPasteAfterCharwise(t *testing.T) {
	b := NewBuffer()
	b.SetValue("ac")
	b.register = "b"
	b.regLine = false
	b.col = 0 // cursor on 'a', paste after → "abc"
	b.PasteAfter()
	if b.Value() != "abc" {
		t.Errorf("want 'abc', got %q", b.Value())
	}
}

func TestBufferPasteAfterLinewise(t *testing.T) {
	b := NewBuffer()
	b.SetValue("a\nc")
	b.register = "b\n"
	b.regLine = true
	b.row = 0
	b.PasteAfter()
	if b.Value() != "a\nb\nc" {
		t.Errorf("want 'a\\nb\\nc', got %q", b.Value())
	}
	if b.CursorRow() != 1 {
		t.Errorf("cursor should be at row 1, got %d", b.CursorRow())
	}
}

func TestBufferUndoRedo(t *testing.T) {
	b := NewBuffer()
	b.SetValue("hello")
	b.PushUndo()
	b.SetValue("world")
	if b.Value() != "world" {
		t.Errorf("want 'world', got %q", b.Value())
	}
	b.Undo()
	if b.Value() != "hello" {
		t.Errorf("after undo want 'hello', got %q", b.Value())
	}
	b.Redo()
	if b.Value() != "world" {
		t.Errorf("after redo want 'world', got %q", b.Value())
	}
}

func TestBufferOpenLineBelow(t *testing.T) {
	b := NewBuffer()
	b.SetValue("a\nc")
	b.row = 0
	b.OpenLineBelow()
	if b.Value() != "a\n\nc" {
		t.Errorf("want 'a\\n\\nc', got %q", b.Value())
	}
	if b.CursorRow() != 1 || b.CursorCol() != 0 {
		t.Errorf("cursor should be at (1,0), got (%d,%d)", b.CursorRow(), b.CursorCol())
	}
}

func TestBufferOpenLineAbove(t *testing.T) {
	b := NewBuffer()
	b.SetValue("a\nc")
	b.row = 1
	b.OpenLineAbove()
	if b.Value() != "a\n\nc" {
		t.Errorf("want 'a\\n\\nc', got %q", b.Value())
	}
	if b.CursorRow() != 1 || b.CursorCol() != 0 {
		t.Errorf("cursor should be at (1,0), got (%d,%d)", b.CursorRow(), b.CursorCol())
	}
}

func TestBufferIndentDedent(t *testing.T) {
	b := NewBuffer()
	b.SetValue("foo\nbar\nbaz")
	b.IndentLines(0, 1)
	if b.Value() != "    foo\n    bar\nbaz" {
		t.Errorf("unexpected after indent: %q", b.Value())
	}
	b.DedentLines(0, 1)
	if b.Value() != "foo\nbar\nbaz" {
		t.Errorf("unexpected after dedent: %q", b.Value())
	}
}

func TestBufferMoveLineEnd(t *testing.T) {
	b := NewBuffer()
	b.SetValue("hello")
	b.MoveLineEnd()
	if b.CursorCol() != 4 {
		t.Errorf("want col 4, got %d", b.CursorCol())
	}
}

func TestBufferMoveFirstNonBlank(t *testing.T) {
	b := NewBuffer()
	b.SetValue("   hello")
	b.MoveFirstNonBlank()
	if b.CursorCol() != 3 {
		t.Errorf("want col 3, got %d", b.CursorCol())
	}
}

func TestStateNormalToInsertAndBack(t *testing.T) {
	s := NewState()
	s.Buf.SetValue("hello")
	if s.Mode != ModeNormal {
		t.Fatal("expected Normal mode initially")
	}
	s.HandleKey("i")
	if s.Mode != ModeInsert {
		t.Fatal("expected Insert mode after 'i'")
	}
	s.HandleKey("esc")
	if s.Mode != ModeNormal {
		t.Fatal("expected Normal mode after Esc")
	}
}

func TestStateInsertTyping(t *testing.T) {
	s := NewState()
	s.Buf.SetValue("")
	s.HandleKey("i")
	s.HandleKey("h")
	s.HandleKey("i")
	if s.Buf.Value() != "hi" {
		t.Errorf("want 'hi', got %q", s.Buf.Value())
	}
}

func TestStateDDDeletesLine(t *testing.T) {
	s := NewState()
	s.Buf.SetValue("a\nb\nc")
	s.Buf.MoveDown(1)
	s.HandleKey("d")
	s.HandleKey("d")
	if s.Buf.Value() != "a\nc" {
		t.Errorf("want 'a\\nc', got %q", s.Buf.Value())
	}
}

func TestStateYYPastesLine(t *testing.T) {
	s := NewState()
	s.Buf.SetValue("hello\nworld")
	s.HandleKey("y")
	s.HandleKey("y")
	s.HandleKey("p")
	if s.Buf.Value() != "hello\nhello\nworld" {
		t.Errorf("want 'hello\\nhello\\nworld', got %q", s.Buf.Value())
	}
}

func TestStateCountedMovement(t *testing.T) {
	s := NewState()
	s.Buf.SetValue("a\nb\nc\nd\ne")
	s.HandleKey("3")
	s.HandleKey("j")
	if s.Buf.CursorRow() != 3 {
		t.Errorf("want row 3 after 3j, got %d", s.Buf.CursorRow())
	}
}

func TestStateVisualYank(t *testing.T) {
	s := NewState()
	s.Buf.SetValue("hello world")
	s.HandleKey("v")
	s.HandleKey("4")
	s.HandleKey("l") // select "hello"
	s.HandleKey("y")
	if s.Mode != ModeNormal {
		t.Fatal("expected Normal mode after y in visual")
	}
	reg, regLine := s.Buf.Register()
	if reg != "hello" || regLine {
		t.Errorf("register should be 'hello' charwise, got %q linewise=%v", reg, regLine)
	}
}

func TestStateVisualYankThenPaste(t *testing.T) {
	s := NewState()
	s.Buf.SetValue("hello world")
	s.Mode = ModeVisual
	s.Anchor = Pos{Row: 0, Col: 0}
	s.Buf.col = 4 // select "hello"
	s.HandleKey("y")
	s.HandleKey("$")
	s.HandleKey("p")
	if s.Buf.Value() != "hello worldhello" {
		t.Fatalf("want visual yank followed by p to paste register, got %q", s.Buf.Value())
	}
}

func TestStateUndo(t *testing.T) {
	s := NewState()
	s.Buf.SetValue("hello")
	s.HandleKey("d")
	s.HandleKey("d")
	if s.Buf.Value() != "" {
		t.Errorf("expected empty after dd, got %q", s.Buf.Value())
	}
	s.HandleKey("u")
	if s.Buf.Value() != "hello" {
		t.Errorf("expected 'hello' after undo, got %q", s.Buf.Value())
	}
}
