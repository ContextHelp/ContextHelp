package rederive

import (
	"os"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/testguard"
)

// TestMain isolates HOME and the XDG dirs, so nothing a test touches lands
// in the developer's real directories.
func TestMain(m *testing.M) {
	os.Exit(testguard.Main(m, nil))
}
