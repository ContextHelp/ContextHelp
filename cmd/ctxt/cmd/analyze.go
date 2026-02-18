package cmd

import (
	"fmt"
	"io"
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
	analyzeCmd.Flags().String("type", "auto", "input type (text|url|image|audio|video|feed|auto)")
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
}

func runAnalyze(cmd *cobra.Command, args []string) error {
	var content string

	// Determine input source
	file := viper.GetString("analyze.file")
	if file != "" {
		// Read from file
		data, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("failed to read file: %w", err)
		}
		content = string(data)
	} else if len(args) > 0 {
		// Use argument
		content = args[0]
	} else {
		// Read from stdin
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

	// TODO: Implement actual analyze logic
	// For now, just print what we would do
	fmt.Println("Would analyze content with configuration:")
	fmt.Printf("  Type: %s\n", viper.GetString("analyze.type"))
	fmt.Printf("  Profile: %s\n", viper.GetString("profile.default"))
	fmt.Printf("  Pipeline: %s\n", viper.GetString("analyze.pipeline"))
	fmt.Printf("  Hints: %s\n", viper.GetString("analyze.hints"))
	fmt.Printf("  Mentions: %s\n", viper.GetString("analyze.mentions"))
	fmt.Printf("  Language: %s\n", viper.GetString("analyze.lang"))
	fmt.Printf("  Raw mode: %v\n", viper.GetBool("analyze.raw"))
	fmt.Printf("  Wait: %v\n", viper.GetBool("analyze.wait"))
	fmt.Printf("\nContent preview: %s\n", content[:min(len(content), 100)])

	// Return job ID
	fmt.Println("\nJob ID: job_12345678")

	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
