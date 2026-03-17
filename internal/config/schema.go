package config

// Config is the root configuration structure populated from config.lua.
type Config struct {
	Connections []ConnectionProfile
	Editor      EditorConfig
	Keys        KeyConfig
	Theme       ThemeConfig
	Startup     map[string]string // connection name → SQL to run on connect
}

type ConnectionProfile struct {
	Name     string
	Driver   string // "mssql", "postgres", "sqlite"
	Host     string
	Port     int
	Database string
	Username string
	// Password is never stored here; fetched from OS keychain at connect time.
	SSLMode     string
	WindowsAuth bool
	AzureAD     string // "", "interactive", "sp", "msi"
	AppName     string
	Encrypt     string // "true", "false", "strict"
	FilePath    string // SQLite only
	SSH         *SSHTunnel
	Extra       map[string]string
}

type SSHTunnel struct {
	Host     string
	Port     int
	User     string
	KeyPath  string
	Password string
}

type EditorConfig struct {
	TabSize          int
	UseSpaces        bool
	VimMode          bool
	Wrap             bool
	RowLimit         int
	ResultLimit      int // default rows fetched by schema browser SELECTs
	Theme            string
	ChromaTheme      string
	FontWidth        int
	UndoLimit        int
	FormatLineLength int
}

type KeyConfig struct {
	Execute          string
	ExecuteBlock     string
	ExecuteAll       string
	FormatQuery      string
	ExpandStar       string
	ToggleComment    string
	ToggleSchema     string
	ConnectionPicker string
	History          string
	CommandPalette   string
}

type ThemeConfig struct {
	Border            string
	Background        string
	Foreground        string
	Cursor            string
	Selection         string
	TabActive         string
	TabInactive       string
	NullColor         string
	ErrorColor        string
	WarnColor         string
	ConnColors        []string
	LineNumber        string // gutter: non-cursor lines
	ActiveLineNumber  string // gutter: lines in the active Ctrl+E block (not cursor)
	CursorLineNumber  string // gutter: line the cursor is on
	ActiveQueryGutter string // gutter: active Ctrl+E block marker background
	InsertCursor      string // vim insert-mode cursor color
}
