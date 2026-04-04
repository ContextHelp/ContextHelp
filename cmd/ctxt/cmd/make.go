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

var makeCmd = &cobra.Command{
	Use:   "make <type>",
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
  ctxt make brief --tag ux,onboarding

  # Generate a plan with specific mentions
  ctxt make plan --mention @project.signup-redesign

  # Generate summary since a specific date
  ctxt make summary --since 2025-01-01

  # Generate draft and save to file
  ctxt make draft --tag launch --output launch-plan.md

  # Export as JSON (includes structured citations)
  ctxt make brief --tag launch --export json

  # Disable citations
  ctxt make brief --tag launch --no-citations`,
	Args: cobra.ExactArgs(1),
	RunE: runMake,
}

func init() {
	rootCmd.AddCommand(makeCmd)

	// Filter flags
	makeCmd.Flags().String("mention", "", "focus on specific mentions")
	makeCmd.Flags().String("tag", "", "focus on specific tags (comma-separated)")
	makeCmd.Flags().String("since", "", "include knowledge since date (ISO)")

	// Output flags
	makeCmd.Flags().StringP("output-file", "o", "", "write to file")

	// Citation flags
	makeCmd.Flags().Bool("no-citations", false, "disable inline [ref:ID] citations")
	makeCmd.Flags().String("export", "markdown", "output format: markdown or json")

	// Bind flags to viper
	viper.BindPFlag("make.mention", makeCmd.Flags().Lookup("mention"))
	viper.BindPFlag("make.tag", makeCmd.Flags().Lookup("tag"))
	viper.BindPFlag("make.since", makeCmd.Flags().Lookup("since"))
	viper.BindPFlag("make.output-file", makeCmd.Flags().Lookup("output-file"))
	viper.BindPFlag("make.no-citations", makeCmd.Flags().Lookup("no-citations"))
	viper.BindPFlag("make.export", makeCmd.Flags().Lookup("export"))
}

func runMake(cmd *cobra.Command, args []string) error {
	compositionType := args[0]

	switch compositionType {
	case "brief", "plan", "summary", "draft":
	default:
		return fmt.Errorf("unknown composition type: %s (expected brief|plan|summary|draft)", compositionType)
	}

	exportFormat := viper.GetString("make.export")
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
		Tag:     viper.GetString("make.tag"),
		Mention: viper.GetString("make.mention"),
		Limit:   100,
	}
	if since := viper.GetString("make.since"); since != "" {
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

	noCitations := viper.GetBool("make.no-citations")

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

	outputFile := viper.GetString("make.output-file")
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
