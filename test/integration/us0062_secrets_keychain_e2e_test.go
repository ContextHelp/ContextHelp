package integration

import (
	"context"
	"errors"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/secrets"
	"hop.top/kit/go/storage/secret"
)

// keychainAvailable does a probe Set/Delete to detect whether the test host
// has a usable keyring. macOS Keychain on dev workstations: yes. CI hosts /
// headless Linux without secret-service running: no. Caller skips on false.
func keychainAvailable(t *testing.T) bool {
	t.Helper()
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		return false
	}
	r, err := secrets.New(config.SecretsConfig{
		Backend:         "keychain",
		KeychainService: "ctxt-keychain-probe",
	})
	if err != nil {
		return false
	}
	probeKey := "__keychain_probe__"
	if err := r.Set(context.Background(), probeKey, []byte("probe")); err != nil {
		return false
	}
	_ = r.Delete(context.Background(), probeKey)
	return true
}

// TestUS0062_KeychainBackendRoundTrip verifies Set → Get → Delete on the OS
// keychain, scoped to a unique test service to avoid colliding with real
// stored credentials.
func TestUS0062_KeychainBackendRoundTrip(t *testing.T) {
	if !keychainAvailable(t) {
		t.Skip("OS keychain not available (CI / headless host)")
	}
	svc := "ctxt-test-" + t.Name()
	r, err := secrets.New(config.SecretsConfig{
		Backend:         "keychain",
		KeychainService: svc,
	})
	require.NoError(t, err)

	ctx := context.Background()
	key := "round-trip-key"
	want := "round-trip-value"

	require.NoError(t, r.Set(ctx, key, []byte(want)))
	t.Cleanup(func() { _ = r.Delete(ctx, key) })

	got, err := r.Get(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, want, string(got.Value))

	exists, err := r.Exists(ctx, key)
	require.NoError(t, err)
	assert.True(t, exists)

	require.NoError(t, r.Delete(ctx, key))

	_, err = r.Get(ctx, key)
	assert.True(t, errors.Is(err, secret.ErrNotFound), "want ErrNotFound after Delete, got %v", err)
}

// TestUS0062_KeychainBackendOverwrite verifies Set on an existing key
// overwrites silently (matches the security/secret-tool/-U semantics).
func TestUS0062_KeychainBackendOverwrite(t *testing.T) {
	if !keychainAvailable(t) {
		t.Skip("OS keychain not available")
	}
	svc := "ctxt-test-" + t.Name()
	r, err := secrets.New(config.SecretsConfig{
		Backend:         "keychain",
		KeychainService: svc,
	})
	require.NoError(t, err)

	ctx := context.Background()
	key := "overwrite-key"
	t.Cleanup(func() { _ = r.Delete(ctx, key) })

	require.NoError(t, r.Set(ctx, key, []byte("v1")))
	require.NoError(t, r.Set(ctx, key, []byte("v2")))

	got, err := r.Get(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, "v2", string(got.Value))
}
