package connections

import (
	"fmt"
	"net/url"
	"path/filepath"
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
	s = strings.TrimSpace(s)
	driver = DetectDriver(s)
	if driver == "" {
		return "", nil, fmt.Errorf("cannot detect database driver from connection string")
	}
	params = map[string]string{"raw": s}
	if driver == "sqlite" {
		params["file"] = s
	}
	if password := extractPassword(s); password != "" {
		params["password"] = password
	}
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

// StripPassword removes any password component from a connection string so it
// can be stored safely on disk. Handles both URL-style and key=value formats.
func StripPassword(s string) string {
	s = strings.TrimSpace(s)

	if u, err := url.Parse(s); err == nil && u.Scheme != "" && (u.Host != "" || u.User != nil) {
		if u.User != nil {
			u.User = url.User(u.User.Username())
		}
		return u.String()
	}

	return removeKV(s, "pwd", "password")
}

// InjectPassword adds or replaces the password within a connection string.
// Handles URL-style and key=value formats.
func InjectPassword(s, password string) string {
	s = strings.TrimSpace(s)
	if password == "" {
		return s
	}

	if u, err := url.Parse(s); err == nil && u.Scheme != "" && (u.Host != "" || u.User != nil) {
		if u.User != nil {
			u.User = url.UserPassword(u.User.Username(), password)
		}
		return u.String()
	}

	key := "password"
	if DetectDriver(s) == "mssql" && strings.Contains(s, ";") {
		key = "Password"
	}
	return upsertKV(s, key, password)
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
	if label := buildLabel(user, host, db); label != "connected" {
		return label
	}

	if DetectDriver(s) == "sqlite" {
		if strings.HasPrefix(strings.ToLower(s), "file:") {
			trimmed := strings.TrimPrefix(s, "file:")
			trimmed = strings.TrimPrefix(trimmed, "//")
			if trimmed != "" {
				return filepath.Base(trimmed)
			}
		}
		return filepath.Base(s)
	}

	return "connected"
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
	if strings.Contains(s, ";") {
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

	for _, part := range strings.Fields(s) {
		eq := strings.IndexByte(part, '=')
		if eq < 0 {
			continue
		}
		k := strings.ToLower(strings.TrimSpace(part[:eq]))
		v := strings.TrimSpace(part[eq+1:])
		m[k] = strings.Trim(v, `"'`)
	}
	return m
}

func removeKV(s string, keys ...string) string {
	if strings.Contains(s, ";") {
		parts := strings.Split(s, ";")
		kept := make([]string, 0, len(parts))
		for _, part := range parts {
			eq := strings.IndexByte(part, '=')
			if eq < 0 {
				if strings.TrimSpace(part) != "" {
					kept = append(kept, part)
				}
				continue
			}
			name := strings.ToLower(strings.TrimSpace(part[:eq]))
			if containsKey(name, keys...) {
				continue
			}
			kept = append(kept, part)
		}
		out := strings.Join(kept, ";")
		if strings.HasSuffix(s, ";") && out != "" {
			out += ";"
		}
		return out
	}

	parts := strings.Fields(s)
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		eq := strings.IndexByte(part, '=')
		if eq < 0 {
			kept = append(kept, part)
			continue
		}
		name := strings.ToLower(strings.TrimSpace(part[:eq]))
		if containsKey(name, keys...) {
			continue
		}
		kept = append(kept, part)
	}
	return strings.Join(kept, " ")
}

func upsertKV(s, key, value string) string {
	if strings.Contains(s, ";") {
		parts := strings.Split(s, ";")
		updated := make([]string, 0, len(parts)+1)
		replaced := false
		for _, part := range parts {
			if strings.TrimSpace(part) == "" {
				continue
			}
			eq := strings.IndexByte(part, '=')
			if eq < 0 {
				updated = append(updated, part)
				continue
			}
			name := strings.ToLower(strings.TrimSpace(part[:eq]))
			if containsKey(name, key, "pwd", "password") {
				updated = append(updated, key+"="+value)
				replaced = true
				continue
			}
			updated = append(updated, part)
		}
		if !replaced {
			updated = append(updated, key+"="+value)
		}
		return strings.Join(updated, ";") + ";"
	}

	parts := strings.Fields(s)
	updated := make([]string, 0, len(parts)+1)
	replaced := false
	for _, part := range parts {
		eq := strings.IndexByte(part, '=')
		if eq < 0 {
			updated = append(updated, part)
			continue
		}
		name := strings.ToLower(strings.TrimSpace(part[:eq]))
		if containsKey(name, key, "pwd", "password") {
			updated = append(updated, key+"="+value)
			replaced = true
			continue
		}
		updated = append(updated, part)
	}
	if !replaced {
		updated = append(updated, key+"="+value)
	}
	return strings.Join(updated, " ")
}

func containsKey(name string, keys ...string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, key := range keys {
		if name == strings.ToLower(strings.TrimSpace(key)) {
			return true
		}
	}
	return false
}

func extractPassword(s string) string {
	s = strings.TrimSpace(s)
	if u, err := url.Parse(s); err == nil && u.Scheme != "" {
		if pw, ok := u.User.Password(); ok {
			return pw
		}
	}
	kv := parseKV(s)
	return first(kv, "password", "pwd")
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
