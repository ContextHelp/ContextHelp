package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/adapter/contacts/cardamum"
	"github.com/ideacrafterslabs/ctxt/internal/adapter/email/himalaya"
	"github.com/ideacrafterslabs/ctxt/internal/adapter/legacy"
	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/ingest"
	"github.com/spf13/cobra"
)

var ingestCmd = &cobra.Command{
	Use:   "ingest",
	Short: "Ingest objects from external adapters",
	Long: `Ingest reads structured objects from an adapter and
stores them in the local knowledge base with deduplication.

Adapters are named sources that emit JSON arrays of objects.
Built-in adapters: cardamum, himalaya.

Examples:
  # Ingest contacts from cardamum
  ctxt ingest --source cardamum --addressbook default

  # Pipe adapter output via stdin
  my-adapter | ctxt ingest --source my-adapter --stdin

  # Continuous mode: poll every 5 minutes
  ctxt ingest --source cardamum --addressbook default --every 5m`,
	RunE: runIngest,
}

func init() {
	rootCmd.AddCommand(ingestCmd)
	cliconv.WithSideEffect(ingestCmd, cliconv.SideEffectWrite)
	cliconv.WithExamples(ingestCmd, []cliconv.Example{
		{Title: "Ingest contacts from cardamum", Command: "ctxt ingest --source cardamum --addressbook default"},
		{Title: "Pipe adapter output via stdin", Command: "my-adapter | ctxt ingest --source my-adapter --stdin"},
		{Title: "Continuous mode (poll every 5m)", Command: "ctxt ingest --source cardamum --every 5m"},
	})
	cliconv.WithNextSteps(ingestCmd, []cliconv.NextStep{
		{When: "on success", Suggest: "ctxt list", Reason: "browse the freshly ingested objects"},
		{When: "if a job stalled", Suggest: "ctxt job status", Reason: "inspect the queue"},
	})
	// "ingest" is not in kit's defaultIdempotency table; the runner
	// dedups against existing objects, so re-ingesting the same adapter
	// payload converges instead of growing the graph. Mark idempotent.
	cliconv.WithIdempotency(ingestCmd, cliconv.IdempotencyYes)

	ingestCmd.Flags().String("source", "", "adapter name (required)")
	ingestCmd.Flags().Bool("stdin", false, "read JSON array from stdin")
	ingestCmd.Flags().Duration("every", 0, "continuous mode: re-poll on this cadence (one-shot when absent)")
	ingestCmd.Flags().String("type", "", "override object type")

	// cardamum-specific flags
	ingestCmd.Flags().String("addressbook", "default", "cardamum addressbook ID")
	ingestCmd.Flags().String("account", "", "adapter account name")
	ingestCmd.Flags().String("binary", "", "path to adapter binary")

	// himalaya-specific flags
	ingestCmd.Flags().String("folder", "INBOX", "himalaya IMAP folder")
	ingestCmd.Flags().Int("max-items", 0, "max items to fetch (0=all)")

	_ = ingestCmd.MarkFlagRequired("source")
}

func runIngest(cmd *cobra.Command, _ []string) error {
	source, _ := cmd.Flags().GetString("source")
	fromStdin, _ := cmd.Flags().GetBool("stdin")
	every, _ := cmd.Flags().GetDuration("every")

	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	runner := ingest.NewRunner(svc.Store.Objects())

	if fromStdin {
		res, err := runner.RunFromStdin(cmd.Context(), source, os.Stdin)
		if err != nil {
			return err
		}
		printIngestResult(res)
		return nil
	}

	adapter, err := buildAdapter(cmd, source)
	if err != nil {
		return err
	}

	if every <= 0 {
		res, err := runner.Run(cmd.Context(), adapter)
		if err != nil {
			return err
		}
		printIngestResult(res)
		return nil
	}

	// Continuous mode: re-poll the adapter on the cadence until interrupted.
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
	defer stop()

	fmt.Fprintf(os.Stderr, "ingesting %s every %s (ctrl-c to stop)\n",
		source, every)
	for {
		res, err := runner.Run(ctx, adapter)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
		} else {
			printIngestResult(res)
		}

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(every):
		}
	}
}

func buildAdapter(cmd *cobra.Command, source string) (ingest.Adapter, error) {
	switch source {
	case "cardamum":
		return buildCardamumAdapter(cmd)
	case "himalaya":
		return buildHimalayaAdapter(cmd)
	default:
		return nil, fmt.Errorf("unknown adapter: %s", source)
	}
}

func buildCardamumAdapter(cmd *cobra.Command) (ingest.Adapter, error) {
	addressbook, _ := cmd.Flags().GetString("addressbook")
	account, _ := cmd.Flags().GetString("account")
	binary, _ := cmd.Flags().GetString("binary")

	typed := cardamum.New(addressbook, cardamum.Config{
		Account: account,
		Binary:  binary,
	})
	return legacy.AsLegacy(typed), nil
}

func buildHimalayaAdapter(cmd *cobra.Command) (ingest.Adapter, error) {
	account, _ := cmd.Flags().GetString("account")
	folder, _ := cmd.Flags().GetString("folder")
	binary, _ := cmd.Flags().GetString("binary")
	maxItems, _ := cmd.Flags().GetInt("max-items")

	typed := himalaya.New(himalaya.Config{
		Account:  account,
		Folder:   folder,
		MaxItems: maxItems,
		Binary:   binary,
	})
	return legacy.AsLegacy(typed), nil
}

func printIngestResult(res *ingest.Result) {
	fmt.Printf("source: %s | total: %d | created: %d | skipped: %d | errors: %d | %s\n",
		res.Source, res.Total, res.Created, res.Skipped, res.Errors, res.Elapsed.Round(time.Millisecond))
}

// IngestRegistry returns the default adapter registry with built-in
// adapters. Used by tests to verify registration. Both built-ins now
// route through the typed substrate via legacy.AsLegacy so the
// registry sees the same surface but the underlying impl uses the
// new internal/adapter contract.
func IngestRegistry() *ingest.Registry {
	reg := ingest.NewRegistry()
	reg.Register("cardamum", func(args []string) (ingest.Adapter, error) {
		ab := "default"
		if len(args) > 0 {
			ab = args[0]
		}
		return legacy.AsLegacy(cardamum.New(ab, cardamum.Config{})), nil
	})
	reg.Register("himalaya", func(args []string) (ingest.Adapter, error) {
		cfg := himalaya.Config{}
		if len(args) > 0 {
			cfg.Account = args[0]
		}
		return legacy.AsLegacy(himalaya.New(cfg)), nil
	})
	return reg
}

// resolveContext returns the command context or a background context.
func resolveContext(cmd *cobra.Command) context.Context {
	if ctx := cmd.Context(); ctx != nil {
		return ctx
	}
	return context.Background()
}
