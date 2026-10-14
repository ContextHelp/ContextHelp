package blob

import (
	"os"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/testguard"
)

// TestMain runs the package isolated from the developer's machine: HOME
// and XDG dirs point at a throwaway tree, so Load never reads the real
// config, the OS keychain is go-keyring's in-memory mock, and a request
// to a default local server port is refused and fails the run. See
// internal/testguard.
func TestMain(m *testing.M) {
	os.Exit(testguard.Main(m, nil))
}
