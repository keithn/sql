# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
go build ./...                        # Build all packages
go build -o sql ./cmd/sql/            # Build binary
go test ./...                         # Run all tests
go test ./internal/format -v          # Run single package tests
go test ./internal/connections -run TestDetectDriver/sqlserver_scheme -v  # Single test
go test -cover ./...                  # Tests with coverage
```

## Architecture

Elm-pattern (bubbletea) TUI app. All inter-component communication uses `tea.Msg`; async work uses `tea.Cmd` goroutines.

**Root model hierarchy:**
```
app.Model
├── editor.Model    — multi-tab query buffer with autocomplete, vim mode, goto-line, tab rename
├── results.Model   — scrollable grid, sort, stacked filters, row detail, row numbers, export
├── schema.Model    — left-side tree overlay (Ctrl+B), row counts, action menu
├── statusbar.Model — connection name, focused pane, column type, vim mode, txn state, errors
├── cellview.Model  — single-cell value viewer with text selection (v key)
├── rowedit.Model   — multi-column row edit form (E key), generates multi-SET UPDATE
├── palette.Model   — generic fuzzy palette (connection switcher, commands, history)
├── modal.Model     — add-connection modal with keychain-backed save, confirmations
└── help.Model      — F1 help/settings overlay
```

**Key packages:**
- `internal/app/` — root model, all message dispatch (`update.go`), layout (`view.go`), message types (`messages.go`)
- `internal/db/` — `Driver` interface, `Session` wrapper, `connect.go` (DetectAndConnect), three driver packages (`mssql/`, `postgres/`, `sqlite/`) — all have working `Introspect()`
- `internal/ui/editor/` — textarea+vim tabs, autocomplete (`complete.go`), refactors, formatter
- `internal/ui/editor/vim/` — `Buffer` ([][]rune) + `State` (mode machine): Normal/Insert/Visual/V-LINE
- `internal/format/sql.go` — hand-rolled SQL formatter
- `internal/connections/` — `parse.go` driver detection, `store.go` keychain-backed password storage
- `internal/workspace/` — tab persistence (`workspace/<connname>/queryN.sql`), session restore, query history (SQLite)
- `internal/config/` — Lua config loader, `DataDir()` / `ConfigDir()` path resolution
- `internal/testdb/` — test helpers: `SQLiteDB(t)` and `MSSQLDB(t)` (skip if `TEST_MSSQL_PASSWORD` unset)

**Async flow for query execution:**
1. `Ctrl+E` → `ExecuteBlockMsg` → `app.Update()` → `executeCmd()` (goroutine)
2. Goroutine calls `session.Execute()`, returns `QueryDoneMsg`
3. `results.Model.SetResults()` renders grid

**Keybindings:**
- `Ctrl+E` — execute block; `F5` — execute buffer
- `Ctrl+B` / `F2` — toggle schema overlay
- `F3` / `Alt+1` — focus editor; `F4` / `Alt+2` — focus results
- `Ctrl+Q` — quit (saves session)
- `Tab` — insert 4 spaces (no popup); autocomplete triggers on 1+ word chars, Tab cycles, Ctrl+E accepts, Esc dismisses
- `Ctrl+R` — refactor popup (expand SELECT *, convert SELECT↔UPDATE, wrap IDENTITY_INSERT, rename tab)
- `Ctrl+G` — goto-line bar (also `:` in vim Normal mode)
- `Ctrl+F` / `Ctrl+Shift+F` — format active SQL block
- `Ctrl+\` — toggle line comment
- `Ctrl+H` — query history palette
- `Ctrl+K` — connection switcher palette
- `Ctrl+P` — command palette
- `Ctrl+N` — add connection modal
- `Ctrl+Alt+V` — toggle vim mode
- Results: `/` find across all columns (n/N navigate, Esc clear), `f` filter column, `F` clear all filters, `s` cycle sort, `#` row numbers, `L` set limit, `Enter` row detail, `e` edit cell (single column), `E` row edit form (multi-column UPDATE), `X` export, `v` cell viewer, `y` yank cell
- Schema: `Enter` SELECT, `Tab` column select, `a` action menu, `r` row count, `Esc` close

**Persistence paths (Windows):**
- Config: `%APPDATA%\sql\config.lua`
- Workspace: `%LOCALAPPDATA%\sql\workspace\<connname>\queryN.sql`
- Default workspace conn name: `_adhoc`
- History: `%LOCALAPPDATA%\sql\workspace\history.db` (SQLite)

## Config (config.lua)

```lua
editor = {
  tab_size = 4,
  vim_mode = false,
  result_limit = 500,
}
connections = {
  { name = "prod", driver = "mssql", host = "server", database = "mydb" },
}
```
