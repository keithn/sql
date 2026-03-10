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
	lua.OpenPackage(L)  // needed for require machinery; restricted below
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
	// TODO: implement full extraction of connections, editor, keys, theme, startup tables.
	// Each sub-table will be walked and mapped to the corresponding Go struct.
	_ = L
	_ = cfg
}
