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

type connInputMode int

const (
	modeString connInputMode = iota
	modeForm
)

var formDrivers = []string{"postgres", "mssql", "sqlite"}

type AddConnSubmittedMsg struct {
	Name       string
	ConnString string
	Action     AddConnAction
}

// TestConnectionMsg asks the app to test a connection string.
type TestConnectionMsg struct{ ConnString string }

type ConfirmedMsg struct{ ID string }
type CancelledMsg struct{}

type Model struct {
	kind      Kind
	title     string
	width     int
	height    int
	// shared
	nameInput textinput.Model
	focus     int
	action    AddConnAction
	err       string
	// confirm
	confirmID string
	message   string
	confirm   string
	cancel    string
	// string mode
	connInput textinput.Model
	// form mode
	inputMode connInputMode
	driverIdx int
	hostInput textinput.Model
	portInput textinput.Model
	dbInput   textinput.Model
	userInput textinput.Model
	passInput textinput.Model
	// test status
	testStatus string
}

var (
	panelStyle        = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	titleStyle        = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#4ec9b0"))
	labelStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("#9cdcfe"))
	helpStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("#808080"))
	errStyle          = lipgloss.NewStyle().Foreground(lipgloss.Color("#f14c4c"))
	okStyle           = lipgloss.NewStyle().Foreground(lipgloss.Color("#4ec9b0"))
	activeBtn         = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ffffff")).Background(lipgloss.Color("#007acc")).Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#4fc1ff")).Padding(0, 1)
	buttonStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("#d4d4d4")).Border(lipgloss.RoundedBorder()).Padding(0, 1)
	modeActiveStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#1e1e1e")).Background(lipgloss.Color("#4ec9b0")).Padding(0, 1)
	modeInactiveStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#808080")).Padding(0, 1)
	modeFocusedStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ffffff")).Background(lipgloss.Color("#005f5f")).Padding(0, 1)
	driverActiveStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ce9178"))
)

func newTI(placeholder string, limit int) textinput.Model {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.Prompt = ""
	if limit > 0 {
		ti.CharLimit = limit
	}
	return ti
}

func New() Model {
	m := Model{
		title:     "Add Connection",
		nameInput: newTI("e.g. prod", 0),
		connInput: newTI("e.g. postgres://user:pass@host/db or file:local.db", 4096),
		hostInput: newTI("localhost", 0),
		portInput: newTI("5432", 8),
		dbInput:   newTI("mydb", 0),
		userInput: newTI("user", 0),
		passInput: newTI("", 0),
		action:    AddConnConnect,
		confirm:   "Confirm",
		cancel:    "Cancel",
	}
	m.passInput.EchoMode = textinput.EchoPassword
	m.passInput.EchoCharacter = '•'
	return m
}

func (m Model) Active() bool { return m.kind != KindNone }

func (m Model) SetSize(w, h int) Model {
	if w > 86 {
		w = 86
	}
	if w < 40 {
		w = 40
	}
	if h > 22 {
		h = 22
	}
	if h < 10 {
		h = 10
	}
	m.width = w
	m.height = h
	fw := w - 8
	m.nameInput.Width = fw
	m.connInput.Width = fw
	half := (fw - 10) / 2
	if half < 10 {
		half = 10
	}
	m.hostInput.Width = fw - 14
	m.portInput.Width = 10
	m.dbInput.Width = fw
	m.userInput.Width = half
	m.passInput.Width = half
	return m
}

func (m Model) OpenAddConnection() (Model, tea.Cmd) {
	m.kind = KindAddConn
	m.err = ""
	m.testStatus = ""
	m.focus = 0
	m.action = AddConnConnect
	m.inputMode = modeString
	m.driverIdx = 0
	m.nameInput.SetValue("")
	m.connInput.SetValue("")
	m.hostInput.SetValue("")
	m.portInput.SetValue("")
	m.dbInput.SetValue("")
	m.userInput.SetValue("")
	m.passInput.SetValue("")
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
	m.blurAll()
	return m, nil
}

func (m Model) Close() Model {
	m.kind = KindNone
	m.err = ""
	m.testStatus = ""
	m.focus = 0
	m.confirmID = ""
	m.message = ""
	m.confirm = "Confirm"
	m.cancel = "Cancel"
	m.blurAll()
	return m
}

func (m Model) SetTestStatus(status string) Model {
	m.testStatus = status
	return m
}

func (m *Model) blurAll() {
	m.nameInput.Blur()
	m.connInput.Blur()
	m.hostInput.Blur()
	m.portInput.Blur()
	m.dbInput.Blur()
	m.userInput.Blur()
	m.passInput.Blur()
}

// actionsFocusIdx is the focus index for the action buttons row.
func (m Model) actionsFocusIdx() int {
	if m.inputMode == modeString {
		return 3 // 0=name 1=mode 2=conn 3=actions
	}
	if formDrivers[m.driverIdx] == "sqlite" {
		return 4 // 0=name 1=mode 2=driver 3=file 4=actions
	}
	return 8 // 0=name 1=mode 2=driver 3=host 4=port 5=db 6=user 7=pass 8=actions
}

// focusIsInput reports whether the current focus is on a text input (not the
// mode selector, driver selector, or action buttons row).
func (m Model) focusIsInput() bool {
	af := m.actionsFocusIdx()
	if m.focus == 0 {
		return true
	}
	if m.focus == 1 {
		return false // mode selector
	}
	if m.inputMode == modeForm && m.focus == 2 {
		return false // driver selector
	}
	if m.focus == af {
		return false // actions
	}
	return true
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

		case "ctrl+t":
			cs := m.effectiveConnString()
			if cs == "" {
				m.err = "enter connection details first"
				return m, nil
			}
			m.testStatus = "Testing…"
			m.err = ""
			return m, func() tea.Msg { return TestConnectionMsg{ConnString: cs} }

		case "tab", "shift+tab":
			return m.moveFocus(key.String())

		case "up", "down":
			// up/down only navigate focus when not in a text input
			if !m.focusIsInput() {
				return m.moveFocus(key.String())
			}

		case " ":
			// Space on mode or driver selector toggles/cycles
			if m.focus == 1 {
				return m.switchMode()
			}
			if m.inputMode == modeForm && m.focus == 2 {
				m.driverIdx = (m.driverIdx + 1) % len(formDrivers)
				m.updatePortDefault()
				return m, nil
			}

		case "left":
			switch {
			case m.focus == m.actionsFocusIdx():
				m.action = prevAction(m.action)
				m.testStatus = ""
				return m, nil
			case m.focus == 1:
				// mode selector: switch to string
				if m.inputMode != modeString {
					return m.switchMode()
				}
				return m, nil
			case m.inputMode == modeForm && m.focus == 2:
				m.driverIdx = (m.driverIdx + len(formDrivers) - 1) % len(formDrivers)
				m.updatePortDefault()
				return m, nil
			}
			// fall through to text input routing

		case "right":
			switch {
			case m.focus == m.actionsFocusIdx():
				m.action = nextAction(m.action)
				m.testStatus = ""
				return m, nil
			case m.focus == 1:
				// mode selector: switch to form
				if m.inputMode != modeForm {
					return m.switchMode()
				}
				return m, nil
			case m.inputMode == modeForm && m.focus == 2:
				m.driverIdx = (m.driverIdx + 1) % len(formDrivers)
				m.updatePortDefault()
				return m, nil
			}
			// fall through to text input routing

		case "enter":
			if m.focus < m.actionsFocusIdx() && !m.focusIsInput() {
				// on non-input selectors, Enter advances focus
				return m.moveFocus("tab")
			}
			if m.focus < m.actionsFocusIdx() {
				break // on text inputs, Enter does nothing special
			}
			if err := m.validate(); err != "" {
				m.err = err
				return m, nil
			}
			if m.action == AddConnCancel {
				return m.Close(), func() tea.Msg { return CancelledMsg{} }
			}
			out := AddConnSubmittedMsg{
				Name:       strings.TrimSpace(m.nameInput.Value()),
				ConnString: m.effectiveConnString(),
				Action:     m.action,
			}
			return m, func() tea.Msg { return out }
		}
	}

	// Route to focused text input.
	var cmd tea.Cmd
	switch {
	case m.inputMode == modeString:
		switch m.focus {
		case 0:
			m.nameInput, cmd = m.nameInput.Update(msg)
		case 2:
			m.connInput, cmd = m.connInput.Update(msg)
		}
	case m.inputMode == modeForm:
		switch m.focus {
		case 0:
			m.nameInput, cmd = m.nameInput.Update(msg)
		case 3:
			m.hostInput, cmd = m.hostInput.Update(msg)
		case 4:
			m.portInput, cmd = m.portInput.Update(msg)
		case 5:
			m.dbInput, cmd = m.dbInput.Update(msg)
		case 6:
			m.userInput, cmd = m.userInput.Update(msg)
		case 7:
			m.passInput, cmd = m.passInput.Update(msg)
		}
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
	}
	return m, nil
}

func (m Model) switchMode() (Model, tea.Cmd) {
	m.blurAll()
	m.testStatus = ""
	m.err = ""
	if m.inputMode == modeString {
		m.inputMode = modeForm
	} else {
		m.inputMode = modeString
	}
	// Stay on focus 1 (the mode selector) so the user sees the change.
	return m, nil
}

func (m Model) moveFocus(key string) (Model, tea.Cmd) {
	step := 1
	if key == "shift+tab" || key == "up" {
		step = -1
	}
	count := m.actionsFocusIdx() + 1
	m.focus = (m.focus + step + count) % count
	m.blurAll()
	switch {
	case m.focus == 0:
		return m, m.nameInput.Focus()
	case m.inputMode == modeString && m.focus == 2:
		return m, m.connInput.Focus()
	case m.inputMode == modeForm && m.focus == 3:
		return m, m.hostInput.Focus()
	case m.inputMode == modeForm && m.focus == 4:
		return m, m.portInput.Focus()
	case m.inputMode == modeForm && m.focus == 5:
		return m, m.dbInput.Focus()
	case m.inputMode == modeForm && m.focus == 6:
		return m, m.userInput.Focus()
	case m.inputMode == modeForm && m.focus == 7:
		return m, m.passInput.Focus()
	}
	return m, nil
}

func (m *Model) updatePortDefault() {
	switch formDrivers[m.driverIdx] {
	case "postgres":
		m.portInput.Placeholder = "5432"
	case "mssql":
		m.portInput.Placeholder = "1433"
	default:
		m.portInput.Placeholder = ""
	}
}

func (m Model) effectiveConnString() string {
	if m.inputMode == modeString {
		return strings.TrimSpace(m.connInput.Value())
	}
	return m.assembleConnString()
}

func (m Model) assembleConnString() string {
	switch formDrivers[m.driverIdx] {
	case "sqlite":
		path := strings.TrimSpace(m.hostInput.Value())
		if path == "" {
			return ""
		}
		if !strings.HasPrefix(path, "file:") {
			return "file:" + path
		}
		return path
	case "postgres":
		host := strOrDef(m.hostInput.Value(), "localhost")
		port := strOrDef(m.portInput.Value(), "5432")
		db := strings.TrimSpace(m.dbInput.Value())
		user := strings.TrimSpace(m.userInput.Value())
		pass := strings.TrimSpace(m.passInput.Value())
		if db == "" {
			db = user
		}
		switch {
		case user == "" && pass == "":
			return fmt.Sprintf("postgres://%s:%s/%s", host, port, db)
		case pass == "":
			return fmt.Sprintf("postgres://%s@%s:%s/%s", user, host, port, db)
		default:
			return fmt.Sprintf("postgres://%s:%s@%s:%s/%s", user, pass, host, port, db)
		}
	case "mssql":
		host := strOrDef(m.hostInput.Value(), "localhost")
		port := strOrDef(m.portInput.Value(), "1433")
		db := strings.TrimSpace(m.dbInput.Value())
		user := strings.TrimSpace(m.userInput.Value())
		pass := strings.TrimSpace(m.passInput.Value())
		var cs string
		if user == "" && pass == "" {
			cs = fmt.Sprintf("sqlserver://%s:%s", host, port)
		} else {
			cs = fmt.Sprintf("sqlserver://%s:%s@%s:%s", user, pass, host, port)
		}
		if db != "" {
			cs += "?database=" + db
		}
		return cs
	}
	return ""
}

func strOrDef(s, def string) string {
	if s = strings.TrimSpace(s); s != "" {
		return s
	}
	return def
}

// ---------- View ----------

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
			helpStyle.Render("←/→ or Tab choose • Enter confirm • Esc cancel"),
		}, "\n"))
	}

	lines := []string{
		titleStyle.Render(m.title),
		"",
		labelStyle.Render("Name (required for save):"),
		m.nameInput.View(),
		"",
		m.renderModeSelector(),
		"",
	}

	if m.inputMode == modeString {
		lines = append(lines,
			labelStyle.Render("Connection string:"),
			m.connInput.View(),
			"",
			helpStyle.Render("Detected driver: ")+detectedDriverLabel(m.connInput.Value()),
		)
	} else {
		lines = append(lines, m.renderFormFields()...)
	}

	if m.testStatus != "" {
		switch {
		case strings.HasPrefix(m.testStatus, "✓"):
			lines = append(lines, okStyle.Render(m.testStatus))
		case strings.HasPrefix(m.testStatus, "✗"):
			lines = append(lines, errStyle.Render(m.testStatus))
		default:
			lines = append(lines, helpStyle.Render(m.testStatus))
		}
	}

	lines = append(lines, m.renderActions())
	lines = append(lines, helpStyle.Render("Tab/Shift+Tab navigate • ←/→ action or driver • Ctrl+T test • Esc cancel"))
	if m.err != "" {
		lines = append(lines, errStyle.Render(m.err))
	}
	return panelStyle.Width(m.width).Height(m.height).Render(strings.Join(lines, "\n"))
}

func (m Model) renderModeSelector() string {
	focusedOnMode := m.focus == 1

	styleFor := func(mode connInputMode) lipgloss.Style {
		active := m.inputMode == mode
		switch {
		case active && focusedOnMode:
			return modeFocusedStyle
		case active:
			return modeActiveStyle
		default:
			return modeInactiveStyle
		}
	}

	str := styleFor(modeString).Render("Connection String")
	frm := styleFor(modeForm).Render("Form Builder")

	line := str + "  " + frm
	if focusedOnMode {
		line += "  " + helpStyle.Render("←/→ or Space to switch")
	}
	return line
}

func (m Model) renderFormFields() []string {
	driver := formDrivers[m.driverIdx]
	focusedOnDriver := m.focus == 2

	// Driver selector
	prev := formDrivers[(m.driverIdx+len(formDrivers)-1)%len(formDrivers)]
	next := formDrivers[(m.driverIdx+1)%len(formDrivers)]
	driverLine := labelStyle.Render("Driver: ")
	if focusedOnDriver {
		driverLine += helpStyle.Render("‹ " + prev + "  ") + driverActiveStyle.Render(strings.ToUpper(driver)) + helpStyle.Render("  "+next+" ›") + helpStyle.Render("  ←/→ or Space")
	} else {
		driverLine += helpStyle.Render("‹ "+prev+"  ") + driverActiveStyle.Render(strings.ToUpper(driver)) + helpStyle.Render("  "+next+" ›")
	}

	if driver == "sqlite" {
		lines := []string{
			driverLine,
			"",
			labelStyle.Render("File path:"),
			m.hostInput.View(),
		}
		if cs := m.assembleConnString(); cs != "" {
			lines = append(lines, helpStyle.Render("→ "+cs))
		}
		lines = append(lines, "")
		return lines
	}

	// postgres / mssql
	lines := []string{
		driverLine,
		"",
		labelStyle.Render("Host:"),
		m.hostInput.View(),
		labelStyle.Render("Port:"),
		m.portInput.View(),
		labelStyle.Render("Database:"),
		m.dbInput.View(),
		labelStyle.Render("Username:"),
		m.userInput.View(),
		labelStyle.Render("Password:"),
		m.passInput.View(),
	}
	if cs := m.assembleConnString(); cs != "" {
		lines = append(lines, "", helpStyle.Render("→ "+cs))
	}
	lines = append(lines, "")
	return lines
}

func (m Model) renderConfirmActions() string {
	labels := []string{m.confirm, m.cancel}
	parts := make([]string, 0, len(labels))
	for i, label := range labels {
		s := buttonStyle
		if m.focus == i {
			s = activeBtn
		}
		parts = append(parts, s.Render(label))
	}
	return lipgloss.JoinHorizontal(lipgloss.Left, parts...)
}

func (m Model) renderActions() string {
	type btn struct {
		action AddConnAction
		label  string
	}
	btns := []btn{
		{AddConnConnect, "Connect"},
		{AddConnSaveConnect, "Save & Connect"},
		{AddConnSaveOnly, "Save Only"},
		{AddConnCancel, "Cancel"},
	}
	parts := make([]string, 0, len(btns))
	for _, b := range btns {
		s := buttonStyle
		if m.focus == m.actionsFocusIdx() && m.action == b.action {
			s = activeBtn
		}
		parts = append(parts, s.Render(b.label))
	}
	return lipgloss.JoinHorizontal(lipgloss.Left, parts...)
}

func (m Model) validate() string {
	cs := m.effectiveConnString()
	if cs == "" {
		if m.inputMode == modeString {
			return "connection string is required"
		}
		return "fill in the connection details"
	}
	if connections.DetectDriver(cs) == "" {
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
	return driver
}

func nextAction(a AddConnAction) AddConnAction { return AddConnAction((int(a) + 1) % 4) }
func prevAction(a AddConnAction) AddConnAction { return AddConnAction((int(a) + 3) % 4) }
