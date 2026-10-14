package registry_test

import (
	"os"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/testguard"
)

// TestMain isolates the package from the developer's machine. Above all
// it swaps the OS keychain for go-keyring's in-memory mock: TokenStore is
// keychain-backed, and on macOS with HOME sandboxed any real keychain
// access raises a "Keychain Not Found" dialog. Tests still inject an
// in-memory store (newTestResolver, fake TokenSource); this guard catches
// a test that forgets to.
func TestMain(m *testing.M) {
	os.Exit(testguard.Main(m, nil))
}
