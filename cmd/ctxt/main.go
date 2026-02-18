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
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
