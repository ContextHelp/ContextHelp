//go:build darwin || linux

package registry

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/secrets"
)

// newTestResolver returns a KeychainResolver scoped to a test-unique service name.
// All entries written during the test are cleaned up automatically.
func newTestResolver(t *testing.T) *secrets.KeychainResolver {
	t.Helper()
	svc := "ctxt-registry-test-" + t.Name()
	return secrets.NewKeychainResolver(svc)
}
