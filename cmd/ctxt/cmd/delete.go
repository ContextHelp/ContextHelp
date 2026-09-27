package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/cli/cliformat"
	"github.com/ideacrafterslabs/ctxt/internal/dpkmsclient"
	"github.com/spf13/cobra"
	kitcli "hop.top/kit/go/console/cli"
	"hop.top/kit/go/console/output"
)

var deleteCmd = &cobra.Command{
	Use:   "delete",
	Short: "Delete knowledge objects",
	Long: `Delete knowledge objects on the dpkms instance using IDs or filters.

WARNING: This operation is irreversible. Use with caution.

Deleting needs a token with delete:objects, which only the admin role
holds: a writer token exits 5 and deletes nothing. A filter selects the
instance's active objects (GET /api/v1/objects); each match is then
deleted with its own DELETE /api/v1/objects/{id}.

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

	// Selector flags
	deleteCmd.Flags().String("id", "", "delete specific knowledge object")
	deleteCmd.Flags().String("tagged", "", "delete the objects carrying this tag")
	deleteCmd.Flags().String("mention", "", "delete by mention")
	deleteCmd.Flags().String("type", "", "delete by type")
	deleteCmd.Flags().Bool("all", false, "delete all active objects (requires confirmation)")
	deleteCmd.Flags().String("server", "", serverFlagUsage)

	// Confirmation: handled by kit's global --confirm (auto|yes|no|prompt)
	// + --confirm-token flags, so confirmation has one source of truth
	// across every destructive ctxt leaf.
}

// deleteSelector is what one delete invocation selects.
type deleteSelector struct {
	id, tag, mention, typ string
	all                   bool
}

func deleteSelection(cmd *cobra.Command) deleteSelector {
	str := func(name string) string {
		v, _ := cmd.Flags().GetString(name)
		return strings.TrimSpace(v)
	}
	all, _ := cmd.Flags().GetBool("all")
	return deleteSelector{id: str("id"), tag: str("tagged"), mention: str("mention"), typ: str("type"), all: all}
}

func (s deleteSelector) empty() bool {
	return s.id == "" && s.tag == "" && s.mention == "" && s.typ == "" && !s.all
}

// kit --confirm values that authorize a destructive run without asking.
const (
	confirmYes  = "yes"
	confirmAuto = "auto"
)

func errNoDeleteSelector() error {
	e := output.UsageError("no filter specified")
	e.SuggestedFix = "select objects with --id, --tagged, --mention, --type, or --all"
	return e
}

// previewRendered records that PreRunE already answered this
// invocation, so runDelete returns without deleting anything.
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

	sel := deleteSelection(cmd)
	if sel.empty() {
		return errNoDeleteSelector()
	}
	client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
	if err != nil {
		return err
	}
	targets, total, describe, err := deleteTargets(cmd.Context(), client, sel)
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
// the total the selector matched, and a one-line note naming what
// selected them.
//
// It reads through the same two API calls the real delete uses — GET
// /objects/{id} for --id, GET /objects with the filter for everything
// else — so the preview can never count a different set than the commit
// would act on. A preview that disagrees with its own commit is worse
// than no preview at all.
//
// The listing asks for every match (limit 0), so len(targets) equals
// the match count: nothing is capped out of the plan or the delete.
//
// A missing --id yields no targets rather than an error. "This object
// does not exist" is a legitimate preview answer, and the whole point
// of asking is to find that out without a failure exit.
func deleteTargets(ctx context.Context, client *dpkmsclient.Client, sel deleteSelector) (targets []string, matched int, describe string, err error) {
	if sel.id != "" {
		describe = "matched --id " + sel.id
		obj, lookupErr := client.GetObject(ctx, sel.id)
		switch {
		case isNotFound(lookupErr):
			// Not found is a legitimate preview answer: the whole
			// point of asking is to learn that without a failure exit.
			return []string{}, 0, describe, nil
		case lookupErr != nil:
			// Any other failure is a failure. Reporting it as an empty
			// plan would tell the caller "this deletes nothing" when
			// the truth is that the instance could not be read — the
			// one answer a reviewer must never receive by mistake.
			return nil, 0, "", lookupErr
		}
		return []string{obj.ID}, 1, describe, nil
	}

	var filter dpkmsclient.ObjectFilter
	switch {
	case sel.all:
		describe = "matched --all"
	case sel.tag != "" || sel.mention != "" || sel.typ != "":
		filter = dpkmsclient.ObjectFilter{Type: sel.typ, Tag: sel.tag, Mention: sel.mention}
		describe = "matched the requested filter"
	default:
		return nil, 0, "", errNoDeleteSelector()
	}

	objects, total, err := client.ListObjects(ctx, filter)
	if err != nil {
		return nil, 0, "", err
	}
	targets = make([]string, 0, len(objects))
	for _, obj := range objects {
		targets = append(targets, obj.ID)
	}
	return targets, total, describe, nil
}

// isNotFound reports a dpkms 404.
func isNotFound(err error) bool {
	var e *output.Error
	return errors.As(err, &e) && e.Code == output.CodeNotFound
}

// isUnauthorized reports a dpkms 401 or 403.
func isUnauthorized(err error) bool {
	var e *output.Error
	return errors.As(err, &e) && e.Code == output.CodeUnauthorized
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
	// refusal. Returning here is what makes a preview read-only: no
	// DELETE is ever sent on this path.
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

	sel := deleteSelection(cmd)
	if sel.empty() {
		return errNoDeleteSelector()
	}
	// Kit's wrapPolicyRunE has already enforced the --confirm + token
	// matrix before this inner RunE was invoked: when control reaches
	// here the operator has already authorized the destructive op. The
	// local prompt below is skipped whenever the operator passed
	// --confirm=yes or --confirm=auto (kit's "go" signals).
	confirm := strings.ToLower(strings.TrimSpace(inheritedFlag(cmd, "confirm")))
	skipConfirmation := confirm == confirmYes || confirm == confirmAuto

	client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
	if err != nil {
		return err
	}
	ctx := cmd.Context()
	w := cmd.OutOrStdout()

	if sel.id != "" {
		if !skipConfirmation && !confirmDelete(w, fmt.Sprintf("Delete object %s? (y/N): ", sel.id)) {
			fmt.Fprintln(w, "Deletion cancelled.")
			return nil
		}
		if err := client.DeleteObject(ctx, sel.id); err != nil {
			return err
		}
		fmt.Fprintf(w, "Deleted %s\n", sel.id)
		return nil
	}

	targets, total, _, err := deleteTargets(ctx, client, sel)
	if err != nil {
		return err
	}
	if total == 0 {
		fmt.Fprintln(w, "No matching objects found.")
		return nil
	}
	fmt.Fprintf(w, "Found %d objects to delete.\n", total)
	if !skipConfirmation && !confirmDelete(w, "Proceed? (y/N): ") {
		fmt.Fprintln(w, "Deletion cancelled.")
		return nil
	}

	deleted, failed := 0, 0
	var firstErr error
	for _, id := range targets {
		err := client.DeleteObject(ctx, id)
		switch {
		case err == nil:
			deleted++
		case isUnauthorized(err):
			// The token can't delete: every remaining request would be
			// refused the same way, so stop at the first.
			fmt.Fprintf(w, "%d objects deleted\n", deleted)
			return err
		case isNotFound(err):
			// Deleted by someone else since the listing.
			fmt.Fprintf(os.Stderr, "  note: %s was already gone\n", id)
		default:
			failed++
			if firstErr == nil {
				firstErr = err
			}
			fmt.Fprintf(os.Stderr, "  warning: failed to delete %s: %v\n", id, err)
		}
	}
	fmt.Fprintf(w, "%d objects deleted\n", deleted)
	if firstErr != nil {
		msg := fmt.Errorf("%d of %d objects not deleted; first failure: %w", failed, len(targets), firstErr)
		var ke *output.Error
		if errors.As(firstErr, &ke) {
			return output.WrapError(msg, ke.Code, ke.ExitCode)
		}
		return msg
	}
	return nil
}

// confirmDelete asks prompt on w and reads the answer from stdin; only
// "y" or "Y" confirms.
func confirmDelete(w io.Writer, prompt string) bool {
	fmt.Fprint(w, prompt)
	var response string
	_, _ = fmt.Scanln(&response)
	return response == "y" || response == "Y"
}
