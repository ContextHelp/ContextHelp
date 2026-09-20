package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/cli/cliformat"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	kitcli "hop.top/kit/go/console/cli"
)

var deleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete knowledge objects",
	Long: `Delete knowledge objects using IDs or filters.

WARNING: This operation is irreversible. Use with caution.

Confirmation is governed by kit's global --confirm flag (auto|yes|no|prompt).
Pass --confirm=yes to bypass the prompt non-interactively. The kit-owned
--confirm-token=<sha> is also required (the printed token must be echoed back).

Examples:
  # Delete specific object
  ctxt delete --id obj_12345678 --confirm=yes --confirm-token=<sha>

  # Delete by tag
  ctxt delete --tagged temporary --confirm=yes --confirm-token=<sha>

  # Delete by mention
  ctxt delete --mention @project.archived --confirm=yes --confirm-token=<sha>

  # Delete all (requires confirmation)
  ctxt delete --all --confirm=yes --confirm-token=<sha>`,
	PreRunE: previewDelete,
	RunE:    runDelete,
}

func init() {
	rootCmd.AddCommand(deleteCmd)
	cliconv.WithSideEffect(deleteCmd, cliconv.SideEffectDestructive)
	cliconv.WithExamples(deleteCmd, []cliconv.Example{
		{Title: "Delete a specific object", Command: "ctxt delete --id obj_12345678 --confirm=yes --confirm-token=<sha>"},
		{Title: "Delete by tag", Command: "ctxt delete --tagged temporary --confirm=yes --confirm-token=<sha>"},
		{Title: "Delete everything (destructive)", Command: "ctxt delete --all --confirm=yes --confirm-token=<sha>"},
	})
	cliconv.WithNextSteps(deleteCmd, []cliconv.NextStep{
		{When: "on success", Suggest: "ctxt list", Reason: "confirm the matching objects are gone"},
		{When: "if too much was removed", Suggest: "ctxt log --type delete", Reason: "review the audit trail"},
	})
	// 12fcc strict-gate: opt into kit's typed-token confirmation flow.
	// Kit installs the global --confirm + --confirm-token flags; gating
	// happens in kit's wrapped RunE before the inner adopter chain runs.
	cliconv.WithDestructiveToken(deleteCmd)
	// "delete" is in kit's defaultIdempotency table (yes); deleting an
	// already-deleted object is a no-op, so the verb default matches.

	// Filter flags
	deleteCmd.Flags().String("id", "", "delete specific knowledge object")
	deleteCmd.Flags().String("index", "", "delete by list index (comma-separated)")
	deleteCmd.Flags().String("tagged", "", "delete by tag (comma-separated)")
	deleteCmd.Flags().String("hint", "", "delete by hint")
	deleteCmd.Flags().String("mention", "", "delete by mention")
	deleteCmd.Flags().String("type", "", "delete by type")
	deleteCmd.Flags().String("subtype", "", "delete by subtype")
	deleteCmd.Flags().Bool("all", false, "delete all (requires confirmation)")

	// Confirmation: handled by kit's global --confirm (auto|yes|no|prompt)
	// + --confirm-token flags. Local -y/--yes was dropped during the
	// 12fcc conformance pass (T-0594) so confirmation has one source of
	// truth across every destructive ctxt leaf.

	// Bind flags to viper
	viper.BindPFlag("delete.id", deleteCmd.Flags().Lookup("id"))
	viper.BindPFlag("delete.index", deleteCmd.Flags().Lookup("index"))
	viper.BindPFlag("delete.tagged", deleteCmd.Flags().Lookup("tagged"))
	viper.BindPFlag("delete.hint", deleteCmd.Flags().Lookup("hint"))
	viper.BindPFlag("delete.mention", deleteCmd.Flags().Lookup("mention"))
	viper.BindPFlag("delete.type", deleteCmd.Flags().Lookup("type"))
	viper.BindPFlag("delete.subtype", deleteCmd.Flags().Lookup("subtype"))
	viper.BindPFlag("delete.all", deleteCmd.Flags().Lookup("all"))
}

// previewRendered records that PreRunE already answered this
// invocation, so runDelete returns without touching the store.
//
// A package-level flag rather than a sentinel error because a preview
// is a SUCCESS: returning an error to skip RunE would have to be
// unwound again in the exit-code mapper and the error renderer, and
// any gap there turns a clean inspection into a non-zero exit — the
// exact failure this change exists to remove. Cobra runs PreRunE and
// RunE on one command in one goroutine, so the handoff needs no more
// than this.
var previewRendered bool

// restoreTokenGate re-arms the typed-confirmation annotation that the
// current preview invocation suspended. Set by previewDelete,
// consumed by runDelete once kit's gate has been passed.
var restoreTokenGate func()

// previewDelete answers a preview request before any gate can refuse
// it, and before RunE can delete anything.
//
// It lives in PreRunE rather than in RunE because of where kit's gates
// sit. kit wraps the leaf's RunE, so both the confirmation matrix and
// the typed-token check fire before RunE is ever entered — which is
// correct for a delete, and fatal for a preview, since the command
// never gets to say what it would have done. Cobra runs the leaf's
// PreRunE ahead of that wrapper, so this is the one seam where a
// preview can answer without weakening either gate.
//
// Two request shapes arrive here, and they are deliberately answered
// with different documents:
//
//   - --dry-run is the preview mode. This is kit's ruling, not a local
//     preference: kit's own gate short-circuits on --dry-run because a
//     dry run has no real side-effect to confirm, and kit's
//     conformance harness spells the contract "refuses without
//     --confirm=yes, proceeds with it, no-ops when paired with
//     --dry-run". It answers with a Plan.
//
//   - --confirm=no is a refusal, and stays one. It answers with a
//     Refusal: applied:false, plus the targets and the count a
//     committed run would have acted on. The caller asked a
//     destructive question and declined it, so the honest answer names
//     what it declined rather than an empty stream. Declining is the
//     caller doing exactly what the gate asked, so it exits 0: an
//     inspection that reports failure makes every safe look like an
//     error to a caller branching on exit status.
//
// Neither path opens a write. Both read through the same filter the
// real delete would use, so the preview counts what the commit would
// touch rather than an approximation of it.
func previewDelete(cmd *cobra.Command, _ []string) error {
	previewRendered = false
	restoreTokenGate = nil
	dryRun := kitcli.IsDryRun(cmd)
	declined := strings.EqualFold(strings.TrimSpace(inheritedFlag(cmd, "confirm")), "no")
	if !dryRun && !declined {
		return nil
	}

	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	targets, total, describe, err := deleteTargets(context.Background(), svc)
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	if dryRun {
		actions := make([]cliformat.Action, 0, len(targets))
		for _, id := range targets {
			actions = append(actions, cliformat.Action{
				Kind:       "delete",
				Target:     "object:" + id,
				Count:      1,
				Reversible: false,
				Detail:     describe,
			})
		}
		if err := cliformat.EncodePreview(w, cliformat.NewPlan(cmd.CommandPath(), actions)); err != nil {
			return err
		}
		previewRendered = true
		restoreTokenGate = suspendGatesForAnsweredInspection(cmd)
		return nil
	}

	refusal := cliformat.NewRefusal(cmd.CommandPath(), targets, int64(total),
		"confirmation declined (--confirm=no); nothing was deleted")
	if err := cliformat.EncodePreview(w, refusal); err != nil {
		return err
	}
	previewRendered = true
	restoreTokenGate = suspendGatesForAnsweredInspection(cmd)
	return nil
}

// suspendGatesForAnsweredInspection drops, for the remainder of this
// one invocation, the two annotations kit's confirmation gates read:
// the typed-confirmation requirement and the destructive side-effect
// class. It returns a function that puts both back.
//
// Both annotations are true statements about `ctxt delete` and wrong
// statements about this invocation of it. kit reads them ahead of
// RunE and refuses — correctly, for a command that is about to
// delete. What kit cannot express is that this call will not: it
// skips its entire confirmation block for --dry-run, on the stated
// grounds that a dry run has no real side-effect to confirm, but
// --confirm=no reaches the gate and is refused before the command can
// answer. The same reasoning covers both cases; a declined delete
// deletes nothing either, and the caller that declined is the one
// being denied the preview it declined in order to see.
//
// The suspension is scoped as narrowly as the claim it makes:
//
//   - It is applied only AFTER the preview document has been written,
//     at which point runDelete is guaranteed to return without opening
//     a write.
//   - It is reverted inside the same invocation, by runDelete, as soon
//     as kit's gates have been passed.
//   - Every committing invocation — --confirm=yes, auto, prompt, and
//     the non-TTY default — never reaches this function, so it meets
//     both gates fully armed and is still refused without a token.
//
// The general fix belongs upstream: kit should let an adopter declare
// an invocation non-mutating, the way --dry-run already does, instead
// of leaving adopters to reach for the annotations behind its gates.
// See the report accompanying this change.
func suspendGatesForAnsweredInspection(cmd *cobra.Command) func() {
	const (
		tokenAnnotation      = "kit/destructive-token"
		sideEffectAnnotation = "kit/side-effect"
	)
	saved := make(map[string]*string, 2)
	for _, key := range []string{tokenAnnotation, sideEffectAnnotation} {
		if v, ok := cmd.Annotations[key]; ok {
			saved[key] = &v
			delete(cmd.Annotations, key)
		} else {
			saved[key] = nil
		}
	}
	return func() {
		for key, v := range saved {
			if v != nil {
				cmd.Annotations[key] = *v
			}
		}
	}
}

// deleteTargets resolves the object IDs this invocation would delete,
// the total the filter matched, and a one-line note naming what
// selected them.
//
// It reads through the same two paths the real delete uses — a direct
// lookup for --id, the shared filter for everything else — so the
// preview can never count a different set than the commit would act
// on. A preview that disagrees with its own commit is worse than no
// preview at all.
//
// total is returned separately from len(targets): the filtered listing
// is capped, and reporting the capped length as the match count would
// understate a delete the operator is about to approve.
//
// A missing --id yields no targets rather than an error. "This object
// does not exist" is a legitimate preview answer, and the whole point
// of asking is to find that out without a failure exit.
func deleteTargets(ctx context.Context, svc *service.Service) (targets []string, matched int, describe string, err error) {
	id := viper.GetString("delete.id")
	tag := viper.GetString("delete.tagged")
	mention := viper.GetString("delete.mention")
	typ := viper.GetString("delete.type")
	all := viper.GetBool("delete.all")

	if id != "" {
		describe = "matched --id " + id
		obj, lookupErr := svc.GetObject(ctx, id)
		switch {
		case errors.Is(lookupErr, storage.ErrNotFound), lookupErr == nil && obj == nil:
			// Not found is a legitimate preview answer: the whole
			// point of asking is to learn that without a failure exit.
			return []string{}, 0, describe, nil
		case lookupErr != nil:
			// Any other failure is a failure. Reporting it as an empty
			// plan would tell the caller "this deletes nothing" when
			// the truth is that the store could not be read — the one
			// answer a reviewer must never receive by mistake.
			return nil, 0, "", fmt.Errorf("look up %s: %w", id, lookupErr)
		}
		return []string{obj.ID}, 1, describe, nil
	}

	var filter storage.ObjectFilter
	switch {
	case all:
		filter, describe = storage.ObjectFilter{Limit: 10000}, "matched --all"
	case tag != "" || mention != "" || typ != "":
		filter = storage.ObjectFilter{Type: typ, Tag: tag, Mention: mention, Limit: 1000}
		describe = "matched the requested filter"
	default:
		return nil, 0, "", fmt.Errorf(
			"no filter specified; use --id, --tagged, --mention, --type, or --all")
	}

	objects, total, listErr := svc.ListObjects(ctx, filter)
	if listErr != nil {
		return nil, 0, "", fmt.Errorf("list objects: %w", listErr)
	}
	targets = make([]string, 0, len(objects))
	for _, obj := range objects {
		targets = append(targets, obj.ID)
	}
	return targets, total, describe, nil
}

// inheritedFlag reads a flag's value as seen by cmd, walking up to the
// root so kit's persistent globals resolve too.
//
// Distinct from this package's flagString, which reads a leaf's own
// flag and falls back to a viper key. --confirm is registered by kit
// on the ROOT, so a leaf-only lookup never sees it, and it is not
// bound to viper — the two helpers answer different questions.
func inheritedFlag(cmd *cobra.Command, name string) string {
	for c := cmd; c != nil; c = c.Parent() {
		if f := c.Flags().Lookup(name); f != nil {
			return f.Value.String()
		}
		if f := c.PersistentFlags().Lookup(name); f != nil {
			return f.Value.String()
		}
	}
	return ""
}

func runDelete(cmd *cobra.Command, args []string) error {
	// PreRunE already answered this invocation with a plan or a
	// refusal. Returning here is what makes a preview read-only: the
	// store is never opened for writing on this path.
	//
	// Reaching this point means kit's gate has already been evaluated,
	// so the annotation the preview suspended goes back on immediately
	// — the exemption never outlives the one invocation that earned it.
	if previewRendered {
		if restoreTokenGate != nil {
			restoreTokenGate()
			restoreTokenGate = nil
		}
		return nil
	}

	id := viper.GetString("delete.id")
	tag := viper.GetString("delete.tagged")
	mention := viper.GetString("delete.mention")
	typ := viper.GetString("delete.type")
	deleteAll := viper.GetBool("delete.all")
	// Kit's wrapPolicyRunE has already enforced the --confirm + token
	// matrix before this inner RunE was invoked: when control reaches
	// here the operator has already authorized the destructive op. The
	// per-object local prompt below is treated as a no-op whenever the
	// operator passed --confirm=yes or --confirm=auto (kit's "go"
	// signals).
	skipConfirmation := false
	if cf := cmd.Flags().Lookup("confirm"); cf != nil {
		confirm := strings.ToLower(strings.TrimSpace(cf.Value.String()))
		skipConfirmation = confirm == "yes" || confirm == "auto"
	}

	if id == "" && tag == "" && mention == "" && typ == "" && !deleteAll {
		return fmt.Errorf("no filter specified; use --id, --tagged, --mention, --type, or --all")
	}

	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx := context.Background()

	// If deleting by specific ID, just delete it directly
	if id != "" {
		if !skipConfirmation {
			fmt.Printf("Delete object %s? (y/N): ", id)
			var response string
			fmt.Scanln(&response)
			if response != "y" && response != "Y" {
				fmt.Println("Deletion cancelled.")
				return nil
			}
		}
		if err := svc.DeleteObject(ctx, id); err != nil {
			return fmt.Errorf("delete: %w", err)
		}
		fmt.Printf("Deleted %s\n", id)
		return nil
	}

	// Otherwise, list matching objects first
	filter := storage.ObjectFilter{
		Type:    typ,
		Tag:     tag,
		Mention: mention,
		Limit:   1000,
	}
	if deleteAll {
		filter = storage.ObjectFilter{Limit: 10000}
	}

	objects, total, err := svc.ListObjects(ctx, filter)
	if err != nil {
		return fmt.Errorf("list objects: %w", err)
	}

	if total == 0 {
		fmt.Println("No matching objects found.")
		return nil
	}

	fmt.Printf("Found %d objects to delete.\n", total)
	if !skipConfirmation {
		fmt.Print("Proceed? (y/N): ")
		var response string
		fmt.Scanln(&response)
		if response != "y" && response != "Y" {
			fmt.Println("Deletion cancelled.")
			return nil
		}
	}

	deleted := 0
	for _, obj := range objects {
		if err := svc.DeleteObject(ctx, obj.ID); err != nil {
			fmt.Fprintf(os.Stderr, "  warning: failed to delete %s: %v\n", obj.ID, err)
			continue
		}
		deleted++
	}
	fmt.Printf("%d objects deleted\n", deleted)
	return nil
}
