package cmd

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	kitcli "hop.top/kit/go/console/cli"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

var registryPendingCmd = &cobra.Command{
	Use:   "pending",
	Short: "List entities waiting for approval",
	Long: `List the approval queue: entities an untrusted registry served that
sync did not write (a registry with trust_level unset is untrusted).
Each row has the ID approve and reject take, the registry, the entity
slug and version, whether the registry's index signature was verified,
and when sync first and last saw it. Sandboxed registries' entities are
blocked, never queued. Read-only.`,
	Args: cobra.NoArgs,
	RunE: runRegistryPending,
}

var registryApproveCmd = &cobra.Command{
	Use:   "approve [<id>...]",
	Short: "Write entities waiting for approval",
	Long: `Write queued entities to the local entity store as a sync of a
trusted registry would (the registry's sync_mode), recording provenance
"approved from <registry>", and drop them from the queue. Pass the IDs
ctxt registry pending lists, or --all --registry <name> for every entity
that registry has queued. Nothing is written when an ID is not queued or
its registry is now sandboxed.

` + cliconv.DestructiveTokenHelp + `
--dry-run lists what would be approved and needs no token.`,
	RunE: runRegistryApprove,
}

var registryRejectCmd = &cobra.Command{
	Use:   "reject <id>...",
	Short: "Drop entities waiting for approval",
	Long: `Drop queued entities without writing them. A rejected entity is
queued again if its registry still serves it at the next sync. Nothing
is dropped when an ID is not queued. No confirmation: nothing is lost
that the next sync does not bring back. --dry-run lists what would be
rejected.`,
	Args: cobra.MinimumNArgs(1),
	RunE: runRegistryReject,
}

func init() {
	registryCmd.AddCommand(registryPendingCmd)
	registryCmd.AddCommand(registryApproveCmd)
	registryCmd.AddCommand(registryRejectCmd)

	registryPendingCmd.Flags().String("registry", "", "list only this registry's queued entities")
	registryApproveCmd.Flags().Bool("all", false, "approve every entity --registry has queued")
	registryApproveCmd.Flags().String("registry", "", "registry whose queued entities --all approves")

	cliconv.WithSideEffect(registryPendingCmd, cliconv.SideEffectRead)
	cliconv.WithIdempotency(registryPendingCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(registryPendingCmd, []cliconv.Example{
		{Title: "List every queued entity", Command: "ctxt registry pending"},
		{Title: "One registry's queue as JSON", Command: "ctxt registry pending --registry uxpatterns --format json"},
	})
	cliconv.WithNextSteps(registryPendingCmd, []cliconv.NextStep{
		{Suggest: "ctxt registry approve <id> --dry-run", Reason: "preview, then approve with --confirm-token=<sha> to write it"},
		{Suggest: "ctxt registry reject <id>", Reason: "drop an entity you do not want"},
	})

	// Same gate as registry delete: the typed token. Approval writes
	// unreviewed registry content into the graph.
	cliconv.WithSideEffect(registryApproveCmd, cliconv.SideEffectDestructive)
	cliconv.WithDestructiveToken(registryApproveCmd)
	// Replaying --all --registry approves nothing more; replaying IDs
	// fails NOT_FOUND, since approval took them off the queue.
	cliconv.WithIdempotency(registryApproveCmd, cliconv.IdempotencyConditional)
	cliconv.WithExamples(registryApproveCmd, []cliconv.Example{
		{Title: "Approve one entity", Command: "ctxt registry approve 3f9a1c2b7d4e5f60 --confirm-token=<sha>"},
		{Title: "Approve everything one registry queued", Command: "ctxt registry approve --all --registry uxpatterns --confirm-token=<sha>"},
	})
	cliconv.WithNextSteps(registryApproveCmd, []cliconv.NextStep{
		{Suggest: "ctxt entity show <slug>", Reason: "check the written entity"},
		{Suggest: "ctxt registry pending", Reason: "review what is still queued"},
	})

	// A plain write, ungated: reject only deletes queue rows, and the
	// entity is queued again if its registry still serves it at the
	// next sync.
	cliconv.WithSideEffect(registryRejectCmd, cliconv.SideEffectWrite)
	// A replay fails NOT_FOUND: the IDs are off the queue.
	cliconv.WithIdempotency(registryRejectCmd, cliconv.IdempotencyNo)
	cliconv.WithExamples(registryRejectCmd, []cliconv.Example{
		{Title: "Reject one entity", Command: "ctxt registry reject 3f9a1c2b7d4e5f60"},
		{Title: "Reject several", Command: "ctxt registry reject 3f9a1c2b7d4e5f60 8e21d0c4b5a69f73"},
	})
	cliconv.WithNextSteps(registryRejectCmd, []cliconv.NextStep{
		{Suggest: "ctxt registry pending", Reason: "review what is still queued"},
	})
}

func runRegistryPending(cmd *cobra.Command, _ []string) error {
	registry, _ := cmd.Flags().GetString("registry")
	svc, cleanup, err := newService(cmd)
	if err != nil {
		return err
	}
	defer cleanup()

	pending, err := svc.ListPendingEntities(asOwner(cmdContext(cmd)), registry)
	if err != nil {
		return fmt.Errorf("list pending entities: %w", err)
	}
	w := cmd.OutOrStdout()
	if isJSONOutput() {
		return outputJSON(w, map[string]any{"pending": service.RedactPending(pending), "total": len(pending)})
	}
	if len(pending) == 0 {
		fmt.Fprintln(w, "No entities waiting for approval.")
		return nil
	}
	printPendingTable(w, pending, true)
	return nil
}

func runRegistryApprove(cmd *cobra.Command, args []string) error {
	all, _ := cmd.Flags().GetBool("all")
	registry, _ := cmd.Flags().GetString("registry")
	sel := service.PendingSelection{IDs: args, All: all, Registry: registry}
	return runPendingAction(cmd, sel, "approve", "approved", (*service.Service).ApprovePendingEntities)
}

func runRegistryReject(cmd *cobra.Command, args []string) error {
	return runPendingAction(cmd, service.PendingSelection{IDs: args}, "reject", "rejected",
		(*service.Service).RejectPendingEntities)
}

// runPendingAction applies act (verb: approve or reject; done: its past
// tense, the JSON key) to sel and reports the entities it touched; under
// --dry-run it only resolves sel.
func runPendingAction(
	cmd *cobra.Command,
	sel service.PendingSelection,
	verb, key string,
	act func(*service.Service, context.Context, service.PendingSelection) ([]*storage.PendingEntity, error),
) error {
	svc, cleanup, err := newService(cmd)
	if err != nil {
		return err
	}
	defer cleanup()

	ctx := asOwner(cmdContext(cmd))
	dryRun := kitcli.IsDryRun(cmd)
	var done []*storage.PendingEntity
	if dryRun {
		done, err = svc.SelectPendingEntities(ctx, sel)
	} else {
		done, err = act(svc, ctx, sel)
	}
	w := cmd.OutOrStdout()
	if err != nil {
		if len(done) > 0 && !isJSONOutput() {
			fmt.Fprintf(w, "%s %d before the failure:\n", key, len(done))
			printPendingTable(w, done, false)
		}
		return fmt.Errorf("%s: %w", verb, err)
	}
	if isJSONOutput() {
		return outputJSON(w, map[string]any{key: service.RedactPending(done), "total": len(done), "dry_run": dryRun})
	}
	if dryRun {
		fmt.Fprintf(w, "Would be %s: %d (dry-run, nothing changed)\n", key, len(done))
	} else {
		fmt.Fprintf(w, "Entities %s: %d\n", key, len(done))
	}
	if len(done) > 0 {
		printPendingTable(w, done, false)
	}
	return nil
}

// printPendingTable prints queued entities; seen adds the first and
// last sync that saw each.
func printPendingTable(w io.Writer, ps []*storage.PendingEntity, seen bool) {
	headers := []string{"ID", "Registry", "Slug", "Title", "Version", "Signature"}
	if seen {
		headers = append(headers, "First Seen", "Last Seen")
	}
	rows := make([][]string, 0, len(ps))
	for _, p := range ps {
		version := p.Version
		if strings.HasPrefix(version, "sha256:") && len(version) > 19 {
			version = version[:19]
		}
		row := []string{p.ID, p.RegistryName, p.Slug, p.Title, version, p.SignatureStatus}
		if seen {
			row = append(row, p.FirstSeenAt.Local().Format("2006-01-02 15:04"), p.LastSeenAt.Local().Format("2006-01-02 15:04"))
		}
		rows = append(rows, row)
	}
	printTable(w, headers, rows)
}
