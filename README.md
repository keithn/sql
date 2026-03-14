# sql

A terminal SQL client for developers who live in the terminal. First-class MS SQL Server support, plus PostgreSQL and SQLite. Keyboard-driven, fast, works over SSH and inside tmux.

![Go](https://img.shields.io/badge/go-1.21+-00ADD8?logo=go)

## Features

- **Multi-database** — SQL Server, PostgreSQL, SQLite (pure Go, no CGo)
- **Named/saved connections** — raw DSNs, saved connections, keychain-backed passwords, last-used connection restore, and auto-reconnect on dropped connections
- **Connection management in-app** — `Ctrl+K` switcher (with `Ctrl+D` to delete); `Ctrl+N` add-connection modal with connection-string or form-builder mode
- **Command + history palettes** — `Ctrl+P` for app actions (including snippets), `Ctrl+H` for recent SQL
- **Named snippets** — save/browse/paste reusable SQL blocks from the command palette
- **Smart block execution** — `Ctrl+E` runs the logical statement under the cursor; `F5` runs the full buffer
- **Transaction workflow** — begin/commit/rollback from the command palette; run current block or full buffer inside a transaction; explain plans
- **Editor refactors** — `Ctrl+R` popup: alias tables, expand `SELECT *`, convert SELECT↔UPDATE, `IDENTITY_INSERT` wrapping, tab rename
- **SQL formatting** — format the active block with `Ctrl+Shift+F`
- **Goto line** — `Ctrl+G` (editor and vim `:42` syntax)
- **Schema-aware assistance** — autocomplete, JOIN predicate inference, missing-table highlighting
- **Schema browser** — `Ctrl+B`/`F2`; fuzzy filter, column selection for JOIN-aware SELECT, action menu (`a`), row counts (`r`)
- **Vim mode** — toggleable; persistent across sessions; supports motions, operators, visual, undo/redo; shown in status bar
- **Results grid** — virtual scrolling, column sort (`s`), row numbers (`#`), stacked column filters (`f`), configurable row limit (`L`), poll/auto-refresh (`P`), fullscreen (`Ctrl+L`)
- **Row detail view** — `Enter` opens a vertical column/value overlay; navigate rows with `h`/`l`
- **Row tagging** — `Space` tags rows; `V` range-tag; `Ctrl+A` tag all/clear; tagged rows highlighted; export respects selection
- **Result diff/pin** — `p` pins the current result; re-running the same query shows added/removed/changed rows
- **Cell edit → UPDATE** — `e` in the results grid or row detail view opens an inline cell editor (with vim mode); on confirm an UPDATE preview panel appears showing the generated SQL — execute it with `Ctrl+E`, copy with `y`, close with `Esc`; results refresh automatically on success; `Ctrl+D` sets the value to NULL
- **Export** — `E` in results: CSV, Markdown, JSON, or SQL INSERT — to clipboard or file
- **Screenshot** — `F10` captures the current view to clipboard or file
- **MCP server mode** — `--mcp` starts a background JSON-RPC server so Claude Code can drive the TUI as an agent
- **Multi-tab editor** — `Ctrl+N` new tab, `Ctrl+W` close, `Ctrl+PgDn`/`Ctrl+PgUp` switch
- **Session restore** — open tabs and cursor positions saved on quit and restored on reconnect
- **Help/settings overlay** — `F1` shows keybindings, state, and loaded config values
- **Lua config** — `%APPDATA%\sql\config.lua` (Windows) / `~/.config/sql/config.lua`

## Installation

### Download a release

Grab a pre-built binary from the [Releases](../../releases) page and put it on your `PATH`.

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

# List saved connections
sql --list

# Save a named connection
sql --add "server=1.2.3.4;user id=sa;password=...;database=mydb" --name prod

# Connect using a saved connection
sql prod

# SQLite
sql ./mydb.sqlite

# PostgreSQL
sql "postgres://user:pass@localhost/mydb"

# Start TUI with MCP socket server on port 45678
sql --mcp
sql --mcp prod

# Relay an MCP client (e.g. Claude Code) to a running TUI
sql --mcp-relay
sql --mcp-relay --mcp-port 9999
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
| `Alt+Up` / `Alt+Down` | Previous / next query block |
| `Ctrl+G` | Goto line |
| `Ctrl+R` | Refactor popup |
| `Ctrl+\` | Toggle comment |
| `Tab` | Insert 4 spaces |
| `Ctrl+Alt+V` | Toggle vim mode |

Refactor popup (`Ctrl+R`):

| Key | Action |
|-----|--------|
| `N` | Name/alias current table |
| `E` | Expand `SELECT *` |
| `u` / `U` | Convert SELECT → UPDATE (or append UPDATE) |
| `s` / `S` | Convert UPDATE → SELECT (or append SELECT) |
| `i` | Wrap INSERT with `IDENTITY_INSERT ON/OFF` |
| `T` | Rename current tab |

Command palette (`Ctrl+P`) also exposes:

- begin / commit / rollback transaction
- run current block / full buffer in transaction
- explain current block / full buffer
- save current block as snippet
- browse snippets

### Results

| Key | Action |
|-----|--------|
| `↑` / `↓` or `j` / `k` | Scroll rows |
| `PgUp` / `PgDn` | Scroll a page |
| `Home` / `End` | Jump to top / bottom |
| `←` / `→` or `h` / `l` | Scroll columns |
| `Alt+PgUp` / `Alt+PgDn` | Previous / next result set |
| `Enter` | Row detail view (vertical) |
| `s` | Sort by cursor column (cycles: none → ▲ → ▼) |
| `f` | Filter by cursor column (stacks — add multiple) |
| `F` | Clear all filters |
| `#` | Toggle row numbers |
| `L` | Change row limit |
| `p` | Pin result as diff baseline (re-run to see diff) |
| `P` | Toggle poll / auto-refresh |
| `Space` | Tag / untag current row |
| `V` | Range-tag rows |
| `Ctrl+A` | Tag all rows / clear all tags |
| `e` | Edit cell → UPDATE preview panel |
| `E` | Export (CSV / Markdown / JSON / SQL INSERT) |
| `y` | Copy current cell to clipboard |
| `Ctrl+L` | Toggle results fullscreen |
| `F10` | Screenshot to clipboard / file |

### Row detail view

| Key | Action |
|-----|--------|
| `j` / `k` or `↑` / `↓` | Scroll fields |
| `h` / `←` | Previous row |
| `l` / `→` | Next row |
| `y` | Copy focused field to clipboard |
| `e` | Edit focused cell → UPDATE preview panel |
| `Esc` | Close |

### Cell edit overlay

| Key | Action |
|-----|--------|
| Type | Edit value (vim mode supported) |
| `Ctrl+S` | Confirm and open UPDATE preview |
| `Ctrl+D` | Set value to NULL and open UPDATE preview |
| `Esc` | Cancel |

### UPDATE preview panel

| Key | Action |
|-----|--------|
| `Ctrl+E` / `Enter` | Execute UPDATE |
| `y` / `Ctrl+C` | Copy SQL to clipboard |
| `↑` / `↓` / `j` / `k` | Scroll |
| `Esc` | Close |

### Schema browser

| Key | Action |
|-----|--------|
| Type | Filter tables |
| `↓` / `Enter` | Move to table list |
| `j` / `k` | Navigate tables |
| `Enter` | Paste SELECT into editor |
| `Tab` | Select / deselect column |
| `r` | Fetch row count for selected table |
| `a` | Action menu (copy name, DDL, indexes, row count) |
| `/` | Return to search input |
| `Esc` | Close browser |
| `Alt+←` / `Alt+→` | Resize panel |

## Configuration

Config file location:
- **Windows**: `%APPDATA%\sql\config.lua`
- **macOS / Linux**: `~/.config/sql/config.lua`

```lua
-- config.lua
editor = {
  result_limit = 500,   -- default rows fetched per query
  vim_mode     = false, -- start in vim mode
}

theme = {
  line_number        = "#555555",
  cursor_line_number = "#aaaaaa",
}
```

## MCP server mode

`--mcp` enables MCP (Model Context Protocol) support. There are two modes depending on how the process is launched:

### Headless / stdio mode (for Claude Code and other MCP clients)

When stdin is a pipe (i.e. launched by an MCP client), `sql --mcp` runs as a headless JSON-RPC 2.0 server over stdio with no TUI. This is the mode used by `.claude/mcp.json`:

```json
{
  "mcpServers": {
    "sqltui": {
      "command": "sql",
      "args": ["--mcp", "myconnection"]
    }
  }
}
```

Replace `myconnection` with a saved connection name or a raw DSN. The agent can then query the database directly.

### TUI + socket mode (query building assistant)

When launched in a terminal, `--mcp` starts the TUI normally and also opens a TCP socket server on **port 45678** (default) so an agent can drive the live TUI — reading/writing the editor, executing queries, and seeing results.

```sh
sql --mcp                        # TUI + socket on default port 45678
sql --mcp myconn                 # connect and open socket
sql --mcp --mcp-port 9999 myconn # custom port
```

Press `F1` to see the socket address. The agent writes queries into a new editor tab and executes them so you can watch the results appear live.

### Relay mode (Claude Code + live TUI)

`--mcp-relay` connects to a running TUI's socket and bridges it over stdio, allowing Claude Code to drive the live TUI via `.claude/mcp.json`:

```json
{
  "mcpServers": {
    "sqltui": {
      "command": "sql",
      "args": ["--mcp", "myconnection"],
      "description": "Headless SQL client. Use for running queries and exploring the database."
    },
    "sqltui-live": {
      "command": "sql",
      "args": ["--mcp-relay"],
      "description": "Live SQL TUI. Use when the user wants queries written into their editor. Requires sql --mcp to be running in a terminal."
    }
  }
}
```

- Use **`sqltui`** when you want Claude to query the database independently.
- Use **`sqltui-live`** (or ask Claude to "write it in the editor") when you want to see the query built live in your terminal.

### Available MCP tools

| Tool | Description |
|------|-------------|
| `read_editor` | Return current editor content |
| `write_editor(sql, mode)` | Write SQL — `new_tab` (default), `replace`, `append` |
| `list_tabs` | List open tab names |
| `switch_tab(name)` | Switch to a named tab |
| `execute_query(sql)` | Execute SQL and return results as JSON |
| `get_results` | Return the current results grid |
| `get_schema` | Return the introspected schema as JSON |

## Building

```sh
go build ./...          # build all packages
go test ./...           # run all tests
go test -cover ./...    # with coverage
```
