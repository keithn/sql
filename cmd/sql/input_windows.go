package main

import (
	"io"
	"os"
)

// openConsoleInput opens CONIN$ as a separate file handle.
//
// When passed to tea.WithInput this causes bubbletea's newInputReader to take
// the fallback path (raw ReadFile via ANSI reader) instead of the Win32
// console-event reader (ReadConsoleInput). In that path, bubbletea enables
// ENABLE_VIRTUAL_TERMINAL_INPUT, so VT sequences — including the bracketed
// paste markers \x1b[200~ … \x1b[201~ — are delivered as raw bytes, which
// lets readAnsiInputs call detectBracketedPaste and set msg.Paste = true.
//
// If CONIN$ cannot be opened (e.g. no console attached) we fall back to
// os.Stdin so the app still starts normally.
func openConsoleInput() io.Reader {
	f, err := os.OpenFile("CONIN$", os.O_RDWR, 0o644)
	if err != nil {
		return os.Stdin
	}
	return f
}
