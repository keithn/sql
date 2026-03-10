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
├── editor.Model    — multi-tab query buffer with autocomplete popup
├── results.Model   — scrollable grid, NULL shown as ∅
├── schema.Model    — left-side tree overlay (Ctrl+B to toggle)
└── statusbar.Model — connection name, focused pane, errors
```

**Key packages:**
- `internal/app/` — root model, all message dispatch (`update.go`), layout (`view.go`), message types (`messages.go`)
- `internal/db/` — `Driver` interface, `Session` wrapper, `connect.go` (DetectAndConnect), three driver packages (`mssql/`, `postgres/`, `sqlite/`) — **Introspect() is stubbed in all three**
- `internal/ui/editor/` — textarea tabs, autocomplete (`complete.go` has 30 SQL keywords + schema names)
- `internal/format/sql.go` — hand-rolled SQL formatter (89.9% test coverage)
- `internal/connections/parse.go` — driver detection from connection strings (38.6% test coverage)
- `internal/workspace/` — tab persistence to `<dataDir>/workspace/<connname>/queryN.sql`, session restore via `session.json`
- `internal/config/` — Lua config loader, `DataDir()` / `ConfigDir()` path resolution

**Async flow for query execution:**
1. `Ctrl+E` → `ExecuteBlockMsg` → `app.Update()` → `executeCmd()` (goroutine)
2. Goroutine calls `session.Execute()`, returns `QueryDoneMsg`
3. `results.Model.SetResults()` renders grid

**Keybindings:**
- `Ctrl+E` — execute block; `F5` — execute buffer
- `Ctrl+B` / `F2` — toggle schema overlay
- `F3` / `Alt+1` — focus editor; `F4` / `Alt+2` — focus results
- `Ctrl+Q` — quit (saves session)
- `Tab` — insert 4 spaces (no popup); autocomplete triggers on 1+ word chars, Tab cycles, Enter accepts, Esc dismisses

**Persistence paths (Windows):**
- Config: `%APPDATA%\sql\config.lua`
- Workspace: `%LOCALAPPDATA%\sql\workspace\<connname>\queryN.sql`
- Default workspace conn name: `_adhoc`

## Current Gaps

- `Introspect()` in all three DB drivers returns empty `*db.Schema` (stubs)
- `schema.Model.SetSchema()` is a stub — tree is not populated from introspected data
- CLI `--add` and `--list` flags are TODOs in `cmd/sql/main.go`
- No tests yet for `internal/app`, `internal/db`, `internal/ui/*`, `internal/workspace`
