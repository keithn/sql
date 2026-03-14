package mcp

import (
	"fmt"
	"io"
	"net"
	"os"
)

// Relay connects to a running sqltui socket server on the given port and
// copies stdio ↔ socket, acting as a bridge for MCP clients that only support
// the command/stdio transport (e.g. Claude Code).
func Relay(port int) error {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return fmt.Errorf("relay: cannot connect to sqltui on %s — is it running with --mcp? (%w)", addr, err)
	}
	defer conn.Close()

	done := make(chan struct{}, 2)

	// stdin → socket
	go func() {
		_, _ = io.Copy(conn, os.Stdin)
		done <- struct{}{}
	}()

	// socket → stdout
	go func() {
		_, _ = io.Copy(os.Stdout, conn)
		done <- struct{}{}
	}()

	<-done
	return nil
}
