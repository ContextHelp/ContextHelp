package secrets

import (
	"fmt"
	"os"
)

// Resolver abstracts reading and writing secrets from any backend.
type Resolver interface {
	// Get returns the secret value for key, or an error if not found.
	Get(key string) (string, error)
	// Set stores key=value in the configured backend.
	Set(key, value string) error
}

// Lister is an optional interface for backends that support key enumeration.
// Not all backends can list stored keys — check for this interface before calling Keys().
// Backends that implement Lister: age-file, gh-secrets.
// Backends that do not: env, keychain, 1password.
type Lister interface {
	// Keys returns the names of all secrets stored in this backend.
	Keys() ([]string, error)
}

// ErrNotFound is returned when a secret is not found in any backend.
type ErrNotFound struct {
	Key string
}

func (e ErrNotFound) Error() string {
	return fmt.Sprintf("secrets: key %q not found", e.Key)
}

// EnvResolver reads secrets from environment variables.
// Set() is a no-op (environment is read-only at runtime).
type EnvResolver struct{}

// NewEnvResolver returns an EnvResolver.
func NewEnvResolver() *EnvResolver { return &EnvResolver{} }

func (r *EnvResolver) Get(key string) (string, error) {
	if v := os.Getenv(key); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("secrets: env var %q not set", key)
}

func (r *EnvResolver) Set(key, value string) error {
	return fmt.Errorf("secrets: EnvResolver is read-only; set %s in the environment manually", key)
}
