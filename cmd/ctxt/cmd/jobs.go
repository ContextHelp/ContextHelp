package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var jobsCmd = &cobra.Command{
	Use:        "job",
	Short:      "[deprecated] moved to dpkms job",
	Deprecated: "use 'dpkms job' instead",
	RunE:       movedToDpkms("job"),
}

func init() {
	rootCmd.AddCommand(jobsCmd)

	for _, sub := range []string{
		"list", "status", "log", "retry", "cancel",
	} {
		name := sub
		jobsCmd.AddCommand(&cobra.Command{
			Use:        name,
			Short:      fmt.Sprintf("[deprecated] moved to dpkms job %s", name),
			Deprecated: fmt.Sprintf("use 'dpkms job %s' instead", name),
			RunE:       movedToDpkms("job " + name),
			DisableFlagParsing: true,
		})
	}
}
