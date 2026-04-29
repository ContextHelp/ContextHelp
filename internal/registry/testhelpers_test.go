package registry

import (
	"testing"

	"hop.top/kit/go/storage/secret"
	"hop.top/kit/go/storage/secret/keyring"
)

// newTestResolver returns a kit keyring store scoped to a test-unique service
// name. All entries written during the test are cleaned up automatically.
func newTestResolver(t *testing.T) secret.MutableStore {
	t.Helper()
	svc := "ctxt-registry-test-" + t.Name()
	return keyring.New(svc)
}
