package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/spf13/cobra"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
)

var workerCmd = &cobra.Command{
	Use:    "worker",
	Short:  "Internal worker utilities",
	Hidden: true,
}

var workerRunDraftCmd = &cobra.Command{
	Use:   "run-draft",
	Short: "Run Stage 1 ingestion pipeline on a JSON job request without opening storage",
	RunE:  runWorkerRunDraft,
}

func init() {
	workerRunDraftCmd.Flags().String("input", "", "path to JSON DraftJobRequest file (default: read from stdin)")
	cliconv.WithSideEffect(workerRunDraftCmd, cliconv.SideEffectRead)
	workerCmd.AddCommand(workerRunDraftCmd)
	rootCmd.AddCommand(workerCmd)
}

func runWorkerRunDraft(cmd *cobra.Command, _ []string) error {
	inputPath, _ := cmd.Flags().GetString("input")
	var raw []byte
	var err error
	if inputPath != "" {
		raw, err = os.ReadFile(inputPath) // #nosec G304 -- explicit CLI --input file
	} else {
		raw, err = io.ReadAll(cmd.InOrStdin())
	}
	if err != nil {
		return fmt.Errorf("read draft request: %w", err)
	}

	var req jobs.DraftJobRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return encodeDraftResult(cmd.OutOrStdout(), jobs.DraftJobResult{
			Permanent: true,
			Error:     fmt.Sprintf("invalid draft request JSON: %v", err),
		})
	}

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	// Keep builtins pruning/info log lines off stdout so stdout carries pure JSON.
	prevLogOut := log.Writer()
	log.SetOutput(cmd.ErrOrStderr())
	defer log.SetOutput(prevLogOut)

	draft, err := jobs.RunDraft(ctx, builtins.Registry(), &req.Job, req.MaxHops)
	if err != nil {
		return encodeDraftResult(cmd.OutOrStdout(), jobs.DraftJobResult{
			Permanent: pipeline.IsPermanent(err),
			Error:     err.Error(),
		})
	}
	return encodeDraftResult(cmd.OutOrStdout(), jobs.DraftJobResult{Draft: draft})
}

func encodeDraftResult(w io.Writer, res jobs.DraftJobResult) error {
	enc := json.NewEncoder(w)
	return enc.Encode(res)
}
