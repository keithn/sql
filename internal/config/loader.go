package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	lua "github.com/yuin/gopher-lua"
)

// Load reads config.lua from the platform config directory and merges it
// with the built-in defaults. Missing config file is not an error.
func Load() (*Config, error) {
	cfg := defaults()

	path, err := configFilePath()
	if err != nil {
		return cfg, nil // no config dir, use defaults
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		return cfg, nil // no config file, use defaults
	}

	if err := loadLua(cfg, path); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}

	return cfg, nil
}

// ConfigDir returns the platform-appropriate config directory.
func ConfigDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "sql"), nil
}

// DataDir returns the platform-appropriate data directory.
func DataDir() (string, error) {
	if runtime.GOOS == "windows" {
		local := os.Getenv("LOCALAPPDATA")
		if local == "" {
			return "", fmt.Errorf("LOCALAPPDATA not set")
		}
		return filepath.Join(local, "sql"), nil
	}
	// XDG_DATA_HOME or ~/.local/share
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "sql"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "sql"), nil
}

func configFilePath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.lua"), nil
}

func loadLua(cfg *Config, path string) error {
	L := lua.NewState(lua.Options{SkipOpenLibs: true})
	defer L.Close()

	// Open only safe standard libraries.
	openSafeLibs(L)

	// Install restricted require loader.
	installRequireLoader(L, filepath.Dir(path))

	if err := L.DoFile(path); err != nil {
		return err
	}

	// Extract top-level globals into cfg.
	extractConfig(L, cfg)

	return nil
}

// openSafeLibs opens only the harmless Lua standard libraries.
func openSafeLibs(L *lua.LState) {
	lua.OpenPackage(L) // needed for require machinery; restricted below
	lua.OpenBase(L)
	lua.OpenTable(L)
	lua.OpenString(L)
	lua.OpenMath(L)
	// Intentionally NOT opening: io, os, debug, coroutine
}

// installRequireLoader replaces the default package loader with one that
// only searches the config directory. Module names containing path separators
// or ".." are rejected.
func installRequireLoader(L *lua.LState, configDir string) {
	loader := func(L *lua.LState) int {
		name := L.CheckString(1)

		// Reject path traversal attempts.
		for _, ch := range []string{"/", "\\", ".."} {
			if containsStr(name, ch) {
				L.RaiseError("require: illegal module name %q", name)
				return 0
			}
		}

		fullPath := filepath.Join(configDir, name+".lua")
		if err := L.DoFile(fullPath); err != nil {
			L.RaiseError("require %q: %v", name, err)
			return 0
		}
		return 0
	}

	// Replace the loaders table so only our loader is used.
	pkg := L.GetGlobal("package").(*lua.LTable)
	loaders := L.NewTable()
	loaders.RawSetInt(1, L.NewFunction(loader))
	pkg.RawSetString("loaders", loaders)
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && stringContains(s, sub))
}

func stringContains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// extractConfig walks the Lua globals and populates cfg fields.
// Unknown keys are ignored.
func extractConfig(L *lua.LState, cfg *Config) {
	if tbl, ok := L.GetGlobal("connections").(*lua.LTable); ok {
		cfg.Connections = extractConnections(tbl)
	}
	if tbl, ok := L.GetGlobal("editor").(*lua.LTable); ok {
		extractEditor(tbl, &cfg.Editor)
	}
	if tbl, ok := L.GetGlobal("keys").(*lua.LTable); ok {
		extractKeys(tbl, &cfg.Keys)
	}
	if tbl, ok := L.GetGlobal("theme").(*lua.LTable); ok {
		extractTheme(tbl, &cfg.Theme)
	}
	if tbl, ok := L.GetGlobal("startup").(*lua.LTable); ok {
		cfg.Startup = extractStartup(tbl)
	}
}

func extractEditor(tbl *lua.LTable, cfg *EditorConfig) {
	if v, ok := tableOptionalInt(tbl, "tab_size"); ok {
		cfg.TabSize = v
	}
	if v, ok := tableOptionalBool(tbl, "use_spaces"); ok {
		cfg.UseSpaces = v
	}
	if v, ok := tableOptionalBool(tbl, "vim_mode"); ok {
		cfg.VimMode = v
	}
	if v, ok := tableOptionalBool(tbl, "wrap"); ok {
		cfg.Wrap = v
	}
	if v, ok := tableOptionalInt(tbl, "row_limit"); ok {
		cfg.RowLimit = v
	}
	if v, ok := tableOptionalInt(tbl, "result_limit"); ok {
		cfg.ResultLimit = v
	}
	if v, ok := tableOptionalString(tbl, "theme"); ok {
		cfg.Theme = v
	}
	if v, ok := tableOptionalString(tbl, "chroma_theme"); ok {
		cfg.ChromaTheme = v
	}
	if v, ok := tableOptionalInt(tbl, "font_width"); ok {
		cfg.FontWidth = v
	}
	if v, ok := tableOptionalInt(tbl, "undo_limit"); ok {
		cfg.UndoLimit = v
	}
	if v, ok := tableOptionalInt(tbl, "format_line_length"); ok {
		cfg.FormatLineLength = v
	}
}

func extractKeys(tbl *lua.LTable, cfg *KeyConfig) {
	if v, ok := tableOptionalString(tbl, "execute"); ok {
		cfg.Execute = v
	}
	if v, ok := tableOptionalString(tbl, "execute_block"); ok {
		cfg.ExecuteBlock = v
	}
	if v, ok := tableOptionalString(tbl, "execute_all"); ok {
		cfg.ExecuteAll = v
	}
	if v, ok := tableOptionalString(tbl, "format_query"); ok {
		cfg.FormatQuery = v
	}
	if v, ok := tableOptionalString(tbl, "expand_star"); ok {
		cfg.ExpandStar = v
	}
	if v, ok := tableOptionalString(tbl, "toggle_comment"); ok {
		cfg.ToggleComment = v
	}
	if v, ok := tableOptionalString(tbl, "toggle_schema"); ok {
		cfg.ToggleSchema = v
	}
	if v, ok := tableOptionalString(tbl, "connection_picker"); ok {
		cfg.ConnectionPicker = v
	}
	if v, ok := tableOptionalString(tbl, "history"); ok {
		cfg.History = v
	}
	if v, ok := tableOptionalString(tbl, "command_palette"); ok {
		cfg.CommandPalette = v
	}
}

func extractTheme(tbl *lua.LTable, cfg *ThemeConfig) {
	if v, ok := tableOptionalString(tbl, "border"); ok {
		cfg.Border = v
	}
	if v, ok := tableOptionalString(tbl, "background"); ok {
		cfg.Background = v
	}
	if v, ok := tableOptionalString(tbl, "foreground"); ok {
		cfg.Foreground = v
	}
	if v, ok := tableOptionalString(tbl, "cursor"); ok {
		cfg.Cursor = v
	}
	if v, ok := tableOptionalString(tbl, "selection"); ok {
		cfg.Selection = v
	}
	if v, ok := tableOptionalString(tbl, "tab_active"); ok {
		cfg.TabActive = v
	}
	if v, ok := tableOptionalString(tbl, "tab_inactive"); ok {
		cfg.TabInactive = v
	}
	if v, ok := tableOptionalString(tbl, "null_color"); ok {
		cfg.NullColor = v
	}
	if v, ok := tableOptionalString(tbl, "error_color"); ok {
		cfg.ErrorColor = v
	}
	if v, ok := tableOptionalString(tbl, "warn_color"); ok {
		cfg.WarnColor = v
	}
	if v := tableStringSlice(tbl.RawGetString("conn_colors")); len(v) > 0 {
		cfg.ConnColors = v
	}
	if v, ok := tableOptionalString(tbl, "line_number"); ok {
		cfg.LineNumber = v
	}
	if v, ok := tableOptionalString(tbl, "active_line_number"); ok {
		cfg.ActiveLineNumber = v
	}
	if v, ok := tableOptionalString(tbl, "cursor_line_number"); ok {
		cfg.CursorLineNumber = v
	}
	if v, ok := tableOptionalString(tbl, "active_query_gutter"); ok {
		cfg.ActiveQueryGutter = v
	}
	if v, ok := tableOptionalString(tbl, "insert_cursor"); ok {
		cfg.InsertCursor = v
	}
}

func extractStartup(tbl *lua.LTable) map[string]string {
	out := map[string]string{}
	tbl.ForEach(func(key, value lua.LValue) {
		if key.Type() == lua.LTString && value.Type() == lua.LTString {
			out[key.String()] = value.String()
		}
	})
	if len(out) == 0 {
		return map[string]string{}
	}
	return out
}

func extractConnections(tbl *lua.LTable) []ConnectionProfile {
	profiles := []ConnectionProfile{}
	tbl.ForEach(func(key, value lua.LValue) {
		t, ok := value.(*lua.LTable)
		if !ok {
			return
		}
		profile := ConnectionProfile{
			Name:        tableString(t, "name"),
			Driver:      tableString(t, "driver"),
			Host:        tableString(t, "host"),
			Port:        tableInt(t, "port"),
			Database:    tableString(t, "database"),
			Username:    tableString(t, "username"),
			SSLMode:     tableString(t, "sslmode"),
			WindowsAuth: tableBool(t, "windows_auth"),
			AzureAD:     tableString(t, "azure_ad"),
			AppName:     tableString(t, "app_name"),
			Encrypt:     tableString(t, "encrypt"),
			FilePath:    tableString(t, "file_path"),
			Extra:       tableStringMap(t.RawGetString("extra")),
		}
		if profile.Name == "" && key.Type() == lua.LTString {
			profile.Name = key.String()
		}
		if profile.Driver == "" && profile.FilePath != "" {
			profile.Driver = "sqlite"
		}
		if profile.Name != "" {
			profiles = append(profiles, profile)
		}
	})
	return profiles
}

func tableString(tbl *lua.LTable, key string) string {
	if v := tbl.RawGetString(key); v.Type() == lua.LTString {
		return v.String()
	}
	return ""
}

func tableInt(tbl *lua.LTable, key string) int {
	if v := tbl.RawGetString(key); v.Type() == lua.LTNumber {
		return int(v.(lua.LNumber))
	}
	return 0
}

func tableBool(tbl *lua.LTable, key string) bool {
	if v := tbl.RawGetString(key); v.Type() == lua.LTBool {
		return lua.LVAsBool(v)
	}
	return false
}

func tableStringMap(v lua.LValue) map[string]string {
	tbl, ok := v.(*lua.LTable)
	if !ok {
		return nil
	}
	out := map[string]string{}
	tbl.ForEach(func(key, value lua.LValue) {
		if key.Type() == lua.LTString && value.Type() == lua.LTString {
			out[key.String()] = value.String()
		}
	})
	if len(out) == 0 {
		return nil
	}
	return out
}

func tableStringSlice(v lua.LValue) []string {
	tbl, ok := v.(*lua.LTable)
	if !ok {
		return nil
	}
	out := []string{}
	tbl.ForEach(func(_, value lua.LValue) {
		if value.Type() == lua.LTString {
			out = append(out, value.String())
		}
	})
	if len(out) == 0 {
		return nil
	}
	return out
}

func tableOptionalString(tbl *lua.LTable, key string) (string, bool) {
	v := tbl.RawGetString(key)
	if v.Type() == lua.LTString {
		return v.String(), true
	}
	return "", false
}

func tableOptionalInt(tbl *lua.LTable, key string) (int, bool) {
	v := tbl.RawGetString(key)
	if v.Type() == lua.LTNumber {
		return int(v.(lua.LNumber)), true
	}
	return 0, false
}

func tableOptionalBool(tbl *lua.LTable, key string) (bool, bool) {
	v := tbl.RawGetString(key)
	if v.Type() == lua.LTBool {
		return lua.LVAsBool(v), true
	}
	return false, false
}
