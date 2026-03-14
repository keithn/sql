package mcp

import (
	"fmt"
	"net"
)

// listen creates a TCP listener on localhost, preferring the given port and
// falling back to a random port if it is already in use.
func listen(port int) (string, net.Listener, error) {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	l, err := net.Listen("tcp", addr)
	if err != nil {
		// Preferred port busy — fall back to random.
		l, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return "", nil, err
		}
	}
	return l.Addr().String(), l, nil
}
