package storageutil

import (
	"os"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/testguard"
)

// TestMain runs the package isolated from the developer's machine (HOME,
// XDG dirs, the OS keychain, default local server ports); POSTGRES_*
// survive for the integration test. See internal/testguard.
func TestMain(m *testing.M) {
	os.Exit(testguard.Main(m, nil))
}
