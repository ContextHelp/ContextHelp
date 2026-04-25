package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var detectorCmd = &cobra.Command{
	Use:        "detector",
	Short:      "[deprecated] moved to dpkms detector",
	Deprecated: "use 'dpkms detector' instead",
	RunE:       movedToDpkms("detector"),
}

func init() {
	rootCmd.AddCommand(detectorCmd)

	for _, sub := range []string{
		"add", "list", "remove", "enable", "disable",
	} {
		name := sub
		detectorCmd.AddCommand(&cobra.Command{
			Use:        name,
			Short:      fmt.Sprintf("[deprecated] moved to dpkms detector %s", name),
			Deprecated: fmt.Sprintf("use 'dpkms detector %s' instead", name),
			RunE:       movedToDpkms("detector " + name),
			DisableFlagParsing: true,
		})
	}
}
