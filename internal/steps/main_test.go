package steps_test

import (
	"os"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/testguard"
)

// TestMain isolates the package from the developer's machine and swaps
// the OS keychain for go-keyring's in-memory mock: with HOME sandboxed,
// any real keychain access (a keychain-backed registry TokenStore) raises
// a macOS "Keychain Not Found" dialog. Tests inject in-memory token
// sources; this guard catches one that forgets to.
func TestMain(m *testing.M) {
	os.Exit(testguard.Main(m, nil))
}
