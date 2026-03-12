# sql

A terminal SQL client for developers who live in the terminal. First-class MS SQL Server support, plus PostgreSQL and SQLite. Keyboard-driven, fast, works over SSH and inside tmux.

![Go](https://img.shields.io/badge/go-1.21+-00ADD8?logo=go)

## Features

- **Multi-database** — SQL Server, PostgreSQL, SQLite (pure Go, no CGo)
- **Named/saved connections** — raw DSNs, saved connections, keychain-backed passwords, and last-used connection restore
- **Connection management in-app** — `Ctrl+K` connection switcher, plus add/save/connect flow from inside the TUI
- **Command + history palettes** — `Ctrl+P` for app actions and `Ctrl+H` for recent executed SQL
- **Smart block execution** — `Ctrl+E` runs the logical statement under the cursor; `F5` confirms before running the full buffer
- **Execution workflow state** — status bar shows transaction state, row counts, and query duration; transactions can be started/committed/rolled back, explain plans can be run, and current block/full buffer can be executed directly inside a transaction from the command palette
- **Editor refactors and formatting** — active-block formatting plus `Ctrl+R` refactors for aliasing, `SELECT *` expansion, `SELECT`/`UPDATE` conversions, and SQL Server `IDENTITY_INSERT` wrapping for `INSERT` blocks
- **Schema-aware SQL assistance** — autocomplete, JOIN predicate inference, and missing table/qualified-column highlighting
- **Syntax highlighting** — SQL highlighted as you type, with schema-aware missing-name highlighting when metadata is available
- **Schema browser** — `Ctrl+B` / `F2` to toggle a left-side tree of tables and columns
- **Vim mode** — toggleable vim editing with persistent mode state and insert-mode cursor handling
- **Help/settings overlay** — `F1` shows runtime keybindings, state, and loaded config/theme values
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

# List saved/named connections
sql --list

# Save a named connection
sql --add "server=1.2.3.4;user id=sa;password=...;database=mydb" --name prod

# Connect using a saved/named connection
sql prod

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
| `F1` | Help / settings overlay |
| `Ctrl+P` | Command palette |
| `Ctrl+H` | Query history palette |
| `Ctrl+K` | Connection switcher |
| `Ctrl+E` | Execute block under cursor |
| `Ctrl+Shift+F` / `Ctrl+F` | Format active block |
| `F5` | Confirm, then execute full buffer |
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
| `Alt+Up` / `Alt+Down` | Previous / next query block |
| `Ctrl+R` | Open refactor popup |
| `Ctrl+\` | Toggle comment on current line |
| `Tab` | Insert 4 spaces |
| `Esc` | Dismiss autocomplete |

Refactor popup actions currently include:

- `N` — name/alias current table in the active query block
- `E` — expand top-level `SELECT *`
- `u` / `U` — convert current `SELECT` to `UPDATE`, or append an `UPDATE` below it
- `s` / `S` — convert current `UPDATE` to `SELECT`, or append a `SELECT` below it
- `i` — wrap current `INSERT` with `SET IDENTITY_INSERT <table> ON/OFF`

When connected, the command palette also exposes transaction actions:

- begin transaction
- commit transaction
- rollback transaction
- run current block in transaction
- run full buffer in transaction
- explain current block
- explain full buffer

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
