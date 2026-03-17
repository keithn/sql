package app

import (
	"fmt"
	"path/filepath"
	"sort"

	"github.com/sqltui/sql/internal/config"
	"github.com/sqltui/sql/internal/connections"
	uihelp "github.com/sqltui/sql/internal/ui/help"
)

const (
	HelpTabEditor  = 0
	HelpTabResults = 1
	HelpTabGeneral = 2
	HelpTabSettings = 3
)

func (m Model) helpTabs() []uihelp.Tab {
	configDir, _ := config.ConfigDir()
	dataDir, _ := config.DataDir()
	named, _ := connections.List(m.cfg)
	vimMode := m.editor.VimMode()
	if vimMode == "" {
		vimMode = "off"
	}
	activeConn := m.activeConn
	if activeConn == "" {
		activeConn = "_adhoc"
	}

	editorTab := uihelp.Tab{
		Title: "Editor",
		Sections: []uihelp.Section{{
			Title: "Editing",
			Lines: []string{
				"• Ctrl+Z — undo not supported in non-vim mode; switch to vim for u / Ctrl+R undo",
				"• Ctrl+E — execute block under cursor",
				"• F5 — confirm then execute full buffer",
				"• Ctrl+Shift+F / Ctrl+F — format active block",
				"• Ctrl+\\ — toggle comment on current line",
				"• Ctrl+R — refactor popup (expand *, SELECT↔UPDATE, IDENTITY_INSERT, rename tab)",
				"• Alt+Up / Alt+Down — jump to previous / next query block",
				"• Ctrl+G / : (vim) — goto line",
				"• Ctrl+V / Shift+Ins — paste from clipboard",
			},
		}, {
			Title: "Tabs",
			Lines: []string{
				"• Ctrl+N — new tab",
				"• Ctrl+W — close tab",
				"• Ctrl+PgDn / Alt+L — next tab",
				"• Ctrl+PgUp / Alt+H — previous tab",
			},
		}, {
			Title: "Vim mode",
			Lines: []string{
				"• Ctrl+Alt+V / Alt+V — toggle vim mode (current: " + vimMode + ")",
				"• i/I/a/A/o/O — enter INSERT",
				"• hjkl, w/b/e, 0/^/$, gg/G, {/}, Ctrl-u/d — movement",
				"• count prefix: 3j, 5w, etc.",
				"• x/X — delete char; dd/cc/yy; d/c/y + motion",
				"• p/P — paste; u / Ctrl+R — undo/redo",
				"• v — charwise visual; V — linewise visual; y/d/>/< on selection",
			},
		}, {
			Title: "Editor settings",
			Lines: []string{
				fmt.Sprintf("• tab_size=%d", m.cfg.Editor.TabSize),
				fmt.Sprintf("• use_spaces=%t", m.cfg.Editor.UseSpaces),
				fmt.Sprintf("• vim_mode_default=%t", m.cfg.Editor.VimMode),
				fmt.Sprintf("• wrap=%t", m.cfg.Editor.Wrap),
				fmt.Sprintf("• row_limit=%d", m.cfg.Editor.RowLimit),
				fmt.Sprintf("• theme=%s", m.cfg.Editor.Theme),
				fmt.Sprintf("• chroma_theme=%s", m.cfg.Editor.ChromaTheme),
				fmt.Sprintf("• font_width=%d", m.cfg.Editor.FontWidth),
				fmt.Sprintf("• undo_limit=%d", m.cfg.Editor.UndoLimit),
				fmt.Sprintf("• format_line_length=%d", m.cfg.Editor.FormatLineLength),
			},
		}},
	}

	resultsTab := uihelp.Tab{
		Title: "Results",
		Sections: []uihelp.Section{{
			Title: "Navigation",
			Lines: []string{
				"• Arrows / hjkl — move cell",
				"• PgUp / PgDn — page up/down",
				"• Home / End — first / last row",
				"• 0 / $ — first / last column",
				"• Alt+PgUp / Alt+PgDn — previous / next result set",
			},
		}, {
			Title: "Viewing",
			Lines: []string{
				"• Enter — row detail view (j/k fields, h/l rows, y copy, Esc close)",
				"• v — open cell value viewer",
				"• y — yank cell value to clipboard",
				"• # — toggle row numbers",
				"• z — zoom: shrink editor to active block only",
				"• Z — zoom: results fullscreen (hides editor)",
			},
		}, {
			Title: "Filtering & sorting",
			Lines: []string{
				"• / — find across all columns (n/N next/prev, Esc clear)",
				"• f — filter column (regex, stacks)",
				"• F — clear all filters",
				"• s — cycle column sort (asc / desc / off)",
				"• L — change result limit",
			},
		}, {
			Title: "Row & column selection",
			Lines: []string{
				"• Space — tag / untag current row (advances cursor)",
				"• V — range-tag rows",
				"• Ctrl+A — tag all rows / clear all tags",
				"• | — toggle current column selection (purple header)",
				"• \\ — column picker (fuzzy search; ↓/Tab → list, Space tag, Enter apply)",
				"• Ctrl+\\ — clear all column selections",
				"• t — toggle SELECTED ONLY view (shows only tagged rows + selected columns)",
			},
		}, {
			Title: "Actions",
			Lines: []string{
				"• e — edit cell (generates single-column UPDATE, pastes into editor)",
				"• E — row edit form (Tab/Shift+Tab fields, Ctrl+S multi-column UPDATE, Esc cancel)",
				"• X — export (CSV / Markdown / JSON / SQL INSERT / WHERE IN → file or clipboard)",
				"• p — pin baseline; re-run query then p again to see diff",
				"• P — set poll interval (auto-refresh)",
				"• r — re-run last query",
			},
		}},
	}

	generalTab := uihelp.Tab{
		Title: "General",
		Sections: []uihelp.Section{{
			Title: "Global navigation",
			Lines: []string{
				"• F1 — this help screen",
				"• F3 / Alt+1 — focus editor",
				"• F4 / Alt+2 — focus results",
				"• Ctrl+B / F2 — toggle schema browser",
				"• Ctrl+P — command palette (explain, transactions, snippets…)",
				"• Ctrl+H — query history palette",
				"• Ctrl+K — connection switcher",
				"• Ctrl+N outside editor — add connection",
				"• Ctrl+Q — quit and save session",
			},
		}, {
			Title: "Schema browser",
			Lines: []string{
				"• Enter / ↓ — go to list; type to filter; / — back to search",
				"• ↑↓ / jk — navigate list",
				"• Tab — column select",
				"• a — actions menu; r — row count",
				"• Esc — back / close",
			},
		}, {
			Title: "Overlays & dialogs",
			Lines: []string{
				"• Command palette: type to filter, ↑↓ select, Enter run, Esc close",
				"• History palette: type to filter recent SQL, Enter paste, Esc close",
				"• Connection switcher: ↑↓ select, Enter connect, Ctrl+N add, Esc close",
				"• Confirm dialogs: ←→ / Tab choose, Enter confirm, Esc cancel",
				"• Help screen: ←→ switch tabs, ↑↓ scroll, Esc / F1 close",
			},
		}, {
			Title: "Runtime state",
			Lines: []string{
				"• Focused pane: " + m.paneLabel(),
				"• Active connection: " + activeConn,
				"• Vim mode: " + vimMode,
				"• Named connections available: " + fmt.Sprintf("%d", len(named)),
				"• Startup queries loaded: " + fmt.Sprintf("%d", len(m.cfg.Startup)),
				mcpLine(m.mcpMode, m.mcpAddr),
			},
		}},
	}

	settingsTab := uihelp.Tab{
		Title: "Settings",
		Sections: []uihelp.Section{{
			Title: "Paths",
			Lines: []string{
				"• Config file: " + filepath.Join(configDir, "config.lua"),
				"• Data dir:    " + dataDir,
			},
		}, {
			Title: "Key bindings (from config)",
			Lines: []string{
				"• Note: key settings are loaded from config; some commands still use built-in bindings.",
				fmt.Sprintf("• execute=%s", m.cfg.Keys.Execute),
				fmt.Sprintf("• execute_block=%s", m.cfg.Keys.ExecuteBlock),
				fmt.Sprintf("• execute_all=%s", m.cfg.Keys.ExecuteAll),
				fmt.Sprintf("• format_query=%s", m.cfg.Keys.FormatQuery),
				fmt.Sprintf("• expand_star=%s", m.cfg.Keys.ExpandStar),
				fmt.Sprintf("• toggle_comment=%s", m.cfg.Keys.ToggleComment),
				fmt.Sprintf("• toggle_schema=%s", m.cfg.Keys.ToggleSchema),
				fmt.Sprintf("• connection_picker=%s", m.cfg.Keys.ConnectionPicker),
				fmt.Sprintf("• history=%s", m.cfg.Keys.History),
				fmt.Sprintf("• command_palette=%s", m.cfg.Keys.CommandPalette),
			},
		}, {
			Title: "Theme",
			Lines: themeAndStartupLines(m),
		}},
	}

	return []uihelp.Tab{editorTab, resultsTab, generalTab, settingsTab}
}

func themeAndStartupLines(m Model) []string {
	lines := []string{
		fmt.Sprintf("• border=%s", m.cfg.Theme.Border),
		fmt.Sprintf("• background=%s", m.cfg.Theme.Background),
		fmt.Sprintf("• foreground=%s", m.cfg.Theme.Foreground),
		fmt.Sprintf("• cursor=%s", m.cfg.Theme.Cursor),
		fmt.Sprintf("• selection=%s", m.cfg.Theme.Selection),
		fmt.Sprintf("• tab_active=%s", m.cfg.Theme.TabActive),
		fmt.Sprintf("• tab_inactive=%s", m.cfg.Theme.TabInactive),
		fmt.Sprintf("• null_color=%s", m.cfg.Theme.NullColor),
		fmt.Sprintf("• error_color=%s", m.cfg.Theme.ErrorColor),
		fmt.Sprintf("• warn_color=%s", m.cfg.Theme.WarnColor),
		fmt.Sprintf("• line_number=%s", m.cfg.Theme.LineNumber),
		fmt.Sprintf("• cursor_line_number=%s", m.cfg.Theme.CursorLineNumber),
		fmt.Sprintf("• active_query_gutter=%s", m.cfg.Theme.ActiveQueryGutter),
		fmt.Sprintf("• insert_cursor=%s", m.cfg.Theme.InsertCursor),
		fmt.Sprintf("• connection color count=%d", len(m.cfg.Theme.ConnColors)),
		fmt.Sprintf("• config connection profiles=%d", len(m.cfg.Connections)),
	}
	if len(m.cfg.Startup) == 0 {
		return append(lines, "• startup queries=(none)")
	}
	keys := make([]string, 0, len(m.cfg.Startup))
	for key := range m.cfg.Startup {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	lines = append(lines, "• startup queries for: "+join(keys, ", "))
	return lines
}

func yesNo(v bool) string {
	if v {
		return "open"
	}
	return "closed"
}

func join(items []string, sep string) string {
	if len(items) == 0 {
		return ""
	}
	out := items[0]
	for i := 1; i < len(items); i++ {
		out += sep + items[i]
	}
	return out
}

func mcpLine(active bool, addr string) string {
	if !active {
		return "• MCP server: off"
	}
	return "• MCP server: on  addr=" + addr
}
