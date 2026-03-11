package config

func defaults() *Config {
	return &Config{
		Editor: EditorConfig{
			TabSize:          2,
			UseSpaces:        true,
			VimMode:          false,
			Wrap:             false,
			RowLimit:         10000,
			Theme:            "dark",
			ChromaTheme:      "monokai",
			FontWidth:        1,
			UndoLimit:        100,
			FormatLineLength: 80,
		},
		Keys: KeyConfig{
			Execute:          "f5",
			ExecuteBlock:     "ctrl+enter",
			ExecuteAll:       "ctrl+shift+enter",
			FormatQuery:      "ctrl+shift+f",
			ExpandStar:       "ctrl+e",
			ToggleComment:    "ctrl+\\",
			ToggleSchema:     "ctrl+b",
			ConnectionPicker: "ctrl+k",
			History:          "ctrl+h",
			CommandPalette:   "ctrl+p",
		},
		Theme: ThemeConfig{
			Border:            "#444444",
			Background:        "#1e1e1e",
			Foreground:        "#d4d4d4",
			Cursor:            "#a6e3a1",
			Selection:         "#264f78",
			TabActive:         "#007acc",
			TabInactive:       "#3c3c3c",
			NullColor:         "#666666",
			ErrorColor:        "#f44747",
			WarnColor:         "#ffcc00",
			ConnColors:        []string{"#4ec9b0", "#ce9178", "#569cd6", "#dcdcaa"},
			LineNumber:        "#4a4a4a",
			CursorLineNumber:  "#858585",
			ActiveQueryGutter: "#a64d73",
			InsertCursor:      "#a6e3a1",
		},
		Startup: map[string]string{},
	}
}
