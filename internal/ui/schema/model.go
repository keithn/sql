package schema

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sqltui/sql/internal/db"
)

var (
	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, true, false, false).
			BorderForeground(lipgloss.Color("#444444"))

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#007acc")).
			Padding(0, 1)

	nodeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#d4d4d4"))

	selectedNodeStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#ffffff")).
				Background(lipgloss.Color("#264f78"))

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#555555"))
)

type nodeKind int

const (
	kindConnection nodeKind = iota
	kindDatabase
	kindSchema
	kindTableGroup
	kindTable
	kindColumn
)

// treeNode is one item in the schema tree.
type treeNode struct {
	label    string
	kind     nodeKind
	open     bool
	children []*treeNode
}

// flatNode is a treeNode plus its computed depth for display.
type flatNode struct {
	node  *treeNode
	depth int
}

// Model is the schema browser overlay.
type Model struct {
	width   int
	height  int
	roots   []*treeNode
	cursor  int // index into visibleNodes()
	focused bool
}

func New() Model {
	root := &treeNode{label: "(no connection)", kind: kindConnection}
	return Model{roots: []*treeNode{root}}
}

// SetSchema rebuilds the tree from an introspected schema.
func (m Model) SetSchema(s *db.Schema, connName string) Model {
	root := &treeNode{label: connName, kind: kindConnection, open: true}
	for _, database := range s.Databases {
		dbNode := &treeNode{label: database.Name, kind: kindDatabase, open: true}
		for _, schema := range database.Schemas {
			schNode := &treeNode{label: schema.Name, kind: kindSchema, open: true}

			if len(schema.Tables) > 0 {
				tblGroup := &treeNode{label: "Tables", kind: kindTableGroup, open: true}
				for _, t := range schema.Tables {
					tblNode := &treeNode{label: t.Name, kind: kindTable}
					for _, c := range t.Columns {
						label := c.Name + "  " + c.Type
						if c.PrimaryKey {
							label += " 🔑"
						}
						tblNode.children = append(tblNode.children, &treeNode{label: label, kind: kindColumn})
					}
					tblGroup.children = append(tblGroup.children, tblNode)
				}
				schNode.children = append(schNode.children, tblGroup)
			}

			if len(schema.Views) > 0 {
				viewGroup := &treeNode{label: "Views", kind: kindTableGroup, open: false}
				for _, v := range schema.Views {
					vNode := &treeNode{label: v.Name, kind: kindTable}
					for _, c := range v.Columns {
						vNode.children = append(vNode.children, &treeNode{label: c.Name + "  " + c.Type, kind: kindColumn})
					}
					viewGroup.children = append(viewGroup.children, vNode)
				}
				schNode.children = append(schNode.children, viewGroup)
			}

			dbNode.children = append(dbNode.children, schNode)
		}
		root.children = append(root.children, dbNode)
	}
	m.roots = []*treeNode{root}
	m.cursor = 0
	return m
}

// visibleNodes returns the currently visible flattened list.
func (m Model) visibleNodes() []flatNode {
	var out []flatNode
	var walk func(n *treeNode, depth int)
	walk = func(n *treeNode, depth int) {
		out = append(out, flatNode{node: n, depth: depth})
		if n.open {
			for _, child := range n.children {
				walk(child, depth+1)
			}
		}
	}
	for _, r := range m.roots {
		walk(r, 0)
	}
	return out
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if !m.focused {
		return m, nil
	}
	visible := m.visibleNodes()
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "j", "down":
			if m.cursor < len(visible)-1 {
				m.cursor++
			}
		case "k", "up":
			if m.cursor > 0 {
				m.cursor--
			}
		case "g":
			m.cursor = 0
		case "G":
			m.cursor = len(visible) - 1
		case "l", "enter", "right":
			if m.cursor < len(visible) {
				visible[m.cursor].node.open = true
			}
		case "h", "left":
			if m.cursor < len(visible) {
				visible[m.cursor].node.open = false
			}
		}
	}
	return m, nil
}

func (m Model) View() string {
	title := titleStyle.Render("Schema")
	inner := m.renderTree()

	lines := strings.Split(inner, "\n")
	for len(lines) < m.height-2 {
		lines = append(lines, "")
	}

	content := title + "\n" + strings.Join(lines, "\n")
	return panelStyle.
		Width(m.width - 1).
		Height(m.height).
		Render(content)
}

func (m Model) renderTree() string {
	visible := m.visibleNodes()
	var sb strings.Builder
	for i, fn := range visible {
		indent := strings.Repeat("  ", fn.depth)
		icon := nodeIcon(fn.node)
		line := indent + icon + " " + fn.node.label

		avail := m.width - 3
		if avail > 0 && len(line) > avail {
			line = line[:avail-1] + "…"
		}

		if i == m.cursor {
			sb.WriteString(selectedNodeStyle.Width(m.width - 2).Render(line))
		} else {
			sb.WriteString(nodeStyle.Render(line))
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

func nodeIcon(n *treeNode) string {
	switch n.kind {
	case kindConnection:
		return "●"
	case kindDatabase:
		if n.open {
			return "▼"
		}
		return "▶"
	case kindSchema:
		if n.open {
			return "▼"
		}
		return "▷"
	case kindTableGroup:
		if n.open {
			return "▼"
		}
		return "▷"
	case kindTable:
		if len(n.children) > 0 {
			if n.open {
				return "▼"
			}
			return "⊞"
		}
		return "⊞"
	case kindColumn:
		return " "
	}
	return " "
}

func (m Model) SetSize(w, h int) Model {
	m.width = w
	m.height = h
	return m
}

func (m Model) Focus() Model {
	m.focused = true
	return m
}

func (m Model) Blur() Model {
	m.focused = false
	return m
}

// unused but kept to avoid lint errors
var _ = fmt.Sprintf
