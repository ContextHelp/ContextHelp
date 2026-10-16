package cmd

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/cli/helpflags"
)

// TestHelpText_FlagsDeclared fails when any dpkms command's Long,
// Example, kit/examples or kit/next-steps names a flag the command it
// runs does not declare, or shows a kit/destructive-token command run
// without --confirm-token.
func TestHelpText_FlagsDeclared(t *testing.T) {
	for _, p := range helpflags.Check(rootCmd) {
		t.Error(p)
	}
}
