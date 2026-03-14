package main

import (
	"os"
	"syscall"
)

// isTerminal reports whether f is a real Windows console handle.
// GetConsoleMode fails on pipes and files, so this correctly returns false
// when the process is launched as a subprocess by an MCP client.
func isTerminal(f *os.File) bool {
	var mode uint32
	return syscall.GetConsoleMode(syscall.Handle(f.Fd()), &mode) == nil
}
