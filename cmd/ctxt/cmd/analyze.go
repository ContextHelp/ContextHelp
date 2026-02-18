package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	gohttp "net/http"
	"os"

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

Examples:
  # Analyze text with hints and mentions
  echo "Fix signup flow" | ctxt analyze --type text --hints "#ux #bad" --mentions "@ui.best-practice"

  # Analyze an image
  ctxt analyze --file screenshot.png --type image --mentions "@ui.layout"

  # Analyze a URL with a focus profile
  ctxt analyze https://example.com --type url --profile growth

  # Wait for job completion
  ctxt analyze https://example.com --wait`,
	RunE: runAnalyze,
}

func init() {
	rootCmd.AddCommand(analyzeCmd)

	// Input flags
	analyzeCmd.Flags().String("type", "text", "input type (text|url|image|audio|video|feed|auto)")
	analyzeCmd.Flags().String("file", "", "read input from file")

	// Metadata flags
	analyzeCmd.Flags().String("hints", "", "influence tagging (e.g., \"#ux #bug\")")
	analyzeCmd.Flags().String("mentions", "", "explicit mentions to attach (e.g., \"@entity.slug\")")

	// Pipeline flags
	analyzeCmd.Flags().String("pipeline", "", "force specific pipeline")
	analyzeCmd.Flags().String("lang", "", "input language override")
	analyzeCmd.Flags().String("translate", "", "translation mode (none to skip)")

	// Execution flags
	analyzeCmd.Flags().Bool("raw", false, "disable AI; store raw knowledge object")
	analyzeCmd.Flags().Bool("wait", false, "block until job completes")

	// Server connection
	analyzeCmd.Flags().String("server", "", "dpkms server URL (default http://localhost:8080)")

	// Bind flags to viper
	viper.BindPFlag("analyze.type", analyzeCmd.Flags().Lookup("type"))
	viper.BindPFlag("analyze.file", analyzeCmd.Flags().Lookup("file"))
	viper.BindPFlag("analyze.hints", analyzeCmd.Flags().Lookup("hints"))
	viper.BindPFlag("analyze.mentions", analyzeCmd.Flags().Lookup("mentions"))
	viper.BindPFlag("analyze.pipeline", analyzeCmd.Flags().Lookup("pipeline"))
	viper.BindPFlag("analyze.lang", analyzeCmd.Flags().Lookup("lang"))
	viper.BindPFlag("analyze.translate", analyzeCmd.Flags().Lookup("translate"))
	viper.BindPFlag("analyze.raw", analyzeCmd.Flags().Lookup("raw"))
	viper.BindPFlag("analyze.wait", analyzeCmd.Flags().Lookup("wait"))
	viper.BindPFlag("server.url", analyzeCmd.Flags().Lookup("server"))
}

func runAnalyze(cmd *cobra.Command, args []string) error {
	var content string

	// Determine input source.
	file := viper.GetString("analyze.file")
	if file != "" {
		data, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("failed to read file: %w", err)
		}
		content = string(data)
	} else if len(args) > 0 {
		content = args[0]
	} else {
		stat, _ := os.Stdin.Stat()
		if (stat.Mode() & os.ModeCharDevice) == 0 {
			data, err := io.ReadAll(os.Stdin)
			if err != nil {
				return fmt.Errorf("failed to read stdin: %w", err)
			}
			content = string(data)
		} else {
			return fmt.Errorf("no input provided (use argument, --file, or stdin)")
		}
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
		"source":   "cli",
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
