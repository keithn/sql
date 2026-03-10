package modal

import tea "github.com/charmbracelet/bubbletea"

// Kind identifies the type of modal being shown.
type Kind int

const (
	KindNone Kind = iota
	KindConfirm     // [y/N] prompt
	KindInput       // single-line text input
	KindAddConn     // add connection (name + connection string)
	KindParam       // parameter substitution values
)

// Model is a generic overlay modal/dialog.
type Model struct {
	kind    Kind
	title   string
	message string
	input   string
	onDone  func(result string) tea.Cmd
}

func New() Model { return Model{} }

func (m Model) Active() bool { return m.kind != KindNone }

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	// TODO: handle key input, confirm/cancel, call onDone.
	return m, nil
}

func (m Model) View() string {
	if m.kind == KindNone {
		return ""
	}
	// TODO: render modal overlay with lipgloss border.
	return "[modal: " + m.title + "]"
}
