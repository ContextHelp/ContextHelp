package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	gohttp "net/http"
	"os"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/cli"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var analyzeCmd = &cobra.Command{
	Use:   "analyze [content]",
	Short: "Enqueue content for ingestion",
	Long: `Analyze enqueues content into the ingestion pipeline.

This does not run pipelines directly; the worker (dpkms serve) handles execution.

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

	// Register flags on analyzeCmd for `ctxt analyze --help`.
	analyzeCmd.Flags().String("type", "text", "input type (text|url|image|audio|video|feed|auto)")
	analyzeCmd.Flags().StringP("file", "f", "", "read input from file")
	analyzeCmd.Flags().String("hint", "", "influence tagging hints (e.g., \"#ux #bug\")")
	analyzeCmd.Flags().String("mention", "", "explicit mentions to attach (e.g., \"@entity.slug\")")
	analyzeCmd.Flags().String("pipeline", "", "force specific pipeline")
	analyzeCmd.Flags().String("language", "", "input language override")
	analyzeCmd.Flags().String("translate", "", "translation mode (none to skip)")
	analyzeCmd.Flags().Bool("raw", false, "disable AI; store raw knowledge object")
	analyzeCmd.Flags().Bool("no-fanout", false, "skip post-ingest fan-out enrichment")
	analyzeCmd.Flags().Bool("no-dedup", false, "skip duplicate detection")
	analyzeCmd.Flags().String("source-key", "", "external dedup key (Slack ts, tweet ID, etc.)")
	analyzeCmd.Flags().Bool("wait", false, "block until job completes")
	analyzeCmd.Flags().String("server", "", "dpkms server URL (default http://localhost:8080)")

	// Mirror flags on rootCmd (local, not persistent) so `ctxt <content> --type url` works
	// without leaking these flags into every subcommand's help.
	rootCmd.Flags().String("type", "text", "input type (text|url|image|audio|video|feed|auto)")
	rootCmd.Flags().StringP("file", "f", "", "read input from file")
	rootCmd.Flags().String("hint", "", "influence tagging hints (e.g., \"#ux #bug\")")
	rootCmd.Flags().String("mention", "", "explicit mentions to attach (e.g., \"@entity.slug\")")
	rootCmd.Flags().String("pipeline", "", "force specific pipeline")
	rootCmd.Flags().String("language", "", "input language override")
	rootCmd.Flags().String("translate", "", "translation mode (none to skip)")
	rootCmd.Flags().Bool("raw", false, "disable AI; store raw knowledge object")
	rootCmd.Flags().Bool("no-fanout", false, "skip post-ingest fan-out enrichment")
	rootCmd.Flags().Bool("no-dedup", false, "skip duplicate detection")
	rootCmd.Flags().String("source-key", "", "external dedup key (Slack ts, tweet ID, etc.)")
	rootCmd.Flags().Bool("wait", false, "block until job completes")
	rootCmd.Flags().String("server", "", "dpkms server URL (default http://localhost:8080)")

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
	viper.BindPFlag("server.url", analyzeCmd.Flags().Lookup("server"))
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

	// Determine server URL.
	serverURL := flagString(cmd, "server", "server.url")
	if serverURL == "" {
		serverURL = "http://localhost:8080"
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

	// Use actual source value; "argument"/"stdin"/"clipboard"/"file" are not
	// fetchable URLs — downstream steps (url_fetcher) must validate before use.
	reqSource := source

	// Build request body.
	reqBody := map[string]any{
		"content":    content,
		"type":       flagString(cmd, "type", "analyze.type"),
		"pipeline":   flagString(cmd, "pipeline", "analyze.pipeline"),
		"source":     reqSource,
		"raw":        rawMode,
		"no_fanout":  noFanout,
		"force":      noDedup,
		"source_key": sourceKey,
	}
	if len(userMentions) > 0 {
		reqBody["mentions"] = userMentions
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	// POST to dpkms.
	resp, err := gohttp.Post(serverURL+"/api/v1/analyze", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("request to dpkms: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != gohttp.StatusAccepted && resp.StatusCode != gohttp.StatusOK {
		return fmt.Errorf("dpkms returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result map[string]string
	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}

	fmt.Printf("Job ID: %s\n", result["job_id"])

	return nil
}
