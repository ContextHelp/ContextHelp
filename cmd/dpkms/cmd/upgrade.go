package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	kitupgrade "hop.top/kit/go/core/upgrade"
)

// upgrade is grouped under MANAGEMENT in root.go's commandGroups map (T-0467)

const (
	upgradeBinaryName = "dpkms"
	upgradeGitHubRepo = "jadb/ContextHelp"
)

// newUpgradeChecker builds a kit/upgrade Checker configured for dpkms.
// Cache TTL is set to zero on `upgrade check` so the user always sees fresh
// data; other paths inherit kit's default 4h cache.
func newUpgradeChecker(noCache bool) *kitupgrade.Checker {
	opts := []kitupgrade.Option{
		kitupgrade.WithBinary(upgradeBinaryName, version),
		kitupgrade.WithGitHub(upgradeGitHubRepo),
		kitupgrade.WithTimeout(10 * time.Second),
	}
	if noCache {
		opts = append(opts, kitupgrade.WithCacheTTL(0))
	}
	return kitupgrade.New(opts...)
}

var upgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Manage dpkms self-upgrades",
	Long: `Check for, install, snooze, or read notes for new dpkms releases.

Backed by hop.top/kit/go/core/upgrade. Release source defaults to the
GitHub repo ` + upgradeGitHubRepo + `; cache, snooze, and download paths
follow XDG state conventions.`,
}

var upgradeCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Report whether a newer release exists",
	RunE:  runUpgradeCheck,
}

var upgradeInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Download and replace the dpkms binary in place",
	RunE:  runUpgradeInstall,
}

var upgradeSnoozeCmd = &cobra.Command{
	Use:   "snooze",
	Short: "Defer the upgrade prompt for the configured duration",
	RunE:  runUpgradeSnooze,
}

var upgradeNotesCmd = &cobra.Command{
	Use:   "notes",
	Short: "Print release notes for the latest version",
	RunE:  runUpgradeNotes,
}

func init() {
	rootCmd.AddCommand(upgradeCmd)
	upgradeCmd.AddCommand(upgradeCheckCmd)
	upgradeCmd.AddCommand(upgradeInstallCmd)
	upgradeCmd.AddCommand(upgradeSnoozeCmd)
	upgradeCmd.AddCommand(upgradeNotesCmd)
}

func runUpgradeCheck(cmd *cobra.Command, _ []string) error {
	c := newUpgradeChecker(true)
	r := c.Check(context.Background())
	if r.Err != nil {
		return fmt.Errorf("upgrade check: %w", r.Err)
	}
	if isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), map[string]any{
			"current":      r.Current,
			"latest":       r.Latest,
			"update_avail": r.UpdateAvail,
			"checked_at":   r.CheckedAt,
		})
	}
	if !r.UpdateAvail {
		fmt.Fprintf(cmd.OutOrStdout(), "dpkms is up to date (%s)\n", r.Current)
		return nil
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Update available: %s → %s\n", r.Current, r.Latest)
	if r.Notes != "" {
		fmt.Fprintln(cmd.OutOrStdout(), r.Notes)
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Run `dpkms upgrade install` to apply.")
	return nil
}

func runUpgradeInstall(cmd *cobra.Command, _ []string) error {
	c := newUpgradeChecker(true)
	return kitupgrade.RunCLI(context.Background(), c, kitupgrade.CLIOptions{
		AutoUpgrade: true,
		Out:         cmd.OutOrStdout(),
	})
}

func runUpgradeSnooze(cmd *cobra.Command, _ []string) error {
	c := newUpgradeChecker(false)
	if err := c.Snooze(); err != nil {
		return fmt.Errorf("snooze: %w", err)
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Upgrade prompt snoozed.")
	return nil
}

func runUpgradeNotes(cmd *cobra.Command, _ []string) error {
	c := newUpgradeChecker(true)
	notes := c.WhatsNew(context.Background())
	if notes == "" {
		fmt.Fprintln(os.Stderr, "No release notes available.")
		return nil
	}
	fmt.Fprintln(cmd.OutOrStdout(), notes)
	return nil
}
