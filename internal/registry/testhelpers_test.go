package registry

import (
	"testing"

	"hop.top/kit/go/storage/secret"
	"hop.top/kit/go/storage/secret/memory"
)

// newTestResolver returns an in-memory store, one per test. TokenStore's
// tests exercise its own logic; they never reach the OS keychain.
func newTestResolver(t *testing.T) secret.MutableStore {
	t.Helper()
	return memory.New()
}
