package connections

import "testing"

func TestDetectDriver(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		// --- MSSQL URL styles ---
		{
			name:  "sqlserver scheme",
			input: "sqlserver://sa:pass@localhost/AdventureWorks",
			want:  "mssql",
		},
		{
			name:  "mssql scheme",
			input: "mssql://sa:pass@localhost:1433/mydb",
			want:  "mssql",
		},
		// --- MSSQL ADO.NET styles ---
		{
			name:  "ADO.NET Server=",
			input: "Server=myhost;Database=mydb;User Id=sa;Password=pass;",
			want:  "mssql",
		},
		{
			name:  "ADO.NET Data Source=",
			input: "Data Source=myhost;Initial Catalog=mydb;Integrated Security=True;",
			want:  "mssql",
		},
		{
			name:  "ADO.NET Trusted_Connection",
			input: "Server=myhost;Trusted_Connection=yes;Database=mydb;",
			want:  "mssql",
		},
		{
			name:  "ADO.NET case-insensitive",
			input: "SERVER=myhost;DATABASE=mydb;USER ID=sa;PASSWORD=pass;",
			want:  "mssql",
		},
		{
			name:  "ADO.NET Initial Catalog",
			input: "Data Source=myhost;Initial Catalog=mydb;User Id=sa;Password=pass;",
			want:  "mssql",
		},
		// --- PostgreSQL URL styles ---
		{
			name:  "postgres scheme",
			input: "postgres://admin:pass@db.example.com/myapp",
			want:  "postgres",
		},
		{
			name:  "postgresql scheme",
			input: "postgresql://admin:pass@db.example.com:5432/myapp",
			want:  "postgres",
		},
		{
			name:  "postgres with sslmode param",
			input: "postgres://admin@db.example.com/myapp?sslmode=verify-full",
			want:  "postgres",
		},
		// --- PostgreSQL DSN styles ---
		{
			name:  "postgres DSN host=",
			input: "host=db.example.com dbname=myapp user=admin password=pass",
			want:  "postgres",
		},
		{
			name:  "postgres DSN with sslmode=",
			input: "host=localhost dbname=mydb sslmode=disable",
			want:  "postgres",
		},
		{
			name:  "postgres DSN dbname only",
			input: "dbname=myapp user=admin",
			want:  "postgres",
		},
		// --- SQLite URL styles ---
		{
			name:  "file scheme",
			input: "file:path/to/my.db",
			want:  "sqlite",
		},
		{
			name:  "file scheme with options",
			input: "file:mydb.sqlite?mode=ro",
			want:  "sqlite",
		},
		{
			name:  "file in-memory",
			input: "file::memory:",
			want:  "sqlite",
		},
		// --- SQLite file path styles ---
		{
			name:  ".db extension",
			input: "/home/user/projects/myapp/dev.db",
			want:  "sqlite",
		},
		{
			name:  ".sqlite extension",
			input: "~/projects/myapp/data.sqlite",
			want:  "sqlite",
		},
		{
			name:  ".sqlite3 extension",
			input: "~/data.sqlite3",
			want:  "sqlite",
		},
		{
			name:  "relative .db path",
			input: "./local.db",
			want:  "sqlite",
		},
		{
			name:  "bare filename .db",
			input: "mydb.db",
			want:  "sqlite",
		},
		{
			name:  "Windows absolute path .db",
			input: `C:\Users\keith\dev\myapp.db`,
			want:  "sqlite",
		},
		// --- Whitespace handling ---
		{
			name:  "leading/trailing whitespace",
			input: "  postgres://localhost/mydb  ",
			want:  "postgres",
		},
		// --- Unknown ---
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "unrecognised string",
			input: "not a connection string",
			want:  "",
		},
		{
			name:  "http URL is not a DB",
			input: "http://example.com/api",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectDriver(tt.input)
			if got != tt.want {
				t.Errorf("DetectDriver(%q)\n  got  %q\n  want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestStripPassword(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "url style",
			input: "postgres://app:secret@localhost/mydb",
			want:  "postgres://app@localhost/mydb",
		},
		{
			name:  "postgres dsn style",
			input: "host=localhost dbname=mydb user=app password=secret sslmode=disable",
			want:  "host=localhost dbname=mydb user=app sslmode=disable",
		},
		{
			name:  "mssql adonet style",
			input: "Server=myhost;Database=mydb;User Id=sa;Password=secret;",
			want:  "Server=myhost;Database=mydb;User Id=sa;",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := StripPassword(tt.input); got != tt.want {
				t.Fatalf("StripPassword(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestInjectPassword(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		password string
		want     string
	}{
		{
			name:     "url style",
			input:    "postgres://app@localhost/mydb",
			password: "secret",
			want:     "postgres://app:secret@localhost/mydb",
		},
		{
			name:     "postgres dsn style",
			input:    "host=localhost dbname=mydb user=app sslmode=disable",
			password: "secret",
			want:     "host=localhost dbname=mydb user=app sslmode=disable password=secret",
		},
		{
			name:     "mssql adonet style",
			input:    "Server=myhost;Database=mydb;User Id=sa;",
			password: "secret",
			want:     "Server=myhost;Database=mydb;User Id=sa;Password=secret;",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := InjectPassword(tt.input, tt.password); got != tt.want {
				t.Fatalf("InjectPassword(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
