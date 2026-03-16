//go:build darwin || linux

package secrets

import (
	"os/exec"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKeychainResolverImplementsLister(t *testing.T) {
	r := NewKeychainResolver("ctxt")
	_, ok := any(r).(Lister)
	assert.True(t, ok, "KeychainResolver should implement Lister")
}

func TestKeychainResolverListKeys(t *testing.T) {
	if _, err := exec.LookPath("security"); err != nil {
		t.Skip("security not in PATH (Linux or no keychain)")
	}

	svc := "ctxt-test-list-" + t.Name()

	// Store two test entries.
	require.NoError(t, exec.Command("security", "add-generic-password",
		"-s", svc, "-a", "ALPHA_KEY", "-w", "v1", "-U").Run())
	require.NoError(t, exec.Command("security", "add-generic-password",
		"-s", svc, "-a", "BETA_KEY", "-w", "v2", "-U").Run())
	t.Cleanup(func() {
		exec.Command("security", "delete-generic-password", "-s", svc, "-a", "ALPHA_KEY").Run()
		exec.Command("security", "delete-generic-password", "-s", svc, "-a", "BETA_KEY").Run()
	})

	r := NewKeychainResolver(svc)
	lister, ok := any(r).(Lister)
	require.True(t, ok)

	keys, err := lister.Keys()
	require.NoError(t, err)
	sort.Strings(keys)
	assert.Equal(t, []string{"ALPHA_KEY", "BETA_KEY"}, keys)
}
