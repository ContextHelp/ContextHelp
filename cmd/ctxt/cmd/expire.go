package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/expiry"
)

var expireCmd = &cobra.Command{
	Use:   "expire",
	Short: "Manage automatic expiration on knowledge objects",
}

var expireSetCmd = &cobra.Command{
	Use:   "set <id> <time-or-ttl>",
	Short: "Schedule automatic expiration for a knowledge object",
	Long: `Schedule automatic expiration for an existing knowledge object.

By default the entire object (and its derived objects/blobs) is erased when the
expiry timestamp passes, unless an active hold prevents deletion. Pass --scope
with a natural-language description (e.g. "PII and phone numbers" or "raw
content, keep summary") to selectively redact only that data in place.

Examples:
  ctxt expire set obj_123 7d
  ctxt expire set obj_123 "2026-12-31T00:00:00Z"
  ctxt expire set obj_123 30d --scope "raw content, keep summary"`,
	Args: cobra.MinimumNArgs(2),
	RunE: runExpireSet,
}

var expireClearCmd = &cobra.Command{
	Use:   "clear <id>",
	Short: "Remove automatic expiration from a knowledge object",
	Long:  "Remove any scheduled expiration timestamp and selective expiry scope from a knowledge object so it is retained indefinitely.",
	Args:  cobra.ExactArgs(1),
	RunE:  runExpireClear,
}

func init() {
	rootCmd.AddCommand(expireCmd)
	expireCmd.AddCommand(expireSetCmd, expireClearCmd)
	expireSetCmd.Flags().String("scope", "", "optional natural-language description of what to expire (default: entire object)")

	cliconv.WithSideEffect(expireSetCmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(expireSetCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(expireSetCmd, []cliconv.Example{
		{Title: "Expire an object in 7 days", Command: "ctxt expire set obj_123 7d"},
		{Title: "Expire only PII in 30 days", Command: "ctxt expire set obj_123 30d --scope \"PII and phone numbers\""},
	})
	cliconv.WithNextSteps(expireSetCmd, []cliconv.NextStep{
		{When: "after scheduling expiry", Suggest: "ctxt show <id>", Reason: "inspect the object and verify its metadata"},
	})
	cliconv.WithSideEffect(expireClearCmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(expireClearCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(expireClearCmd, []cliconv.Example{
		{Title: "Clear scheduled expiry", Command: "ctxt expire clear obj_123"},
	})
	cliconv.WithNextSteps(expireClearCmd, []cliconv.NextStep{
		{When: "after clearing expiry", Suggest: "ctxt show <id>", Reason: "confirm expiry was removed"},
	})
}

func runExpireSet(cmd *cobra.Command, args []string) error {
	id := args[0]
	expr := strings.Join(args[1:], " ")
	now := time.Now()
	at, err := expiry.ParseExpiry(expr, now)
	if err != nil {
		return err
	}
	if !at.After(now.UTC()) {
		return fmt.Errorf("expiry time %s must be in the future", at.Format(time.RFC3339))
	}
	scope, _ := cmd.Flags().GetString("scope")
	client, err := newDpkmsClient(cmd, captureTimeout)
	if err != nil {
		return err
	}
	if err := client.SetObjectExpiry(cmdContext(cmd), id, at, strings.TrimSpace(scope)); err != nil {
		return err
	}
	if scope != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "Expiry set for %s at %s (scope: %s)\n", id, at.Format(time.RFC3339), scope)
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "Expiry set for %s at %s\n", id, at.Format(time.RFC3339))
	}
	return nil
}

func runExpireClear(cmd *cobra.Command, args []string) error {
	id := args[0]
	client, err := newDpkmsClient(cmd, captureTimeout)
	if err != nil {
		return err
	}
	if err := client.ClearObjectExpiry(cmdContext(cmd), id); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Expiry cleared for %s\n", id)
	return nil
}
