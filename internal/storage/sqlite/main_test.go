package sqlite

import (
	"os"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/testguard"
)

// TestMain isolates HOME and the XDG dirs, so scratch files and caches
// never land in the developer's real directories.
func TestMain(m *testing.M) {
	os.Exit(testguard.Main(m, nil))
}
