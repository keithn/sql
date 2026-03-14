// Package updatepreview shows a generated UPDATE statement in a popup panel,
// allowing the user to execute it or copy it without touching the results grid.
package updatepreview

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ExecuteMsg is sent when the user confirms execution.
type ExecuteMsg struct{ SQL string }

// CopyMsg is sent when the user copies the SQL.
type CopyMsg struct{ SQL string }

// CloseMsg is sent when the user dismisses the panel.
type CloseMsg struct{}

// StatusKind distinguishes the result state.
type StatusKind int

const (
	StatusReady     StatusKind = iota
	StatusExecuting StatusKind = iota
	StatusOK        StatusKind = iota
	StatusErr       StatusKind = iota
)

var (
	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#007acc")).
			Padding(0, 1)

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#4ec9b0"))

	sqlStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#d4d4d4"))

	hintStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#888888"))

	okStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#4ec9b0"))

	errStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#f44747"))

	execStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#dcdcaa"))
)

// Model is the update-preview overlay.
type Model struct {
	active     bool
	sql        string
	lines      []string
	scrollRow  int
	statusKind StatusKind
	statusText string
	width      int
	height     int
	innerW     int
}

func New() Model { return Model{} }

func (m Model) Active() bool { return m.active }

// Open opens the panel pre-loaded with the given SQL.
func (m Model) Open(sql string) Model {
	m.active = true
	m.sql = sql
	m.scrollRow = 0
	m.statusKind = StatusReady
	m.statusText = "ready"
	m.lines = splitLines(sql, m.innerW)
	return m
}

// Close closes the panel.
func (m Model) Close() Model {
	m.active = false
	return m
}

// SetExecuting marks the panel as mid-execution.
func (m Model) SetExecuting() Model {
	m.statusKind = StatusExecuting
	m.statusText = "executing…"
	return m
}

// SetResult records the outcome of the execution.
func (m Model) SetResult(rowsAffected int64, err error) Model {
	if err != nil {
		m.statusKind = StatusErr
		m.statusText = err.Error()
	} else {
		m.statusKind = StatusOK
		switch rowsAffected {
		case 1:
			m.statusText = "1 row affected"
		default:
			m.statusText = strings.Join([]string{itoa(rowsAffected), " rows affected"}, "")
		}
	}
	return m
}

func (m Model) SetSize(w, h int) Model {
	overlayW := w - 6
	if overlayW > 110 {
		overlayW = 110
	}
	if overlayW < 40 {
		overlayW = 40
	}
	overlayH := h - 6
	if overlayH > 30 {
		overlayH = 30
	}
	if overlayH < 8 {
		overlayH = 8
	}
	m.width = overlayW
	m.height = overlayH
	m.innerW = overlayW - 4 // border(2) + padding(2)
	if m.active {
		m.lines = splitLines(m.sql, m.innerW)
	}
	return m
}

// visibleLines returns how many SQL lines fit in the panel.
// overhead: title(1) + blank(1) + status(1) + blank(1) + hints(1) = 5
func (m Model) visibleLines() int {
	n := m.height - 5
	if n < 2 {
		return 2
	}
	return n
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if !m.active {
		return m, nil
	}
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "esc":
		return m.Close(), func() tea.Msg { return CloseMsg{} }

	case "ctrl+e", "enter":
		if m.statusKind == StatusExecuting {
			return m, nil // already running
		}
		sql := m.sql
		m = m.SetExecuting()
		return m, func() tea.Msg { return ExecuteMsg{SQL: sql} }

	case "y", "ctrl+c":
		sql := m.sql
		return m, func() tea.Msg { return CopyMsg{SQL: sql} }

	case "up", "k":
		if m.scrollRow > 0 {
			m.scrollRow--
		}
	case "down", "j":
		vis := m.visibleLines()
		if m.scrollRow < len(m.lines)-vis {
			m.scrollRow++
		}
	case "pgup":
		vis := m.visibleLines()
		m.scrollRow -= vis
		if m.scrollRow < 0 {
			m.scrollRow = 0
		}
	case "pgdown":
		vis := m.visibleLines()
		m.scrollRow += vis
		if max := len(m.lines) - vis; m.scrollRow > max {
			if max < 0 {
				max = 0
			}
			m.scrollRow = max
		}
	}
	return m, nil
}

func (m Model) View() string {
	if !m.active {
		return ""
	}
	vis := m.visibleLines()
	total := len(m.lines)
	rowEnd := m.scrollRow + vis
	if rowEnd > total {
		rowEnd = total
	}

	var sb strings.Builder

	sb.WriteString(titleStyle.Render("Update Preview") + "\n")
	sb.WriteByte('\n')

	// SQL lines
	for r := m.scrollRow; r < rowEnd; r++ {
		line := m.lines[r]
		sb.WriteString(sqlStyle.Render(padRight(line, m.innerW)) + "\n")
	}
	// Fill blank rows
	for i := rowEnd - m.scrollRow; i < vis; i++ {
		sb.WriteString(strings.Repeat(" ", m.innerW) + "\n")
	}

	// Status
	sb.WriteByte('\n')
	switch m.statusKind {
	case StatusReady:
		sb.WriteString(hintStyle.Render(m.statusText))
	case StatusExecuting:
		sb.WriteString(execStyle.Render(m.statusText))
	case StatusOK:
		sb.WriteString(okStyle.Render("✓ " + m.statusText))
	case StatusErr:
		sb.WriteString(errStyle.Render("✗ " + m.statusText))
	}
	sb.WriteByte('\n')

	// Hints
	hints := "Ctrl+E execute  •  y copy  •  Esc close"
	if total > vis {
		hints += "  •  ↑↓ scroll"
	}
	sb.WriteString(hintStyle.Render(hints))

	return panelStyle.Width(m.innerW).Render(sb.String())
}

func splitLines(s string, width int) []string {
	if width <= 0 {
		width = 80
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	var out []string
	for _, line := range strings.Split(s, "\n") {
		runes := []rune(line)
		if len(runes) <= width {
			out = append(out, line)
			continue
		}
		for len(runes) > 0 {
			if len(runes) <= width {
				out = append(out, string(runes))
				break
			}
			out = append(out, string(runes[:width]))
			runes = runes[width:]
		}
	}
	return out
}

func padRight(s string, n int) string {
	r := []rune(s)
	if len(r) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(r))
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
