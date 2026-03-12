## sql outstanding work plan

Last updated: 2026-03-11 (after IDENTITY_INSERT refactor action)

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
- `[~]` Editor roadmap: major UX/tooling slices are now landed, including vim support, refactors, JOIN inference, query-block navigation, and schema-aware validation
- `[x]` Configuration loader now extracts connections, editor, keys, theme, and startup settings from `config.lua`
- `[x]` Saved/named connection flow is now wired for raw DSNs, config profiles, keychain-backed managed connections, and last-used named connection restore
- `[x]` Connection surfaces now include a switcher palette, in-app add-connection modal, and general command palette
- `[~]` UX surfaces now include connection management, help/settings, and query history; export/result follow-ups are still outstanding

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
- `[x]` Command palette (`Ctrl+P`)
- `[~]` Modal wiring needed to support add/select/confirm flows

#### P1 — execution workflow gaps
- `[x]` Query history store (`history.db`)
- `[x]` History palette (`Ctrl+H`)
- `[x]` Transaction mode toggle + commit / rollback UI wiring
- `[x]` Status bar updates for row count / duration / transaction state
- `[x]` Explain current block / full buffer commands via command palette
- `[x]` Explicit run current block / full buffer in transaction commands via command palette

#### P1 — editor and query tooling gaps
- `[x]` Wire formatter into editor commands
- `[x]` Implement SELECT `*` expansion
- `[x]` Implement comment toggle
- `[x]` Add an editor refactor transient popup on `Ctrl+R`, with `N` as a listed action for naming/aliasing the current table in the current query block and rewriting its matching references within that block only
- `[x]` Add JOIN `ON`-clause autocomplete / inference so that after the user writes a `JOIN` and starts the `ON` section, the editor can suggest or insert the join predicate using schema metadata first and strong local clues second (naming patterns, existing aliases, nearby query context). Trigger this from typing `ON `, typing `=` after one side of a predicate, selecting a joined table in autocomplete, and similar join-context completions.
- `[x]` Add `Ctrl+R` refactors for converting `SELECT` ↔ `UPDATE` and appending the opposite form beneath the active statement, including conservative join-aware handling
- `[x]` Add `Alt+Up` / `Alt+Down` query-block navigation
- `[x]` Highlight missing table/view names and missing qualified columns in red when schema metadata proves they do not exist
- `[x]` Finish query execution modes beyond current block/buffer behavior

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
1. Return to remaining broader modal/palette polish.
2. Then move into schema/results export follow-ups.
3. Then keep updating this file after each completed slice.

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
- `2026-03-11`: Updated the living plan after editor UX progress: formatter and line-comment toggle are now done, and added a planned transient editor refactor popup on `Ctrl+R`, including an `N` action that should alias the table under the cursor and rewrite references within the current query block only.
- `2026-03-11`: Refined the planned JOIN tooling feature: instead of a standalone join-writer, the editor should offer `JOIN ... ON` autocomplete once the user is in the `ON` clause, inferring predicates from schema metadata first and strong local naming/context clues second within the current query block. Planned triggers include typing `ON `, typing `=` after one side of the join predicate, and join-related autocomplete selections.
- `2026-03-11`: Implemented the first JOIN inference slice in the editor: when schema metadata is available, autocomplete now suggests join predicates after `ON `, suggests the matching RHS after `=`, and can auto-append `ON ...` after selecting a joined table completion when the inferred FK relation is unambiguous. This works in textarea and vim modes and includes conservative heuristic fallback from available schema column names.
- `2026-03-11`: Implemented a transient editor refactor popup on `Ctrl+R`, including an `N` action that adds a short alias to the current table in the active query block and rewrites matching references in that block only, for both textarea and vim modes.
- `2026-03-11`: Landed a broad editor UX/tooling pass: improved JOIN ranking and labels; added SQL Server FK introspection plus stronger heuristic matching for prefixed, underscored, and self-referential keys; fixed vim insert-mode `Delete`; added compact visually lighter JOIN popup details; and added `Alt+Up` / `Alt+Down` query-block navigation with robust first/last-block no-op behavior.
- `2026-03-11`: Added conservative schema-aware validation/highlighting in the editor so missing table/view names and missing qualified columns render in an error color when loaded schema metadata proves they do not exist.
- `2026-03-11`: Expanded the editor refactor popup with `E` to expand top-level `SELECT *`, and with `u` / `U` / `s` / `S` to convert between `SELECT` and `UPDATE` statements or append the opposite form beneath the active block. The new `SELECT → UPDATE` path preserves `FROM` / `JOIN` / `WHERE` structure conservatively for joined statements and only scaffolds `SET` entries for the target table.
- `2026-03-11`: Generalized the palette infrastructure beyond the connection switcher and added a `Ctrl+P` command palette with help, connection, pane-focus, vim-mode, and tab-management actions, while preserving connection-switcher quick-add behavior.
- `2026-03-11`: Added a persistent query history store at `workspace/history.db`, recorded block/buffer execution into it, and added a `Ctrl+H` history palette that filters recent SQL and pastes the selected query into the editor.
- `2026-03-11`: Polished execution workflow state by wiring transaction begin / commit / rollback actions into the app and command palette, and by updating the status bar with transaction state, total row count, and query duration after execution.
- `2026-03-11`: Added non-executing explain-plan support across SQLite, PostgreSQL, and SQL Server drivers, surfaced it as `Explain current block` / `Explain full buffer` command-palette actions, and rendered explain output in the normal results pane with one result set per explainable statement.
- `2026-03-11`: Added explicit `Run current block in transaction` and `Run full buffer in transaction` command-palette actions. They begin a transaction if needed, execute inside the transaction, leave it open for manual commit/rollback, reuse confirmation for full-buffer runs, record history as `BLOCK_TX` / `BUFFER_TX`, and were validated with focused app regressions plus full test/build runs.
- `2026-03-11`: Added a conservative `Ctrl+R` → `i` editor refactor that wraps the active `INSERT [INTO] <table>` block with `SET IDENTITY_INSERT <table> ON/OFF`, preserving qualified/bracketed table names, working in both textarea and vim modes, and leaving non-INSERT blocks unchanged.