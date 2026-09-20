package main

import (
	"fmt"
	"os"

	"github.com/ideacrafterslabs/ctxt/cmd/ctxt/cmd"
)

var (
	// Version information (set via ldflags)
	Version   = "dev"
	BuildTime = "unknown"
	GitCommit = "unknown"
)

func main() {
	// Set version info
	cmd.SetVersionInfo(Version, BuildTime, GitCommit)

	// Execute root command
	if err := cmd.Execute(); err != nil {
		// A failure the CLI already rendered as a structured envelope
		// owns its stderr. Printing here too would put a prose line
		// under the envelope, and under --format json the two together
		// parse as neither.
		if !cmd.ErrorAlreadyRendered(err) {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		}
		os.Exit(cmd.ExitCodeFor(err))
	}
}
