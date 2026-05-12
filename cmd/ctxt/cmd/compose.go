package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
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
  ctxt compose brief --tagged ux,onboarding

  # Generate a plan with specific mentions
  ctxt compose plan --mention @project.signup-redesign

  # Generate summary since a specific date
  ctxt compose summary --since 2025-01-01

  # Generate summary across a time range
  ctxt compose summary --since "yesterday afternoon" --until now

  # Generate brief scoped to one work session (per ADR-067)
  ctxt compose brief --session sess_a1b2c3d4e5f6

  # Generate draft and save to file
  ctxt compose draft --tagged launch --output launch-plan.md

  # Export as JSON (includes structured citations)
  ctxt compose brief --tagged launch --export json

  # Disable citations
  ctxt compose brief --tagged launch --no-citations`,
	Args: cobra.ExactArgs(1),
	RunE: runCompose,
}

func init() {
	rootCmd.AddCommand(composeCmd)
	cliconv.WithSideEffect(composeCmd, cliconv.SideEffectWrite)
	// "compose" is not in kit's defaultIdempotency table; LLM-backed
	// generation is non-deterministic by default, so the same inputs may
	// produce different outputs across runs.
	cliconv.WithIdempotency(composeCmd, cliconv.IdempotencyNo)

	// Filter flags
	composeCmd.Flags().String("mention", "", "focus on specific mentions")
	composeCmd.Flags().String("tagged", "", "focus on specific tags (comma-separated)")
	composeCmd.Flags().String("since", "", "include knowledge since date (ISO) or natural-language ('yesterday', '2 hours ago')")
	composeCmd.Flags().String("until", "", "include knowledge up to date (ISO) or natural-language ('now', 'yesterday evening')")
	composeCmd.Flags().String("session", "", "scope to one work session (sess_xxx; per ADR-067)")

	// Output flags. -o shorthand is reserved by kit/cli's --output (write
	// path), so --output-file is long-name only. A follow-up should drop
	// --output-file entirely in favor of kit's --output.
	composeCmd.Flags().String("output-file", "", "write to file")

	// Citation flags
	composeCmd.Flags().Bool("no-citations", false, "disable inline [ref:ID] citations")
	composeCmd.Flags().String("export", "markdown", "output format: markdown or json")

	// Bind flags to viper
	viper.BindPFlag("compose.mention", composeCmd.Flags().Lookup("mention"))
	viper.BindPFlag("compose.tagged", composeCmd.Flags().Lookup("tagged"))
	viper.BindPFlag("compose.since", composeCmd.Flags().Lookup("since"))
	viper.BindPFlag("compose.until", composeCmd.Flags().Lookup("until"))
	viper.BindPFlag("compose.session", composeCmd.Flags().Lookup("session"))
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
		Tag:     viper.GetString("compose.tagged"),
		Mention: viper.GetString("compose.mention"),
		Limit:   100,
	}
	if since := viper.GetString("compose.since"); since != "" {
		if t, err := time.Parse("2006-01-02", since); err == nil {
			filter.After = &t
		}
	}
	if until := viper.GetString("compose.until"); until != "" {
		if t, err := time.Parse("2006-01-02", until); err == nil {
			filter.Before = &t
		}
	}
	// --session scopes the compose to one work session (ADR-067).
	// dpkms-side filter wiring (objects.session_id column + ObjectFilter
	// extension) is tracked separately as part of T-0505's storage
	// migration. The flag is plumbed here so the substrate is ready
	// when that migration lands; until then, the value is ignored
	// server-side (no harm, no error).
	if sess := viper.GetString("compose.session"); sess != "" {
		// Forward-compatible: when ObjectFilter gains a SessionID field,
		// uncomment this assignment. Until then, leaving as a no-op
		// preserves the flag-parsing contract for users.
		_ = sess
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
