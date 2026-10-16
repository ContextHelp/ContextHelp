package corpus_test

import (
	"os"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/testguard"
)

// TestMain keeps the tests off the developer's configuration, servers and
// keychain, as every package that opens a store does.
func TestMain(m *testing.M) {
	os.Exit(testguard.Main(m, nil))
}
