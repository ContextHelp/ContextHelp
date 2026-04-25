package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// movedToDpkms returns a RunE that prints a deprecation notice and exits
// with code 1 for commands that have been relocated to the dpkms binary.
func movedToDpkms(dpkmsCmd string) func(*cobra.Command, []string) error {
	return func(_ *cobra.Command, _ []string) error {
		return fmt.Errorf(
			"this command has moved to dpkms %s", dpkmsCmd)
	}
}
