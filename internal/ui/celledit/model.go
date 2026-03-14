// Package celledit provides a popup overlay for editing a single cell value.
package celledit

import (
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sqltui/sql/internal/ui/editor/vim"
)

// SubmittedMsg is sent when the user confirms the edit.
type SubmittedMsg struct {
	NewValue string
	SetNull  bool
}

// CancelledMsg is sent when the user cancels.
type CancelledMsg struct{}

var (
	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#007acc")).
			Padding(0, 1)

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#4ec9b0"))

	hintStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#808080"))

	colStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#ce9178"))

	modeStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#dcdcaa"))

	// NORMAL mode: solid blue block
	cursorNormalStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#007acc")).
				Foreground(lipgloss.Color("#ffffff"))

	// INSERT mode: underline cursor to simulate a bar/beam
	cursorInsertStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#d4d4d4")).
				Underline(true)

	textStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#d4d4d4"))
)

// Model is the cell edit overlay.
type Model struct {
	active     bool
	colName    string
	ta         textarea.Model
	vimEnabled bool
	vs         *vim.State
	scrollRow  int
	width      int
	height     int
}

func New() Model {
	ta := textarea.New()
	ta.Prompt = ""
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.SetWidth(60)
	ta.SetHeight(10)
	return Model{ta: ta}
}

func (m Model) Active() bool { return m.active }

// SetVimMode enables or disables vim keybindings in the overlay.
func (m Model) SetVimMode(on bool) Model {
	m.vimEnabled = on
	return m
}

func (m Model) SetSize(w, h int) Model {
	overlayW := w - 6
	if overlayW > 100 {
		overlayW = 100
	}
	if overlayW < 30 {
		overlayW = 30
	}
	overlayH := h - 6
	if overlayH > 30 {
		overlayH = 30
	}
	if overlayH < 6 {
		overlayH = 6
	}
	m.width = overlayW
	m.height = overlayH
	// textarea fills the inner area: overlay minus border(2) + padding(2) + title(1) + hint(1)
	taW := overlayW - 4
	taH := overlayH - 5
	if taH < 3 {
		taH = 3
	}
	m.ta.SetWidth(taW)
	m.ta.SetHeight(taH)
	if m.vs != nil {
		m.vs.SetSize(taW, taH)
	}
	return m
}

// Open opens the overlay pre-populated with value for the given column.
func (m Model) Open(colName, value string) (Model, tea.Cmd) {
	m.active = true
	m.colName = colName
	m.scrollRow = 0
	if m.vimEnabled {
		m.vs = vim.NewState()
		innerW := m.width - 4
		innerH := m.height - 5
		if innerH < 3 {
			innerH = 3
		}
		m.vs.SetSize(innerW, innerH)
		m.vs.Buf.SetValue(value)
		// Start in normal mode; user can press i to enter insert.
		return m, nil
	}
	m.ta.Reset()
	m.ta.SetValue(value)
	return m, m.ta.Focus()
}

func (m Model) Close() Model {
	m.active = false
	m.vs = nil
	m.ta.Reset()
	return m
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if !m.active {
		return m, nil
	}
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "ctrl+s":
			var val string
			if m.vimEnabled && m.vs != nil {
				val = m.vs.Buf.Value()
			} else {
				val = m.ta.Value()
			}
			return m.Close(), func() tea.Msg { return SubmittedMsg{NewValue: val} }
		case "ctrl+d":
			return m.Close(), func() tea.Msg { return SubmittedMsg{SetNull: true} }
		}

		if m.vimEnabled && m.vs != nil {
			k := key.String()
			// Esc in normal mode cancels; in insert mode returns to normal.
			if k == "esc" {
				if m.vs.Mode == vim.ModeInsert {
					m.vs.HandleKey("esc")
					return m, nil
				}
				return m.Close(), func() tea.Msg { return CancelledMsg{} }
			}
			m.vs.HandleKey(k)
			// Keep cursor visible.
			taH := m.height - 5
			if taH < 3 {
				taH = 3
			}
			m.vs.ScrollToReveal(taH)
			return m, nil
		}

		if key.String() == "esc" {
			return m.Close(), func() tea.Msg { return CancelledMsg{} }
		}
	}
	var cmd tea.Cmd
	m.ta, cmd = m.ta.Update(msg)
	return m, cmd
}

func (m Model) View() string {
	if !m.active {
		return ""
	}
	title := titleStyle.Render("Edit ") + colStyle.Render(m.colName)

	var body string
	var hint string

	if m.vimEnabled && m.vs != nil {
		body = m.renderVim()
		mode := "NORMAL"
		if m.vs.Mode == vim.ModeInsert {
			mode = "INSERT"
		}
		hint = modeStyle.Render(mode) + hintStyle.Render("  •  Ctrl+S confirm  •  Ctrl+D set NULL  •  Esc normal/cancel")
	} else {
		body = m.ta.View()
		hint = hintStyle.Render("Ctrl+S confirm  •  Ctrl+D set NULL  •  Esc cancel  •  Enter newline")
	}

	inner := strings.Join([]string{title, "", body, "", hint}, "\n")
	return panelStyle.Width(m.width).Render(inner)
}

func (m Model) renderVim() string {
	if m.vs == nil {
		return ""
	}
	buf := m.vs.Buf
	innerW := m.width - 4
	taH := m.height - 5
	if taH < 3 {
		taH = 3
	}
	lineCount := buf.LineCount()
	curRow := buf.CursorRow()
	curCol := buf.CursorCol()

	cs := cursorNormalStyle
	if m.vs.Mode == vim.ModeInsert {
		cs = cursorInsertStyle
	}

	var sb strings.Builder
	for i := 0; i < taH; i++ {
		row := m.vs.TopRow + i
		if row >= lineCount {
			sb.WriteString(strings.Repeat(" ", innerW))
			if i < taH-1 {
				sb.WriteByte('\n')
			}
			continue
		}
		line := buf.Line(row)
		var lineSB strings.Builder
		for c, ch := range line {
			if row == curRow && c == curCol {
				lineSB.WriteString(cs.Render(string(ch)))
			} else {
				lineSB.WriteString(textStyle.Render(string(ch)))
			}
		}
		// Cursor past end of line.
		if row == curRow && curCol >= len(line) {
			lineSB.WriteString(cs.Render(" "))
		}
		// Pad to width.
		visual := len(line)
		if row == curRow && curCol >= len(line) {
			visual++
		}
		if visual < innerW {
			lineSB.WriteString(strings.Repeat(" ", innerW-visual))
		}
		sb.WriteString(lineSB.String())
		if i < taH-1 {
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}
