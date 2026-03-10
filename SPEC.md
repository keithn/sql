# sql — TUI SQL Client: Product & Technical Specification

**Version**: 0.1-draft
**Date**: 2026-03-10
**Status**: Draft — open questions marked [OQ]

---

## Table of Contents

1. [Overview](#1-overview)
2. [Technology Stack](#2-technology-stack)
3. [Project Structure](#3-project-structure)
4. [Architecture Overview](#4-architecture-overview)
5. [Database Abstraction Layer](#5-database-abstraction-layer)
6. [Layout & UI Model](#6-layout--ui-model)
7. [Editor Pane](#7-editor-pane)
8. [Vim Mode](#8-vim-mode)
9. [Results Pane](#9-results-pane)
10. [Schema Browser](#10-schema-browser)
11. [Connections](#11-connections)
12. [Query Execution](#12-query-execution)
13. [Query History](#13-query-history)
14. [Configuration (Lua)](#14-configuration-lua)
15. [Keybindings Reference](#15-keybindings-reference)
16. [Theming](#16-theming)
17. [Error Handling & UX](#17-error-handling--ux)
18. [Feature Priority Table](#18-feature-priority-table)
19. [Open Questions](#19-open-questions)

---

## 1. Overview

**sql** is a terminal-user-interface SQL client written in Go. It targets power users who live in the terminal: developers, DBAs, and data engineers who need a fast, keyboard-driven, zero-mouse SQL environment that works over SSH, inside tmux, and on headless servers.

### Goals

- First-class MS SQL Server support (T-SQL, Windows Auth, Azure AD, multiple result sets).
- Full PostgreSQL and SQLite support out of the box.
- Extensible to any `database/sql`-compatible driver.
- Vim-style modal editing in the query editor, configurable.
- Schema-aware autocomplete and `SELECT *` expansion.
- Lua-based configuration — no TOML/YAML fragmentation, full programmability.
- Non-blocking async execution with query cancellation.
- Add a connection in seconds: paste a connection string, give it a name, done.
- Fast connection switching by name — from the TUI or from the command line.
- Query files organized on disk per connection, auto-saved, session-restored.
- Smart block execution: `Ctrl-Enter` runs the logical SQL statement the cursor is in.

### Non-Goals (v1)

- GUI / mouse-first workflow.
- Built-in charting or data visualization.
- Full ORM / migration tooling.
- MySQL/MariaDB as a first-class supported driver (driver can be added but is not maintained in-tree for MVP).

---

## 2. Technology Stack

| Layer | Library / Tool | Notes |
|---|---|---|
| Language | Go 1.22+ | Generics used freely; minimum version pinned in `go.mod` |
| TUI framework | `github.com/charmbracelet/bubbletea` v1.x | Elm-architecture message loop |
| Styling | `github.com/charmbracelet/lipgloss` v1.x | All colors, borders, layout |
| Standard components | `github.com/charmbracelet/bubbles` | Textinput, viewport, spinner, key, paginator |
| Syntax highlighting | `github.com/alecthomas/chroma/v2` | SQL lexer + terminal formatter |
| Config language | `github.com/yuin/gopher-lua` v1.x | Lua 5.1 compatible |
| MSSQL driver | `github.com/microsoft/go-mssqldb` | Windows Auth, Azure AD, TVPs |
| PostgreSQL driver | `github.com/jackc/pgx/v5/stdlib` | `pgx/v5` via `database/sql` adapter. Actively maintained, best type support, proper context cancellation. `lib/pq` is maintenance-only. |
| SQLite driver | `modernc.org/sqlite` | Pure Go — no CGo, no C compiler required on Windows. Used for both user SQLite DBs and internal history/session storage. |
| Local storage | `modernc.org/sqlite` | Same driver as above; no separate dependency needed. |
| Fuzzy matching | `github.com/sahilm/fuzzy` | Schema browser filter, history search |
| SSH tunneling | `golang.org/x/crypto/ssh` | Built-in, no external binary required |
| Logging | `log/slog` (stdlib) | Structured, leveled; debug log to file |
| Testing | `testing` stdlib + `github.com/stretchr/testify` | Table-driven tests |

### Intentional Omissions

- No `cobra`/`urfave/cli` — the app has minimal CLI flags; a small hand-rolled parser suffices.
- No `viper` — Lua config replaces it entirely.

### CLI Usage

```
sql                          # open with last-used connection
sql <name>                   # open and connect to named connection
sql "Server=...;Database=…"  # connect ad-hoc via raw connection string (not saved)
sql --add <connection-string> --name <name>  # register new connection and exit
sql --list                   # list saved connection names and exit
```

The positional argument is tried first as a saved connection name. If no match, it is treated as a raw connection string (driver auto-detected from the string prefix). This lets you do:

```
sql prod-db          # named connection
sql postgres://...   # ad-hoc postgres
sql "Server=myhost"  # ad-hoc mssql
sql ./local.db       # ad-hoc sqlite (file path)
```

---

## 3. Project Structure

```
sql/
├── cmd/
│   └── sql/
│       └── main.go              # Entry point: parse flags, load config, start TUI
├── internal/
│   ├── app/
│   │   ├── model.go             # Root bubbletea Model; owns all sub-models
│   │   ├── update.go            # Root Update() dispatch
│   │   ├── view.go              # Root View() layout assembly
│   │   └── messages.go          # All tea.Msg types used app-wide
│   ├── ui/
│   │   ├── editor/
│   │   │   ├── model.go         # Editor pane model
│   │   │   ├── highlight.go     # Chroma integration, re-highlight on change
│   │   │   ├── autocomplete.go  # Autocomplete popup model
│   │   │   ├── vim/
│   │   │   │   ├── mode.go      # Mode enum (Normal/Insert/Visual)
│   │   │   │   ├── motion.go    # All movement commands
│   │   │   │   ├── operator.go  # d, c, y operators + text objects
│   │   │   │   ├── register.go  # Yank/paste registers (unnamed + named)
│   │   │   │   ├── search.go    # /, ?, n, N, *, #
│   │   │   │   └── repeat.go    # . command implementation
│   │   │   └── tabs.go          # Tab bar model (multi-buffer management)
│   │   ├── results/
│   │   │   ├── model.go         # Results pane model
│   │   │   ├── grid.go          # Scrollable grid renderer
│   │   │   ├── editmode.go      # Cell edit mode; DML generation
│   │   │   └── export/
│   │   │       ├── export.go    # Format dispatch + scope resolution
│   │   │       ├── csv.go       # CSV / TSV / custom separator
│   │   │       ├── json.go      # JSON array + JSON Lines
│   │   │       ├── markdown.go  # Markdown pipe table
│   │   │       ├── pretty.go    # Fixed-width box-drawing table
│   │   │       └── sql.go       # SQL INSERT (per-row + multi-row)
│   │   ├── schema/
│   │   │   ├── model.go         # Schema browser tree model
│   │   │   ├── tree.go          # Tree node types and rendering
│   │   │   └── actions.go       # Key-driven action menu
│   │   ├── statusbar/
│   │   │   └── model.go         # Status bar model
│   │   ├── palette/
│   │   │   ├── command.go       # Command palette (fuzzy)
│   │   │   ├── history.go       # History search palette
│   │   │   └── connection.go    # Connection switcher palette
│   │   └── modal/
│   │       └── model.go         # Generic modal/dialog (confirm, input prompt)
│   ├── db/
│   │   ├── driver.go            # Driver interface definition
│   │   ├── registry.go          # Driver registration map
│   │   ├── session.go           # Connection session wrapper
│   │   ├── introspect.go        # Schema introspection types
│   │   ├── mssql/
│   │   │   ├── driver.go        # MSSQL Driver implementation
│   │   │   └── introspect.go    # MSSQL-specific sys.* queries
│   │   ├── postgres/
│   │   │   ├── driver.go        # PostgreSQL Driver implementation
│   │   │   └── introspect.go    # pg_catalog queries
│   │   └── sqlite/
│   │       ├── driver.go        # SQLite Driver implementation
│   │       └── introspect.go    # sqlite_master queries
│   ├── history/
│   │   ├── store.go             # SQLite-backed history store
│   │   └── model.go             # History entry type
│   ├── connections/
│   │   ├── store.go             # Managed connection registry (JSON, not Lua)
│   │   └── parse.go             # Connection string auto-detection + parsing
│   ├── workspace/
│   │   ├── workspace.go         # Workspace root, per-connection dir management
│   │   └── session.go           # Save/restore open tabs (file paths + cursor pos)
│   ├── config/
│   │   ├── loader.go            # Lua VM setup, config file loading
│   │   ├── schema.go            # Go structs that config maps into
│   │   └── defaults.go          # Built-in default values
│   ├── ssh/
│   │   └── tunnel.go            # SSH tunnel lifecycle management
│   ├── format/
│   │   └── sql.go               # SQL pretty-printer (wraps a formatter or hand-rolled)
│   └── util/
│       ├── clipboard.go         # OS clipboard abstraction
│       └── terminal.go          # Terminal capability detection
├── themes/
│   ├── dark.lua                 # Built-in dark theme definition
│   ├── light.lua                # Built-in light theme definition
│   └── high-contrast.lua        # Built-in high-contrast theme
├── docs/
│   └── config-reference.md
├── go.mod
├── go.sum
├── SPEC.md                      # This document
└── README.md
```

---

## 4. Architecture Overview

### 4.1 Bubbletea Model Hierarchy

sql follows the Elm architecture imposed by bubbletea. The root `app.Model` owns all sub-models and routes `tea.Msg` to the focused component.

```
app.Model
├── FocusedPane  (enum: SchemaBrowser | Editor | Results)
├── schema.Model
├── editor.Model
│   ├── []TabModel           (one per open query buffer)
│   ├── ActiveTab int
│   ├── autocomplete.Model
│   └── vim.State
├── results.Model
│   └── []ResultSet
├── statusbar.Model
├── palette.Model (nullable — shown when active)
└── modal.Model   (nullable — shown when active)
```

All inter-component communication is via `tea.Cmd` returning `tea.Msg`. No shared mutable state crosses component boundaries except through explicit message passing. The root `Update()` fans messages out to each sub-model and merges the resulting commands.

### 4.2 Async Execution

Query execution runs in a goroutine launched via `tea.Cmd`. The goroutine communicates results back to the TUI loop exclusively through `tea.Msg` values (`QueryStartedMsg`, `QueryRowMsg` / `QueryResultMsg`, `QueryDoneMsg`, `QueryErrorMsg`). The UI remains fully responsive during execution.

Cancellation uses a `context.Context` that is stored on the active session. `Ctrl-C` in the results pane sends a `CancelQueryMsg`; the app calls `cancel()` on the context, which propagates to the driver's `QueryContext` call.

### 4.3 Pane Resizing

Pane dimensions are tracked in the root model as `SchemaBrowserWidth`, `EditorHeight`, `ResultsHeight`. Resize keybindings send `ResizePaneMsg{Pane, Delta}`. The root View() recomputes lipgloss container sizes on every render; no caching of rendered strings is done for the layout frame itself (only syntax-highlighted content is cached until the buffer changes).

### 4.4 State Persistence

The only persistent state is:
- `~/.config/sql/config.lua` — user configuration.
- `~/.local/share/sql/history.db` — query history (SQLite).
- `~/.local/share/sql/workspace/` — per-connection query file directories and session restore files (see §11a).

**Platform path mapping:**

| Purpose | Linux | Windows |
|---|---|---|
| Config (`config.lua`) | `~/.config/sql/` (`$XDG_CONFIG_HOME`) | `%APPDATA%\sql\` |
| Data (history, workspace, connections) | `~/.local/share/sql/` (`$XDG_DATA_HOME`) | `%LOCALAPPDATA%\sql\` |

`%APPDATA%` is the roaming profile (synced in domain environments) — appropriate for the config file only. `%LOCALAPPDATA%` is machine-local — correct for history, workspace files, and the connections store, which should not roam across machines.

Path resolution in code: config dir via `os.UserConfigDir()` (returns `%APPDATA%` on Windows, `~/.config` on Linux); data dir via `os.Getenv("LOCALAPPDATA")` on Windows and `$XDG_DATA_HOME` (falling back to `~/.local/share`) on Linux.

---

## 5. Database Abstraction Layer

### 5.1 Driver Interface

Every supported database implements the following Go interface in `internal/db/driver.go`:

```go
// Dialect identifies the SQL dialect for syntax highlighting and introspection.
type Dialect string

const (
    DialectTSQL     Dialect = "tsql"
    DialectPostgres Dialect = "postgres"
    DialectSQLite   Dialect = "sqlite"
    DialectGeneric  Dialect = "sql"
)

// ColumnMeta describes a single column in a result set or table definition.
type ColumnMeta struct {
    Name       string
    TypeName   string  // Database-native type string
    Nullable   bool
    IsPK       bool
    IsFK       bool
    FKTable    string
    FKColumn   string
    MaxLength  int     // -1 = unlimited / not applicable
    Precision  int
    Scale      int
    Default    string
    IsIdentity bool    // MSSQL IDENTITY / serial / autoincrement
}

// ResultSet holds one complete result set from a query.
type ResultSet struct {
    Columns []ColumnMeta
    Rows    [][]any          // any is one of: nil, bool, int64, float64, string, []byte, time.Time
    Affected int64           // rows affected (DML); -1 if not applicable
    Duration time.Duration
}

// SchemaObject represents a node in the schema tree.
type SchemaObject struct {
    Type     SchemaObjectType  // Database, Schema, Table, View, Procedure, Function, Column, Index
    Name     string
    FullName string            // schema-qualified
    Columns  []ColumnMeta      // populated when Type==Table or View
    DDL      string            // populated lazily on demand
    Children []*SchemaObject   // populated lazily on expand
}

type SchemaObjectType int

const (
    ObjDatabase SchemaObjectType = iota
    ObjSchema
    ObjTable
    ObjView
    ObjProcedure
    ObjFunction
    ObjColumn
    ObjIndex
)

// ExplainResult holds the raw text/plan output for EXPLAIN queries.
type ExplainResult struct {
    Format string // "text", "json", "xml"
    Body   string
}

// Driver is the interface every database backend must satisfy.
type Driver interface {
    // Dialect returns the SQL dialect for this connection.
    Dialect() Dialect

    // Open establishes a connection using the provided profile.
    // The returned session is usable immediately.
    Open(ctx context.Context, profile ConnectionProfile) (Session, error)
}

// Session is a live database connection.
type Session interface {
    // Ping verifies the connection is alive.
    Ping(ctx context.Context) error

    // Execute runs a query that may return one or more result sets.
    // Results are returned all-at-once for simplicity in MVP;
    // streaming variant added in V2 (see StreamQuery).
    Execute(ctx context.Context, query string, params []any) ([]ResultSet, error)

    // StreamQuery executes a query and calls rowFn for each row of the first
    // result set. Used for large result pagination. [V2]
    StreamQuery(ctx context.Context, query string, params []any,
        colsFn func([]ColumnMeta), rowFn func([]any)) error

    // Introspect returns the top-level schema tree for the connection.
    // Deep population of Children is deferred (lazy load on expand).
    Introspect(ctx context.Context) (*SchemaObject, error)

    // IntrospectNode populates the Children of a SchemaObject node.
    IntrospectNode(ctx context.Context, node *SchemaObject) error

    // GetDDL returns the DDL string for a named object.
    GetDDL(ctx context.Context, obj *SchemaObject) (string, error)

    // Databases returns the list of databases available on this server.
    Databases(ctx context.Context) ([]string, error)

    // UseDatabase switches the active database on the session.
    UseDatabase(ctx context.Context, name string) error

    // BeginTx starts a transaction and returns a TxSession.
    BeginTx(ctx context.Context) (TxSession, error)

    // Explain runs the query through the database's explain mechanism.
    Explain(ctx context.Context, query string, analyze bool) (*ExplainResult, error)

    // Close releases all resources held by the session.
    Close() error

    // DriverName returns the registered driver name (e.g. "mssql", "postgres").
    DriverName() string
}

// TxSession wraps a Session inside an explicit transaction.
type TxSession interface {
    Session
    Commit(ctx context.Context) error
    Rollback(ctx context.Context) error
}

// ConnectionProfile holds all parameters needed to open a connection.
type ConnectionProfile struct {
    Name        string
    Driver      string       // "mssql", "postgres", "sqlite"
    Host        string
    Port        int
    Database    string
    Username    string
    Password    string       // stored in OS keychain keyed by connection name; empty means prompt at connect
    SSLMode     string       // "disable", "require", "verify-full", etc.
    SSHConfig   *SSHTunnel
    Extra       map[string]string  // driver-specific KV pairs

    // MSSQL-specific
    WindowsAuth bool
    AzureAD     bool
    AppName     string
    Encrypt     string       // "true", "false", "strict"

    // SQLite-specific
    FilePath    string
}

type SSHTunnel struct {
    Host       string
    Port       int
    User       string
    KeyPath    string       // path to private key file
    Password   string       // used if KeyPath is empty or key is encrypted
}
```

### 5.2 Driver Registration

`internal/db/registry.go` maintains a package-level map `map[string]Driver`. Each driver package calls `db.Register("mssql", &mssqlDriver{})` in its `init()`. The main package blank-imports driver packages to trigger registration — the same pattern as `database/sql`.

### 5.3 MSSQL-Specific Notes

- Uses `go-mssqldb`; Windows Auth via `trusted_connection=yes` in the DSN.
- Azure AD auth via the `azuread` sub-package of `go-mssqldb`, which supports interactive browser, client credentials, and managed identity flows.
- Multiple result sets are handled by iterating `sql.Rows.NextResultSet()`.
- Error messages from MSSQL include server-side line numbers; the driver maps these to buffer line numbers in the returned error value.

### 5.4 Parameter Styles

The abstraction layer normalizes parameter placeholders before delegating to the underlying driver:

- User writes `:name` or `$1` style in the editor.
- Before execution, the query processor detects the style, collects parameter names, prompts for values via a modal, then rewrites to the driver's native style (`@name` for MSSQL, `$1` for PostgreSQL, `?` for SQLite).

---

## 6. Layout & UI Model

### 6.1 Default Layout

Two vertical panes — editor on top, results on bottom — occupying the full terminal width. The schema browser is a floating overlay, not a permanent pane.

```
┌──────────────────────────────────────────────────────────────────┐
│  [tab1: query.sql] [tab2: orders.sql] +                          │
│                                                                   │
│  SELECT e.id,                                                     │
│         e.first_name,                                            │
│         d.name AS dept                                            │
│  FROM   employees e                                               │
│  JOIN   departments d ON d.id = e.dept_id                        │
│  WHERE  e.active = 1                                              │
│                                                                   │
├───────────────────────────────────────────────────────────────────┤
│  [Results]                                                        │
│  id  first_name  dept                                             │
│  1   Alice       Engineering                                      │
│  2   Bob         Marketing                                        │
│                                                                   │
├───────────────────────────────────────────────────────────────────┤
│  ● myserver  AdventureWorks  NORMAL  42 rows  123ms              │
└──────────────────────────────────────────────────────────────────┘
```

### 6.2 Schema Browser Overlay

Activated with `Ctrl-B` (or `F2`). Renders as a floating panel anchored to the left edge, overlapping the editor/results panes. Dismissed with `Esc`, `Ctrl-B`, or `F2` (toggle). Focus returns to the previously active pane on dismiss.

```
┌──────────────────────────────────────────────────────────────────┐
│  [tab1: query.sql] [tab2: orders.sql] +                          │
│ ┌─── Schema ──────────────┐                                       │
│ │ > myserver              │  e.first_name,                        │
│ │   ▼ AdventureWorks      │  d.name AS dept                       │
│ │     ▼ dbo               │  employees e                          │
│ │       ▼ Tables          │  departments d ON d.id = e.dept_id    │
│ │         ▷ employees     │  e.active = 1                         │
│ │         ▷ departments   │                                       │
│ │       ▷ Views           │                                       │
│ │       ▷ Procedures      │                                       │
│ │ /filter...              │                                       │
│ └─────────────────────────┘                                       │
├───────────────────────────────────────────────────────────────────┤
│  [Results]  id  first_name  dept ...                              │
├───────────────────────────────────────────────────────────────────┤
│  ● myserver  AdventureWorks  NORMAL  42 rows  123ms              │
└──────────────────────────────────────────────────────────────────┘
```

Overlay properties:
- Width: default 30% of terminal width, min 24 chars, adjustable with `Alt-Right` / `Alt-Left` while open.
- Height: full terminal height minus status bar.
- The underlying panes remain rendered but dimmed (reduced contrast) while the overlay is active.
- The overlay is not a permanent split — closing it reclaims all horizontal space for the editor/results.

### 6.3 Pane Dimensions

- Editor height: default 60% of available vertical space (terminal height minus tab bar minus status bar).
- Results height: remaining space.
- Both are recalculated on `tea.WindowSizeMsg`.

### 6.4 Resize Keybindings

| Action | Key |
|---|---|
| Grow editor (shrink results) | `Alt-Up` |
| Grow results (shrink editor) | `Alt-Down` |
| Reset editor/results split | `Alt-0` |
| Toggle schema overlay | `Ctrl-B` / `F2` |
| Widen schema overlay (when open) | `Alt-Right` |
| Narrow schema overlay (when open) | `Alt-Left` |

### 6.5 Status Bar

Left-to-right segments:
1. Active connection name + indicator dot (green=ok, yellow=connecting, red=error).
2. Active database name.
3. Vim mode indicator (`NORMAL`, `INSERT`, `VISUAL`, `V-LINE`) — hidden when vim mode is off.
4. Transaction status (`TXN: active` in amber) — hidden when not in a transaction.
5. Row count / affected rows from last query.
6. Query duration.
7. Right-aligned: terminal dimensions, clock (optional, config-controlled).

### 6.5 Command Palette

Activated with `Ctrl-P`. A full-width overlay modal with a fuzzy-searchable list of all named commands. Each entry shows: command name, description, current keybinding. Selecting executes the command. Implemented using `bubbles/list` with a custom delegate.

---

## 7. Editor Pane

### 7.1 Multi-Tab Model

Each tab holds:
- `Buffer`: a `[][]rune` rope-like structure (slice of lines).
- `CursorPos`: `{Row, Col int}`.
- `Name`: displayed in the tab bar (auto-named "Query 1", "Query 2", or the first non-whitespace line of the buffer).
- `ConnectionID`: which connection this tab is associated with.
- `VimState`: per-tab vim state (mode, registers, search).
- `IsDirty bool`.
- `Params`: map of substitution parameters set for this buffer.

Tab bar renders as `[Query 1] [Query 2*] [+]` where `*` indicates unsaved/dirty.

### 7.2 Syntax Highlighting

Chroma is used in `terminal256` or `truecolor` formatter mode depending on `$COLORTERM`. The SQL lexer selection:

| Connection dialect | Chroma lexer |
|---|---|
| MSSQL | `tsql` |
| PostgreSQL | `postgres` (falls back to `sql`) |
| SQLite | `sqlite3` (falls back to `sql`) |

Highlighting is re-run on every buffer change with a short debounce (50ms). The rendered highlighted lines are cached; only changed lines are re-highlighted. Cache is invalidated on cursor movement that changes the highlighted token (e.g., `*` word highlight).

### 7.3 Autocomplete

Triggered by `Tab` or `Ctrl-Space`. A popup renders above or below the cursor depending on available space (bubbles `viewport` used for the list).

Completion sources, in priority order:
1. SQL keywords for the active dialect (static list, loaded at startup).
2. Table names from the active connection's schema cache.
3. Column names from tables referenced in the current `FROM`/`JOIN` clause (requires lightweight SQL parse to detect table references).
4. Aliases defined in the current buffer.
5. Function names from schema cache.

The lightweight parser for alias detection is a hand-rolled token scanner, not a full AST parser. It extracts `FROM table_name alias` and `JOIN table_name alias` patterns.

Autocomplete popup keys:
- `Tab` / `↓` — next item.
- `Shift-Tab` / `↑` — previous item.
- `Enter` — accept.
- `Esc` — dismiss.

### 7.4 SELECT * Expansion

When cursor is on or immediately after a `*` in a `SELECT` list, `Ctrl-E` (or configurable key) expands it. The expansion:
1. Parses the `FROM` clause to determine the source table(s)/alias(es).
2. Looks up columns from the schema cache for each table.
3. Replaces `*` with `t1.col1, t1.col2, t2.col3, ...` (qualified with alias if multiple tables).

If the schema cache is stale, it triggers a refresh before expanding.

### 7.5 Query Formatting

`Ctrl-Shift-F` (or command palette: "Format Query"). Hand-rolled formatter in `internal/format/sql.go` — no external dependency, no subprocess, works identically on Windows and Linux.

### 7.5a Formatter Rules

The formatter is a single-pass token-based reformatter. It does not require a full parse tree — it operates on the token stream produced by the same lexer used for syntax highlighting (Chroma). This means it degrades gracefully on syntax errors rather than refusing to format.

**Keyword casing:** All SQL keywords are uppercased. Identifiers and string literals are left as-is.

**Clause line breaks:** The following keywords always start on a new line at the base indent level (column 0):

```
SELECT  FROM    WHERE   GROUP BY   HAVING
ORDER BY        LIMIT   OFFSET     UNION   UNION ALL
INTERSECT       EXCEPT  INSERT INTO        UPDATE  DELETE FROM
WITH    VALUES  SET     ON          RETURNING
```

**JOIN alignment:** `INNER JOIN`, `LEFT JOIN`, `RIGHT JOIN`, `FULL JOIN`, `CROSS JOIN` start on a new line at base indent. Their `ON` condition is indented one level.

```sql
SELECT e.id,
       e.name,
       d.name AS dept
FROM employees e
INNER JOIN departments d
    ON d.id = e.dept_id
WHERE e.active = 1
```

**SELECT column list:** After `SELECT`, each column is on its own line, indented one level and aligned. The first column follows `SELECT` on the same line only if there is one column; otherwise it drops to the next line.

```sql
SELECT
    e.id,
    e.first_name,
    d.name AS dept
FROM ...
```

**WHERE / HAVING predicates:** `AND` and `OR` at the top level of the predicate start on a new line, indented one level. Nested parenthesized groups add another indent level.

```sql
WHERE e.active = 1
  AND e.dept_id IN (1, 2, 3)
  AND (
      e.hire_date > '2020-01-01'
      OR e.role = 'contractor'
  )
```

**Subqueries:** Opening `(` before a `SELECT` adds an indent level. Closing `)` returns to the outer level.

**Commas:** Trailing commas (comma at end of line) preferred over leading commas.

**Blank lines:** One blank line between top-level statements in a multi-statement buffer.

**Preserved:** String literal contents, comments (`--` and `/* */`), and whitespace inside parenthesized non-subquery lists (e.g., `IN (1, 2, 3)` stays on one line if it fits within 80 chars).

**Line length:** Lines exceeding 80 characters (configurable via `editor.format_line_length`) are wrapped at the nearest comma or operator with one extra level of indent.

**Idempotent:** Formatting an already-formatted query produces the same output (running `Ctrl-Shift-F` twice changes nothing).

**Dialect awareness:** `GO` batch separators (MSSQL) are preserved as-is on their own line and not uppercased. T-SQL-specific keywords (`NOCOUNT`, `TOP`, `NVARCHAR`, etc.) are uppercased but otherwise not restructured.

### 7.6 Query Execution Modes

| Action | Description | Default Key |
|---|---|---|
| Run logical block | Runs the SQL block the cursor is in or nearest to (see 7.6a) | `Ctrl-Enter` |
| Run full buffer | Executes the entire buffer text | `F5` |
| Run selection | Executes visually selected text | `F5` when text is selected |
| Run all statements | Splits on block boundaries and runs each sequentially | `Ctrl-Shift-Enter` |

### 7.6a Logical Block Detection (Ctrl-Enter)

A **logical block** is the single executable SQL unit containing or nearest to the cursor. Block boundaries are detected as follows, in order:

**MSSQL (T-SQL):** `GO` on its own line (case-insensitive) is the primary boundary. Semicolons are secondary. Whitespace-only lines between statements without `GO`/`;` are treated as separators if the gap is ≥ 2 blank lines.

**PostgreSQL / SQLite:** Semicolons are the primary boundary. Two or more consecutive blank lines are secondary separators.

**Detection algorithm:**
1. Walk backward from cursor line to find the start of the current block (hits boundary or BOF).
2. Walk forward from cursor line to find the end of the current block (hits boundary or EOF).
3. Strip leading/trailing whitespace from the extracted range.
4. If the cursor is exactly on a boundary line (e.g. `GO`, or a blank separator), prefer the block **below** the cursor.
5. The detected block is highlighted briefly (200ms) before execution so the user sees what ran.

**Example (MSSQL, cursor at `▌`):**
```sql
SELECT 1

▌SELECT name FROM sys.tables
WHERE type = 'U'

SELECT 2
```
`Ctrl-Enter` executes only `SELECT name FROM sys.tables\nWHERE type = 'U'`.

**Edge cases:**
- Single statement with no delimiters: entire buffer is the block.
- Cursor on a comment-only line: execute the block that comment belongs to (nearest non-empty block).

### 7.7 Parameter Substitution

If the buffer contains `:name` or `$1` placeholders, execution first opens a modal prompting for each value. Values are typed-checked loosely (numeric strings converted to numbers). Previously entered values are suggested. Parameters are stored per-tab.

### 7.8 Comment Toggle

`Ctrl-/`:
- Single line (or no selection): toggles `-- ` prefix on current line.
- Multi-line selection: toggles `-- ` on each selected line (line comment mode).
- `Ctrl-Shift-/`: wraps/unwraps `/* ... */` block comment around selection.

---

## 8. Vim Mode

### 8.1 Mode State Machine

```
            i/I/a/A/o/O/cc/cw/c$
NORMAL ─────────────────────────────▶ INSERT
  │  ◀──────────────────────────────── │
  │              Esc                   │
  │
  │    v
  ├──────────────────────────────────▶ VISUAL (char)
  │  ◀──────────────────────────────── │
  │              Esc                   │
  │
  │    V
  └──────────────────────────────────▶ VISUAL LINE
     ◀──────────────────────────────── │
                   Esc
```

Pressing `Esc` in INSERT or VISUAL always returns to NORMAL. `Esc` in NORMAL clears any pending count/operator and dismisses search highlight.

### 8.2 Normal Mode — Movement

| Key(s) | Motion |
|---|---|
| `h`, `l` | Character left/right |
| `j`, `k` | Line down/up |
| `w`, `W` | Word forward (word / WORD) |
| `b`, `B` | Word backward |
| `e`, `E` | End of word forward |
| `0` | Start of line (col 0) |
| `^` | First non-whitespace |
| `$` | End of line |
| `gg` | First line |
| `G` | Last line (or `{count}G` = go to line) |
| `{`, `}` | Prev/next blank line (paragraph) |
| `Ctrl-u` | Half-page up |
| `Ctrl-d` | Half-page down |

All motions accept an optional count prefix: `5j`, `3w`, etc.

### 8.3 Normal Mode — Editing

| Key | Action |
|---|---|
| `i` | Insert before cursor |
| `I` | Insert at start of line |
| `a` | Insert after cursor |
| `A` | Insert at end of line |
| `o` | Open new line below, enter INSERT |
| `O` | Open new line above, enter INSERT |
| `x` | Delete character under cursor |
| `dd` | Delete current line (to unnamed register) |
| `yy` | Yank current line |
| `p` | Paste after cursor/line |
| `P` | Paste before cursor/line |
| `u` | Undo |
| `Ctrl-r` | Redo |
| `cc` | Change entire line |
| `cw` | Change word |
| `c$` | Change to end of line |
| `dw` | Delete word |
| `d$` | Delete to end of line |
| `.` | Repeat last change |

Operator + count: `2dd` (delete 2 lines), `3dw` (delete 3 words).

### 8.4 Search

| Key | Action |
|---|---|
| `/pattern` | Search forward |
| `?pattern` | Search backward |
| `n` | Next match (same direction) |
| `N` | Previous match (opposite direction) |
| `*` | Search forward for word under cursor |
| `#` | Search backward for word under cursor |

Search highlights all matches in the visible buffer. Highlight cleared on `Esc` in NORMAL.

Search pattern uses **RE2 syntax** (Go `regexp` package — not Vim regex). The status bar shows `RE2` next to the search prompt as a constant reminder. Common differences from Vim regex: no `\v` very-magic, no lookahead/lookbehind, use `(?i)` for case-insensitive.

### 8.5 Visual Mode

| Key | Action |
|---|---|
| `v` | Character-wise visual |
| `V` | Line-wise visual |
| `y` | Yank selection |
| `d` | Delete selection |
| `>` | Indent selection (add one tab/spaces) |
| `<` | Dedent selection |

### 8.6 Text Objects

Supported in `d{obj}`, `c{obj}`, `y{obj}`, visual `v{obj}`:

| Object | Description |
|---|---|
| `iw` / `aw` | Inner word / a word (with surrounding space) |
| `i"` / `a"` | Inner / outer double-quoted string |
| `i'` / `a'` | Inner / outer single-quoted string |
| `i(` / `a(` | Inner / outer parentheses |
| `i[` / `a[` | Inner / outer square brackets |

### 8.7 Registers

Unnamed register (`"`) is used by default for `d`, `c`, `y`. Named registers (`"a`–`"z`) supported for yank and paste. The OS clipboard is mapped to `"+` register via `util/clipboard.go`.

### 8.8 Undo/Redo

Undo history is per-buffer, stored as a stack of `[][]rune` snapshots. MVP: coarse-grained (each edit action is one undo step). V2: fine-grained change records to match Vim's undo granularity.

---

## 9. Results Pane

### 9.1 Grid Rendering

The results grid is a custom component (not a bubbles component) built on `lipgloss.Table` or a hand-rendered approach using `lipgloss.Style`. Requirements:

- Fixed column headers that remain visible during vertical scroll.
- Horizontal scroll when columns exceed terminal width.
- Column widths auto-sized to max(header_len, max_data_len) up to a configurable cap (default 40 chars), then truncated with `…`.
- Selected cell highlighted.
- `NULL` values rendered as `∅` in a dimmed/gray style.

Rendering uses `bubbles/viewport` for the scrollable body; headers are rendered separately outside the viewport.

### 9.2 Navigation

| Key | Action |
|---|---|
| Arrow keys / `hjkl` | Move selected cell |
| `Home` / `g` | First row |
| `End` / `G` | Last row |
| `Ctrl-Left/Right` | First/last column |
| `Ctrl-Home` | First cell |
| `Page Up/Down` | Scroll page |

### 9.3 Column Resize

`Alt-[` / `Alt-]` narrowing/widening the currently focused column. Column width overrides are stored per result set (not persisted across sessions).

### 9.4 Sorting

`s` on a focused column header (or `Ctrl-S` while a column is selected) cycles: unsorted → ascending → descending → unsorted. Sorting is client-side; the raw `ResultSet.Rows` slice is sorted in-memory.

### 9.5 Client-Side Filter

`/` in the results pane opens a filter input. Typed text is matched against the string representation of every cell in every row. Non-matching rows are hidden. Filter is cleared with `Esc`.

### 9.6 Export & Copy

#### 9.6.1 Scope

Export operates on one of three scopes, selected automatically:

| Scope | When |
|---|---|
| **Selection** | One or more rows/cells are visually selected |
| **Filtered view** | A client-side filter is active (exports visible rows only) |
| **All rows** | No selection, no filter — entire result set |

The active scope is shown in the export menu header so the user always knows what will be exported.

#### 9.6.2 Export Menu

`e` opens the export menu (also reachable from the command palette). The menu is a two-step picker:

**Step 1 — Choose format:**

```
┌─ Export Results ─────────────────────────────┐
│  Scope: all 1,204 rows                       │
│                                              │
│  > CSV            (comma, with headers)      │
│    TSV            (tab, with headers)        │
│    Markdown       (pipe table)               │
│    Pretty         (aligned text table)       │
│    JSON           (array of objects)         │
│    JSON Lines     (one object per line)      │
│    SQL INSERT     (INSERT INTO ... VALUES)   │
│    SQL INSERT     (multi-row INSERT)         │
│    Cell value     (focused cell only)        │
│    Custom…        (configure separator)      │
└──────────────────────────────────────────────┘
```

**Step 2 — Choose destination:**

```
┌─ Export as CSV ──────────────────────────────┐
│                                              │
│  > Copy to clipboard                         │
│    Save to file…                             │
│    Open in editor tab                        │
└──────────────────────────────────────────────┘
```

`Save to file…` opens an inline path input pre-filled with the connection's workspace dir and a generated filename (e.g. `results_20260310_143022.csv`). The user can edit the full path.

`Open in editor tab` writes the output into a new editor tab (read-only, named e.g. `[export].csv`). Useful for inspecting output or piping it into another query.

#### 9.6.3 Format Specifications

**CSV** — RFC 4180. Header row always included. Values containing commas, quotes, or newlines are double-quoted and internal quotes escaped as `""`.

```
id,first_name,dept
1,Alice,Engineering
2,"O'Brien, Bob",Marketing
```

**TSV** — Tab-separated. Header row included. Tabs within values are replaced with a space (or escaped as `\t` — configurable). Safe for pasting into Excel.

**Markdown** — GitHub Flavored Markdown pipe table. Right-aligns numeric columns. NULL shown as empty cell.

```markdown
| id | first_name    | dept        |
|----|---------------|-------------|
|  1 | Alice         | Engineering |
|  2 | O'Brien, Bob  | Marketing   |
```

**Pretty** — Fixed-width aligned table with box-drawing characters. Same as what the results grid looks like; useful for pasting into documentation, Slack, or a terminal session log.

```
┌────┬───────────────┬─────────────┐
│ id │ first_name    │ dept        │
├────┼───────────────┼─────────────┤
│  1 │ Alice         │ Engineering │
│  2 │ O'Brien, Bob  │ Marketing   │
└────┴───────────────┴─────────────┘
```

**JSON** — Array of objects. Column names are keys. NULL serialized as `null`. Column types preserved where possible (numbers as numbers, not strings).

```json
[
  { "id": 1, "first_name": "Alice", "dept": "Engineering" },
  { "id": 2, "first_name": "O'Brien, Bob", "dept": "Marketing" }
]
```

**JSON Lines** — One JSON object per line (NDJSON). Preferred for large exports / streaming pipelines.

```
{"id":1,"first_name":"Alice","dept":"Engineering"}
{"id":2,"first_name":"O'Brien, Bob","dept":"Marketing"}
```

**SQL INSERT (per-row)** — One `INSERT` statement per row. Table name guessed from the query's `FROM` clause; if ambiguous or not determinable, a prompt asks for the table name before export.

```sql
INSERT INTO employees (id, first_name, dept) VALUES (1, 'Alice', 'Engineering');
INSERT INTO employees (id, first_name, dept) VALUES (2, 'O''Brien, Bob', 'Marketing');
```

**SQL INSERT (multi-row)** — Single `INSERT` with all rows as a `VALUES` list. Fewer statements, but one failure aborts all.

```sql
INSERT INTO employees (id, first_name, dept) VALUES
  (1, 'Alice', 'Engineering'),
  (2, 'O''Brien, Bob', 'Marketing');
```

NULL values in INSERT output are rendered as `NULL` (unquoted). String values use single-quote escaping appropriate to the active connection's dialect (MSSQL doubles single quotes; PostgreSQL same; SQLite same).

**Cell value** — Copies the raw string value of the focused cell only, with no quoting or formatting. NULL copies as empty string (or the literal text `NULL` — configurable).

**Custom** — Opens a prompt to specify the separator character and whether to include headers. Enables pipe-separated, semicolon-separated, etc.

#### 9.6.4 Clipboard Behaviour

Clipboard write uses `golang.design/x/clipboard` (cross-platform, no external tools). On systems where clipboard access is unavailable (headless SSH), a fallback writes to stdout and instructs the user to pipe output. OSC 52 escape sequence support is a stretch goal for remote clipboard via terminal.

#### 9.6.5 Quick-Copy Shortcuts

For the most common cases without opening the menu:

| Key | Action |
|---|---|
| `y` | Copy focused cell value to clipboard |
| `Y` | Copy all rows as CSV (with headers) to clipboard — fastest path |
| `e` | Open full export menu |

### 9.7 Large Results & Pagination

MVP: Execute returns all rows up to a configurable limit (default 10,000). A status warning shows if the limit was hit.

V2: `StreamQuery` is used; results are streamed into the grid 100 rows at a time. The user can request more pages with `Ctrl-N`. A progress indicator shows during streaming.

### 9.8 Multiple Result Sets

MSSQL stored procedures and batches can return multiple result sets. Each result set is shown as a labeled tab within the results pane: `[Result 1] [Result 2] [Messages]`. The `Messages` tab shows PRINT output and RAISERROR messages.

### 9.9 Edit Mode

`Ctrl-E` in the results pane toggles edit mode (only when the result set has a deterministic primary key and comes from a single table — detected via column metadata or user confirmation prompt).

In edit mode:
- Focused cell becomes editable (enters a text input widget).
- `Enter` confirms the cell edit; the changed row is marked dirty (highlighted).
- `+` inserts a new blank row at the bottom.
- `dd` marks the focused row for deletion.
- `Ctrl-S` generates and displays (in the editor) the `UPDATE`/`INSERT`/`DELETE` statements for all dirty rows, then prompts to execute.
- `Esc` cancels edit mode, discards uncommitted changes.

---

## 10. Schema Browser

The schema browser is a floating overlay toggled with `Ctrl-B` / `F2`. It is not a permanent split pane. When open, it anchors to the left edge and overlaps the editor and results panes. Pressing `Esc`, `Ctrl-B`, or `F2` again dismisses it and returns focus to the previously active pane.

### 10.1 Tree Structure

```
▼ myserver  (connection)
  ▼ AdventureWorks  (database)
    ▼ dbo  (schema)
      ▶ Tables
        ▼ Employee  (table)
            id          INT NOT NULL PK
            first_name  NVARCHAR(100) NOT NULL
            dept_id     INT FK→departments.id
        ▶ Views
        ▶ Stored Procedures
        ▶ Functions
    ▶ HumanResources
  ▶ master
```

Each node type has a distinct icon (Unicode, single char): `⊞` connection, `◉` database, `⊟` schema, `▤` table, `⊡` view, `λ` function/procedure, `∘` column.

### 10.2 Navigation

| Key | Action |
|---|---|
| `j` / `k` | Down / up |
| `h` / `←` | Collapse node |
| `l` / `→` or `Enter` | Expand node |
| `gg` / `G` | First / last visible node |
| `Ctrl-F` or `/` | Open fuzzy filter input |
| `Esc` | Clear filter |
| `F5` or `r` | Refresh subtree |
| `Space` | Open action menu for node |

### 10.3 Action Menu

`Space` on a node opens a vertical action menu (lipgloss overlay). Items are context-sensitive:

**Table/View node:**
- Preview data (SELECT TOP 100 / LIMIT 100)
- View DDL
- Copy full name
- Copy SELECT statement
- Copy INSERT template
- Refresh

**Column node:**
- Copy column name
- Copy qualified name (`table.column`)

**Connection node:**
- Disconnect
- Refresh all
- New tab for this connection

**Procedure/Function node:**
- View definition
- Copy EXEC / SELECT template
- Refresh

### 10.4 Fuzzy Filter

`/` in the schema browser opens a filter bar at the bottom of the overlay. Typed text is matched against node names using `github.com/sahilm/fuzzy`. Non-matching nodes and their ancestors are hidden; matching nodes and their full ancestor path are shown. Filter is live (updates as you type).

### 10.5 Lazy Loading

Children are loaded on first expand. A spinner node is shown while loading. Errors during load are shown inline as an error node.

---

## 11. Connections

### 11.1 Two-tier Connection Storage

Connections are stored in two places, each with a different purpose:

| Store | Location | Purpose |
|---|---|---|
| **Managed store** | `~/.local/share/sql/connections.json` | App-managed; written by quick-add and `--add` CLI. Never hand-edited. |
| **Lua config** | `~/.config/sql/config.lua` | Hand-authored; advanced options, SSH tunnels, startup queries. Read-only to the app. |

Both sources are merged at startup. If the same name exists in both, the Lua config wins (allows overriding managed entries with advanced settings). The managed store uses JSON so the app can read/write it without running a Lua VM.

### 11.2 Quick-Add Connection

`Ctrl-N` (when not in editor) opens the **Add Connection** modal — a two-field form:

```
┌─ Add Connection ─────────────────────────────────────────┐
│  Name:    [prod-db                                      ] │
│                                                           │
│  Connection string:                                       │
│  [Server=myhost;Database=mydb;User Id=sa;Password=…     ] │
│                                                           │
│  Detected: MS SQL Server                                  │
│                                                           │
│  [Connect]  [Save & Connect]  [Save Only]  [Cancel]       │
└───────────────────────────────────────────────────────────┘
```

- Driver is **auto-detected** from the connection string as the user types:
  - `Server=` / `Data Source=` / `sqlserver://` → MSSQL
  - `postgres://` / `postgresql://` / `host=` → PostgreSQL
  - Ends in `.db` / `.sqlite` / `file:` / bare path → SQLite
- The detected driver is shown inline so the user can confirm before connecting.
- **Connect**: connect immediately, do not save.
- **Save & Connect**: write to managed store + connect.
- **Save Only**: write to managed store, do not connect.
- Passwords are **never written to `connections.json`** or the Lua config. They are stored in the OS keychain keyed by connection name (see §11.5).

#### Connection String Formats Accepted

| Driver | Accepted formats |
|---|---|
| MSSQL | `Server=h;Database=d;User Id=u;Password=p;` (ADO.NET style), `sqlserver://u:p@h/d` (URL style) |
| PostgreSQL | `postgres://u:p@h/d`, `host=h dbname=d user=u password=p` (DSN style) |
| SQLite | `/path/to/file.db`, `./relative.db`, `file:path.db`, `file::memory:` |

### 11.3 Connection Switcher

`Ctrl-K` opens the connection switcher palette — a fuzzy-searchable list of all connections from both stores. Each entry shows the connection name, driver icon, and host. Selecting:
- If a session for that connection is already open: switches focus to it.
- Otherwise: opens a new session and creates a tab in that connection's workspace.

### 11.4 Simultaneous Connections

Multiple connections can be open at the same time. Each tab is associated with exactly one connection. The status bar shows the active tab's connection. Each connection has a unique color assigned (cycled from a palette defined in the theme); tab labels and the status bar indicator use this color.

### 11.4a SSH Tunnels

SSH tunnels are managed by `internal/ssh/tunnel.go`. Lifecycle:
1. On connect, if `ssh` config is present, establish the tunnel first.
2. The local listener binds to `127.0.0.1:0` (random port); the actual DB host/port are forwarded through.
3. The DB driver connects to `127.0.0.1:{localPort}`.
4. Tunnel is torn down on session close or app exit.
5. Key authentication is tried first; password fallback is prompted via modal.

### 11.5 Password Storage

Passwords are stored in the **OS keychain**, never in `connections.json` or any config file on disk.

| Platform | Keychain backend | Library |
|---|---|---|
| Windows | Windows Credential Manager (`wincred`) | `github.com/zalando/go-keyring` |
| Linux | SecretService (GNOME Keyring / KWallet) | same |
| macOS | Keychain | same |
| Headless / no keychain | Encrypted file fallback (AES-256-GCM, key derived from machine ID) | internal |

**Keychain key format:** `sql/<connection-name>` — one entry per connection.

**Password lifecycle:**
- **Quick-add modal**: if the connection string contains a password, it is stripped from the string stored in `connections.json` and written to the keychain immediately under the connection name.
- **Connect**: if the keychain entry exists, it is fetched silently. If not, a password prompt modal is shown. The user can opt to save it to the keychain from the prompt.
- **Edit connection**: `Ctrl-E` on a connection in the switcher opens an edit modal. The password field shows `••••••••` if a keychain entry exists; clearing it and saving deletes the keychain entry.
- **Delete connection**: deleting a connection also deletes its keychain entry.

**Headless fallback**: on systems with no keychain (CI, Docker, minimal Linux), `go-keyring` returns an error. sql falls back to an AES-256-GCM encrypted file at `~/.local/share/sql/credentials.enc`. The encryption key is derived from the machine's stable ID (`/etc/machine-id` on Linux, `MachineGuid` registry key on Windows). This is not as secure as a real keychain but avoids plaintext. A warning is shown on first use of the fallback.

### 11.6 MSSQL Auth Modes

| Mode | Config | Notes |
|---|---|---|
| SQL auth | `username` + `password` | Default |
| Windows Auth | `windows_auth = true` | Uses current OS user; SSPI on Windows, Kerberos on Linux/Mac |
| Azure AD Interactive | `azure_ad = "interactive"` | Opens browser for OIDC flow |
| Azure AD Service Principal | `azure_ad = "sp"` + `client_id` + `client_secret` | Non-interactive |
| Azure AD Managed Identity | `azure_ad = "msi"` | For use inside Azure VMs/containers |

---

## 11a. Workspace & Query Files

### 11a.1 Directory Layout

Each named connection gets its own subdirectory under the workspace root. Query buffers are plain `.sql` files on disk.

```
~/.local/share/sql/
├── connections.json          # managed connection store
├── history.db                # query history (SQLite)
└── workspace/
    ├── prod-db/              # one dir per connection name
    │   ├── query1.sql
    │   ├── orders.sql
    │   └── session.json      # last open tabs + cursor positions
    ├── local-dev/
    │   ├── scratch.sql
    │   └── session.json
    └── _adhoc/               # for unnamed ad-hoc connections
        └── scratch.sql
```

### 11a.2 Tab ↔ File Binding

Every editor tab is backed by a file. When a new tab is opened:
- Default filename: `query<N>.sql` (e.g., `query1.sql`, `query2.sql`) in the connection's workspace dir.
- The user can rename the tab / file with `Ctrl-Shift-S` (save as), which renames the file on disk.
- Tabs can also be opened from arbitrary paths via `Ctrl-O` (open file).

### 11a.3 Auto-Save

The buffer is written to its backing file:
- On every execution (`F5`, `Ctrl-Enter`, `F9`).
- After 2 seconds of inactivity (debounced).
- On tab close or app exit.

There is no explicit "save" command — files are always up to date. The tab label shows the filename (without directory). A `•` dot indicates unsaved changes that haven't been flushed yet (within the debounce window).

### 11a.4 Session Restore

`session.json` in each connection's workspace dir records:
```json
{
  "tabs": [
    { "file": "orders.sql", "cursor": { "line": 12, "col": 4 } },
    { "file": "query1.sql", "cursor": { "line": 1,  "col": 0 } }
  ],
  "active_tab": 0
}
```

On startup with a named connection (or on reconnect), sql reads `session.json` and reopens the recorded tabs with their cursor positions. Missing files are skipped with a warning.

Session restore is **always on** — there is no config toggle.

### 11a.5 Ad-hoc Connections

Connections opened via raw connection string (not saved) use the `_adhoc/` workspace dir. Tab files persist there, but the connection itself is not saved. On the next launch the user must provide the connection string again; the files remain.

---

## 12. Query Execution

### 12.1 Async Flow

```
User presses F5
       │
       ▼
app.Update receives ExecuteMsg
       │
       ▼
Parse params from buffer → if params present, open param modal → collect values
       │
       ▼
Build tea.Cmd: goroutine calls session.Execute(ctx, sql, params)
       │
       ├── Immediately: results.Model receives QueryStartedMsg → show spinner
       │
       ▼  (goroutine)
session.Execute runs in background
       │
       ├── On success: sends QueryDoneMsg{[]ResultSet}
       └── On error:   sends QueryErrorMsg{error}
       │
       ▼
app.Update receives QueryDoneMsg / QueryErrorMsg
       │
       ▼
results.Model updates; status bar updates; spinner removed
```

### 12.2 Cancellation

The execution context is stored as `session.activeCtx` with its cancel function. `Ctrl-C` while results pane is focused (or while spinner is visible) sends `CancelQueryMsg`, which calls `cancel()`. The driver's `QueryContext` returns a `context.Canceled` error, which is shown as "Query cancelled" in the status bar — not as an error.

### 12.3 Transaction Mode

A toggle (`Ctrl-T`) switches the active connection between auto-commit (default) and manual transaction mode.

In manual mode:
- A `BEGIN TRANSACTION` is automatically issued on the first statement after commit/rollback.
- The status bar shows `TXN: active` in amber.
- `Ctrl-Shift-C` commits. `Ctrl-Shift-R` rolls back.
- Disconnecting or closing the tab while a transaction is open prompts: "Active transaction — Commit, Rollback, or Cancel?"

### 12.4 EXPLAIN

`Ctrl-Shift-E` runs EXPLAIN (without ANALYZE) on the current buffer. `Ctrl-Shift-A` runs EXPLAIN ANALYZE (PostgreSQL) or `SET STATISTICS` (MSSQL). Output is shown in a results tab labeled `[Explain]` rendered as plain text in a viewport.

Dialect mapping:

| Dialect | Explain | Explain Analyze |
|---|---|---|
| PostgreSQL | `EXPLAIN ...` | `EXPLAIN ANALYZE ...` |
| MSSQL | `SET SHOWPLAN_TEXT ON` | `SET STATISTICS IO, TIME ON` |
| SQLite | `EXPLAIN QUERY PLAN ...` | same |

---

## 13. Query History

### 13.1 Storage Schema

History is stored in `~/.local/share/sql/history.db` (SQLite):

```sql
CREATE TABLE history (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    connection  TEXT    NOT NULL,
    database    TEXT    NOT NULL,
    sql         TEXT    NOT NULL,
    executed_at DATETIME NOT NULL DEFAULT (datetime('now')),
    duration_ms INTEGER,
    row_count   INTEGER,
    had_error   BOOLEAN NOT NULL DEFAULT 0
);

CREATE INDEX idx_history_connection ON history(connection, executed_at DESC);
```

Duplicate consecutive identical queries on the same connection are collapsed (only the latest timestamp is updated, not a new row inserted).

### 13.2 History Palette

`Ctrl-H` opens the history palette. A fuzzy-searchable list sorted by `executed_at DESC`. Each entry shows:
- Truncated SQL (first 80 chars).
- Connection name.
- Timestamp (relative: "2 hours ago").
- Duration and row count.
- Error indicator if `had_error = 1`.

Selecting an entry pastes the SQL into the current editor tab (does not auto-execute). `Enter` in the palette selects and pastes; `Ctrl-R` selects and immediately executes.

---

## 14. Configuration (Lua)

### 14.1 Full Config Schema

```lua
-- ~/.config/sql/config.lua

-- Connections (see section 11.1)
connections = { ... }

-- Editor settings
editor = {
  tab_size    = 2,
  use_spaces  = true,
  vim_mode    = true,         -- default vim mode on/off
  wrap        = false,
  row_limit   = 10000,        -- max rows fetched
  theme       = "dark",       -- "dark" | "light" | "high-contrast" | custom table
  chroma_theme = "monokai",   -- any chroma style name
  font_width   = 1,           -- 1 or 2 for wide characters (CJK environments)
  undo_limit   = 100,         -- max undo steps per buffer
  format_line_length = 80,    -- target line length for query formatter
}

-- Keybinding overrides (see section 15)
keys = {
  execute          = "f5",
  execute_selection = "f5",   -- same key; context-sensitive
  execute_statement = "f9",
  format_query     = "ctrl+shift+f",
  -- ... etc
}

-- Per-connection startup queries
startup = {
  ["local-mssql"] = "SET NOCOUNT ON;",
  ["prod-postgres"] = "SET search_path TO myschema, public;",
}

-- Custom theme (optional; overrides built-in theme fields)
theme = {
  border       = "#444444",
  background   = "#1e1e1e",
  foreground   = "#d4d4d4",
  cursor       = "#ffffff",
  selection    = "#264f78",
  tab_active   = "#007acc",
  tab_inactive = "#3c3c3c",
  null_color   = "#666666",
  error_color  = "#f44747",
  warn_color   = "#ffcc00",
  conn_colors  = { "#4ec9b0", "#ce9178", "#569cd6", "#dcdcaa" },
}
```

### 14.2 Config Loading

1. The Lua VM (`gopher-lua`) is initialized with a sandboxed environment: no `io`, `os`, or `require` access by default (security: the config file runs as code).
2. The config file is loaded with `lua.DoFile()`.
3. After execution, the VM's global table is walked to extract `connections`, `editor`, `keys`, `theme`, `startup` into Go structs via `config/schema.go`.
4. Unknown keys generate a warning log, not a fatal error.
5. If no config file exists, `config/defaults.go` provides all defaults.

`require` is allowed in the config, but with a restricted custom loader:

- Search path is limited to the config directory (`~/.config/sql/` on Linux, `%APPDATA%\sql\` on Windows). No other paths are searched.
- Standard library modules that grant system access (`io`, `os`, `package`, `debug`) are removed from the sandbox before the config runs. `math`, `string`, `table` remain available as they are harmless.
- `require("connections")` loads `~/.config/sql/connections.lua`. Path traversal (`require("../../etc/foo")`) is rejected — module names containing `/`, `\`, or `..` are an error.
- Circular requires are detected and produce a clear error rather than hanging.

This allows clean config splitting:

```lua
-- ~/.config/sql/config.lua
require("connections")
require("keymaps")

editor = { vim_mode = true, tab_size = 2 }
```

---

## 15. Keybindings Reference

### 15.1 Global

| Action | Default Key | Priority |
|---|---|---|
| Command palette | `Ctrl-P` | MVP |
| Connection switcher | `Ctrl-K` | MVP |
| History palette | `Ctrl-H` | MVP |
| Vim mode toggle | `Ctrl-Alt-V` | MVP |
| Quit | `Ctrl-Q` | MVP |
| Next pane focus | `Ctrl-Tab` | MVP |
| Prev pane focus | `Ctrl-Shift-Tab` | MVP |
| Toggle schema browser overlay | `Ctrl-B` / `F2` | MVP |
| Focus editor | `Ctrl-1` | MVP |
| Focus results | `Ctrl-2` | MVP |

### 15.2 Tab Management

| Action | Default Key | Priority |
|---|---|---|
| New tab | `Ctrl-N` | MVP |
| Close tab | `Ctrl-W` | MVP |
| Next tab | `Ctrl-PgDn` or `Alt-L` | MVP |
| Prev tab | `Ctrl-PgUp` or `Alt-H` | MVP |
| Go to tab N | `Alt-1` … `Alt-9` | V2 |

### 15.3 Editor

| Action | Default Key | Priority |
|---|---|---|
| Execute logical block (under cursor) | `Ctrl-Enter` | MVP |
| Execute full buffer | `F5` | MVP |
| Execute all blocks sequentially | `Ctrl-Shift-Enter` | MVP |
| Rename tab / save as | `Ctrl-Shift-S` | MVP |
| Open file into tab | `Ctrl-O` | MVP |
| Format query | `Ctrl-Shift-F` | MVP |
| Expand SELECT * | `Ctrl-E` | MVP |
| Toggle line comment | `Ctrl-/` | MVP |
| Toggle block comment | `Ctrl-Shift-/` | MVP |
| Autocomplete | `Tab` / `Ctrl-Space` | MVP |
| Cancel execution | `Ctrl-C` (when running) | MVP |

### 15.4 Schema Browser

| Action | Default Key | Priority |
|---|---|---|
| Expand / collapse | `Enter` or `l`/`h` | MVP |
| Action menu | `Space` | MVP |
| Fuzzy filter | `/` | MVP |
| Clear filter | `Esc` | MVP |
| Refresh | `F5` or `r` | MVP |

### 15.5 Results Pane

| Action | Default Key | Priority |
|---|---|---|
| Navigate cell | Arrow keys / `hjkl` | MVP |
| Sort column | `s` | MVP |
| Filter rows | `/` | MVP |
| Copy focused cell to clipboard | `y` | MVP |
| Copy all rows as CSV to clipboard | `Y` | MVP |
| Export menu (format + destination) | `e` | MVP |
| Toggle edit mode | `Ctrl-E` | V2 |
| Save edits | `Ctrl-S` (in edit mode) | V2 |
| Cancel edit mode | `Esc` (in edit mode) | V2 |
| Next result set | `Alt-PgDn` | MVP |
| Prev result set | `Alt-PgUp` | MVP |

### 15.6 Transaction

| Action | Default Key | Priority |
|---|---|---|
| Toggle transaction mode | `Ctrl-T` | MVP |
| Commit | `Ctrl-Shift-C` | MVP |
| Rollback | `Ctrl-Shift-R` | MVP |

---

## 16. Theming

### 16.1 Built-in Themes

Three built-in themes are defined as Lua tables (loaded from `themes/` at startup):

- `dark` (default): VS Code Dark+ inspired palette.
- `light`: Light background, high-contrast text.
- `high-contrast`: WCAG AA compliant, no subtle grays.

### 16.2 Chroma Syntax Themes

Any Chroma style name is valid for `editor.chroma_theme`. Recommended defaults:
- Dark: `monokai`
- Light: `vs`
- High-contrast: `monokailight` or `tango`

### 16.3 Custom Themes

Users define a `theme = { ... }` table in their config (see section 14.1). Only overridden keys need to be specified; unspecified keys fall back to the active built-in theme.

### 16.4 Color Resolution

All colors are passed to lipgloss as `lipgloss.AdaptiveColor` or `lipgloss.Color`. The terminal's color support is detected via `$COLORTERM` and `$TERM`:
- `truecolor` / `24bit`: full hex colors.
- `256color`: nearest 256-color match via chroma's terminal256 formatter.
- Fallback: ANSI 16 colors (high-contrast theme auto-selected).

---

## 17. Error Handling & UX

### 17.1 Error Display

- **Query errors**: shown in a red banner between the editor and results panes. The banner shows the error message, and for MSSQL, the server-side line number is highlighted in the editor gutter.
- **Connection errors**: shown in the status bar indicator (red dot) with the error message in a tooltip shown on `?` in the status bar.
- **Config errors**: shown at startup in a full-screen error modal before the main TUI launches.
- **Non-fatal warnings**: amber text in the status bar, auto-dismissed after 5 seconds.

### 17.2 MSSQL Error Line Mapping

`go-mssqldb` returns errors with a `LineNo` field. sql maps this to the buffer line by:
1. Identifying the statement in the buffer that was executed.
2. Computing the offset of the statement's start line within the buffer.
3. Adding `LineNo - 1` to get the absolute buffer line.
4. Rendering a `▶` gutter marker on that line.

### 17.3 Connection Loss Detection

A background goroutine pings the active connections every 30 seconds (configurable). On ping failure:
1. The connection dot turns red.
2. A non-blocking notification bar appears: "Connection lost. [Reconnect] [Dismiss]".
3. If the user attempts to execute a query while disconnected, the reconnect prompt is shown modally.
4. Auto-reconnect with exponential backoff is attempted in the background (1s, 2s, 4s … max 60s); the UI shows a spinner on the connection indicator while reconnecting.

### 17.4 Long Query Warning

If a query runs longer than `long_query_threshold` (default 30s, configurable), a non-blocking warning appears: "Query running for 30s. [Cancel]". This is advisory only.

### 17.5 Data Safety

- Edit mode (results pane) does not execute DML automatically. The generated statements are shown in the editor and require explicit execution.
- Destructive actions (delete row in edit mode, rollback transaction) prompt for confirmation via a `[y/N]` modal.
- The app never modifies the database without an explicit user action that triggers execution.

---

## 18. Feature Priority Table

| Feature | Priority | Notes |
|---|---|---|
| 2-pane layout (editor + results) | MVP | Core UX |
| Schema browser overlay (popup) | MVP | Toggle with Ctrl-B / F2 |
| Status bar | MVP | |
| Multi-tab editor | MVP | |
| Syntax highlighting (SQL) | MVP | Chroma |
| Vim mode (Normal + Insert) | MVP | |
| Vim Visual mode | MVP | |
| Vim text objects | MVP | |
| Vim `.` repeat | MVP | |
| Autocomplete (keywords) | MVP | |
| Autocomplete (schema-aware) | MVP | |
| SELECT * expansion | MVP | |
| Query execution (buffer) | MVP | |
| Query execution (selection) | MVP | |
| Query execution (statement) | MVP | |
| Multi-statement support | MVP | |
| Parameter substitution | MVP | |
| Async execution + cancel | MVP | |
| MSSQL driver | MVP | |
| PostgreSQL driver | MVP | |
| SQLite driver | MVP | |
| Results grid (scrollable) | MVP | |
| Results NULL display | MVP | |
| Results client-side sort | MVP | |
| Results client-side filter | MVP | |
| Export: CSV (clipboard + file) | MVP | |
| Export: TSV | MVP | |
| Export: JSON array | MVP | |
| Export: SQL INSERT per-row | MVP | |
| Export: SQL INSERT multi-row | MVP | |
| Export: Markdown table | MVP | |
| Export: Pretty (box-drawing) | MVP | |
| Export: JSON Lines | MVP | |
| Export: cell value | MVP | |
| Export: open in editor tab | MVP | |
| Export: custom separator | V2 | |
| Export: OSC 52 clipboard (remote) | Stretch | |
| Multiple result sets (MSSQL) | MVP | |
| Row count + timing display | MVP | |
| Schema browser (tree) | MVP | |
| Schema browser lazy load | MVP | |
| Schema browser fuzzy filter | MVP | |
| Schema browser action menu | MVP | |
| Connection profiles (Lua) | MVP | |
| Connection switcher palette | MVP | |
| Multiple simultaneous connections | MVP | |
| Per-connection tab coloring | MVP | |
| Windows Auth (MSSQL) | MVP | |
| Query history (store + search) | MVP | |
| Transaction mode toggle | MVP | |
| Commit / Rollback | MVP | |
| Command palette | MVP | |
| Comment toggle | MVP | |
| Query formatter (basic) | MVP | |
| EXPLAIN output | MVP | |
| Pane resizing | MVP | |
| Built-in themes (dark/light/HC) | MVP | |
| Lua config | MVP | |
| Keybinding overrides (Lua) | MVP | |
| SSH tunnel | V2 | |
| Azure AD auth (MSSQL) | V2 | |
| SSL/TLS options | V2 | |
| Results edit mode (DML gen) | V2 | |
| Results streaming / pagination | V2 | |
| Column resize (results) | V2 | |
| Session restore (tab buffers) | V2 | |
| Startup queries per connection | V2 | |
| Per-connection default database | V2 | |
| Custom theme (Lua) | V2 | |
| Query formatter (advanced) | V2 | |
| History: re-run from palette | V2 | |
| History: duration/row count stored | MVP | |
| EXPLAIN ANALYZE / SET STATISTICS | V2 | |
| Go to tab by number | V2 | |
| Block comment toggle | V2 | |
| Vim named registers | V2 | |
| Vim fine-grained undo | V2 | |
| Other database/sql drivers | Stretch | Community contributed |
| Mouse support | Stretch | bubbletea has mouse events |
| OSC 52 clipboard (remote sessions) | Stretch | |
| Notification/LISTEN support (PG) | Stretch | |

---

## 19. Open Questions

| ID | Question | Options | Decision Needed By |
|---|---|---|---|
| ~~OQ-1~~ | ~~PostgreSQL driver?~~ | **Resolved**: `pgx/v5` via `pgx/v5/stdlib`. Actively maintained, superior type support, works on Windows without CGo. `lib/pq` is maintenance-only. | ✅ |
| ~~OQ-2~~ | ~~SQLite CGo vs pure Go?~~ | **Resolved**: `modernc.org/sqlite`. Pure Go — no C compiler needed on Windows. Used for both user SQLite DBs and internal app storage (history, sessions). | ✅ |
| ~~OQ-3~~ | ~~Persist last-open tab buffers across sessions?~~ | **Resolved**: Always on. Tabs are real files in `workspace/<conn>/`. Session restore reads `session.json`. See §11a. | ✅ |
| ~~OQ-4~~ | ~~Password storage?~~ | **Resolved**: OS keychain via `go-keyring` (Windows Credential Manager / SecretService). Passwords never written to disk in plaintext. Headless fallback: AES-256-GCM encrypted file. See §11.5. | ✅ |
| ~~OQ-5~~ | ~~SQL formatter library?~~ | **Resolved**: Hand-rolled token-based formatter in `internal/format/sql.go`. No external deps, no subprocess, works on Windows/Linux. Idempotent, degrades gracefully on syntax errors. See §7.5a. | ✅ |
| ~~OQ-6~~ | ~~Vim search regex flavor?~~ | **Resolved**: RE2 (Go `regexp`). Status bar shows `RE2` label during search. See §8.4. | ✅ |
| ~~OQ-7~~ | ~~Allow `require()` in Lua config?~~ | **Resolved**: Allowed. Custom loader restricts search to the config dir only. `io`/`os`/`package`/`debug` stripped from sandbox. Path traversal rejected. See §14.2. | ✅ |
| ~~OQ-8~~ | ~~App name?~~ | **Resolved**: Binary is named `sql`. Note: conflicts with no standard system binary. Users should ensure it precedes any `sql` aliases on their PATH. | ✅ |
| ~~OQ-9~~ | ~~Windows data path?~~ | **Resolved**: Config → `%APPDATA%\sql\` (roaming, for `config.lua`). Data → `%LOCALAPPDATA%\sql\` (machine-local, for history/workspace/connections). Matches Windows best practice; data files should not sync across machines. | ✅ |
| ~~OQ-10~~ | ~~Maximum undo history depth?~~ | **Resolved**: Default 100 steps per buffer, configurable via `editor.undo_limit` in Lua config. | ✅ |
