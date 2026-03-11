package connections

import (
	"fmt"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/sqltui/sql/internal/config"
	"github.com/sqltui/sql/internal/db"
)

type ResolvedTarget struct {
	DisplayName  string
	WorkspaceKey string
	Driver       string
	DSN          string
	Named        bool
}

type NamedInfo struct {
	Name    string
	Driver  string
	Summary string
}

func StorePath() (string, error) {
	dataDir, err := config.DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dataDir, "connections.json"), nil
}

func LoadManagedStore() (*Store, error) {
	path, err := StorePath()
	if err != nil {
		return nil, err
	}
	return Load(path)
}

func Names(cfg *config.Config) ([]string, error) {
	infos, err := List(cfg)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(infos))
	for _, info := range infos {
		names = append(names, info.Name)
	}
	return names, nil
}

func List(cfg *config.Config) ([]NamedInfo, error) {
	named, err := mergedNamedConnections(cfg)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(named))
	for name := range named {
		names = append(names, name)
	}
	sort.Strings(names)
	infos := make([]NamedInfo, 0, len(names))
	for _, name := range names {
		conn := named[name]
		infos = append(infos, NamedInfo{Name: name, Driver: conn.driver, Summary: connectionSummary(conn.driver, conn.params)})
	}
	return infos, nil
}

func Resolve(cfg *config.Config, nameOrDSN string) (ResolvedTarget, error) {
	key := strings.TrimSpace(nameOrDSN)
	if key == "" {
		return ResolvedTarget{}, fmt.Errorf("connection target is empty")
	}

	named, err := mergedNamedConnections(cfg)
	if err != nil {
		return ResolvedTarget{}, err
	}
	if conn, ok := named[key]; ok {
		conn, err := conn.withPassword(key)
		if err != nil {
			return ResolvedTarget{}, err
		}
		dsn, err := conn.dsn()
		if err != nil {
			return ResolvedTarget{}, err
		}
		return ResolvedTarget{
			DisplayName:  key,
			WorkspaceKey: key,
			Driver:       conn.driver,
			DSN:          dsn,
			Named:        true,
		}, nil
	}

	driver := DetectDriver(key)
	if driver == "" {
		return ResolvedTarget{}, fmt.Errorf("unknown connection %q", key)
	}
	return ResolvedTarget{
		DisplayName:  DisplayName(key),
		WorkspaceKey: "_adhoc",
		Driver:       driver,
		DSN:          key,
	}, nil
}

func SanitizedParamsForStorage(params map[string]string) map[string]string {
	clean := map[string]string{}
	for k, v := range params {
		if strings.EqualFold(k, "password") || strings.EqualFold(k, "pwd") {
			continue
		}
		if strings.EqualFold(k, "raw") {
			clean[k] = StripPassword(v)
			continue
		}
		clean[k] = v
	}
	return clean
}

type namedConnection struct {
	driver string
	params map[string]string
}

func (c namedConnection) dsn() (string, error) {
	driver, err := db.Get(c.driver)
	if err != nil {
		return "", err
	}
	return driver.BuildDSN(c.params)
}

func (c namedConnection) withPassword(name string) (namedConnection, error) {
	password, ok, err := LoadPassword(name)
	if err != nil {
		return namedConnection{}, err
	}
	if !ok || password == "" {
		return c, nil
	}
	params := cloneParams(c.params)
	if raw := strings.TrimSpace(params["raw"]); raw != "" {
		params["raw"] = InjectPassword(raw, password)
	} else {
		params["password"] = password
	}
	return namedConnection{driver: c.driver, params: params}, nil
}

func mergedNamedConnections(cfg *config.Config) (map[string]namedConnection, error) {
	store, err := LoadManagedStore()
	if err != nil {
		return nil, err
	}
	merged := map[string]namedConnection{}
	for _, entry := range store.All() {
		if strings.TrimSpace(entry.Name) == "" || strings.TrimSpace(entry.Driver) == "" {
			continue
		}
		merged[entry.Name] = namedConnection{driver: entry.Driver, params: cloneParams(entry.Params)}
	}
	if cfg != nil {
		for _, profile := range cfg.Connections {
			name := strings.TrimSpace(profile.Name)
			if name == "" {
				continue
			}
			driver := strings.TrimSpace(profile.Driver)
			if driver == "" && profile.FilePath != "" {
				driver = "sqlite"
			}
			if driver == "" {
				return nil, fmt.Errorf("connection profile %q is missing driver", name)
			}
			merged[name] = namedConnection{driver: driver, params: profileParams(profile)}
		}
	}
	return merged, nil
}

func cloneParams(in map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range in {
		out[k] = v
	}
	return out
}

func profileParams(profile config.ConnectionProfile) map[string]string {
	params := cloneParams(profile.Extra)
	if profile.Host != "" {
		params["host"] = profile.Host
	}
	if profile.Port != 0 {
		params["port"] = strconv.Itoa(profile.Port)
	}
	if profile.Database != "" {
		params["database"] = profile.Database
	}
	if profile.Username != "" {
		params["username"] = profile.Username
	}
	if profile.SSLMode != "" {
		params["sslmode"] = profile.SSLMode
	}
	if profile.WindowsAuth {
		params["windows_auth"] = "true"
	}
	if profile.AzureAD != "" {
		params["azure_ad"] = profile.AzureAD
	}
	if profile.AppName != "" {
		params["app_name"] = profile.AppName
	}
	if profile.Encrypt != "" {
		params["encrypt"] = profile.Encrypt
	}
	if profile.FilePath != "" {
		params["file"] = profile.FilePath
	}
	return params
}

func connectionSummary(driver string, params map[string]string) string {
	if raw := strings.TrimSpace(params["raw"]); raw != "" {
		if summary := summaryFromRaw(raw); summary != "" {
			return summary
		}
	}
	if file := strings.TrimSpace(params["file"]); file != "" {
		return filepath.Base(file)
	}
	host := first(params, "host", "server", "data source")
	database := first(params, "database", "dbname", "initial catalog")
	switch {
	case host != "" && database != "":
		return host + "/" + database
	case host != "":
		return host
	case database != "":
		return database
	case driver != "":
		return driver
	default:
		return "connection"
	}
}

func summaryFromRaw(raw string) string {
	raw = strings.TrimSpace(raw)
	if u, err := url.Parse(raw); err == nil && u.Scheme != "" && (u.Host != "" || u.User != nil) {
		db := strings.TrimPrefix(u.Path, "/")
		switch {
		case u.Host != "" && db != "":
			return u.Host + "/" + db
		case u.Host != "":
			return u.Host
		case db != "":
			return db
		}
	}
	if DetectDriver(raw) == "sqlite" {
		if strings.HasPrefix(strings.ToLower(raw), "file:") {
			trimmed := strings.TrimPrefix(raw, "file:")
			trimmed = strings.TrimPrefix(trimmed, "//")
			if trimmed != "" {
				return filepath.Base(trimmed)
			}
		}
		return filepath.Base(raw)
	}
	kv := parseKV(raw)
	host := first(kv, "host", "server", "data source")
	database := first(kv, "database", "dbname", "initial catalog")
	switch {
	case host != "" && database != "":
		return host + "/" + database
	case host != "":
		return host
	case database != "":
		return database
	default:
		return ""
	}
}
