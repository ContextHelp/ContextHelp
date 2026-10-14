package builtins

import (
	"os"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/testguard"
)

// TestMain isolates HOME/XDG and swaps the OS keychain for go-keyring's
// in-memory mock: the factory takes any secret store, and a keyring-backed
// one would otherwise raise a macOS "Keychain Not Found" dialog.
func TestMain(m *testing.M) {
	os.Exit(testguard.Main(m, nil))
}
