package cmd

import (
	"context"
	"errors"
	"fmt"
	gohttp "net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/cli"
	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/dpkmsclient"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var analyzeCmd = &cobra.Command{
	Use:   "analyze [content]",
	Short: "Enqueue content for ingestion",
	Long: `Analyze enqueues content into the ingestion pipeline.

This does not run pipelines directly; the worker (dpkms serve) handles execution.
The content goes only to the one dpkms instance ctxt resolves (--server,
--instance, CTXT_INSTANCE, the current instance, then server.urls or
server.url). When nothing answers there the command exits 70; when the
instance rejects the token it exits 5. Nothing is queued locally and no
other instance is tried.

Content can be provided as:
  - Command argument: ctxt analyze "some text"
  - Stdin: echo "text" | ctxt analyze
  - File: ctxt analyze --file path/to/file.txt
  - Clipboard: ctxt analyze (if no argument, stdin, or file is provided)

Examples:
  # Analyze text with hints and mentions
  echo "Fix signup flow" | ctxt analyze --type text --hint "ux,bad" --mention "@ui.best-practice"

  # Analyze an image
  ctxt analyze --file screenshot.png --type image --mention "@ui.layout"

  # Analyze a URL with a focus profile
  ctxt analyze https://example.com --type url --profile growth

  # Use clipboard content
  ctxt analyze

  # Wait for job completion
  ctxt analyze --wait`,
	RunE: RunAnalyze,
}

func init() {
	rootCmd.AddCommand(analyzeCmd)
	cliconv.WithSideEffect(analyzeCmd, cliconv.SideEffectWrite)
	cliconv.WithExamples(analyzeCmd, []cliconv.Example{
		{Title: "Analyze text from an argument", Command: "ctxt analyze \"Fix signup flow\""},
		{Title: "Analyze text from stdin with hints", Command: "echo \"draft note\" | ctxt analyze --hint research"},
		{Title: "Analyze a URL and wait for the job", Command: "ctxt analyze https://example.com --type url --wait"},
	})
	cliconv.WithNextSteps(analyzeCmd, []cliconv.NextStep{
		{When: "on success", Suggest: "ctxt job status", Reason: "track the enqueued job"},
		{When: "after enrichment completes", Suggest: "ctxt find <topic>", Reason: "discover related items"},
	})
	// "analyze" is not in kit's defaultIdempotency table. Each invocation
	// is one logical submission with its own idempotency key: running the
	// command twice is two submissions and mints two jobs.
	cliconv.WithIdempotency(analyzeCmd, cliconv.IdempotencyConditional)

	// Register flags on analyzeCmd for `ctxt analyze --help`.
	analyzeCmd.Flags().String("type", "text", "input type (text|url|image|audio|video|feed|auto)")
	analyzeCmd.Flags().StringP("file", "f", "", "read input from file")
	analyzeCmd.Flags().StringSlice("hint", nil, "tagging hints attached to the captured object (repeatable, CSV)")
	analyzeCmd.Flags().String("mention", "", "explicit mentions to attach (e.g., \"@entity.slug\")")
	analyzeCmd.Flags().String("pipeline", "", "force specific pipeline")
	analyzeCmd.Flags().String("language", "", "input language override")
	analyzeCmd.Flags().String("translate", "", "translation mode (none to skip)")
	analyzeCmd.Flags().Bool("raw", false, "disable AI; store raw knowledge object")
	analyzeCmd.Flags().Bool("no-fanout", false, "skip post-ingest fan-out enrichment")
	analyzeCmd.Flags().Bool("no-dedup", false, "skip duplicate detection")
	analyzeCmd.Flags().String("source-key", "", "external dedup key (Slack ts, tweet ID, etc.)")
	analyzeCmd.Flags().Bool("wait", false, "block until job completes")
	analyzeCmd.Flags().String("server", "", serverFlagUsage)

	// Mirror flags on rootCmd (local, not persistent) so `ctxt <content> --type url` works
	// without leaking these flags into every subcommand's help.
	rootCmd.Flags().String("type", "text", "input type (text|url|image|audio|video|feed|auto)")
	rootCmd.Flags().StringP("file", "f", "", "read input from file")
	rootCmd.Flags().StringSlice("hint", nil, "tagging hints attached to the captured object (repeatable, CSV)")
	rootCmd.Flags().String("mention", "", "explicit mentions to attach (e.g., \"@entity.slug\")")
	rootCmd.Flags().String("pipeline", "", "force specific pipeline")
	rootCmd.Flags().String("language", "", "input language override")
	rootCmd.Flags().String("translate", "", "translation mode (none to skip)")
	rootCmd.Flags().Bool("raw", false, "disable AI; store raw knowledge object")
	rootCmd.Flags().Bool("no-fanout", false, "skip post-ingest fan-out enrichment")
	rootCmd.Flags().Bool("no-dedup", false, "skip duplicate detection")
	rootCmd.Flags().String("source-key", "", "external dedup key (Slack ts, tweet ID, etc.)")
	rootCmd.Flags().Bool("wait", false, "block until job completes")
	rootCmd.Flags().String("server", "", serverFlagUsage)

	// Bind viper keys: RunAnalyze reads from cmd.Flags() directly, so viper bindings
	// here are for config-file fallback only (flag values take precedence via cmd.Flags()).
	viper.BindPFlag("analyze.type", analyzeCmd.Flags().Lookup("type"))
	viper.BindPFlag("analyze.file", analyzeCmd.Flags().Lookup("file"))
	viper.BindPFlag("analyze.hint", analyzeCmd.Flags().Lookup("hint"))
	viper.BindPFlag("analyze.mention", analyzeCmd.Flags().Lookup("mention"))
	viper.BindPFlag("analyze.pipeline", analyzeCmd.Flags().Lookup("pipeline"))
	viper.BindPFlag("analyze.language", analyzeCmd.Flags().Lookup("language"))
	viper.BindPFlag("analyze.translate", analyzeCmd.Flags().Lookup("translate"))
	viper.BindPFlag("analyze.raw", analyzeCmd.Flags().Lookup("raw"))
	viper.BindPFlag("analyze.wait", analyzeCmd.Flags().Lookup("wait"))
}

// flagString reads a string flag from cmd.Flags(), falling back to viper.
func flagString(cmd *cobra.Command, name, viperKey string) string {
	if f := cmd.Flags().Lookup(name); f != nil && f.Changed {
		v, _ := cmd.Flags().GetString(name)
		return v
	}
	return viper.GetString(viperKey)
}

func RunAnalyze(cmd *cobra.Command, args []string) error {
	// The one instance this invocation talks to (see resolveEndpoint).
	client, _, err := enqueueClient(cmd)
	if err != nil {
		return err
	}

	var content string
	var source string

	// Determine input source.
	file := flagString(cmd, "file", "analyze.file")
	if file != "" {
		data, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("failed to read file: %w", err)
		}
		content = string(data)
		source = "file"
	} else {
		var err error
		content, source, err = cli.GetInput(args)
		if err != nil {
			return err
		}
	}

	if source == "clipboard" {
		fmt.Fprintf(os.Stderr, "Using content from clipboard...\n")
	}

	// Resolve --raw flag (present on both analyzeCmd and rootCmd).
	rawMode := false
	if f := cmd.Flags().Lookup("raw"); f != nil && f.Changed {
		rawMode, _ = cmd.Flags().GetBool("raw")
	} else {
		rawMode = viper.GetBool("analyze.raw")
	}

	// Resolve --no-fanout flag.
	noFanout := false
	if f := cmd.Flags().Lookup("no-fanout"); f != nil && f.Changed {
		noFanout, _ = cmd.Flags().GetBool("no-fanout")
	}

	noDedup := false
	if f := cmd.Flags().Lookup("no-dedup"); f != nil && f.Changed {
		noDedup, _ = cmd.Flags().GetBool("no-dedup")
	}

	sourceKey := flagString(cmd, "source-key", "analyze.source_key")

	// T-0190: ship `--mention "@client.acme @project.foo"` through to the
	// server so user-asserted mentions become real edges + entity rows
	// (handled by service.Analyze + jobs/worker.go). The CLI flag is
	// --mention; the server-side JSON field is still `mentions`.
	mentionsFlag := flagString(cmd, "mention", "analyze.mention")
	var userMentions []string
	if mentionsFlag != "" {
		userMentions = strings.Fields(mentionsFlag)
	}

	// T-0573: ship `--hint research,ux` (repeatable, CSV) through to the
	// server as a JSON array. service.Analyze pre-populates draft.Tags
	// with Source:"user"; the auto-tagger merges with these rather
	// than overwriting.
	var userHints []string
	if f := cmd.Flags().Lookup("hint"); f != nil && f.Changed {
		userHints, _ = cmd.Flags().GetStringSlice("hint")
	}

	// Use actual source value; "argument"/"stdin"/"clipboard"/"file" are not
	// fetchable URLs — downstream steps (url_fetcher) must validate before use.
	reqSource := source

	req := dpkmsclient.AnalyzeRequest{
		Content:   content,
		Type:      flagString(cmd, "type", "analyze.type"),
		Pipeline:  flagString(cmd, "pipeline", "analyze.pipeline"),
		Source:    reqSource,
		Raw:       rawMode,
		NoFanout:  noFanout,
		Force:     noDedup,
		SourceKey: sourceKey,
		Mentions:  userMentions,
		Hints:     userHints,
	}

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	// One request to the resolved instance. Its failure is final: exit 70
	// when nothing answered, 5 for a rejected token, 2 for a request the
	// instance refused (a 422 names the unrouted type or pipeline).
	jobID, err := client.Analyze(ctx, req)
	if err != nil {
		return err
	}

	fmt.Printf("Job ID: %s\n", jobID)

	// Resolve --wait flag (present on both analyzeCmd and rootCmd).
	wait := false
	if f := cmd.Flags().Lookup("wait"); f != nil && f.Changed {
		wait, _ = cmd.Flags().GetBool("wait")
	} else {
		wait = viper.GetBool("analyze.wait")
	}
	if !wait || jobID == "" {
		return nil
	}

	// T-0562: --wait was previously a no-op. The CLI returned the job ID
	// from POST /analyze and exited even when the job was never persisted,
	// so the user never learned the work was dropped. Poll GET /jobs/{id}
	// on the instance that accepted the enqueue and fail loudly if the ID
	// can't be located within a short window — that 404 is the canonical
	// "silently dropped" signal.
	return waitForJob(ctx, client, jobID)
}

// waitForJob polls GET /api/v1/jobs/{id} on the instance that accepted the
// enqueue until the job reaches a terminal state, the context is cancelled,
// or pollTimeout elapses. If the job ID is not present in the queue within
// notFoundTimeout, return a clear error pointing at the silent-drop class
// of bug — the worker may have rejected the job before persistence.
func waitForJob(ctx context.Context, client *dpkmsclient.Client, jobID string) error {
	const (
		pollTimeout     = 5 * time.Minute
		notFoundTimeout = 5 * time.Second
		pollInterval    = 500 * time.Millisecond
	)

	ctx, cancel := context.WithTimeout(ctx, pollTimeout)
	defer cancel()

	path := "/api/v1/jobs/" + url.PathEscape(jobID)
	deadline404 := time.Now().Add(notFoundTimeout)

	for {
		var job struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			Error  string `json:"error,omitempty"`
		}
		err := client.Get(ctx, path, nil, &job)
		var re *dpkmsclient.RemoteError
		switch {
		case ctx.Err() != nil:
			return fmt.Errorf("wait: timed out after %s polling for job %s", pollTimeout, jobID)
		case errors.As(err, &re) && re.StatusCode == gohttp.StatusNotFound:
			if time.Now().After(deadline404) {
				return fmt.Errorf(
					"error: job %s not found in queue after %s; the job may have been silently dropped — please report this",
					jobID, notFoundTimeout)
			}
			time.Sleep(pollInterval)
			continue
		case err != nil:
			return fmt.Errorf("wait for job %s: %w", jobID, err)
		}

		switch job.Status {
		case "done", "completed", "succeeded":
			fmt.Printf("Job %s: %s\n", jobID, job.Status)
			return nil
		case "failed", "error":
			return fmt.Errorf("job %s failed: %s", jobID, job.Error)
		default:
			time.Sleep(pollInterval)
		}
	}
}
