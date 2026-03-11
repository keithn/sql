package help

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestHelpViewAndScroll(t *testing.T) {
	m := New().SetSize(70, 12).Open([]Section{{
		Title: "Runtime",
		Lines: []string{"line1", "line2", "line3", "line4", "line5", "line6", "line7", "line8"},
	}})
	if !strings.Contains(m.View(), "Help & Settings") {
		t.Fatalf("view should contain title")
	}
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = nm
	if m.scroll != 1 {
		t.Fatalf("scroll = %d, want 1 after down", m.scroll)
	}
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyF1})
	m = nm
	if m.Active() {
		t.Fatalf("help should close on F1")
	}
}
