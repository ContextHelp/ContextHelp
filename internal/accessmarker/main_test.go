package accessmarker

import (
	"os"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/testguard"
)

// TestMain isolates the package from the developer's machine; every
// test works on t.TempDir() files only.
func TestMain(m *testing.M) {
	os.Exit(testguard.Main(m, nil))
}
