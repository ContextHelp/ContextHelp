package storage_test

import (
	"os"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/testguard"
)

// TestMain isolates HOME and the XDG dirs from the developer's machine.
func TestMain(m *testing.M) {
	os.Exit(testguard.Main(m, nil))
}
