package connections

import (
	"fmt"
	"strings"
)

func SaveManaged(name, connString string) (string, error) {
	name = strings.TrimSpace(name)
	connString = strings.TrimSpace(connString)
	if name == "" {
		return "", fmt.Errorf("connection name is required")
	}
	if connString == "" {
		return "", fmt.Errorf("connection string is required")
	}
	driver, params, err := ParseConnString(connString)
	if err != nil {
		return "", err
	}
	store, err := LoadManagedStore()
	if err != nil {
		return "", err
	}
	if password := params["password"]; password != "" {
		if err := SavePassword(name, password); err != nil {
			return "", fmt.Errorf("save password to keychain: %w", err)
		}
	} else if err := DeletePassword(name); err != nil {
		return "", fmt.Errorf("clear saved password from keychain: %w", err)
	}
	if err := store.Add(Entry{
		Name:   name,
		Driver: driver,
		Params: SanitizedParamsForStorage(params),
	}); err != nil {
		return "", err
	}
	return driver, nil
}
