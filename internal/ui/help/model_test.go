package help

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func testTabs() []Tab {
	return []Tab{
		{Title: "Editor", Sections: []Section{{
			Title: "Editing",
			Lines: []string{"line1", "line2", "line3", "line4", "line5", "line6", "line7", "line8"},
		}}},
		{Title: "Results", Sections: []Section{{
			Title: "Navigation",
			Lines: []string{"res1", "res2"},
		}}},
	}
}

func TestHelpViewAndScroll(t *testing.T) {
	m := New().SetSize(70, 12).Open(testTabs(), 0)
	view := m.View()
	if !strings.Contains(view, "Editor") {
		t.Fatalf("view should contain active tab name; got %q", view)
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

func TestHelpTabSwitching(t *testing.T) {
	m := New().SetSize(70, 20).Open(testTabs(), 0)
	if m.activeTab != 0 {
		t.Fatalf("initial tab = %d, want 0", m.activeTab)
	}
	// Right arrow moves to next tab and resets scroll.
	m.scroll = 3
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = nm
	if m.activeTab != 1 {
		t.Fatalf("after right: activeTab = %d, want 1", m.activeTab)
	}
	if m.scroll != 0 {
		t.Fatalf("after tab switch: scroll = %d, want 0", m.scroll)
	}
	// Right at last tab is a no-op.
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = nm
	if m.activeTab != 1 {
		t.Fatalf("at last tab: activeTab = %d, want 1 (no change)", m.activeTab)
	}
	// Left arrow goes back.
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m = nm
	if m.activeTab != 0 {
		t.Fatalf("after left: activeTab = %d, want 0", m.activeTab)
	}
}

func TestHelpInitialTabFromResults(t *testing.T) {
	m := New().SetSize(70, 20).Open(testTabs(), 1)
	if m.activeTab != 1 {
		t.Fatalf("initial tab = %d, want 1 (Results)", m.activeTab)
	}
	view := m.View()
	if !strings.Contains(view, "Results") {
		t.Fatalf("view should show Results tab content; got %q", view)
	}
}

func TestHelpNumberKeysSwitchTabs(t *testing.T) {
	m := New().SetSize(70, 20).Open(testTabs(), 0)
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	m = nm
	if m.activeTab != 1 {
		t.Fatalf("after '2': activeTab = %d, want 1", m.activeTab)
	}
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	m = nm
	if m.activeTab != 0 {
		t.Fatalf("after '1': activeTab = %d, want 0", m.activeTab)
	}
}
