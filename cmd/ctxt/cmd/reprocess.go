package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/steps"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/spf13/cobra"
)

// knownEnrichSteps lists steps available for on-demand enrichment.
var knownEnrichSteps = map[string]bool{
	"structured_metadata": true,
	"entity_extractor":    true,
	"tagger":              true,
}

var reprocessCmd = &cobra.Command{
	Use:   "reprocess <id>",
	Short: "Re-run enrichment step on an existing object",
	Long: `Run a specific enrichment step against an already-ingested object.

The step reads the object's content, extracts metadata, and updates
the object in storage. Useful for backfilling structured metadata on
objects ingested before a step was added to the pipeline.

Examples:
  ctxt reprocess abc123 --step structured-metadata
  ctxt reprocess abc123 --step entity_extractor`,
	Args: cobra.ExactArgs(1),
	RunE: runReprocess,
}

func init() {
	rootCmd.AddCommand(reprocessCmd)
	cliconv.WithSideEffect(reprocessCmd, cliconv.SideEffectWrite)
	// "reprocess" is in kit's defaultIdempotency table (yes); no override
	// needed.
	reprocessCmd.Flags().String("step", "structured_metadata",
		"enrichment step to run (structured_metadata|entity_extractor|tagger)")
}

func runReprocess(cmd *cobra.Command, args []string) error {
	objID := args[0]
	stepName, _ := cmd.Flags().GetString("step")

	// Normalize: accept both hyphen and underscore forms.
	if stepName == "structured-metadata" {
		stepName = "structured_metadata"
	}

	if !knownEnrichSteps[stepName] {
		return fmt.Errorf("unknown step %q; choose from: structured_metadata, entity_extractor, tagger", stepName)
	}

	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx := context.Background()

	obj, err := svc.GetObject(ctx, objID)
	if err != nil {
		return fmt.Errorf("get object %q: %w", objID, err)
	}

	factory := providers.NewFactory(cfg.Providers, nil)

	switch stepName {
	case "structured_metadata":
		s := steps.NewStructuredMetadataExtractorWithLLM(factory.LLM())
		result, err := s.Run(ctx, obj)
		if err != nil {
			return fmt.Errorf("step %q: %w", stepName, err)
		}
		obj = result
	case "entity_extractor":
		s := steps.NewEntityExtractorWithLLM(factory.LLM())
		result, err := s.Run(ctx, obj)
		if err != nil {
			return fmt.Errorf("step %q: %w", stepName, err)
		}
		obj = result
	case "tagger":
		s := steps.NewTaggerWithLLM(factory.LLM())
		result, err := s.Run(ctx, obj)
		if err != nil {
			return fmt.Errorf("step %q: %w", stepName, err)
		}
		obj = result
	}

	if err := svc.UpdateObject(ctx, obj); err != nil {
		return fmt.Errorf("update object %q: %w", objID, err)
	}

	if isJSONOutput() {
		return outputJSON(os.Stdout, map[string]any{
			"id":     objID,
			"step":   stepName,
			"status": "complete",
		})
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Enriched %s with %s\n", objID, stepName)
	return nil
}
