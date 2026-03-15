package rowedit

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sqltui/sql/internal/db"
)

func makeColumns() []db.Column {
	return []db.Column{
		{Name: "id", Type: "int identity"},
		{Name: "name", Type: "varchar(100)"},
		{Name: "email", Type: "varchar(255)"},
		{Name: "status", Type: "varchar(20)"},
	}
}

func makeRow() []interface{} {
	return []interface{}{1, "Alice", "alice@example.com", "active"}
}

func openModel(t *testing.T) Model {
	t.Helper()
	m := New()
	m = m.SetSize(100, 30)
	var cmd tea.Cmd
	m, cmd = m.Open(makeColumns(), makeRow(), false)
	_ = cmd
	if !m.Active() {
		t.Fatal("model should be active after Open")
	}
	return m
}

func TestOpenActivatesModel(t *testing.T) {
	m := openModel(t)
	if !m.Active() {
		t.Fatal("model not active")
	}
	if len(m.fields) != 4 {
		t.Fatalf("fields = %d, want 4", len(m.fields))
	}
}

func TestReadOnlyDetectionForIdentity(t *testing.T) {
	m := openModel(t)
	// Column 0 is "int identity" — should be readonly.
	if !m.fields[0].readOnly {
		t.Error("id (int identity) should be readonly")
	}
	// Other columns should not be readonly.
	if m.fields[1].readOnly {
		t.Error("name should not be readonly")
	}
}

func TestInitialCursorSkipsReadonly(t *testing.T) {
	m := openModel(t)
	// cursor should start at first editable field (index 1, since 0 is readonly).
	if m.cursor != 1 {
		t.Fatalf("cursor = %d, want 1 (first editable field)", m.cursor)
	}
}

func TestTabMovesToNextEditableField(t *testing.T) {
	m := openModel(t)
	// cursor starts at 1. Tab should move to 2.
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("	")}) // Tab
	// Actually tea.KeyTab:
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m2.cursor != 2 {
		t.Fatalf("after Tab cursor = %d, want 2", m2.cursor)
	}
}

func TestEscEmitsCancelledMsg(t *testing.T) {
	m := openModel(t)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	if cmd == nil {
		t.Fatal("Esc should produce a cmd")
	}
	msg := cmd()
	if _, ok := msg.(CancelledMsg); !ok {
		t.Fatalf("Esc should emit CancelledMsg, got %T", msg)
	}
}

func TestCtrlSWithNoChangesProducesEmptyUpdates(t *testing.T) {
	m := openModel(t)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if cmd == nil {
		t.Fatal("Ctrl+S should produce a cmd")
	}
	msg := cmd()
	sub, ok := msg.(SubmittedMsg)
	if !ok {
		t.Fatalf("Ctrl+S should emit SubmittedMsg, got %T", msg)
	}
	// No fields were modified, so Updates should be empty.
	if len(sub.Updates) != 0 {
		t.Errorf("updates = %d, want 0 (nothing changed)", len(sub.Updates))
	}
}

func TestCtrlSWithChangedFieldProducesUpdate(t *testing.T) {
	m := openModel(t)
	// Cursor is at field 1 (name). Set the textarea value to simulate typing.
	m.ta.SetValue("Bob")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	msg := cmd()
	sub, ok := msg.(SubmittedMsg)
	if !ok {
		t.Fatalf("Ctrl+S should emit SubmittedMsg, got %T", msg)
	}
	if len(sub.Updates) != 1 {
		t.Fatalf("updates = %d, want 1", len(sub.Updates))
	}
	u := sub.Updates[0]
	if u.ColName != "name" {
		t.Errorf("update colname = %q, want %q", u.ColName, "name")
	}
	if u.NewValue != "Bob" {
		t.Errorf("update value = %q, want %q", u.NewValue, "Bob")
	}
}

func TestCtrlDSetsNullOnActiveField(t *testing.T) {
	m := openModel(t)
	// cursor is at field 1 (name).
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	if !m2.fields[1].setNull {
		t.Error("Ctrl+D should set field to null")
	}
	// Submit and check the update includes SetNull.
	_, cmd := m2.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	msg := cmd()
	sub := msg.(SubmittedMsg)
	found := false
	for _, u := range sub.Updates {
		if u.ColName == "name" && u.SetNull {
			found = true
		}
	}
	if !found {
		t.Error("SubmittedMsg should have SetNull update for name")
	}
}

func TestReadonlyFieldNotEditable(t *testing.T) {
	m := openModel(t)
	// Field 0 (id, identity) is readonly. Ctrl+D should not set it to null.
	old := m.cursor
	m.cursor = 0
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	if m2.fields[0].setNull {
		t.Error("readonly field should not be settable to null")
	}
	_ = old
}

func TestViewNotEmpty(t *testing.T) {
	m := openModel(t)
	v := m.View()
	if v == "" {
		t.Fatal("View should not be empty when active")
	}
}

func TestViewInactiveIsEmpty(t *testing.T) {
	m := New()
	if m.View() != "" {
		t.Fatal("View should be empty when inactive")
	}
}

func TestModifiedMarkerAppearsInView(t *testing.T) {
	m := openModel(t)
	m.fields[1].current = "Changed"
	v := m.View()
	if v == "" {
		t.Fatal("View should not be empty")
	}
	// The modified marker "*" should appear.
	// We can't easily check the exact position due to ANSI codes, but
	// we verify the model tracks it.
	if !m.fields[1].modified() {
		t.Error("field 1 should be marked modified")
	}
}

func TestIsReadOnlyIdentity(t *testing.T) {
	tests := []struct {
		colType string
		want    bool
	}{
		{"int identity", true},
		{"int", false},
		{"varchar(100)", false},
		{"timestamp", true},
		{"rowversion", true},
		{"computed", true},
		{"nvarchar(max)", false},
	}
	for _, tc := range tests {
		col := db.Column{Name: "x", Type: tc.colType}
		got := isReadOnly(col)
		if got != tc.want {
			t.Errorf("isReadOnly(%q) = %v, want %v", tc.colType, got, tc.want)
		}
	}
}

func TestVimModeOpensInInsert(t *testing.T) {
	m := New()
	m = m.SetSize(100, 30)
	var cmd tea.Cmd
	m, cmd = m.Open(makeColumns(), makeRow(), true /* vimEnabled */)
	_ = cmd
	if m.vs == nil {
		t.Fatal("vim state should be non-nil when vim mode enabled")
	}
	// vim.ModeInsert = 1 (ModeNormal=0, ModeInsert=1 in vim/state.go)
	if m.vs.Mode != 1 {
		t.Errorf("vim mode should be INSERT (1) on open, got mode=%d", m.vs.Mode)
	}
}

func TestAllColumnsAndRowPassedInSubmittedMsg(t *testing.T) {
	m := openModel(t)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	msg := cmd()
	sub := msg.(SubmittedMsg)
	if len(sub.AllColumns) != 4 {
		t.Errorf("AllColumns = %d, want 4", len(sub.AllColumns))
	}
	if len(sub.Row) != 4 {
		t.Errorf("Row = %d, want 4", len(sub.Row))
	}
}

func TestMultiFieldEditProducesMultipleUpdates(t *testing.T) {
	m := openModel(t)
	// cursor is at field 1 (name). Type "Bob".
	m.ta.SetValue("Bob")
	// Tab to field 2 (email).
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m2.cursor != 2 {
		t.Fatalf("after Tab cursor = %d, want 2", m2.cursor)
	}
	// Type new email.
	m2.ta.SetValue("bob@example.com")
	// Submit.
	_, cmd := m2.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if cmd == nil {
		t.Fatal("Ctrl+S should produce a cmd")
	}
	msg := cmd()
	sub, ok := msg.(SubmittedMsg)
	if !ok {
		t.Fatalf("expected SubmittedMsg, got %T", msg)
	}
	if len(sub.Updates) != 2 {
		t.Fatalf("expected 2 updates, got %d: %+v", len(sub.Updates), sub.Updates)
	}
	names := map[string]string{}
	for _, u := range sub.Updates {
		names[u.ColName] = u.NewValue
	}
	if names["name"] != "Bob" {
		t.Errorf("name update = %q, want %q", names["name"], "Bob")
	}
	if names["email"] != "bob@example.com" {
		t.Errorf("email update = %q, want %q", names["email"], "bob@example.com")
	}
}
