package cmd

import (
	"testing"

	"github.com/zalando/go-keyring"
)

// The test binary never reaches the OS keychain: testguard moved HOME,
// so on macOS a real write finds no login keychain and the system shows
// a "Keychain Not Found" dialog. `ctxt registry login|logout` and the
// keyring secrets backend write through go-keyring; under testguard
// those writes land in its in-memory mock.
func TestKeychainIsMockedUnderTestguard(t *testing.T) {
	const svc, user = "ctxt-test-keychain-guard", "probe"
	if err := keyring.Set(svc, user, "v"); err != nil {
		t.Fatalf("keyring write reached the OS keychain: %v", err)
	}
	t.Cleanup(func() { _ = keyring.Delete(svc, user) })

	got, err := keyring.Get(svc, user)
	if err != nil || got != "v" {
		t.Fatalf("keyring read = %q, %v; want the mocked value", got, err)
	}
}
