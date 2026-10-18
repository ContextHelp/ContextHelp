package secrets

import (
	"os"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/testguard"
)

// TestMain runs the package isolated from the developer's machine: HOME
// and XDG dirs point at a throwaway tree and the OS keychain is
// go-keyring's in-memory mock. Env values come from t.Setenv only. See
// internal/testguard.
func TestMain(m *testing.M) {
	os.Exit(testguard.Main(m, nil))
}
