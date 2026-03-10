package statusbar

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

var (
	barStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#007acc")).
			Foreground(lipgloss.Color("#ffffff"))

	connStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#005a9e")).
			Foreground(lipgloss.Color("#ffffff")).
			Padding(0, 1)

	modeNormal = lipgloss.NewStyle().
			Background(lipgloss.Color("#4ec9b0")).
			Foreground(lipgloss.Color("#000000")).
			Bold(true).
			Padding(0, 1)

	modeInsert = lipgloss.NewStyle().
			Background(lipgloss.Color("#569cd6")).
			Foreground(lipgloss.Color("#000000")).
			Bold(true).
			Padding(0, 1)

	modeVisual = lipgloss.NewStyle().
			Background(lipgloss.Color("#ce9178")).
			Foreground(lipgloss.Color("#000000")).
			Bold(true).
			Padding(0, 1)

	txStyle = lipgloss.NewStyle().
		Background(lipgloss.Color("#ffcc00")).
		Foreground(lipgloss.Color("#000000")).
		Bold(true).
		Padding(0, 1)

	metaStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#007acc")).
			Foreground(lipgloss.Color("#cce7ff")).
			Padding(0, 1)
)

// Model renders the single-line status bar at the bottom of the screen.
type Model struct {
	width      int
	conn       string
	database   string
	vimMode    string // "NORMAL", "INSERT", "VISUAL", "V-LINE", or ""
	txActive   bool
	rowCount   int
	durationMs int64
	errMsg     string
	pane       string // "EDITOR", "RESULTS", "SCHEMA"
}

func New() Model {
	return Model{conn: "disconnected", pane: "EDITOR"}
}

func (m Model) View() string {
	// Left side segments.
	var left string

	// Connection indicator.
	connLabel := "⬤  " + m.conn
	if m.database != "" {
		connLabel += "  /  " + m.database
	}
	left += connStyle.Render(connLabel)

	// Vim mode indicator.
	if m.vimMode != "" {
		switch m.vimMode {
		case "INSERT":
			left += modeInsert.Render(m.vimMode)
		case "VISUAL", "V-LINE":
			left += modeVisual.Render(m.vimMode)
		default:
			left += modeNormal.Render(m.vimMode)
		}
	}

	// Transaction indicator.
	if m.txActive {
		left += txStyle.Render("TXN")
	}

	// Right side segments.
	var right string
	if m.errMsg != "" {
		errSty := lipgloss.NewStyle().
			Background(lipgloss.Color("#f44747")).
			Foreground(lipgloss.Color("#ffffff")).
			Padding(0, 1)
		right += errSty.Render("⚠ " + m.errMsg)
	} else if m.rowCount > 0 {
		right += metaStyle.Render(fmt.Sprintf("%d rows  %dms", m.rowCount, m.durationMs))
	}

	right += metaStyle.Render(m.pane)

	// Pad the middle.
	leftLen := lipgloss.Width(left)
	rightLen := lipgloss.Width(right)
	gap := m.width - leftLen - rightLen
	if gap < 0 {
		gap = 0
	}
	middle := barStyle.Render(fmt.Sprintf("%*s", gap, ""))

	return left + middle + right
}

func (m Model) SetWidth(w int) Model       { m.width = w; return m }
func (m Model) SetConnection(s string) Model { m.conn = s; return m }
func (m Model) SetDatabase(s string) Model { m.database = s; return m }
func (m Model) SetVimMode(s string) Model  { m.vimMode = s; return m }
func (m Model) SetTx(active bool) Model    { m.txActive = active; return m }
func (m Model) SetRows(n int) Model        { m.rowCount = n; return m }
func (m Model) SetDuration(ms int64) Model { m.durationMs = ms; return m }
func (m Model) SetError(s string) Model    { m.errMsg = s; return m }
func (m Model) SetPane(s string) Model     { m.pane = s; return m }
