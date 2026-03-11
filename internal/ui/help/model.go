package help

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type Section struct {
	Title string
	Lines []string
}

type Model struct {
	active bool
	title  string
	width  int
	height int
	lines  []string
	scroll int
}

var (
	panelStyle   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	titleStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#4ec9b0"))
	sectionStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#9cdcfe"))
	helpStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#808080"))
)

func New() Model {
	return Model{title: "Help & Settings"}
}

func (m Model) Active() bool { return m.active }

func (m Model) SetSize(w, h int) Model {
	if w > 100 {
		w = 100
	}
	if w < 36 {
		w = 36
	}
	if h > 30 {
		h = 30
	}
	if h < 10 {
		h = 10
	}
	m.width = w
	m.height = h
	return m
}

func (m Model) Open(sections []Section) Model {
	m.active = true
	m.scroll = 0
	m.lines = renderSections(sections)
	return m
}

func (m Model) Close() Model {
	m.active = false
	m.scroll = 0
	return m
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if !m.active {
		return m, nil
	}
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "esc", "f1":
			return m.Close(), nil
		case "up", "k":
			if m.scroll > 0 {
				m.scroll--
			}
		case "down", "j":
			if m.scroll < m.maxScroll() {
				m.scroll++
			}
		case "pgup":
			m.scroll -= m.bodyHeight()
			if m.scroll < 0 {
				m.scroll = 0
			}
		case "pgdown":
			m.scroll += m.bodyHeight()
			if m.scroll > m.maxScroll() {
				m.scroll = m.maxScroll()
			}
		case "home", "g":
			m.scroll = 0
		case "end", "G":
			m.scroll = m.maxScroll()
		}
	}
	return m, nil
}

func (m Model) View() string {
	if !m.active {
		return ""
	}
	body := m.visibleLines()
	lines := []string{
		titleStyle.Render(m.title),
		helpStyle.Render("Up/Down PgUp/PgDn Home/End • Esc/F1 close"),
		"",
		strings.Join(body, "\n"),
		"",
		helpStyle.Render(m.positionHint()),
	}
	return panelStyle.Width(m.width).Height(m.height).Render(strings.Join(lines, "\n"))
}

func (m Model) visibleLines() []string {
	bodyH := m.bodyHeight()
	start := m.scroll
	if start > len(m.lines) {
		start = len(m.lines)
	}
	end := start + bodyH
	if end > len(m.lines) {
		end = len(m.lines)
	}
	out := append([]string(nil), m.lines[start:end]...)
	for len(out) < bodyH {
		out = append(out, "")
	}
	return out
}

func (m Model) bodyHeight() int {
	n := m.height - 6
	if n < 1 {
		return 1
	}
	return n
}

func (m Model) maxScroll() int {
	max := len(m.lines) - m.bodyHeight()
	if max < 0 {
		return 0
	}
	return max
}

func (m Model) positionHint() string {
	if len(m.lines) == 0 {
		return "0 lines"
	}
	start := m.scroll + 1
	end := m.scroll + m.bodyHeight()
	if end > len(m.lines) {
		end = len(m.lines)
	}
	return strings.TrimSpace(strings.Join([]string{"lines", itoa(start) + "-" + itoa(end), "of", itoa(len(m.lines))}, " "))
}

func renderSections(sections []Section) []string {
	lines := make([]string, 0, 64)
	for i, section := range sections {
		if i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, sectionStyle.Render(section.Title))
		lines = append(lines, section.Lines...)
	}
	return lines
}

func itoa(v int) string {
	return strconv.Itoa(v)
}
