package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var keyCmd = &cobra.Command{
	Use:        "key",
	Short:      "[deprecated] moved to dpkms key",
	Deprecated: "use 'dpkms key' instead",
	RunE:       movedToDpkms("key"),
}

func init() {
	rootCmd.AddCommand(keyCmd)

	for _, sub := range []string{"init", "rotate"} {
		name := sub
		keyCmd.AddCommand(&cobra.Command{
			Use:        name,
			Short:      fmt.Sprintf("[deprecated] moved to dpkms key %s", name),
			Deprecated: fmt.Sprintf("use 'dpkms key %s' instead", name),
			RunE:       movedToDpkms("key " + name),
			DisableFlagParsing: true,
		})
	}
}
