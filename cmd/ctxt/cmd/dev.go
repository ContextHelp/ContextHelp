package cmd

import (
	"context"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/spf13/cobra"
)

var devCmd = &cobra.Command{
	Use:   "dev",
	Short: "Developer and maintenance utilities",
	Long:  `Developer utilities for inspecting and maintaining the knowledge store.`,
}

var devReindexVectorsCmd = &cobra.Command{
	Use:   "reindex-vectors",
	Short: "Re-embed objects that are missing vector embeddings",
	Long: `Re-embed all active knowledge objects that currently lack a stored embedding
vector. Uses the configured embedding provider (Ollama by default).

Objects with no raw content or summaries are skipped and counted as failed.

Examples:
  # Re-index all objects missing embeddings
  ctxt dev reindex-vectors

  # JSON output (indexed/failed counts + object IDs)
  ctxt dev reindex-vectors --output json`,
	RunE: runDevReindexVectors,
}

func init() {
	rootCmd.AddCommand(devCmd)
	devCmd.AddCommand(devReindexVectorsCmd)
}

func runDevReindexVectors(cmd *cobra.Command, args []string) error {
	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx := context.Background()

	factory := providers.NewFactory(cfg.Providers, nil)
	ep := factory.Embedding()

	fmt.Fprintln(cmd.OutOrStdout(), "Starting vector re-indexing...")

	indexed, failed, err := svc.ReindexVectors(ctx, ep)
	if err != nil {
		return fmt.Errorf("reindex-vectors: %w", err)
	}

	if isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), map[string]any{
			"indexed": indexed,
			"failed":  failed,
		})
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Done. indexed=%d  failed=%d\n", indexed, failed)
	return nil
}
