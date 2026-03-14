package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/sqltui/sql/internal/config"
	"github.com/sqltui/sql/internal/connections"
	"github.com/sqltui/sql/internal/db"

	_ "github.com/sqltui/sql/internal/db/mssql"
	_ "github.com/sqltui/sql/internal/db/postgres"
	_ "github.com/sqltui/sql/internal/db/sqlite"
)

// headless holds the state for a stdio MCP session with no TUI.
type headless struct {
	session *db.Session
	schema  *db.Schema
	buffer  string
}

// ServeStdio connects to nameOrDSN (if non-empty) and serves MCP JSON-RPC 2.0
// over os.Stdin / os.Stdout.  This is the headless mode used when sqltui is
// launched by an MCP client (e.g. Claude Code) via the command transport.
func ServeStdio(cfg *config.Config, nameOrDSN string) error {
	h := &headless{}
	if nameOrDSN != "" {
		target, err := connections.Resolve(cfg, nameOrDSN)
		if err != nil {
			return fmt.Errorf("connect: %w", err)
		}
		sess, _, err := db.DetectAndConnect(context.Background(), target.DSN)
		if err != nil {
			return fmt.Errorf("connect: %w", err)
		}
		h.session = sess
		if schema, err := sess.Driver.Introspect(context.Background(), sess.DB); err == nil {
			h.schema = schema
		}
	}
	return h.serve(os.Stdin, os.Stdout)
}

func (h *headless) serve(r io.Reader, w io.Writer) error {
	enc := json.NewEncoder(w)
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	send := func(resp rpcResponse) {
		_ = enc.Encode(resp)
	}

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			send(rpcResponse{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: "parse error"}})
			continue
		}

		switch req.Method {
		case "initialize":
			send(rpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result: map[string]any{
					"protocolVersion": "2024-11-05",
					"capabilities":    map[string]any{"tools": map[string]any{}},
					"serverInfo":      map[string]any{"name": "sqltui", "version": "1.0"},
				},
			})

		case "notifications/initialized":
			// No response needed.

		case "tools/list":
			send(rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{"tools": headlessToolsList()}})

		case "tools/call":
			name, _ := req.Params["name"].(string)
			args, _ := req.Params["arguments"].(map[string]any)
			if args == nil {
				args = map[string]any{}
			}
			result, errMsg := h.callTool(name, args)
			if errMsg != "" {
				send(rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32603, Message: errMsg}})
			} else {
				send(rpcResponse{
					JSONRPC: "2.0",
					ID:      req.ID,
					Result: map[string]any{
						"content": []map[string]any{
							{"type": "text", "text": fmt.Sprintf("%v", result)},
						},
					},
				})
			}

		default:
			if req.ID != nil {
				send(rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32601, Message: "method not found: " + req.Method}})
			}
		}
	}
	return scanner.Err()
}

func (h *headless) callTool(name string, args map[string]any) (any, string) {
	switch name {
	case "read_editor":
		return h.buffer, ""

	case "write_editor":
		sql, _ := args["sql"].(string)
		mode, _ := args["mode"].(string)
		if mode == "" {
			mode = "replace"
		}
		switch mode {
		case "replace", "new_tab":
			h.buffer = sql
		case "append", "insert":
			if h.buffer != "" && !strings.HasSuffix(h.buffer, "\n") {
				h.buffer += "\n"
			}
			h.buffer += sql
		}
		return "ok", ""

	case "list_tabs":
		b, _ := json.Marshal([]string{"query.sql"})
		return string(b), ""

	case "switch_tab":
		return "ok", ""

	case "get_schema":
		if h.schema == nil {
			return "[]", ""
		}
		search, _ := args["search"].(string)
		return schemaJSON(h.schema, search), ""

	case "execute_query":
		sql, _ := args["sql"].(string)
		if strings.TrimSpace(sql) == "" {
			return nil, "sql parameter is required"
		}
		if h.session == nil {
			return nil, "not connected — pass a connection name or DSN as the first argument to sql --mcp"
		}
		results, err := h.session.Execute(context.Background(), sql)
		if err != nil {
			return nil, err.Error()
		}
		if len(results) == 0 {
			return "[]", ""
		}
		return headlessFormatResults(results[0]), ""

	case "get_results":
		return "[]", "" // no persistent results in headless mode

	default:
		return nil, "unknown tool: " + name
	}
}

// schemaJSON builds a compact JSON summary of a db.Schema.
// If search is non-empty, only tables whose name contains search (case-insensitive) are included.
// The result is capped at ~40 KB to avoid exceeding MCP response size limits.
func schemaJSON(s *db.Schema, search string) string {
	search = strings.ToLower(search)
	const maxBytes = 40 * 1024
	var sb strings.Builder
	sb.WriteByte('[')
	first := true
	for _, database := range s.Databases {
		for _, schema := range database.Schemas {
			for _, t := range append(schema.Tables, schema.Views...) {
				if search != "" && !strings.Contains(strings.ToLower(t.Name), search) {
					continue
				}
				if sb.Len() > maxBytes {
					break
				}
				if !first {
					sb.WriteByte(',')
				}
				first = false
				sb.WriteString(fmt.Sprintf(`{"schema":%q,"name":%q,"columns":[`, schema.Name, t.Name))
				for j, c := range t.Columns {
					if j > 0 {
						sb.WriteByte(',')
					}
					sb.WriteString(fmt.Sprintf(`{"name":%q,"type":%q}`, c.Name, c.Type))
				}
				sb.WriteString("]}")
			}
		}
	}
	sb.WriteByte(']')
	return sb.String()
}

const maxRows = 50
const maxStrLen = 120

// headlessFormatResults serialises a QueryResult to JSON, capping rows and
// truncating long string values to keep MCP response sizes manageable.
func headlessFormatResults(rs db.QueryResult) string {
	cols := make([]string, len(rs.Columns))
	for i, c := range rs.Columns {
		cols[i] = c.Name
	}
	rows := rs.Rows
	truncated := len(rows) > maxRows
	if truncated {
		rows = rows[:maxRows]
	}
	type rowObj = map[string]any
	out := make([]rowObj, 0, len(rows))
	for _, row := range rows {
		obj := make(rowObj, len(cols))
		for j, val := range row {
			if j >= len(cols) {
				break
			}
			if s, ok := val.(string); ok && len(s) > maxStrLen {
				val = s[:maxStrLen] + "…"
			}
			obj[cols[j]] = val
		}
		out = append(out, obj)
	}
	result := map[string]any{"rows": out}
	if truncated {
		result["truncated"] = true
		result["note"] = fmt.Sprintf("showing first %d of %d rows", maxRows, len(rs.Rows))
	}
	b, _ := json.Marshal(result)
	return string(b)
}
