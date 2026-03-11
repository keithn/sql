package palette

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestPaletteFiltersConnections(t *testing.T) {
	m, _ := New().SetSize(60, 10).OpenConnections([]Item{
		{Key: "prod", Title: "prod", Driver: "postgres", Summary: "db.example.com/app"},
		{Key: "local", Title: "local", Driver: "sqlite", Summary: "local.db"},
	})
	m.input.SetValue("prd")
	m.syncFiltered()
	if len(m.filtered) != 1 || m.filtered[0].Key != "prod" {
		t.Fatalf("filtered = %+v, want only prod", m.filtered)
	}
}

func TestPaletteEnterReturnsAcceptedMsg(t *testing.T) {
	m, _ := New().SetSize(60, 10).OpenConnections([]Item{{Key: "prod", Title: "prod", Driver: "postgres", Summary: "db/app"}})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msg := cmd()
	accepted, ok := msg.(AcceptedMsg)
	if !ok {
		t.Fatalf("enter cmd returned %T, want AcceptedMsg", msg)
	}
	if accepted.Key != "prod" {
		t.Fatalf("AcceptedMsg.Key = %q, want %q", accepted.Key, "prod")
	}
}
