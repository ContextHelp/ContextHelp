package cmd

import (
	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/spf13/cobra"
)

var importCmd = &cobra.Command{
	Use:   "import",
	Short: "Import data from external sources",
	Long: `Import external knowledge sources into ContextHelp.

Use source-specific subcommands such as:
  ctxt import chrome --file bookmarks.html`,
}

func init() {
	rootCmd.AddCommand(importCmd)
}

// markImportCmd stamps the conventions every `ctxt import <source>` leaf
// shares: a conditionally idempotent write, example invocations built
// from example (plain and dry-run), and next steps to inspect the result.
func markImportCmd(cmd *cobra.Command, example string) {
	cliconv.WithSideEffect(cmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(cmd, cliconv.IdempotencyConditional)

	cliconv.WithExamples(cmd, []cliconv.Example{
		{
			Title:   "Import from default source",
			Command: example,
		},
		{
			Title:   "Dry-run preview",
			Command: example + " --confirm=no --dry-run",
		},
	})
	cliconv.WithNextSteps(cmd, []cliconv.NextStep{
		{When: "on success", Suggest: "ctxt list", Reason: "verify imported items"},
		{When: "on success", Suggest: "ctxt find <keyword>", Reason: "search the newly-imported data"},
	})
}
