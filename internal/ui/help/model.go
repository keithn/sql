package help

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Section is a titled group of lines within a tab.
type Section struct {
	Title string
	Lines []string
}

// Tab is a named page of sections shown in the help overlay.
type Tab struct {
	Title    string
	Sections []Section
}

type Model struct {
	active    bool
	width     int
	height    int
	tabs      []Tab
	activeTab int
	lines     [][]string // rendered lines per tab
	scroll    int
}

var (
	panelStyle      = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	titleStyle      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#4ec9b0"))
	sectionStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#9cdcfe"))
	helpStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("#808080"))
	tabActiveStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ffffff")).Background(lipgloss.Color("#005f87")).Padding(0, 1)
	tabInactiveStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#808080")).Padding(0, 1)
)

func New() Model {
	return Model{}
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

// Open activates the overlay with the given tabs, starting on initialTab.
func (m Model) Open(tabs []Tab, initialTab int) Model {
	m.active = true
	m.scroll = 0
	m.tabs = tabs
	if initialTab < 0 || initialTab >= len(tabs) {
		initialTab = 0
	}
	m.activeTab = initialTab
	m.lines = make([][]string, len(tabs))
	for i, t := range tabs {
		m.lines[i] = renderSections(t.Sections)
	}
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
		case "left", "h":
			if m.activeTab > 0 {
				m.activeTab--
				m.scroll = 0
			}
		case "right", "l":
			if m.activeTab < len(m.tabs)-1 {
				m.activeTab++
				m.scroll = 0
			}
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
		default:
			// Number keys 1-9 jump to that tab.
			if len(key.String()) == 1 {
				ch := key.String()[0]
				if ch >= '1' && ch <= '9' {
					idx := int(ch-'1')
					if idx < len(m.tabs) {
						m.activeTab = idx
						m.scroll = 0
					}
				}
			}
		}
	}
	return m, nil
}

func (m Model) View() string {
	if !m.active {
		return ""
	}
	tabBar := m.renderTabBar()
	body := m.visibleLines()
	lines := []string{
		tabBar,
		helpStyle.Render(strings.Repeat("─", m.width-2)),
		strings.Join(body, "\n"),
		"",
		helpStyle.Render("←/→ tabs • ↑↓ scroll • PgUp/PgDn • Esc close   " + m.positionHint()),
	}
	return panelStyle.Width(m.width).Height(m.height).Render(strings.Join(lines, "\n"))
}

func (m Model) renderTabBar() string {
	parts := make([]string, len(m.tabs))
	for i, t := range m.tabs {
		label := t.Title
		if i < 9 {
			label = strconv.Itoa(i+1) + ":" + label
		}
		if i == m.activeTab {
			parts[i] = tabActiveStyle.Render(label)
		} else {
			parts[i] = tabInactiveStyle.Render(label)
		}
	}
	return strings.Join(parts, " ")
}

func (m Model) currentLines() []string {
	if m.activeTab < 0 || m.activeTab >= len(m.lines) {
		return nil
	}
	return m.lines[m.activeTab]
}

func (m Model) visibleLines() []string {
	all := m.currentLines()
	bodyH := m.bodyHeight()
	start := m.scroll
	if start > len(all) {
		start = len(all)
	}
	end := start + bodyH
	if end > len(all) {
		end = len(all)
	}
	out := append([]string(nil), all[start:end]...)
	for len(out) < bodyH {
		out = append(out, "")
	}
	return out
}

func (m Model) bodyHeight() int {
	// panel height minus: tab bar(1) + divider(1) + footer(1) + blank(1) + border(2) + padding(0) = 6
	n := m.height - 6
	if n < 1 {
		return 1
	}
	return n
}

func (m Model) maxScroll() int {
	all := m.currentLines()
	max := len(all) - m.bodyHeight()
	if max < 0 {
		return 0
	}
	return max
}

func (m Model) positionHint() string {
	all := m.currentLines()
	if len(all) == 0 {
		return "0 lines"
	}
	start := m.scroll + 1
	end := m.scroll + m.bodyHeight()
	if end > len(all) {
		end = len(all)
	}
	return "lines " + itoa(start) + "-" + itoa(end) + " of " + itoa(len(all))
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
