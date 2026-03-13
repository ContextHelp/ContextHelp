package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	gohttp "net/http"
	"os"

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
  echo "Fix signup flow" | ctxt analyze --type text --hints "#ux #bad" --mentions "@ui.best-practice"

  # Analyze an image
  ctxt analyze --file screenshot.png --type image --mentions "@ui.layout"

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

	// Input flags (Persistent on rootCmd so they work as default command)
	rootCmd.PersistentFlags().String("type", "text", "input type (text|url|image|audio|video|feed|auto)")
	rootCmd.PersistentFlags().String("file", "", "read input from file")

	// Metadata flags
	rootCmd.PersistentFlags().String("hints", "", "influence tagging (e.g., \"#ux #bug\")")
	rootCmd.PersistentFlags().String("mentions", "", "explicit mentions to attach (e.g., \"@entity.slug\")")

	// Pipeline flags
	rootCmd.PersistentFlags().String("pipeline", "", "force specific pipeline")
	rootCmd.PersistentFlags().String("lang", "", "input language override")
	rootCmd.PersistentFlags().String("translate", "", "translation mode (none to skip)")

	// Execution flags
	rootCmd.PersistentFlags().Bool("raw", false, "disable AI; store raw knowledge object")
	rootCmd.PersistentFlags().Bool("wait", false, "block until job completes")

	// Server connection
	rootCmd.PersistentFlags().String("server", "", "dpkms server URL (default http://localhost:8080)")

	// Bind flags to viper
	viper.BindPFlag("analyze.type", rootCmd.PersistentFlags().Lookup("type"))
	viper.BindPFlag("analyze.file", rootCmd.PersistentFlags().Lookup("file"))
	viper.BindPFlag("analyze.hints", rootCmd.PersistentFlags().Lookup("hints"))
	viper.BindPFlag("analyze.mentions", rootCmd.PersistentFlags().Lookup("mentions"))
	viper.BindPFlag("analyze.pipeline", rootCmd.PersistentFlags().Lookup("pipeline"))
	viper.BindPFlag("analyze.lang", rootCmd.PersistentFlags().Lookup("lang"))
	viper.BindPFlag("analyze.translate", rootCmd.PersistentFlags().Lookup("translate"))
	viper.BindPFlag("analyze.raw", rootCmd.PersistentFlags().Lookup("raw"))
	viper.BindPFlag("analyze.wait", rootCmd.PersistentFlags().Lookup("wait"))
	viper.BindPFlag("server.url", rootCmd.PersistentFlags().Lookup("server"))
}

func RunAnalyze(cmd *cobra.Command, args []string) error {
	var content string
	var source string

	// Determine input source.
	file := viper.GetString("analyze.file")
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
	serverURL := viper.GetString("server.url")
	if serverURL == "" {
		serverURL = "http://localhost:8080"
	}

	// Build request body.
	reqBody := map[string]string{
		"content":  content,
		"type":     viper.GetString("analyze.type"),
		"pipeline": viper.GetString("analyze.pipeline"),
		"source":   fmt.Sprintf("cli:%s", source),
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
