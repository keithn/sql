package palette

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"
)

type Item struct {
	Key     string
	Title   string
	Driver  string
	Summary string
}

type AcceptedMsg struct{ Key string }
type CancelledMsg struct{}

type Model struct {
	active   bool
	title    string
	width    int
	height   int
	input    textinput.Model
	items    []Item
	filtered []Item
	cursor   int
}

var (
	panelStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#4ec9b0"))
	promptStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#c586c0"))
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#ffffff")).Background(lipgloss.Color("#007acc"))
	metaStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#9cdcfe"))
	emptyStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#808080"))
)

func New() Model {
	input := textinput.New()
	input.Prompt = "> "
	input.Placeholder = "Filter connections"
	input.CharLimit = 256
	input.PromptStyle = promptStyle
	return Model{title: "Connections", input: input}
}

func (m Model) Active() bool { return m.active }

func (m Model) OpenConnections(items []Item) (Model, tea.Cmd) {
	m.active = true
	m.items = append([]Item(nil), items...)
	m.cursor = 0
	m.input.SetValue("")
	m.syncFiltered()
	return m, m.input.Focus()
}

func (m Model) Close() Model {
	m.active = false
	m.cursor = 0
	m.input.Blur()
	m.input.SetValue("")
	m.filtered = nil
	return m
}

func (m Model) SetSize(w, h int) Model {
	if w > 72 {
		w = 72
	}
	if w < 24 {
		w = 24
	}
	if h > 14 {
		h = 14
	}
	if h < 6 {
		h = 6
	}
	m.width = w
	m.height = h
	m.input.Width = w - 6
	return m
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if !m.active {
		return m, nil
	}
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "esc", "ctrl+c":
			return m.Close(), func() tea.Msg { return CancelledMsg{} }
		case "up", "ctrl+p":
			if m.cursor > 0 {
				m.cursor--
			}
			return m, nil
		case "down", "ctrl+n":
			if m.cursor < len(m.filtered)-1 {
				m.cursor++
			}
			return m, nil
		case "enter":
			if len(m.filtered) == 0 {
				return m, nil
			}
			item := m.filtered[m.cursor]
			return m.Close(), func() tea.Msg { return AcceptedMsg{Key: item.Key} }
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.syncFiltered()
	return m, cmd
}

func (m Model) View() string {
	if !m.active {
		return ""
	}
	lines := []string{titleStyle.Render(m.title), m.input.View(), ""}
	maxItems := m.height - 4
	if maxItems < 1 {
		maxItems = 1
	}
	if len(m.filtered) == 0 {
		lines = append(lines, emptyStyle.Render("No matching connections"))
	} else {
		for i, item := range m.filtered {
			if i >= maxItems {
				break
			}
			line := fmt.Sprintf("  %s  %-2s  %s", item.Title, driverBadge(item.Driver), metaStyle.Render(item.Summary))
			if i == m.cursor {
				line = selectedStyle.Width(max(0, m.width-4)).Render(strings.TrimRight(line, " "))
			}
			lines = append(lines, line)
		}
	}
	lines = append(lines, "", emptyStyle.Render("Enter connect • Ctrl+N/A add connection • Esc close"))
	content := strings.Join(lines, "\n")
	return panelStyle.Width(m.width).Height(m.height).Render(content)
}

func (m *Model) syncFiltered() {
	query := strings.TrimSpace(m.input.Value())
	if query == "" {
		m.filtered = append([]Item(nil), m.items...)
		if m.cursor >= len(m.filtered) {
			m.cursor = max(0, len(m.filtered)-1)
		}
		return
	}
	targets := make([]string, len(m.items))
	for i, item := range m.items {
		targets[i] = item.Title + " " + item.Driver + " " + item.Summary
	}
	matches := fuzzy.Find(strings.ToLower(query), lowerAll(targets))
	filtered := make([]Item, 0, len(matches))
	for _, match := range matches {
		filtered = append(filtered, m.items[match.Index])
	}
	m.filtered = filtered
	if m.cursor >= len(m.filtered) {
		m.cursor = max(0, len(m.filtered)-1)
		return
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

func lowerAll(in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = strings.ToLower(s)
	}
	return out
}

func driverBadge(driver string) string {
	switch strings.ToLower(driver) {
	case "postgres":
		return "PG"
	case "mssql":
		return "MS"
	case "sqlite":
		return "SQ"
	default:
		return "DB"
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
