package connections

import (
	"strings"

	keyring "github.com/zalando/go-keyring"
)

const credentialService = "sql"

func SavePassword(name, password string) error {
	name = strings.TrimSpace(name)
	password = strings.TrimSpace(password)
	if name == "" || password == "" {
		return nil
	}
	return keyring.Set(credentialService, name, password)
}

func LoadPassword(name string) (string, bool, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", false, nil
	}
	password, err := keyring.Get(credentialService, name)
	if err == keyring.ErrNotFound {
		return "", false, nil
	}
	if err != nil {
		// Keyring service unavailable (e.g. no D-Bus on Linux CI) — treat
		// as no stored password rather than a fatal error.
		return "", false, nil
	}
	return password, true, nil
}

func DeletePassword(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	err := keyring.Delete(credentialService, name)
	if err == keyring.ErrNotFound {
		return nil
	}
	return err
}
