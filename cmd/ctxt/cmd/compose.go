package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var composeCmd = &cobra.Command{
	Use:   "compose <type>",
	Short: "Generate compositions from knowledge",
	Long: `Generate compositions such as briefs, plans, summaries, or drafts.

Compositions are generated from your knowledge objects using templates
and your focus profile context.

Types:
  brief   - Generate executive brief
  plan    - Generate action plan
  summary - Generate summary
  draft   - Generate publish-ready draft

Examples:
  # Generate a brief with inline citations (default)
  ctxt compose brief --tag ux,onboarding

  # Generate a plan with specific mentions
  ctxt compose plan --mention @project.signup-redesign

  # Generate summary since a specific date
  ctxt compose summary --since 2025-01-01

  # Generate draft and save to file
  ctxt compose draft --tag launch --output launch-plan.md

  # Export as JSON (includes structured citations)
  ctxt compose brief --tag launch --export json

  # Disable citations
  ctxt compose brief --tag launch --no-citations`,
	Args: cobra.ExactArgs(1),
	RunE: runCompose,
}

func init() {
	rootCmd.AddCommand(composeCmd)

	// Filter flags
	composeCmd.Flags().String("mention", "", "focus on specific mentions")
	composeCmd.Flags().String("tag", "", "focus on specific tags (comma-separated)")
	composeCmd.Flags().String("since", "", "include knowledge since date (ISO)")

	// Output flags. -o shorthand is reserved by kit/cli's --output (write
	// path), so --output-file is long-name only. A follow-up should drop
	// --output-file entirely in favor of kit's --output.
	composeCmd.Flags().String("output-file", "", "write to file")

	// Citation flags
	composeCmd.Flags().Bool("no-citations", false, "disable inline [ref:ID] citations")
	composeCmd.Flags().String("export", "markdown", "output format: markdown or json")

	// Bind flags to viper
	viper.BindPFlag("compose.mention", composeCmd.Flags().Lookup("mention"))
	viper.BindPFlag("compose.tag", composeCmd.Flags().Lookup("tag"))
	viper.BindPFlag("compose.since", composeCmd.Flags().Lookup("since"))
	viper.BindPFlag("compose.output-file", composeCmd.Flags().Lookup("output-file"))
	viper.BindPFlag("compose.no-citations", composeCmd.Flags().Lookup("no-citations"))
	viper.BindPFlag("compose.export", composeCmd.Flags().Lookup("export"))
}

func runCompose(cmd *cobra.Command, args []string) error {
	compositionType := args[0]

	switch compositionType {
	case "brief", "plan", "summary", "draft":
	default:
		return fmt.Errorf("unknown composition type: %s (expected brief|plan|summary|draft)", compositionType)
	}

	exportFormat := viper.GetString("compose.export")
	switch exportFormat {
	case "markdown", "json", "":
	default:
		return fmt.Errorf("unknown export format: %s (expected markdown or json)", exportFormat)
	}

	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx := context.Background()

	filter := storage.ObjectFilter{
		Tag:     viper.GetString("compose.tag"),
		Mention: viper.GetString("compose.mention"),
		Limit:   100,
	}
	if since := viper.GetString("compose.since"); since != "" {
		if t, err := time.Parse("2006-01-02", since); err == nil {
			filter.After = &t
		}
	}

	objects, _, err := svc.ListObjects(ctx, filter)
	if err != nil {
		return fmt.Errorf("list objects: %w", err)
	}

	if len(objects) == 0 {
		fmt.Println("No matching objects found for composition.")
		return nil
	}

	noCitations := viper.GetBool("compose.no-citations")

	var output string

	if noCitations || exportFormat == "" {
		// Plain composition without citation markers.
		result, err := svc.Compose(ctx, objects, compositionType)
		if err != nil {
			return fmt.Errorf("compose: %w", err)
		}
		output = result
	} else {
		// Citation-enriched composition (default path).
		result, err := svc.ComposeWithCitations(ctx, objects, compositionType)
		if err != nil {
			return fmt.Errorf("compose: %w", err)
		}

		if exportFormat == "json" {
			b, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				return fmt.Errorf("marshal: %w", err)
			}
			output = string(b)
		} else {
			// markdown (default)
			output = result.Content
		}
	}

	outputFile := viper.GetString("compose.output-file")
	if outputFile != "" {
		if err := os.WriteFile(outputFile, []byte(output), 0600); err != nil {
			return fmt.Errorf("write file: %w", err)
		}
		fmt.Printf("Composition written to %s\n", outputFile)
		return nil
	}

	fmt.Print(output)
	return nil
}
