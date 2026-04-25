package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var secretCmd = &cobra.Command{
	Use:        "secret",
	Short:      "[deprecated] moved to dpkms secret",
	Deprecated: "use 'dpkms secret' instead",
	RunE:       movedToDpkms("secret"),
}

func init() {
	rootCmd.AddCommand(secretCmd)

	for _, sub := range []string{"get", "set", "list"} {
		name := sub
		secretCmd.AddCommand(&cobra.Command{
			Use:        name,
			Short:      fmt.Sprintf("[deprecated] moved to dpkms secret %s", name),
			Deprecated: fmt.Sprintf("use 'dpkms secret %s' instead", name),
			RunE:       movedToDpkms("secret " + name),
			DisableFlagParsing: true,
		})
	}
}
