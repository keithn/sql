//go:build !windows

package main

import (
	"io"
	"os"
)

// openConsoleInput returns os.Stdin unchanged on non-Windows platforms.
func openConsoleInput() io.Reader {
	return os.Stdin
}
