package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var devCmd = &cobra.Command{
	Use:        "dev",
	Short:      "[deprecated] moved to dpkms dev",
	Deprecated: "use 'dpkms dev' instead",
	RunE:       movedToDpkms("dev"),
}

func init() {
	rootCmd.AddCommand(devCmd)

	for _, sub := range []string{
		"reindex-vectors", "validate-registry", "init-plugin", "gen-docs",
	} {
		name := sub
		devCmd.AddCommand(&cobra.Command{
			Use:        name,
			Short:      fmt.Sprintf("[deprecated] moved to dpkms dev %s", name),
			Deprecated: fmt.Sprintf("use 'dpkms dev %s' instead", name),
			RunE:       movedToDpkms("dev " + name),
			// Accept any args/flags so old invocations produce the deprecation
			// message instead of a usage error.
			DisableFlagParsing: true,
		})
	}
}
