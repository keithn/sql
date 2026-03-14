package app

import "github.com/charmbracelet/lipgloss"

func (m Model) View() string {
	resultsView := m.results.View()
	statusView := m.statusbar.View()

	var content string
	if m.resultsFullscreen {
		// Hide editor; give all space to statusbar + results.
		content = lipgloss.JoinVertical(lipgloss.Left, statusView, resultsView)
	} else {
		editorView := m.editor.View()
		content = lipgloss.JoinVertical(lipgloss.Left, editorView, statusView, resultsView)
	}

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

	if m.cellEdit.Active() {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, m.cellEdit.View())
	}

	if m.updatePreview.Active() {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, m.updatePreview.View())
	}

	if m.cellView.Active() {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, m.cellView.View())
	}

	if m.results.RowDetailOpen() {
		return m.results.RowDetailView(m.width, m.height)
	}

	return content
}
