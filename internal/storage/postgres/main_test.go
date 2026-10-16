package postgres_test

import (
	"os"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/testguard"
)

// TestMain runs the package isolated from the developer's machine: HOME
// and XDG dirs point at a throwaway tree, the OS keychain is go-keyring's
// in-memory mock (the test binary reaches kit's keyring backend through
// storageutil), and a request to a default local server port is refused
// and fails the run. POSTGRES_* survive, so the integration tests still
// find their server. See internal/testguard.
func TestMain(m *testing.M) {
	os.Exit(testguard.Main(m, nil))
}
