package cmd

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/dpkmsclient"
	"github.com/spf13/cobra"
)

var captureCmd = &cobra.Command{
	Use:   "capture [source]",
	Short: "Capture content from a URL, file, stdin, or clipboard",
	Long: `Capture is the canonical user-facing verb for explicit reads from any
source the dPKMS substrate handles — URLs, files, stdin, and (Track 2)
the ambient set of configured sensors.

Source resolution:
  - Positional <source>  URL, file path, or quoted string
  - --stdin              read from stdin
  - (none)               fall back to the system clipboard

Examples:
  # URL capture (server detects the right pipeline)
  ctxt capture https://example.com/post

  # File capture
  ctxt capture ./notes.md

  # Stdin capture
  cat README.md | ctxt capture --stdin

  # Continuous mode (re-poll every 15m, ctrl-c to stop)
  ctxt capture https://hnrss.org/frontpage.rss --every 15m

  # Hints + mentions
  ctxt capture https://example.com --hint research --mention @project.alpha

  # Route through the inbox instead of running the pipeline now
  ctxt capture ./meeting.m4a --inbox

Track 2 (not yet implemented): --ambient, --input, --skip, --window.`,
	RunE: RunCapture,
}

func init() {
	rootCmd.AddCommand(captureCmd)
	cliconv.WithSideEffect(captureCmd, cliconv.SideEffectWrite)
	cliconv.WithExamples(captureCmd, []cliconv.Example{
		{Title: "Capture a URL", Command: "ctxt capture https://example.com/post"},
		{Title: "Capture a file", Command: "ctxt capture ./notes.md"},
		{Title: "Capture from stdin", Command: "cat README.md | ctxt capture --stdin"},
	})
	cliconv.WithNextSteps(captureCmd, []cliconv.NextStep{
		{When: "on success", Suggest: "ctxt show <id>", Reason: "inspect the captured object"},
		{When: "to find captured content later", Suggest: "ctxt find <query>", Reason: "search across captures"},
	})
	// "capture" is not in kit's defaultIdempotency table; each invocation
	// enqueues a fresh job unless the caller supplies --source-key or
	// --idempotency-key for replay-safe dedup.
	cliconv.WithIdempotency(captureCmd, cliconv.IdempotencyConditional)

	// --- Source selection ---
	captureCmd.Flags().String("source", "", "override auto-detection on the positional source")
	captureCmd.Flags().Bool("stdin", false, "read input from stdin")
	captureCmd.Flags().String("type", "text", "input type (text|url|image|audio|video|feed|auto)")

	// --- Routing ---
	captureCmd.Flags().String("pipeline", "", "override server-side pipeline pick")
	captureCmd.Flags().Bool("inbox", false, "route captured object through the inbox instead of running the pipeline now")

	// --- Continuous mode ---
	captureCmd.Flags().Duration("every", 0, "continuous mode: re-run on this cadence (one-shot when absent)")

	// --- Metadata ---
	captureCmd.Flags().StringSlice("hint", nil, "hints attached to the captured object (repeatable, CSV)")
	captureCmd.Flags().StringSlice("mention", nil, "mention targets (repeatable, CSV)")
	captureCmd.Flags().String("note", "", "audit note explaining the capture")
	// --profile is inherited from kit's persistent flag set; do not
	// re-register it locally. capture reads it via cmd.Flags().GetString("profile")
	// at run time (cobra resolves inherited persistent flags through Flags()).

	// --- Pipeline behavior ---
	captureCmd.Flags().Bool("raw", false, "disable AI; store raw knowledge object")
	captureCmd.Flags().Bool("no-fanout", false, "skip post-ingest fan-out enrichment")
	captureCmd.Flags().Bool("no-dedup", false, "skip duplicate detection")
	captureCmd.Flags().String("source-key", "", "external dedup key (Slack ts, tweet ID, etc.)")
	captureCmd.Flags().Bool("wait", false, "block until job completes")
	captureCmd.Flags().String("server", "", serverFlagUsage)

	// --- Track 2 stubs (defined but error when used) ---
	captureCmd.Flags().Bool("ambient", false, "(Track 2) sweep every sensor enabled in policy/ambient.yaml")
	captureCmd.Flags().StringSlice("input", nil, "(Track 2) restrict ambient sweep to listed sensors")
	captureCmd.Flags().StringSlice("skip", nil, "(Track 2) exclude listed sensors from the ambient sweep")
	captureCmd.Flags().Duration("window", 0, "(Track 2) capture state from the last duration")
}

// validateCaptureFlags enforces mutex rules for ctxt capture:
//   - --ambient is unimplemented in Track 1.
//   - --input / --skip / --window belong to ambient mode and also error.
//   - positional source is mutually exclusive with --stdin.
func validateCaptureFlags(cmd *cobra.Command, args []string) error {
	if cmd.Flags().Changed("ambient") {
		return fmt.Errorf("ambient mode not yet implemented (Track 2)")
	}
	for _, name := range []string{"input", "skip", "window"} {
		if cmd.Flags().Changed(name) {
			return fmt.Errorf("flag --%s is part of ambient mode (Track 2 — not yet implemented)", name)
		}
	}
	stdin, _ := cmd.Flags().GetBool("stdin")
	if stdin && len(args) > 0 {
		return fmt.Errorf("cannot combine --stdin with a positional source")
	}
	return nil
}

// captureTimeout bounds each capture-side request to dpkms, so an
// unreachable server cannot hang the CLI; in --every loops the
// per-request context is also cancellable via SIGINT so Ctrl-C
// interrupts an in-flight POST instead of waiting for the timeout.
const captureTimeout = 30 * time.Second

// RunCapture is the entry point for `ctxt capture`. Implements positional /
// file / stdin / clipboard capture by POSTing to /api/v1/analyze. When
// --every is set, wraps captureOnce in a ticker loop until SIGINT.
func RunCapture(cmd *cobra.Command, args []string) error {
	if err := validateCaptureFlags(cmd, args); err != nil {
		return err
	}
	every, _ := cmd.Flags().GetDuration("every")
	if every <= 0 {
		return captureOnce(cmd.Context(), cmd, args)
	}
	return captureLoop(cmd, args, every)
}

// captureLoop runs captureOnce immediately, then on every tick of `every`
// until the context is cancelled (SIGINT). Per-tick errors are logged to
// stderr and don't kill the loop — only ctx cancellation does. The loop
// context threads into each captureOnce call so an in-flight HTTP
// request cancels promptly on Ctrl-C instead of waiting for the
// per-request timeout.
func captureLoop(cmd *cobra.Command, args []string, every time.Duration) error {
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
	defer stop()

	label := captureSourceLabel(cmd, args)
	fmt.Fprintf(os.Stderr, "capturing %s every %s (ctrl-c to stop)\n", label, every)

	if err := captureOnce(ctx, cmd, args); err != nil {
		fmt.Fprintf(os.Stderr, "capture: %v\n", err)
	}
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := captureOnce(ctx, cmd, args); err != nil {
				fmt.Fprintf(os.Stderr, "capture: %v\n", err)
			}
		}
	}
}

// captureSourceLabel produces a short label for the loop banner.
func captureSourceLabel(cmd *cobra.Command, args []string) string {
	if stdin, _ := cmd.Flags().GetBool("stdin"); stdin {
		return "stdin"
	}
	if len(args) > 0 {
		return args[0]
	}
	return "clipboard"
}

// captureOnce performs a single capture: resolves input → builds request →
// POSTs to /api/v1/analyze → prints job ID → optionally waits for completion.
//
// ctx threads through to the HTTP request so a SIGINT during --every loops
// cancels the in-flight POST instead of blocking on the client timeout.
func captureOnce(ctx context.Context, cmd *cobra.Command, args []string) error {
	content, source, err := resolveCaptureInput(cmd, args)
	if err != nil {
		return err
	}

	// --source overrides the auto-detected source string. Operators use
	// it to pin pipeline detection (e.g. --source /vault/notes/file.md
	// when the actual content arrived via --stdin). Empty value keeps
	// the auto-detected source from resolveCaptureInput.
	if override, _ := cmd.Flags().GetString("source"); override != "" {
		source = override
	}

	client, err := newDpkmsClient(cmd, captureTimeout)
	if err != nil {
		return err
	}

	contentType, _ := cmd.Flags().GetString("type")
	pipeline, _ := cmd.Flags().GetString("pipeline")
	rawMode, _ := cmd.Flags().GetBool("raw")
	noFanout, _ := cmd.Flags().GetBool("no-fanout")
	noDedup, _ := cmd.Flags().GetBool("no-dedup")
	sourceKey, _ := cmd.Flags().GetString("source-key")
	hints, _ := cmd.Flags().GetStringSlice("hint")
	mentions, _ := cmd.Flags().GetStringSlice("mention")
	profile, _ := cmd.Flags().GetString("profile")
	note, _ := cmd.Flags().GetString("note")
	wait, _ := cmd.Flags().GetBool("wait")
	inbox, _ := cmd.Flags().GetBool("inbox")

	// Build request body. Server-side JSON contract uses `hints` (T-0573)
	// and `mentions` arrays — both are caller-asserted user inputs that
	// merge with auto-extracted entries during pipeline execution.
	reqBody := map[string]any{
		"content":    content,
		"type":       contentType,
		"pipeline":   pipeline,
		"source":     source,
		"raw":        rawMode,
		"no_fanout":  noFanout,
		"force":      noDedup,
		"source_key": sourceKey,
	}
	if len(hints) > 0 {
		reqBody["hints"] = hints
	}
	if profile != "" {
		reqBody["profile"] = profile
	}
	if note != "" {
		reqBody["note"] = note
	}
	if len(mentions) > 0 {
		reqBody["mentions"] = mentions
	}

	if inbox {
		return postInboxCapture(ctx, client, content, source, contentType, hints, mentions, cmd)
	}

	var result struct {
		JobID string `json:"job_id"`
	}
	if err := client.Post(ctx, "/api/v1/analyze", reqBody, &result); err != nil {
		return err
	}
	fmt.Printf("Job ID: %s\n", result.JobID)

	if wait && result.JobID != "" {
		return waitForCaptureJob(ctx, client, result.JobID)
	}
	return nil
}

// postInboxCapture POSTs to /api/v1/inbox (the CaptureInbox handler) instead
// of /api/v1/analyze. The endpoint accepts a different body shape (no
// `tag`/`pipeline`/`raw`; uses `inbox_note` + `hints` instead of `tag`).
//
// --wait is intentionally ignored on this path: the inbox endpoint does NOT
// enqueue a job — it only stores the object in inbox state for later triage
// (svc.CaptureToInbox). There is no job to poll.
func postInboxCapture(
	ctx context.Context,
	client *dpkmsclient.Client,
	content, source, contentType string,
	hints, mentions []string,
	cmd *cobra.Command,
) error {
	body := map[string]any{
		"content":  content,
		"type":     contentType,
		"source":   source,
		"mentions": mentions,
	}
	if note, _ := cmd.Flags().GetString("note"); note != "" {
		body["inbox_note"] = note
	}
	if len(hints) > 0 {
		body["hints"] = hints
	}
	// T-0588: --profile partitions the inbox item to a focus profile.
	if profile, _ := cmd.Flags().GetString("profile"); profile != "" {
		body["profile"] = profile
	}
	var obj map[string]any
	if err := client.Post(ctx, "/api/v1/inbox", body, &obj); err != nil {
		return err
	}
	if id, ok := obj["id"].(string); ok && id != "" {
		fmt.Printf("Inbox object: %s\n", id)
	} else {
		fmt.Println("Inbox object stored")
	}
	return nil
}

// resolveCaptureInput picks the input source:
//   - --stdin       read from os.Stdin
//   - positional    URL, file path, or literal string
//   - (none)        fall back to clipboard
//
// The returned source string mirrors `ctxt analyze`'s convention:
// "stdin" / "argument" / "clipboard" / "file" / <url>.
func resolveCaptureInput(cmd *cobra.Command, args []string) (content, source string, err error) {
	stdin, _ := cmd.Flags().GetBool("stdin")
	if stdin {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", "", fmt.Errorf("read stdin: %w", err)
		}
		return string(data), "stdin", nil
	}

	if len(args) > 0 {
		raw := strings.Join(args, " ")
		// Treat http(s) URLs as URL captures: pass through the raw URL as
		// content; service.Analyze auto-detects type=url and uses the URL
		// as the job source.
		if u, perr := url.Parse(strings.TrimSpace(raw)); perr == nil && (u.Scheme == "http" || u.Scheme == "https") {
			return strings.TrimSpace(raw), strings.TrimSpace(raw), nil
		}
		// File path?
		if info, statErr := os.Stat(raw); statErr == nil && !info.IsDir() {
			data, err := os.ReadFile(raw)
			if err != nil {
				return "", "", fmt.Errorf("read file %q: %w", raw, err)
			}
			// Send the absolute path as `source` so the server's
			// pipeline detector chain can fire both extension-based
			// detectors (foo.png → image.ocr) AND prefix-based ones
			// (/vault/notes/... → watch.file, T-0209). The earlier
			// "file:<path>" prefix broke the latter. Resolution
			// failure falls back to the raw path the operator typed.
			abs, absErr := filepath.Abs(raw)
			if absErr != nil {
				abs = raw
			}
			return string(data), abs, nil
		}
		// Literal string.
		return raw, "argument", nil
	}

	// Clipboard fallback.
	if clipboard.Unsupported || os.Getenv("CTXT_NO_CLIPBOARD") != "" {
		return "", "", fmt.Errorf("no input provided and clipboard is unsupported")
	}
	c, err := clipboard.ReadAll()
	if err != nil || c == "" {
		return "", "", fmt.Errorf("no input provided (use a positional source, --stdin, or copy something to the clipboard)")
	}
	fmt.Fprintln(os.Stderr, "Using content from clipboard...")
	return c, "clipboard", nil
}

// waitForCaptureJob polls /api/v1/jobs/<id> on the instance that accepted
// the capture until terminal status. Mirrors
// cmd/dpkms/cmd/pipeline.go::waitForJob's contract.
func waitForCaptureJob(ctx context.Context, client *dpkmsclient.Client, jobID string) error {
	for {
		var job struct {
			Status string `json:"status"`
			Error  string `json:"error,omitempty"`
		}
		if err := client.Get(ctx, "/api/v1/jobs/"+url.PathEscape(jobID), nil, &job); err != nil {
			return fmt.Errorf("poll job %s: %w", jobID, err)
		}
		switch job.Status {
		case "completed":
			return nil
		case "failed":
			return fmt.Errorf("job %s failed: %s", jobID, job.Error)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}
