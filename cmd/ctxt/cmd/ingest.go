package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/ingest"
	"github.com/ideacrafterslabs/ctxt/internal/ingest/cardamum"
	"github.com/spf13/cobra"
)

var ingestCmd = &cobra.Command{
	Use:   "ingest",
	Short: "Ingest objects from external adapters",
	Long: `Ingest reads structured objects from an adapter and
stores them in the local knowledge base with deduplication.

Adapters are named sources that emit JSON arrays of objects.
Built-in adapters: cardamum.

Examples:
  # Ingest contacts from cardamum
  ctxt ingest --source cardamum --addressbook default

  # Pipe adapter output via stdin
  my-adapter | ctxt ingest --source my-adapter --stdin

  # Watch mode: poll every 5 minutes
  ctxt ingest --source cardamum --addressbook default --watch --interval 5m`,
	RunE: runIngest,
}

func init() {
	rootCmd.AddCommand(ingestCmd)

	ingestCmd.Flags().String("source", "", "adapter name (required)")
	ingestCmd.Flags().Bool("stdin", false, "read JSON array from stdin")
	ingestCmd.Flags().Bool("watch", false, "poll adapter on interval")
	ingestCmd.Flags().Duration("interval", 5*time.Minute, "poll interval for --watch")
	ingestCmd.Flags().String("type", "", "override object type")

	// cardamum-specific flags
	ingestCmd.Flags().String("addressbook", "default", "cardamum addressbook ID")
	ingestCmd.Flags().String("account", "", "cardamum account name")
	ingestCmd.Flags().String("binary", "", "path to adapter binary")

	_ = ingestCmd.MarkFlagRequired("source")
}

func runIngest(cmd *cobra.Command, _ []string) error {
	source, _ := cmd.Flags().GetString("source")
	fromStdin, _ := cmd.Flags().GetBool("stdin")
	watch, _ := cmd.Flags().GetBool("watch")
	interval, _ := cmd.Flags().GetDuration("interval")

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

	if !watch {
		res, err := runner.Run(cmd.Context(), adapter)
		if err != nil {
			return err
		}
		printIngestResult(res)
		return nil
	}

	// Watch mode: poll on interval until interrupted.
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
	defer stop()

	fmt.Fprintf(os.Stderr, "watching %s every %s (ctrl-c to stop)\n",
		source, interval)
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
		case <-time.After(interval):
		}
	}
}

func buildAdapter(cmd *cobra.Command, source string) (ingest.Adapter, error) {
	switch source {
	case "cardamum":
		return buildCardamumAdapter(cmd)
	default:
		return nil, fmt.Errorf("unknown adapter: %s", source)
	}
}

func buildCardamumAdapter(cmd *cobra.Command) (ingest.Adapter, error) {
	addressbook, _ := cmd.Flags().GetString("addressbook")
	account, _ := cmd.Flags().GetString("account")
	binary, _ := cmd.Flags().GetString("binary")

	var opts []cardamum.Option
	if binary != "" {
		opts = append(opts, cardamum.WithBinary(binary))
	}
	if account != "" {
		opts = append(opts, cardamum.WithAccount(account))
	}

	return cardamum.New(addressbook, opts...), nil
}

func printIngestResult(res *ingest.Result) {
	fmt.Printf("source: %s | total: %d | created: %d | skipped: %d | errors: %d | %s\n",
		res.Source, res.Total, res.Created, res.Skipped, res.Errors, res.Elapsed.Round(time.Millisecond))
}

// IngestRegistry returns the default adapter registry with built-in
// adapters. Used by tests to verify registration.
func IngestRegistry() *ingest.Registry {
	reg := ingest.NewRegistry()
	reg.Register("cardamum", func(args []string) (ingest.Adapter, error) {
		ab := "default"
		if len(args) > 0 {
			ab = args[0]
		}
		return cardamum.New(ab), nil
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
