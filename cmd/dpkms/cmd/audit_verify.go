package cmd

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/directopen"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

var auditCmd = &cobra.Command{
	Use:   "audit",
	Short: "Check the read audit",
}

var auditVerifyCmd = &cobra.Command{
	Use:   "verify",
	Short: "Verify the steward audit chain",
	Long: `Walk the chain of steward read audit entries (ADR-083 §5) and check it:
positions run from 1 without a gap or a repeat, each entry names the
digest of the one before it, each digest recomputes from the entry as
stored, each retained object id list matches its digest, and the chain
head is the last entry's. The first break is reported with its position
and the command fails; a chain that holds prints its length.

The object id lists steward.audit_retention has pruned are not held
against their digests. A chain that has lost entries from its tail
cannot be told from a shorter one: the head is stored in the same
database, so truncation is not detected until the signed export
manifest of ADR-083 §6 records the head outside it.

It only reads, so it runs beside the daemon; a protected or public
instance needs --maintenance-override.

Examples:
  dpkms audit verify
  dpkms audit verify --format json`,
	Args: cobra.NoArgs,
	RunE: runAuditVerify,
}

func init() {
	rootCmd.AddCommand(auditCmd)
	auditCmd.AddCommand(auditVerifyCmd)
	addMaintenanceOverrideFlag(auditVerifyCmd, false)
	cliconv.WithSideEffect(auditVerifyCmd, cliconv.SideEffectRead)
	cliconv.WithIdempotency(auditVerifyCmd, cliconv.IdempotencyYes)
}

func runAuditVerify(cmd *cobra.Command, _ []string) error {
	ctx := context.Background()
	t := storeTarget(maintenanceLabel)
	var h *directopen.Handle
	var err error
	if maintenanceOverride(cmd) {
		h, err = directopen.Override(ctx, t, cmd.CommandPath())
	} else {
		h, err = directopen.Read(ctx, t)
	}
	if err != nil {
		return maintenanceRefusal(err)
	}
	defer func() { _ = h.Close(context.Background()) }()

	var keptSince time.Time
	if r := cfg.Steward.AuditRetention; r > 0 {
		keptSince = time.Now().Add(-r)
	}
	n, err := verifyStewardChain(ctx, h.Driver, keptSince)
	if err != nil {
		return err
	}
	if isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), map[string]any{"ok": true, "entries": n})
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "steward audit chain holds: %d entries\n", n)
	return nil
}

// verifyStewardChain checks the steward chain of driver and returns its
// length. A break is an error naming its position.
func verifyStewardChain(ctx context.Context, driver storage.StorageDriver, keptSince time.Time) (int64, error) {
	head, entries, err := driver.Sharing().AccessLog().StewardChain(ctx)
	if err != nil {
		return 0, fmt.Errorf("read the steward chain: %w", err)
	}
	if err := storage.VerifyAccessChain(head, entries, keptSince); err != nil {
		var brk *storage.AccessChainBreak
		if errors.As(err, &brk) {
			return 0, fmt.Errorf("steward audit chain broken at position %d: %s", brk.Seq, brk.Reason)
		}
		return 0, err
	}
	return head.Seq, nil
}
