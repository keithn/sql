## sql outstanding work plan

Last updated: 2026-03-11 (after sticky vim-mode persistence slice)

### Status key
- `[x]` done
- `[~]` in progress / partially wired
- `[ ]` not started

### Current snapshot
- `[x]` Core TUI shell: editor + results + status bar + schema overlay
- `[x]` Query execution, cancellation path, multi-result rendering
- `[x]` Driver coverage for MSSQL / PostgreSQL / SQLite
- `[x]` Schema introspection and schema-driven autocomplete seed data
- `[x]` Workspace-backed query files, session restore, per-tab cursor restore, and sticky vim mode
- `[~]` Editor roadmap: significant local work underway, including vim support
- `[x]` Configuration loader now extracts connections, editor, keys, theme, and startup settings from `config.lua`
- `[x]` Saved/named connection flow is now wired for raw DSNs, config profiles, keychain-backed managed connections, and last-used named connection restore
- `[~]` Connection surfaces now include a switcher palette and an in-app add-connection modal; general command palette work is still outstanding
- `[~]` UX surfaces now include connection management and a help/settings overlay; command/history/export surfaces are still outstanding

### Priority order

#### P0 — saved and named connections
- `[x]` Resolve connection arguments as either raw DSNs or named saved connections
- `[x]` Load managed connections from `connections.json`
- `[x]` Load Lua-defined connection profiles into runtime config
- `[x]` Merge connection sources with clear precedence (config overrides managed store by name)
- `[x]` Implement `sql --list`
- `[x]` Implement `sql --add <conn-string> --name <name>` with passwords stored in the OS keychain
- `[x]` Add tests for connection resolution / CLI helpers
- `[x]` Keep raw connection workspaces under `_adhoc` while showing a display label in the UI
- `[x]` Persist and restore the last-used named connection on startup
- `[x]` Add secure password/keychain handling for saved connections

#### P1 — connection and command surfaces
- `[x]` Connection switcher palette (`Ctrl+K`)
- `[x]` In-app add connection modal (from `Ctrl+N` outside editor or from switcher)
- `[x]` Help/settings overlay (`F1`)
- `[ ]` Command palette (`Ctrl+P`)
- `[~]` Modal wiring needed to support add/select/confirm flows

#### P1 — execution workflow gaps
- `[ ]` Query history store (`history.db`)
- `[ ]` History palette (`Ctrl+H`)
- `[ ]` Transaction mode toggle + commit / rollback UI wiring
- `[ ]` Status bar updates for row count / duration / transaction state

#### P1 — editor and query tooling gaps
- `[ ]` Wire formatter into editor commands
- `[ ]` Implement SELECT `*` expansion
- `[ ]` Implement comment toggle
- `[ ]` Finish query execution modes beyond current block/buffer behavior

#### P2 — schema and results UX
- `[ ]` Schema browser fuzzy filter
- `[ ]` Schema browser action menu
- `[ ]` Export flows from results pane
- `[ ]` Remaining results interactions (sort/filter/edit-mode follow-ups)

#### P2 — config and theming completeness
- `[x]` Extract editor config from Lua
- `[x]` Extract keybinding overrides from Lua
- `[x]` Extract theme overrides from Lua
- `[x]` Extract startup queries from Lua
- `[ ]` Align runtime behavior with README / SPEC where currently overstated

#### P3 — later / V2 areas
- `[ ]` SSH tunnel implementation
- `[ ]` Advanced auth / TLS options
- `[ ]` Advanced history re-run features
- `[ ]` Advanced export / remote clipboard / edit mode follow-ups

### Immediate next step
1. Build the command palette (`Ctrl+P`) on top of the new palette infrastructure.
2. Then move to the history store + history palette (`Ctrl+H`).
3. Keep updating this file after each completed slice.

### Progress log
- `2026-03-11`: Created this plan and started work on the P0 saved/named connection slice.
- `2026-03-11`: Wired config-defined and managed named connections into CLI and app connection resolution; implemented `--list`; implemented safe `--add` for password-free connections; added tests; verified with `go test ./...`, `go build ./...`, and a temporary-dir CLI smoke test.
- `2026-03-11`: Added persistence for the last-used named connection in workspace state; startup now restores it when no explicit connection argument is given; ad-hoc connections clear the remembered connection; added tests; verified with `go test ./...` and `go build ./...`.
- `2026-03-11`: Persisted per-tab cursor line/column in `session.json` and restored them when reopening a connection workspace; added editor and app regression tests; verified with `go test ./...` and `go build ./...`.
- `2026-03-11`: Added OS-keychain-backed password storage for named connections using `go-keyring`; `--add` now strips passwords from `connections.json`, stores secrets in the keychain, and reinjects them at connect time for both managed entries and config profiles; added connection and CLI regression tests; verified with `go test ./...` and `go build ./...`.
- `2026-03-11`: Added a first-pass `Ctrl+K` connection switcher palette with fuzzy filtering over saved/config-defined named connections; selecting an entry now queues the existing connect flow; added palette, connection-summary, and app integration tests; verified with `go test ./...` and `go build ./...`.
- `2026-03-11`: Added an in-app add-connection modal with Connect / Save / Save & Connect actions, wired to the same keychain-backed save path as the CLI; reachable via `Ctrl+N` outside the editor and from the connection switcher; added modal and app integration tests; verified with `go test ./...` and `go build ./...`.
- `2026-03-11`: Added an `F1` help/settings overlay showing runtime keybindings, runtime state, and loaded config/theme/startup values with scroll support; also completed Lua config extraction for editor/keys/theme/startup so the displayed settings reflect actual loaded config values; added config, help, and app regression tests; verified with `go test ./...` and `go build ./...`.
- `2026-03-11`: Persisted vim mode as a sticky workspace preference in `workspace/state.json`; toggling vim mode now saves immediately and startup restores it across launches independently of per-connection tab sessions; added workspace-state and app regression tests; verified with `go test ./...` and `go build ./...`.