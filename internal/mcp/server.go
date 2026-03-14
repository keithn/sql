// Package mcp implements a Model Context Protocol JSON-RPC server that allows
// Claude Code to drive the SQL TUI as an agent.  The server listens on a
// named pipe (Windows) or a Unix socket (Linux/macOS) and accepts connections
// from MCP clients.  The pipe/socket path is printed to stderr on startup.
//
// Protocol: JSON-RPC 2.0 over newline-delimited frames.
// Supported methods:
//   initialize, notifications/initialized, tools/list, tools/call
//
// Available tools:
//   read_editor         — return current editor SQL content
//   write_editor        — replace/append/insert editor content
//   get_schema          — return introspected schema as JSON
//   execute_query       — execute SQL and return results
//   list_tabs           — return current tab names
//   get_results         — return current results grid as CSV-like text
//   switch_tab          — switch to a named tab
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"
)

// RequestMsg is injected into the bubbletea program when an MCP tool is called.
// The handler in app.Update reads model state and sends the reply.
type RequestMsg struct {
	Method  string
	Params  map[string]any
	ReplyCh chan<- Reply
}

// Reply carries the result (or error) back to the MCP goroutine.
type Reply struct {
	Result any
	Err    string
}

// Server is the MCP JSON-RPC server.
type Server struct {
	listener net.Listener
	prog     *tea.Program
	addr     string
	mu       sync.Mutex
	closed   bool
	reqID    atomic.Int64
}

// DefaultPort is the default TCP port for the TUI socket server.
const DefaultPort = 45678

// New creates a new MCP server listening on the platform-appropriate socket.
func New(prog *tea.Program) (*Server, error) {
	addr, l, err := listen(DefaultPort)
	if err != nil {
		return nil, fmt.Errorf("mcp listen: %w", err)
	}
	return &Server{listener: l, prog: prog, addr: addr}, nil
}

// Listen opens the listener on port and returns the address and a server
// without a program attached yet.  Call SetProgram before Serve.
func Listen(port int) (string, *Server, error) {
	addr, l, err := listen(port)
	if err != nil {
		return "", nil, fmt.Errorf("mcp listen: %w", err)
	}
	return addr, &Server{listener: l, addr: addr}, nil
}

// SetProgram attaches the bubbletea program to the server.  Must be called
// before Serve when the server was created via Listen.
func (s *Server) SetProgram(prog *tea.Program) { s.prog = prog }

// Addr returns the pipe/socket path that clients should connect to.
func (s *Server) Addr() string { return s.addr }

// Serve accepts connections and handles JSON-RPC requests.  It blocks until
// the listener is closed.
func (s *Server) Serve(ctx context.Context) {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			s.mu.Lock()
			closed := s.closed
			s.mu.Unlock()
			if closed {
				return
			}
			continue
		}
		go s.handleConn(ctx, conn)
	}
}

// Close stops the server.
func (s *Server) Close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	_ = s.listener.Close()
}

// --- JSON-RPC types ---

type rpcRequest struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      any            `json:"id,omitempty"`
	Method  string         `json:"method"`
	Params  map[string]any `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id,omitempty"`
	Result  any    `json:"result,omitempty"`
	Error   *rpcError `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// handleConn reads JSON-RPC requests from conn and dispatches them.
func (s *Server) handleConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	scanner := bufio.NewScanner(conn)
	enc := json.NewEncoder(conn)

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
			// No response needed for notifications.

		case "tools/list":
			send(rpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  map[string]any{"tools": toolsList()},
			})

		case "tools/call":
			name, _ := req.Params["name"].(string)
			args, _ := req.Params["arguments"].(map[string]any)
			if args == nil {
				args = map[string]any{}
			}
			result, err := s.callTool(ctx, name, args)
			if err != "" {
				send(rpcResponse{
					JSONRPC: "2.0",
					ID:      req.ID,
					Error:   &rpcError{Code: -32603, Message: err},
				})
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
				send(rpcResponse{
					JSONRPC: "2.0",
					ID:      req.ID,
					Error:   &rpcError{Code: -32601, Message: "method not found: " + req.Method},
				})
			}
		}
	}
}

// callTool dispatches a tool call to the TUI via tea.Program.Send and waits for the reply.
func (s *Server) callTool(ctx context.Context, name string, args map[string]any) (any, string) {
	replyCh := make(chan Reply, 1)
	params := args
	if params == nil {
		params = map[string]any{}
	}
	params["_tool"] = name

	s.prog.Send(RequestMsg{
		Method:  name,
		Params:  params,
		ReplyCh: replyCh,
	})

	select {
	case reply := <-replyCh:
		if reply.Err != "" {
			return nil, reply.Err
		}
		return reply.Result, ""
	case <-ctx.Done():
		return nil, "request cancelled"
	}
}

// headlessToolsList returns the tools available in headless/stdio mode.
// Editor tools are omitted since there is no TUI to write to.
func headlessToolsList() []map[string]any {
	str := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
	tool := func(name, desc string, props map[string]any, required []string) map[string]any {
		schema := map[string]any{"type": "object", "properties": props}
		if len(required) > 0 {
			schema["required"] = required
		}
		return map[string]any{"name": name, "description": desc, "inputSchema": schema}
	}
	return []map[string]any{
		tool("execute_query", "Execute SQL and return results as JSON",
			map[string]any{"sql": str("SQL to execute")}, []string{"sql"}),
		tool("get_schema", "Return the introspected schema as JSON. Use 'search' to filter by table name to avoid large responses.",
			map[string]any{"search": str("Optional substring to filter table names (case-insensitive)")}, nil),
		tool("get_results", "Return the last query results as JSON", map[string]any{}, nil),
	}
}

// toolsList returns the MCP tools manifest for the live TUI server.
func toolsList() []map[string]any {
	tool := func(name, desc string, props map[string]any, required []string) map[string]any {
		schema := map[string]any{
			"type":       "object",
			"properties": props,
		}
		if len(required) > 0 {
			schema["required"] = required
		}
		return map[string]any{
			"name":        name,
			"description": desc,
			"inputSchema": schema,
		}
	}
	str := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }

	return []map[string]any{
		tool("get_schema", "ALWAYS call this first before writing any query. Returns tables and their columns. Use the 'search' parameter to filter by table name (e.g. search='user' returns tables whose name contains 'user'). Call with no search to get all tables (may be large — prefer filtering).",
			map[string]any{"search": str("Optional substring to filter table names (case-insensitive)")}, nil),
		tool("write_editor", "Write SQL into a new tab in the user's editor. ALWAYS call get_schema first to verify table/column names. Use mode new_tab (default) to preserve existing tabs.",
			map[string]any{
				"sql":  str("SQL text to write — use specific column names from get_schema, never SELECT *"),
				"mode": str("new_tab (default) | replace | append | insert"),
			}, []string{"sql"}),
		tool("execute_query", "Execute SQL and return results (max 50 rows). ALWAYS call get_schema first to verify table/column names before executing.",
			map[string]any{"sql": str("SQL to execute — use specific column names, never SELECT *")}, []string{"sql"}),
		tool("read_editor", "Return the current SQL editor content", map[string]any{}, nil),
		tool("list_tabs", "Return current tab names and active tab index", map[string]any{}, nil),
		tool("get_results", "Return the current results grid as JSON (max 50 rows)", map[string]any{}, nil),
		tool("switch_tab", "Switch to a named tab",
			map[string]any{"name": str("Tab name to switch to")}, []string{"name"}),
	}
}

// Stderr prints an info line about the MCP server to os.Stderr.
func (s *Server) PrintStartupInfo() {
	fmt.Fprintf(os.Stderr, "[mcp] server listening on %s\n", s.addr)
}

// ReadFrom reads JSON-RPC messages from r (stdio) for the stdio transport variant.
func ReadFrom(r io.Reader, w io.Writer, prog *tea.Program) {
	enc := json.NewEncoder(w)
	scanner := bufio.NewScanner(r)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			_ = enc.Encode(rpcResponse{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: "parse error"}})
			continue
		}

		switch req.Method {
		case "initialize":
			_ = enc.Encode(rpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result: map[string]any{
					"protocolVersion": "2024-11-05",
					"capabilities":    map[string]any{"tools": map[string]any{}},
					"serverInfo":      map[string]any{"name": "sqltui", "version": "1.0"},
				},
			})
		case "notifications/initialized":
			// No response.
		case "tools/list":
			_ = enc.Encode(rpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result:  map[string]any{"tools": toolsList()},
			})
		case "tools/call":
			name, _ := req.Params["name"].(string)
			args, _ := req.Params["arguments"].(map[string]any)
			if args == nil {
				args = map[string]any{}
			}
			args["_tool"] = name
			replyCh := make(chan Reply, 1)
			prog.Send(RequestMsg{Method: name, Params: args, ReplyCh: replyCh})
			reply := <-replyCh
			if reply.Err != "" {
				_ = enc.Encode(rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32603, Message: reply.Err}})
			} else {
				_ = enc.Encode(rpcResponse{
					JSONRPC: "2.0",
					ID:      req.ID,
					Result: map[string]any{
						"content": []map[string]any{{"type": "text", "text": fmt.Sprintf("%v", reply.Result)}},
					},
				})
			}
		default:
			if req.ID != nil {
				_ = enc.Encode(rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32601, Message: "method not found: " + req.Method}})
			}
		}
	}
}
