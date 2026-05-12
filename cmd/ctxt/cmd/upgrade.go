package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/spf13/cobra"
	kitupgrade "hop.top/kit/go/core/upgrade"
)

const (
	upgradeBinaryName = "ctxt"
	upgradeGitHubRepo = "jadb/ContextHelp"
)

// newUpgradeChecker builds a kit/upgrade Checker configured for ctxt.
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
	Short: "Manage ctxt self-upgrades",
	Long: `Check for, install, snooze, or read notes for new ctxt releases.

Backed by hop.top/kit/go/core/upgrade. Release source defaults to the
GitHub repo ` + upgradeGitHubRepo + `; cache, snooze, and download paths
follow XDG state conventions.`,
}

var upgradeCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Report whether a newer release exists",
	Long: `Query the configured release source (GitHub repo ` + upgradeGitHubRepo + `)
for the latest version and report whether the current binary is up
to date. The cache TTL is forced to zero so the check always hits
the network. Read-only.`,
	RunE: runUpgradeCheck,
}

var upgradeInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Download and replace the ctxt binary in place",
	Long: `Download the latest release artefact and atomically replace the
running ctxt binary on disk. The replacement is performed by
hop.top/kit/go/core/upgrade following XDG state conventions for
cache and rollback paths.`,
	RunE: runUpgradeInstall,
}

var upgradeSnoozeCmd = &cobra.Command{
	Use:   "snooze",
	Short: "Defer the upgrade prompt for the configured duration",
	Long: `Defer the in-banner upgrade prompt for the snooze window kit
ships by default. The snooze timestamp is written to the local
state directory under XDG; no network calls are made.`,
	RunE: runUpgradeSnooze,
}

var upgradeNotesCmd = &cobra.Command{
	Use:   "notes",
	Short: "Print release notes for the latest version",
	Long: `Fetch the release notes (body) for the latest release on the
configured release source and print them to stdout. Read-only and
network-bound; cache TTL is forced to zero so the notes are always
fresh.`,
	RunE: runUpgradeNotes,
}

func init() {
	rootCmd.AddCommand(upgradeCmd)
	upgradeCmd.AddCommand(upgradeCheckCmd)
	upgradeCmd.AddCommand(upgradeInstallCmd)
	upgradeCmd.AddCommand(upgradeSnoozeCmd)
	upgradeCmd.AddCommand(upgradeNotesCmd)

	cliconv.WithSideEffect(upgradeCheckCmd, cliconv.SideEffectRead)
	cliconv.WithIdempotency(upgradeCheckCmd, cliconv.IdempotencyYes)
	cliconv.WithSideEffect(upgradeInstallCmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(upgradeInstallCmd, cliconv.IdempotencyYes)
	cliconv.WithSideEffect(upgradeSnoozeCmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(upgradeSnoozeCmd, cliconv.IdempotencyYes)
	cliconv.WithSideEffect(upgradeNotesCmd, cliconv.SideEffectRead)
	cliconv.WithIdempotency(upgradeNotesCmd, cliconv.IdempotencyYes)
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
		fmt.Fprintf(cmd.OutOrStdout(), "ctxt is up to date (%s)\n", r.Current)
		return nil
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Update available: %s → %s\n", r.Current, r.Latest)
	if r.Notes != "" {
		fmt.Fprintln(cmd.OutOrStdout(), r.Notes)
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Run `ctxt upgrade install` to apply.")
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
