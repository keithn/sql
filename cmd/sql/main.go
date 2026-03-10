package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sqltui/sql/internal/app"
	"github.com/sqltui/sql/internal/config"
	_ "github.com/sqltui/sql/internal/db/mssql"
	_ "github.com/sqltui/sql/internal/db/postgres"
	_ "github.com/sqltui/sql/internal/db/sqlite"
)

// Set by goreleaser via -ldflags.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	args := os.Args[1:]

	// --version
	if len(args) == 1 && (args[0] == "--version" || args[0] == "-v") {
		fmt.Printf("sql %s (%s) built %s\n", version, commit, date)
		return
	}

	// --list: print saved connection names and exit
	if len(args) == 1 && args[0] == "--list" {
		listConnections()
		return
	}

	// --add <conn-string> --name <name>
	if len(args) >= 3 && args[0] == "--add" {
		addConnection(args)
		return
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	// Optional positional arg: connection name or raw connection string
	var connectTo string
	if len(args) == 1 {
		connectTo = args[0]
	}

	m := app.New(cfg, connectTo)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func listConnections() {
	// TODO: load connections store and print names
	fmt.Println("(no connections saved)")
}

func addConnection(args []string) {
	// args: --add <conn-string> --name <name>
	// TODO: parse, detect driver, save to connections store
	fmt.Println("TODO: add connection")
}
