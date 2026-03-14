//go:build !windows

package mcp

import (
	"fmt"
	"net"
	"os"
)

// listen creates a Unix socket listener, falling back to TCP on the given port
// (then a random port) if the socket cannot be created.
func listen(port int) (string, net.Listener, error) {
	path := fmt.Sprintf("/tmp/sqltui-%d.sock", os.Getpid())
	_ = os.Remove(path) // clean up stale socket
	l, err := net.Listen("unix", path)
	if err != nil {
		// Fallback to TCP.
		addr := fmt.Sprintf("127.0.0.1:%d", port)
		l, err = net.Listen("tcp", addr)
		if err != nil {
			l, err = net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				return "", nil, err
			}
		}
		return l.Addr().String(), l, nil
	}
	return path, l, nil
}
