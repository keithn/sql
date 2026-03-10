package app

import "github.com/charmbracelet/lipgloss"

func (m Model) View() string {
	editorView := m.editor.View()
	resultsView := m.results.View()
	statusView := m.statusbar.View()

	// Stack editor and results vertically.
	content := lipgloss.JoinVertical(lipgloss.Left, editorView, resultsView)

	// Place schema overlay to the left when open.
	if m.schemaOpen {
		schemaView := m.schema.View()
		content = lipgloss.JoinHorizontal(lipgloss.Top, schemaView, content)
	}

	return lipgloss.JoinVertical(lipgloss.Left, content, statusView)
}
