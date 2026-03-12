package modal

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sqltui/sql/internal/connections"
)

type Kind int
type AddConnAction int

const (
	KindNone Kind = iota
	KindAddConn
	KindConfirm
)

const (
	AddConnConnect AddConnAction = iota
	AddConnSaveConnect
	AddConnSaveOnly
	AddConnCancel
)

type AddConnSubmittedMsg struct {
	Name       string
	ConnString string
	Action     AddConnAction
}

type ConfirmedMsg struct{ ID string }

type CancelledMsg struct{}

type Model struct {
	kind      Kind
	title     string
	width     int
	height    int
	nameInput textinput.Model
	connInput textinput.Model
	focus     int
	action    AddConnAction
	err       string
	confirmID string
	message   string
	confirm   string
	cancel    string
}

var (
	panelStyle  = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#4ec9b0"))
	labelStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#9cdcfe"))
	helpStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#808080"))
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#f14c4c"))
	activeBtn   = lipgloss.NewStyle().Foreground(lipgloss.Color("#ffffff")).Background(lipgloss.Color("#007acc")).Padding(0, 1)
	buttonStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#d4d4d4")).Border(lipgloss.RoundedBorder()).Padding(0, 1)
)

func New() Model {
	nameInput := textinput.New()
	nameInput.Placeholder = "e.g. prod"
	nameInput.Prompt = ""

	connInput := textinput.New()
	connInput.Placeholder = "e.g. postgres://user:pass@host/db or file:local.db"
	connInput.Prompt = ""
	connInput.CharLimit = 4096

	return Model{
		title:     "Add Connection",
		nameInput: nameInput,
		connInput: connInput,
		action:    AddConnConnect,
		confirm:   "Confirm",
		cancel:    "Cancel",
	}
}

func (m Model) Active() bool { return m.kind != KindNone }

func (m Model) SetSize(w, h int) Model {
	if w > 86 {
		w = 86
	}
	if w < 40 {
		w = 40
	}
	if h > 16 {
		h = 16
	}
	if h < 10 {
		h = 10
	}
	m.width = w
	m.height = h
	m.nameInput.Width = w - 8
	m.connInput.Width = w - 8
	return m
}

func (m Model) OpenAddConnection() (Model, tea.Cmd) {
	m.kind = KindAddConn
	m.err = ""
	m.focus = 0
	m.action = AddConnConnect
	m.nameInput.SetValue("")
	m.connInput.SetValue("")
	return m, m.nameInput.Focus()
}

func (m Model) OpenConfirm(id, title, message, confirmLabel string) (Model, tea.Cmd) {
	m.kind = KindConfirm
	m.title = title
	m.confirmID = id
	m.message = strings.TrimSpace(message)
	m.confirm = strings.TrimSpace(confirmLabel)
	if m.confirm == "" {
		m.confirm = "Confirm"
	}
	m.cancel = "Cancel"
	m.err = ""
	m.focus = 0
	m.nameInput.Blur()
	m.connInput.Blur()
	return m, nil
}

func (m Model) Close() Model {
	m.kind = KindNone
	m.err = ""
	m.focus = 0
	m.confirmID = ""
	m.message = ""
	m.confirm = "Confirm"
	m.cancel = "Cancel"
	m.nameInput.Blur()
	m.connInput.Blur()
	return m
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if !m.Active() {
		return m, nil
	}
	if key, ok := msg.(tea.KeyMsg); ok {
		if m.kind == KindConfirm {
			return m.updateConfirm(key)
		}
		switch key.String() {
		case "esc", "ctrl+c":
			return m.Close(), func() tea.Msg { return CancelledMsg{} }
		case "tab", "shift+tab", "up", "down":
			return m.moveFocus(key.String())
		case "left":
			if m.focus == 2 {
				m.action = prevAction(m.action)
			}
			return m, nil
		case "right":
			if m.focus == 2 {
				m.action = nextAction(m.action)
			}
			return m, nil
		case "enter":
			if m.focus < 2 {
				return m.moveFocus("tab")
			}
			if err := m.validate(); err != "" {
				m.err = err
				return m, nil
			}
			if m.action == AddConnCancel {
				return m.Close(), func() tea.Msg { return CancelledMsg{} }
			}
			msg := AddConnSubmittedMsg{
				Name:       strings.TrimSpace(m.nameInput.Value()),
				ConnString: strings.TrimSpace(m.connInput.Value()),
				Action:     m.action,
			}
			return m, func() tea.Msg { return msg }
		}
	}
	var cmd tea.Cmd
	switch m.focus {
	case 0:
		m.nameInput, cmd = m.nameInput.Update(msg)
	case 1:
		m.connInput, cmd = m.connInput.Update(msg)
	}
	m.err = ""
	return m, cmd
}

func (m Model) updateConfirm(key tea.KeyMsg) (Model, tea.Cmd) {
	switch key.String() {
	case "esc", "ctrl+c":
		return m.Close(), func() tea.Msg { return CancelledMsg{} }
	case "tab", "shift+tab", "left", "right", "up", "down":
		m.focus = (m.focus + 1) % 2
		return m, nil
	case "enter":
		if m.focus == 0 {
			id := m.confirmID
			return m.Close(), func() tea.Msg { return ConfirmedMsg{ID: id} }
		}
		return m.Close(), func() tea.Msg { return CancelledMsg{} }
	default:
		return m, nil
	}
}

func (m Model) View() string {
	if !m.Active() {
		return ""
	}
	if m.kind == KindConfirm {
		return panelStyle.Width(m.width).Height(m.height).Render(strings.Join([]string{
			titleStyle.Render(m.title),
			"",
			m.message,
			"",
			m.renderConfirmActions(),
			helpStyle.Render("←/→ or Tab choose action • Enter confirm • Esc cancel"),
		}, "\n"))
	}
	lines := []string{
		titleStyle.Render(m.title),
		"",
		labelStyle.Render("Name (required for save):"),
		m.nameInput.View(),
		"",
		labelStyle.Render("Connection string:"),
		m.connInput.View(),
		"",
		labelStyle.Render("Detected driver: ") + detectedDriverLabel(m.connInput.Value()),
		m.renderActions(),
		helpStyle.Render("Tab/Shift+Tab move focus • ←/→ choose action • Enter submit • Esc cancel"),
	}
	if m.err != "" {
		lines = append(lines, errStyle.Render(m.err))
	}
	return panelStyle.Width(m.width).Height(m.height).Render(strings.Join(lines, "\n"))
}

func (m Model) renderConfirmActions() string {
	labels := []string{m.confirm, m.cancel}
	parts := make([]string, 0, len(labels))
	for i, label := range labels {
		style := buttonStyle
		if m.focus == i {
			style = activeBtn
		}
		parts = append(parts, style.Render(label))
	}
	return lipgloss.JoinHorizontal(lipgloss.Left, parts...)
}

func (m Model) moveFocus(key string) (Model, tea.Cmd) {
	step := 1
	if key == "shift+tab" || key == "up" {
		step = -1
	}
	m.focus = (m.focus + step + 3) % 3
	m.nameInput.Blur()
	m.connInput.Blur()
	switch m.focus {
	case 0:
		return m, m.nameInput.Focus()
	case 1:
		return m, m.connInput.Focus()
	default:
		return m, nil
	}
}

func (m Model) renderActions() string {
	labels := []struct {
		action AddConnAction
		text   string
	}{
		{AddConnConnect, "Connect"},
		{AddConnSaveConnect, "Save & Connect"},
		{AddConnSaveOnly, "Save Only"},
		{AddConnCancel, "Cancel"},
	}
	parts := make([]string, 0, len(labels))
	for _, label := range labels {
		style := buttonStyle
		if m.focus == 2 && m.action == label.action {
			style = activeBtn
		}
		parts = append(parts, style.Render(label.text))
	}
	return lipgloss.JoinHorizontal(lipgloss.Left, parts...)
}

func (m Model) validate() string {
	if strings.TrimSpace(m.connInput.Value()) == "" {
		return "connection string is required"
	}
	if connections.DetectDriver(strings.TrimSpace(m.connInput.Value())) == "" {
		return "could not detect driver from connection string"
	}
	if (m.action == AddConnSaveConnect || m.action == AddConnSaveOnly) && strings.TrimSpace(m.nameInput.Value()) == "" {
		return "name is required when saving a connection"
	}
	return ""
}

func detectedDriverLabel(conn string) string {
	driver := connections.DetectDriver(strings.TrimSpace(conn))
	if driver == "" {
		return helpStyle.Render("unknown")
	}
	return fmt.Sprintf("%s", driver)
}

func nextAction(a AddConnAction) AddConnAction {
	return AddConnAction((int(a) + 1) % 4)
}

func prevAction(a AddConnAction) AddConnAction {
	return AddConnAction((int(a) + 3) % 4)
}
