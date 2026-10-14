package integration

import (
	"os"
	"testing"

	"github.com/zalando/go-keyring"
)

// TestMain swaps the OS keychain for go-keyring's in-memory mock. The test
// runners move HOME, so on macOS a real keychain write finds no login
// keychain and raises a "Keychain Not Found" dialog. The keychain backend
// tests exercise the kit adapter against the mock. testguard.Main is not
// used here: it clears the CTXT_GARAGE_* variables the Garage tests read.
func TestMain(m *testing.M) {
	keyring.MockInit()
	os.Exit(m.Run())
}
