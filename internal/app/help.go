package app

import (
	"fmt"
	"path/filepath"
	"sort"

	"github.com/sqltui/sql/internal/config"
	"github.com/sqltui/sql/internal/connections"
	uihelp "github.com/sqltui/sql/internal/ui/help"
)

func (m Model) helpSections() []uihelp.Section {
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

	sections := []uihelp.Section{{
		Title: "Runtime state",
		Lines: []string{
			"• Focused pane: " + m.paneLabel(),
			"• Active connection: " + activeConn,
			"• Schema overlay: " + yesNo(m.schemaOpen) + fmt.Sprintf(" (%d%% width)", m.schemaWidth),
			"• Vim mode: " + vimMode,
			"• Config file: " + filepath.Join(configDir, "config.lua"),
			"• Data dir: " + dataDir,
			"• Named connections available: " + fmt.Sprintf("%d", len(named)),
			"• Startup queries loaded: " + fmt.Sprintf("%d", len(m.cfg.Startup)),
		},
	}, {
		Title: "Runtime keybindings",
		Lines: []string{
			"• F1 — help & settings",
			"• Ctrl+K — connection switcher",
			"• Ctrl+N outside editor — add connection",
			"• Ctrl+B / F2 — toggle schema overlay",
			"• F3 / Alt+1 — focus editor",
			"• F4 / Alt+2 — focus results",
			"• Ctrl+Alt+V / Alt+V — toggle vim mode",
			"• Ctrl+Q — quit and save session",
			"• Ctrl+E — execute block under cursor",
			"• Ctrl+R — refactor popup",
			"• Alt+Up / Alt+Down — previous/next query block",
			"• Ctrl+Shift+F / Ctrl+F — format active block",
			"• Ctrl+\\ — toggle comment on current line",
			"• F5 — execute full buffer",
			"• Ctrl+N — new tab (inside editor)",
			"• Ctrl+W — close tab",
			"• Ctrl+PgDn / Alt+L — next tab",
			"• Ctrl+PgUp / Alt+H — previous tab",
			"• Results: arrows / hjkl, PgUp/PgDn, Home/End, Alt+PgUp/PgDn",
			"• Switcher: type to filter, ↑↓ select, Enter connect, Ctrl+N/A add, Esc close",
			"• Add connection: Tab focus, ←→ action, Enter submit, Esc cancel",
			"• Help screen: ↑↓ PgUp/PgDn Home/End, Esc/F1 close",
		},
	}, {
		Title: "Loaded editor settings",
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
	}, {
		Title: "Loaded key settings",
		Lines: []string{
			"• Note: key settings are loaded from config; some commands still use built-in bindings while key override wiring is incomplete.",
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
		Title: "Loaded theme / startup settings",
		Lines: themeAndStartupLines(m),
	}}

	return sections
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
