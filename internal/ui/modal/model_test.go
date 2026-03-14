package modal

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestAddConnectionModalSubmitSaveOnly(t *testing.T) {
	m, _ := New().SetSize(80, 14).OpenAddConnection()
	m.nameInput.SetValue("prod")
	m.connInput.SetValue("postgres://app:secret@localhost/mydb")
	m.focus = 3 // actions row (string mode: 0=name 1=mode 2=conn 3=actions)
	m.action = AddConnSaveOnly

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msg := cmd()
	submitted, ok := msg.(AddConnSubmittedMsg)
	if !ok {
		t.Fatalf("submit returned %T, want AddConnSubmittedMsg", msg)
	}
	if submitted.Name != "prod" || submitted.Action != AddConnSaveOnly {
		t.Fatalf("submitted = %+v, want prod/save-only", submitted)
	}
}

func TestAddConnectionModalRequiresNameWhenSaving(t *testing.T) {
	m, _ := New().SetSize(80, 14).OpenAddConnection()
	m.connInput.SetValue("postgres://app@localhost/mydb")
	m.focus = 3 // actions row
	m.action = AddConnSaveOnly

	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nm
	if cmd != nil {
		t.Fatalf("expected no submit command on validation failure")
	}
	if m.err == "" {
		t.Fatalf("expected validation error for missing name")
	}
}

func TestConfirmModalSubmitReturnsConfirmedMsg(t *testing.T) {
	m, _ := New().SetSize(60, 10).OpenConfirm("run_full_buffer", "Run full buffer?", "Execute everything?", "Run")

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msg := cmd()
	confirmed, ok := msg.(ConfirmedMsg)
	if !ok {
		t.Fatalf("submit returned %T, want ConfirmedMsg", msg)
	}
	if confirmed.ID != "run_full_buffer" {
		t.Fatalf("confirmed.ID = %q, want %q", confirmed.ID, "run_full_buffer")
	}
}
