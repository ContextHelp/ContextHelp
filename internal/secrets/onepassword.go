package secrets

import (
	"fmt"
	"os/exec"
	"strings"
)

// OnePasswordResolver reads secrets from 1Password via the `op` CLI.
// Keys are mapped to op://Vault/KeyName/password URIs.
// Set() is not supported (1Password items must be managed via the app or op CLI directly).
type OnePasswordResolver struct {
	vault string
}

// NewOnePasswordResolver creates a OnePasswordResolver for the given vault name.
func NewOnePasswordResolver(vault string) *OnePasswordResolver {
	return &OnePasswordResolver{vault: vault}
}

// itemURI returns the op:// URI for the given key.
func (r *OnePasswordResolver) itemURI(key string) string {
	return fmt.Sprintf("op://%s/%s/password", r.vault, key)
}

func (r *OnePasswordResolver) Get(key string) (string, error) {
	uri := r.itemURI(key)
	out, err := exec.Command("op", "read", uri).Output()
	if err != nil {
		return "", ErrNotFound{Key: key}
	}
	return strings.TrimRight(string(out), "\n"), nil
}

func (r *OnePasswordResolver) Set(key, value string) error {
	return fmt.Errorf("secrets: 1Password backend does not support Set(); manage items via the 1Password app or op CLI")
}
