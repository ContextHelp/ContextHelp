package cmd

import (
	"fmt"

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
	captureCmd.Flags().String("profile", "", "focus profile for the capture")

	// --- Pipeline behavior ---
	captureCmd.Flags().Bool("raw", false, "disable AI; store raw knowledge object")
	captureCmd.Flags().Bool("no-fanout", false, "skip post-ingest fan-out enrichment")
	captureCmd.Flags().Bool("no-dedup", false, "skip duplicate detection")
	captureCmd.Flags().String("source-key", "", "external dedup key (Slack ts, tweet ID, etc.)")
	captureCmd.Flags().Bool("wait", false, "block until job completes")
	captureCmd.Flags().String("server", "", "dpkms server URL (default http://localhost:8080)")

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

// RunCapture is the entry point for `ctxt capture`. T-0567 stubs only the
// flag scaffolding + mutex validation; behavior lands in T-0568 (positional/
// stdin/file → POST /api/v1/analyze) and T-0569 (--every continuous loop).
func RunCapture(cmd *cobra.Command, args []string) error {
	if err := validateCaptureFlags(cmd, args); err != nil {
		return err
	}
	return fmt.Errorf("ctxt capture: not yet implemented (Track 1 wiring lands in T-0568)")
}
