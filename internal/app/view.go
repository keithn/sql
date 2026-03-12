package app

import "github.com/charmbracelet/lipgloss"

func (m Model) View() string {
	editorView := m.editor.View()
	resultsView := m.results.View()
	statusView := m.statusbar.View()

	// Stack editor, statusbar, and results vertically.
	content := lipgloss.JoinVertical(lipgloss.Left, editorView, statusView, resultsView)

	if m.modal.Active() {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, m.modal.View())
	}

	if m.help.Active() {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, m.help.View())
	}

	if m.schema.Active() {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, m.schema.View())
	}

	if m.palette.Active() {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, m.palette.View())
	}

	return content
}
