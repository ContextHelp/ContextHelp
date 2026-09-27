package cmd

import (
	"fmt"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/dpkmsclient"
	"github.com/spf13/cobra"
	"hop.top/kit/go/console/output"
)

var editCmd = &cobra.Command{
	Use:   "edit <id>",
	Short: "Modify knowledge object metadata",
	Long: `Edit metadata for a knowledge object on the dpkms instance.

Editable fields are title, summary, tags, mentions and subtype. The
title is the object's first summary: --title replaces the summaries
with the new title, --summary replaces the first summary and keeps the
others, so the two can't be combined.

The edit goes to the instance through PATCH /api/v1/objects/{id} and
needs a token with write:objects (the writer or admin role).

Examples:
  # Update title
  ctxt edit obj_12345678 --title "New title"

  # Update tags
  ctxt edit obj_12345678 --tags "ux,onboarding,critical"

  # Update mentions
  ctxt edit obj_12345678 --mention "@ui.best-practice @ux.onboarding"

  # Update multiple fields
  ctxt edit obj_12345678 --title "New title" --tags "ux" --subtype "article"`,
	Args: cobra.ExactArgs(1),
	RunE: runEdit,
}

func init() {
	rootCmd.AddCommand(editCmd)
	cliconv.WithSideEffect(editCmd, cliconv.SideEffectWrite)
	cliconv.WithExamples(editCmd, []cliconv.Example{
		{Title: "Update the title", Command: "ctxt edit obj_12345678 --title \"New title\""},
		{Title: "Replace tags", Command: "ctxt edit obj_12345678 --tags ux,onboarding,critical"},
		{Title: "Update multiple fields", Command: "ctxt edit obj_12345678 --title \"X\" --subtype article"},
	})
	cliconv.WithNextSteps(editCmd, []cliconv.NextStep{
		{When: "on success", Suggest: "ctxt show <id>", Reason: "confirm the updated metadata"},
	})
	// "edit" is in kit's defaultIdempotency table (yes); the verb default
	// matches the field-update semantics (re-applying the same update
	// converges), so no override needed.

	editCmd.Flags().String("title", "", "replace the summaries with this title")
	editCmd.Flags().String("summary", "", "replace the first summary")
	editCmd.Flags().String("tags", "", "replace tags (comma-separated)")
	editCmd.Flags().String("mention", "", "replace mentions (space-separated @namespace.slug)")
	editCmd.Flags().String("subtype", "", "update subtype")
	editCmd.Flags().String("server", "", serverFlagUsage)
}

// editField is one edited field, in the order edit reports them.
type editField struct{ name, value string }

func runEdit(cmd *cobra.Command, args []string) error {
	id := args[0]
	flag := func(name string) string {
		v, _ := cmd.Flags().GetString(name)
		return v
	}

	var patch dpkmsclient.ObjectPatch
	var fields []editField
	if v := flag("title"); v != "" {
		patch.Title = &v
		fields = append(fields, editField{"title", v})
	}
	if v := flag("summary"); v != "" {
		patch.Summary = &v
		fields = append(fields, editField{"summary", v})
	}
	if v := flag("tags"); v != "" {
		tags := strings.Split(v, ",")
		patch.Tags = &tags
		fields = append(fields, editField{"tags", v})
	}
	if v := flag("mention"); v != "" {
		ms := strings.Fields(v)
		patch.Mentions = &ms
		fields = append(fields, editField{"mentions", v})
	}
	if v := flag("subtype"); v != "" {
		patch.Subtype = &v
		fields = append(fields, editField{"subtype", v})
	}
	if len(fields) == 0 {
		e := output.UsageError("no fields to update")
		e.SuggestedFix = "pass at least one of --title, --summary, --tags, --mention, --subtype"
		return e
	}

	client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
	if err != nil {
		return err
	}
	if _, err := client.UpdateObject(cmd.Context(), id, patch); err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "Updated %s\n", id)
	for _, f := range fields {
		fmt.Fprintf(w, "  %s: %s\n", f.name, f.value)
	}
	return nil
}
