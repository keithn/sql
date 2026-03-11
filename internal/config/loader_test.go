package config

import (
	"testing"

	lua "github.com/yuin/gopher-lua"
)

func TestExtractConfigConnections(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	err := L.DoString(`
		connections = {
		  { name = "local", driver = "sqlite", file_path = "local.db", extra = { mode = "ro" } },
		  prod = { driver = "postgres", host = "db.example.com", port = 5432, database = "app", username = "appuser", sslmode = "require" },
		  scratch = { file_path = "scratch.db" },
		}
	`)
	if err != nil {
		t.Fatalf("DoString() error = %v", err)
	}

	cfg := defaults()
	extractConfig(L, cfg)

	if len(cfg.Connections) != 3 {
		t.Fatalf("len(cfg.Connections) = %d, want 3", len(cfg.Connections))
	}

	local := cfg.Connections[0]
	if local.Name != "local" || local.Driver != "sqlite" || local.FilePath != "local.db" || local.Extra["mode"] != "ro" {
		t.Fatalf("local connection = %+v, want sqlite local profile with extra.mode", local)
	}

	var foundProd, foundScratch bool
	for _, conn := range cfg.Connections {
		switch conn.Name {
		case "prod":
			foundProd = true
			if conn.Driver != "postgres" || conn.Host != "db.example.com" || conn.Port != 5432 || conn.Database != "app" || conn.Username != "appuser" || conn.SSLMode != "require" {
				t.Fatalf("prod connection = %+v, want postgres profile fields populated", conn)
			}
		case "scratch":
			foundScratch = true
			if conn.Driver != "sqlite" || conn.FilePath != "scratch.db" {
				t.Fatalf("scratch connection = %+v, want inferred sqlite file profile", conn)
			}
		}
	}
	if !foundProd || !foundScratch {
		t.Fatalf("expected to find prod and scratch keyed profiles in %+v", cfg.Connections)
	}
}

func TestExtractConfigSettings(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	err := L.DoString(`
		editor = {
		  tab_size = 4,
		  use_spaces = false,
		  vim_mode = true,
		  wrap = true,
		  row_limit = 500,
		  theme = "light",
		  chroma_theme = "github",
		  font_width = 2,
		  undo_limit = 42,
		  format_line_length = 100,
		}
		keys = {
		  execute = "f6",
		  execute_block = "ctrl+e",
		  execute_all = "ctrl+shift+e",
		  format_query = "ctrl+alt+f",
		  expand_star = "ctrl+space",
		  toggle_comment = "ctrl+_",
		  toggle_schema = "f2",
		  connection_picker = "ctrl+k",
		  history = "ctrl+h",
		  command_palette = "ctrl+p",
		}
		theme = {
		  border = "#111111",
		  background = "#222222",
		  foreground = "#eeeeee",
		  cursor = "#ffffff",
		  selection = "#123456",
		  tab_active = "#654321",
		  tab_inactive = "#abcdef",
		  null_color = "#777777",
		  error_color = "#ff0000",
		  warn_color = "#ffaa00",
		  conn_colors = { "#1", "#2" },
		  line_number = "#333333",
		  cursor_line_number = "#444444",
			  active_query_gutter = "#ffe4ef",
		  insert_cursor = "#555555",
		}
		startup = {
		  prod = "SET search_path TO app;",
		  ["local"] = "PRAGMA foreign_keys = ON;",
		}
	`)
	if err != nil {
		t.Fatalf("DoString() error = %v", err)
	}

	cfg := defaults()
	extractConfig(L, cfg)

	if cfg.Editor.TabSize != 4 || cfg.Editor.UseSpaces || !cfg.Editor.VimMode || !cfg.Editor.Wrap || cfg.Editor.RowLimit != 500 || cfg.Editor.Theme != "light" || cfg.Editor.ChromaTheme != "github" || cfg.Editor.FontWidth != 2 || cfg.Editor.UndoLimit != 42 || cfg.Editor.FormatLineLength != 100 {
		t.Fatalf("editor config = %+v, want extracted editor fields", cfg.Editor)
	}
	if cfg.Keys.Execute != "f6" || cfg.Keys.ExecuteBlock != "ctrl+e" || cfg.Keys.ExecuteAll != "ctrl+shift+e" || cfg.Keys.FormatQuery != "ctrl+alt+f" || cfg.Keys.ExpandStar != "ctrl+space" || cfg.Keys.ToggleComment != "ctrl+_" || cfg.Keys.ToggleSchema != "f2" || cfg.Keys.ConnectionPicker != "ctrl+k" || cfg.Keys.History != "ctrl+h" || cfg.Keys.CommandPalette != "ctrl+p" {
		t.Fatalf("keys config = %+v, want extracted key fields", cfg.Keys)
	}
	if cfg.Theme.Border != "#111111" || cfg.Theme.Background != "#222222" || cfg.Theme.Foreground != "#eeeeee" || cfg.Theme.Cursor != "#ffffff" || cfg.Theme.Selection != "#123456" || cfg.Theme.TabActive != "#654321" || cfg.Theme.TabInactive != "#abcdef" || cfg.Theme.NullColor != "#777777" || cfg.Theme.ErrorColor != "#ff0000" || cfg.Theme.WarnColor != "#ffaa00" || len(cfg.Theme.ConnColors) != 2 || cfg.Theme.LineNumber != "#333333" || cfg.Theme.CursorLineNumber != "#444444" || cfg.Theme.ActiveQueryGutter != "#ffe4ef" || cfg.Theme.InsertCursor != "#555555" {
		t.Fatalf("theme config = %+v, want extracted theme fields", cfg.Theme)
	}
	if len(cfg.Startup) != 2 || cfg.Startup["prod"] != "SET search_path TO app;" || cfg.Startup["local"] != "PRAGMA foreign_keys = ON;" {
		t.Fatalf("startup config = %+v, want extracted startup map", cfg.Startup)
	}
}
