package cmd

import (
	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Long: `Print ctxt's semantic version, build date, and (when available) the
git commit SHA. Used by automation to gate on a minimum version and by
human operators to confirm which build is installed.

Examples:
  ctxt version
  ctxt version --output json`,
	Run: func(cmd *cobra.Command, args []string) {
		printVersion(cmd)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
	cliconv.WithSideEffect(versionCmd, cliconv.SideEffectRead)
	cliconv.WithExamples(versionCmd, []cliconv.Example{
		{Title: "Print version", Command: "ctxt version"},
		{Title: "JSON output for automation", Command: "ctxt version --output json"},
	})
	// "version" is not in kit's defaultIdempotency table; repeated calls
	// only print build metadata, which is naturally idempotent.
	cliconv.WithIdempotency(versionCmd, cliconv.IdempotencyYes)
}
