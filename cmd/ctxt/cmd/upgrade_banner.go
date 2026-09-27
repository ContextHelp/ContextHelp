package cmd

import (
	"github.com/spf13/cobra"

	"github.com/ideacrafterslabs/ctxt/internal/cli/banner"
)

// upgradeBannerTTY reports whether the banner's writer is a terminal.
// Tests replace it.
var upgradeBannerTTY = banner.IsTerminal

// armUpgradeBanner sets up the ADR-070 upgrade banner for the command
// about to run (see banner.Arm): printed once on stderr from the upgrade
// header on dpkms API responses.
func armUpgradeBanner(cmd *cobra.Command) { banner.Arm(cmd, upgradeBannerTTY) }
