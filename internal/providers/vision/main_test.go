package vision

import (
	"os"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/testguard"
)

// TestMain runs the package isolated from the developer's machine: HOME
// and XDG dirs point at a throwaway tree and a request to a default
// local server port is refused. Hosted API calls go to httptest servers
// or injected transports only. See internal/testguard.
func TestMain(m *testing.M) {
	os.Exit(testguard.Main(m, nil))
}
