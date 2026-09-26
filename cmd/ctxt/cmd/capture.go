package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	gohttp "net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/idxbridge"
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
	captureCmd.Flags().String("server", "",
		"pin routing to this single dpkms instance, bypassing the configured server.urls failover list (default http://127.0.0.1:8080 when nothing is configured)")

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

// captureHTTPClient is the HTTP client used for all capture-side POSTs
// to dpkms. The 30s timeout protects against an unreachable server
// hanging the CLI indefinitely; in --every loops the per-request
// context is also cancellable via SIGINT so Ctrl-C interrupts an
// in-flight POST instead of waiting for the timeout.
var captureHTTPClient = &gohttp.Client{Timeout: 30 * time.Second}

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
// per-request 30s timeout.
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

	endpoints := captureEndpoints(cmd)

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
		return postInboxCapture(ctx, endpoints, content, source, contentType, hints, mentions, cmd)
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	resp, err := postCapture(ctx, cmd, endpoints, "/api/v1/analyze", body)
	if err != nil {
		return fmt.Errorf("request to dpkms: %w", err)
	}
	if resp.status != gohttp.StatusAccepted && resp.status != gohttp.StatusOK {
		return fmt.Errorf("dpkms returned %d: %s", resp.status, string(resp.body))
	}

	var result map[string]string
	if err := json.Unmarshal(resp.body, &result); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}
	jobID := result["job_id"]
	fmt.Printf("Job ID: %s\n", jobID)

	if wait && jobID != "" {
		return waitForCaptureJob(ctx, resp.servedBy, jobID)
	}
	return nil
}

// captureEndpoints resolves where capture sends, exactly as `ctxt analyze`
// does: an explicit --server pins routing to that single instance (reusing
// its configured token, if any); otherwise the configured server.urls list
// (primary first), then server.url, then the shared client default.
// Tokens only ever come from config.
func captureEndpoints(cmd *cobra.Command) []idxbridge.Endpoint {
	if f := cmd.Flags().Lookup("server"); f != nil && f.Changed && f.Value.String() != "" {
		return []idxbridge.Endpoint{pinnedEndpoint(f.Value.String())}
	}
	return clientEndpoints()
}

// captureResponse is one completed HTTP exchange with the instance that
// served it.
type captureResponse struct {
	status   int
	body     []byte
	servedBy idxbridge.Endpoint
}

// postCapture POSTs body to path on the first instance in endpoints that
// takes it, attaching that instance's bearer token. It walks on — the
// failover analyze applies — only when nothing can have been stored:
//   - the instance cannot be dialed (no request byte was sent);
//   - the instance rejects the credentials (401/403 come from the auth
//     middleware, before any handler runs), with a warning naming it.
//
// Any other answer ends the walk and is returned to the caller: a live
// instance's decision is never replayed elsewhere, and a request that may
// have reached an instance is never resent, since capture carries no
// idempotency key. The last instance's auth rejection is returned as a
// response so the caller reports its status.
func postCapture(
	ctx context.Context,
	cmd *cobra.Command,
	endpoints []idxbridge.Endpoint,
	path string,
	body []byte,
) (*captureResponse, error) {
	for i, ep := range endpoints {
		last := i == len(endpoints)-1
		base := strings.TrimRight(ep.URL, "/")
		req, err := gohttp.NewRequestWithContext(ctx, gohttp.MethodPost, base+path, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("build request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		if ep.Token != "" {
			req.Header.Set("Authorization", "Bearer "+ep.Token)
		}
		resp, err := captureHTTPClient.Do(req)
		if err != nil {
			if last || ctx.Err() != nil || !isDialError(err) {
				return nil, err
			}
			continue
		}
		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("read response: %w", err)
		}
		if !last && (resp.StatusCode == gohttp.StatusUnauthorized || resp.StatusCode == gohttp.StatusForbidden) {
			fmt.Fprintf(cmd.ErrOrStderr(),
				"warning: %s rejected credentials (%d); trying next instance\n", base, resp.StatusCode)
			continue
		}
		return &captureResponse{status: resp.StatusCode, body: respBody, servedBy: ep}, nil
	}
	return nil, fmt.Errorf("no dpkms instance configured")
}

// isDialError reports whether err failed while connecting, before any
// part of the request was written.
func isDialError(err error) bool {
	var opErr *net.OpError
	return errors.As(err, &opErr) && opErr.Op == "dial"
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
	endpoints []idxbridge.Endpoint,
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
	raw, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal inbox request: %w", err)
	}
	resp, err := postCapture(ctx, cmd, endpoints, "/api/v1/inbox", raw)
	if err != nil {
		return fmt.Errorf("request to dpkms inbox: %w", err)
	}
	if resp.status != gohttp.StatusCreated &&
		resp.status != gohttp.StatusAccepted &&
		resp.status != gohttp.StatusOK {
		return fmt.Errorf("dpkms inbox returned %d: %s", resp.status, string(resp.body))
	}
	var obj map[string]any
	if err := json.Unmarshal(resp.body, &obj); err != nil {
		return fmt.Errorf("parse inbox response: %w", err)
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
// the capture, with that instance's token, until terminal status. Mirrors
// cmd/dpkms/cmd/pipeline.go::waitForJob's contract.
func waitForCaptureJob(ctx context.Context, ep idxbridge.Endpoint, jobID string) error {
	endpoint := fmt.Sprintf("%s/api/v1/jobs/%s", strings.TrimRight(ep.URL, "/"), jobID)
	for {
		req, err := gohttp.NewRequestWithContext(ctx, gohttp.MethodGet, endpoint, nil)
		if err != nil {
			return fmt.Errorf("build poll request: %w", err)
		}
		if ep.Token != "" {
			req.Header.Set("Authorization", "Bearer "+ep.Token)
		}
		resp, err := captureHTTPClient.Do(req)
		if err != nil {
			return fmt.Errorf("poll job %s: %w", jobID, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != gohttp.StatusOK {
			return fmt.Errorf("dpkms returned %d polling %s: %s", resp.StatusCode, jobID, string(body))
		}
		var job struct {
			Status string `json:"status"`
			Error  string `json:"error,omitempty"`
		}
		if err := json.Unmarshal(body, &job); err != nil {
			return fmt.Errorf("parse job %s: %w", jobID, err)
		}
		switch job.Status {
		case "completed":
			return nil
		case "failed":
			return fmt.Errorf("job %s failed: %s", jobID, job.Error)
		}
		time.Sleep(1 * time.Second)
	}
}
