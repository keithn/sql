package connections

import (
	"net/url"
	"strings"
)

// DetectDriver examines a connection string and returns the driver name.
// Returns "" if the driver cannot be determined.
func DetectDriver(s string) string {
	s = strings.TrimSpace(s)

	// URL-style
	if u, err := url.Parse(s); err == nil {
		switch u.Scheme {
		case "sqlserver", "mssql":
			return "mssql"
		case "postgres", "postgresql":
			return "postgres"
		case "file", "sqlite", "sqlite3":
			return "sqlite"
		}
	}

	lower := strings.ToLower(s)

	// ADO.NET-style MSSQL keywords
	if containsAny(lower, "server=", "data source=", "initial catalog=", "trusted_connection=") {
		return "mssql"
	}

	// PostgreSQL DSN keywords
	if containsAny(lower, "host=", "dbname=", "sslmode=") {
		return "postgres"
	}

	// SQLite: file path ending in .db / .sqlite / .sqlite3
	if hasSuffix(lower, ".db", ".sqlite", ".sqlite3") {
		return "sqlite"
	}

	return ""
}

// ParseConnString parses a connection string into a driver name and a params map.
func ParseConnString(s string) (driver string, params map[string]string, err error) {
	driver = DetectDriver(s)
	params = map[string]string{"raw": s}
	// TODO: expand into individual fields (host, port, database, username, etc.)
	// so the add-connection modal can display and edit them.
	return driver, params, nil
}

// RedactDSN returns the connection string with any password value replaced by
// "***", safe to show in a UI. Handles both URL-style and key=value formats.
func RedactDSN(s string) string {
	s = strings.TrimSpace(s)

	// URL-style: scheme://user:password@host/...
	if u, err := url.Parse(s); err == nil && u.Scheme != "" && u.Host != "" {
		if _, hasPassword := u.User.Password(); hasPassword {
			u.User = url.UserPassword(u.User.Username(), "***")
		}
		return u.String()
	}

	// Key=value style: redact pwd=, password=, user password= (case-insensitive).
	return redactKV(s, "pwd", "password")
}

// DisplayName derives a short human-readable label from a connection string,
// suitable for the statusbar and as a stable workspace directory key.
// Format: user@host/database  (any unknown parts are omitted).
func DisplayName(s string) string {
	s = strings.TrimSpace(s)

	// URL-style.
	if u, err := url.Parse(s); err == nil && u.Scheme != "" && u.Host != "" {
		user := u.User.Username()
		db := strings.TrimPrefix(u.Path, "/")
		return buildLabel(user, u.Host, db)
	}

	// Key=value style.
	kv := parseKV(s)
	host := first(kv, "server", "data source", "host")
	db := first(kv, "database", "initial catalog", "dbname")
	user := first(kv, "user id", "uid", "user", "username")
	return buildLabel(user, host, db)
}

func buildLabel(user, host, db string) string {
	if host == "" && db == "" {
		return "connected"
	}
	label := host
	if db != "" {
		label += "/" + db
	}
	if user != "" {
		label = user + "@" + label
	}
	return label
}

// redactKV replaces the values of the named keys (case-insensitive) in a
// semicolon-delimited key=value string with "***".
func redactKV(s string, keys ...string) string {
	parts := strings.Split(s, ";")
	for i, part := range parts {
		eq := strings.IndexByte(part, '=')
		if eq < 0 {
			continue
		}
		k := strings.ToLower(strings.TrimSpace(part[:eq]))
		for _, target := range keys {
			if k == target {
				parts[i] = part[:eq+1] + "***"
				break
			}
		}
	}
	return strings.Join(parts, ";")
}

// parseKV parses a semicolon-delimited key=value string into a lowercase map.
func parseKV(s string) map[string]string {
	m := map[string]string{}
	for _, part := range strings.Split(s, ";") {
		eq := strings.IndexByte(part, '=')
		if eq < 0 {
			continue
		}
		k := strings.ToLower(strings.TrimSpace(part[:eq]))
		v := strings.TrimSpace(part[eq+1:])
		m[k] = v
	}
	return m
}

// first returns the value for the first key found in m.
func first(m map[string]string, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			return v
		}
	}
	return ""
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func hasSuffix(s string, suffixes ...string) bool {
	for _, suf := range suffixes {
		if strings.HasSuffix(s, suf) {
			return true
		}
	}
	return false
}
