package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version information",
	Long:  `Display engine and protocol version information.`,
	Run:   runVersion,
}

func init() {
	rootCmd.AddCommand(versionCmd)
}

func runVersion(cmd *cobra.Command, args []string) {
	fmt.Println("dPKMS - Decentralized Knowledge Substrate")
	fmt.Println()
	fmt.Printf("dpkms version:          %s\n", version)
	fmt.Printf("Build time:             %s\n", buildTime)
	fmt.Printf("Git commit:             %s\n", gitCommit)
	fmt.Println()
	fmt.Println("Component:              dpkms (substrate)")
	fmt.Println("Registry Protocol:      v0.2.1")
}
