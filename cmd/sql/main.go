package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sqltui/sql/internal/app"
	"github.com/sqltui/sql/internal/config"
	"github.com/sqltui/sql/internal/connections"
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

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	// --list: print saved connection names and exit
	if len(args) == 1 && args[0] == "--list" {
		if err := listConnections(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// --add <conn-string> --name <name>
	if len(args) >= 3 && args[0] == "--add" {
		if err := addConnection(args); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Optional positional arg: connection name or raw connection string
	var connectTo string
	if len(args) == 1 {
		connectTo = args[0]
	}

	m := app.New(cfg, connectTo)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion(), tea.WithInput(openConsoleInput()))
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func listConnections(cfg *config.Config) error {
	names, err := connections.Names(cfg)
	if err != nil {
		return err
	}
	if len(names) == 0 {
		fmt.Println("(no connections saved)")
		return nil
	}
	for _, name := range names {
		fmt.Println(name)
	}
	return nil
}

func addConnection(args []string) error {
	connString, name, err := parseAddArgs(args)
	if err != nil {
		return err
	}
	driver, err := connections.SaveManaged(name, connString)
	if err != nil {
		return err
	}
	fmt.Printf("saved connection %q (%s)\n", name, driver)
	return nil
}

func parseAddArgs(args []string) (string, string, error) {
	if len(args) < 4 || args[0] != "--add" {
		return "", "", fmt.Errorf("usage: sql --add <conn-string> --name <name>")
	}
	connString := args[1]
	var name string
	for i := 2; i < len(args); i++ {
		switch args[i] {
		case "--name":
			if i+1 >= len(args) {
				return "", "", fmt.Errorf("missing value for --name")
			}
			name = args[i+1]
			i++
		default:
			return "", "", fmt.Errorf("unknown argument %q", args[i])
		}
	}
	if name == "" {
		return "", "", fmt.Errorf("usage: sql --add <conn-string> --name <name>")
	}
	return connString, name, nil
}
