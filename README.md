# sql

A terminal SQL client for developers who live in the terminal. First-class MS SQL Server support, plus PostgreSQL and SQLite. Keyboard-driven, fast, works over SSH and inside tmux.

![Go](https://img.shields.io/badge/go-1.21+-00ADD8?logo=go)

## Features

- **Multi-database** — SQL Server, PostgreSQL, SQLite (pure Go, no CGo)
- **Smart block execution** — `Ctrl+E` runs the logical statement under the cursor; `F5` runs the full buffer
- **Syntax highlighting** — SQL highlighted as you type
- **Schema browser** — `Ctrl+B` / `F2` to toggle a left-side tree of tables and columns
- **Schema-aware autocomplete** — keywords + table/column names from the connected database
- **Virtual scrolling** — large result sets (tens of thousands of rows) stay snappy
- **Multi-tab editor** — `Ctrl+N` to open a new tab, `Ctrl+W` to close, `Ctrl+PgDn`/`Ctrl+PgUp` to switch
- **Session restore** — open tabs and active query are saved on quit and restored on reconnect
- **Lua config** — `%APPDATA%\sql\config.lua` (Windows) / `~/.config/sql/config.lua`

## Installation

### Download a release

Grab a pre-built binary for your platform from the [Releases](../../releases) page and put it on your `PATH`.

### Build from source

Requires Go 1.21+.

```sh
go install github.com/sqltui/sql/cmd/sql@latest
```

Or clone and build:

```sh
git clone https://github.com/sqltui/sql
cd sql
go build -o sql ./cmd/sql/
```

## Usage

```sh
# Connect with a connection string
sql "server=1.2.3.4;user id=sa;password=...;database=mydb"

# SQLite
sql ./mydb.sqlite

# PostgreSQL
sql "postgres://user:pass@localhost/mydb"
```

Once connected, the last-used connection is saved and restored on next launch.

## Connection strings

| Database   | Format |
|------------|--------|
| SQL Server | `server=host;user id=user;password=pass;database=db` |
| SQL Server | `sqlserver://user:pass@host?database=db` |
| PostgreSQL | `postgres://user:pass@host/db` |
| PostgreSQL | `host=localhost user=postgres dbname=mydb sslmode=disable` |
| SQLite     | `./path/to/file.db` or `file:path/to/file.db` |

Passwords are never displayed in the UI.

## Keybindings

### Global

| Key | Action |
|-----|--------|
| `Ctrl+E` | Execute block under cursor |
| `F5` | Execute full buffer |
| `F3` / `Alt+1` | Focus editor |
| `F4` / `Alt+2` | Focus results |
| `Ctrl+B` / `F2` | Toggle schema browser |
| `Ctrl+Q` | Quit (saves session) |

### Editor

| Key | Action |
|-----|--------|
| `Ctrl+N` | New tab |
| `Ctrl+W` | Close tab |
| `Ctrl+PgDn` / `Alt+L` | Next tab |
| `Ctrl+PgUp` / `Alt+H` | Previous tab |
| `Tab` | Insert 4 spaces |
| `Esc` | Dismiss autocomplete |

### Results

| Key | Action |
|-----|--------|
| `↑` / `↓` or `j` / `k` | Scroll rows |
| `PgUp` / `PgDn` | Scroll a page |
| `Home` / `End` | Jump to top / bottom |
| `←` / `→` or `h` / `l` | Scroll columns |
| `Alt+PgUp` / `Alt+PgDn` | Previous / next result set |

### Schema browser

| Key | Action |
|-----|--------|
| `Alt+←` / `Alt+→` | Resize panel |

## Configuration

Config file is loaded from:
- **Windows**: `%APPDATA%\sql\config.lua`
- **macOS / Linux**: `~/.config/sql/config.lua`

```lua
-- config.lua
theme = {
  line_number = "#555555",
  cursor_line_number = "#aaaaaa",
}
```

## Building

```sh
go build ./...          # build all packages
go test ./...           # run all tests
go test -cover ./...    # with coverage
```
