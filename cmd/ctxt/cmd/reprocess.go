package cmd

import (
	"fmt"
	"os"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/dpkmsclient"
	"github.com/spf13/cobra"
)

var reprocessCmd = &cobra.Command{
	Use:   "reprocess <id>",
	Short: "Re-run an enrichment step on an existing object",
	Long: `Queue a job on the dpkms instance that runs one enrichment step against
an already-ingested object and writes the result back to it.

The step runs where dpkms runs, with that instance's providers and
configuration; ctxt only queues it and prints the job ID. Useful for
backfilling structured metadata on objects ingested before a step was
added to the pipeline. Needs a token with write:objects (the writer or
admin role).

Steps: structured_metadata (the default), entity_extractor, tagger.

Examples:
  ctxt reprocess abc123 --step structured_metadata
  ctxt reprocess abc123 --step entity_extractor`,
	Args: cobra.ExactArgs(1),
	RunE: runReprocess,
}

func init() {
	rootCmd.AddCommand(reprocessCmd)
	cliconv.WithSideEffect(reprocessCmd, cliconv.SideEffectWrite)
	cliconv.WithExamples(reprocessCmd, []cliconv.Example{
		{Title: "Backfill structured metadata", Command: "ctxt reprocess obj_abc123 --step structured_metadata"},
		{Title: "Re-run entity extraction", Command: "ctxt reprocess obj_abc123 --step entity_extractor"},
		{Title: "Re-run tagger", Command: "ctxt reprocess obj_abc123 --step tagger"},
	})
	cliconv.WithNextSteps(reprocessCmd, []cliconv.NextStep{
		{When: "once the job completes", Suggest: "ctxt show <id>", Reason: "confirm the updated enrichment fields"},
	})
	// "reprocess" is in kit's defaultIdempotency table (yes); no override
	// needed.
	reprocessCmd.Flags().String("step", "structured_metadata",
		"enrichment step to run (structured_metadata|entity_extractor|tagger)")
	reprocessCmd.Flags().String("server", "", serverFlagUsage)
}

func runReprocess(cmd *cobra.Command, args []string) error {
	objID := args[0]
	step, _ := cmd.Flags().GetString("step")

	client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
	if err != nil {
		return err
	}
	jobID, err := client.ReprocessObject(cmd.Context(), objID, step)
	if err != nil {
		return err
	}

	if isJSONOutput() {
		return outputJSON(os.Stdout, map[string]any{
			"id":     objID,
			"step":   step,
			"job_id": jobID,
			"status": "queued",
		})
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Queued %s on %s: job %s\n", step, objID, jobID)
	return nil
}
