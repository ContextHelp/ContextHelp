package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/composition"
	"github.com/ideacrafterslabs/ctxt/internal/dpkmsclient"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"hop.top/kit/go/console/output"
)

var composeCmd = &cobra.Command{
	Use:   "compose <type>",
	Short: "Generate compositions from knowledge",
	Long: `Generate compositions such as briefs, plans, summaries, or drafts.

Compositions are rendered locally from knowledge objects read from the
dpkms instance ctxt resolves (--server, --instance, CTXT_INSTANCE,
ctxt instance use, then config): the 100 most recently created objects
the filters select (objects matching more of the listed tags come first).

--tagged takes a comma list, a repeated flag, or both; any listed tag
matches. A leading + requires a tag (+a,+b,+c needs all three; +a,b,+c
needs a and c, and b only ranks). Objects matching more of the listed
tags come first. Quote a tag that holds spaces.

Types:
  brief   - Generate executive brief
  plan    - Generate action plan
  summary - Generate summary
  draft   - Generate publish-ready draft

Examples:
  # Generate a brief with inline citations (default)
  ctxt compose brief --tagged ux

  # Brief of the last 30 days: a and c required, b optional (ranks first)
  ctxt compose brief --tagged +a,b,+c --since 30d

  # Several tags, any match
  ctxt compose summary --tagged ux --tagged design --since 1w

  # Generate a plan with specific mentions
  ctxt compose plan --mention @project.signup-redesign

  # Generate summary of objects created since a date
  ctxt compose summary --since 2025-01-01

  # Generate summary across a creation-time range: 3d, 42h, 1w, 30m (minutes), 1M or 1mo (month),
  # yesterday, today, YYYY-MM-DD or RFC 3339
  ctxt compose summary --since 2025-01-01 --until 2025-01-31T18:00:00Z

  # Generate draft and save to file
  ctxt compose draft --tagged launch --output-file launch-plan.md

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
	cliconv.WithExamples(composeCmd, []cliconv.Example{
		{Title: "Generate a brief from tagged knowledge", Command: "ctxt compose brief --tagged ux"},
		{Title: "Brief of the last 30 days: a and c required, b ranks first", Command: "ctxt compose brief --tagged +a,b,+c --since 30d"},
		{Title: "Generate a plan scoped to a mention", Command: "ctxt compose plan --mention @project.signup-redesign"},
		{Title: "Write a draft to a file as JSON", Command: "ctxt compose draft --tagged launch --output-file launch.md --export json"},
	})
	cliconv.WithNextSteps(composeCmd, []cliconv.NextStep{
		{When: "on success", Suggest: "ctxt show <id>", Reason: "review the generated composition"},
		{When: "if results miss context", Suggest: "ctxt find <topic>", Reason: "verify what knowledge was available"},
	})
	// "compose" is not in kit's defaultIdempotency table; LLM-backed
	// generation is non-deterministic by default, so the same inputs may
	// produce different outputs across runs.
	cliconv.WithIdempotency(composeCmd, cliconv.IdempotencyNo)

	// Filter flags
	composeCmd.Flags().String("mention", "", "focus on one mention (@ns.slug or ctxt:// URI)")
	addTaggedFlag(composeCmd)
	composeCmd.Flags().String("since", "", sinceFlagUsage)
	composeCmd.Flags().String("until", "", untilFlagUsage)

	// Output flags. -o shorthand is reserved by kit/cli's --output (write
	// path), so --output-file is long-name only. A follow-up should drop
	// --output-file entirely in favor of kit's --output.
	composeCmd.Flags().String("output-file", "", "write to file")

	// Citation flags
	composeCmd.Flags().Bool("no-citations", false, "disable inline [ref:ID] citations")
	composeCmd.Flags().String("export", formatMarkdown, "output format: markdown or json")

	for _, name := range []string{"mention", "tagged", "since", "until", "output-file", "no-citations", "export"} {
		_ = viper.BindPFlag("compose."+name, composeCmd.Flags().Lookup(name))
	}
}

func runCompose(cmd *cobra.Command, args []string) error {
	compositionType := args[0]

	switch compositionType {
	case "brief", "plan", "summary", "draft":
	default:
		return output.UsageError(fmt.Sprintf("unknown composition type: %s (expected brief|plan|summary|draft)", compositionType))
	}

	exportFormat := viper.GetString("compose.export")
	switch exportFormat {
	case formatMarkdown, formatJSON, "":
	default:
		return output.UsageError(fmt.Sprintf("unknown export format: %s (expected markdown or json)", exportFormat))
	}

	filter, err := composeFilter(cmd)
	if err != nil {
		return err
	}

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
	if err != nil {
		return err
	}
	objects, _, err := client.ListObjects(ctx, filter)
	if err != nil {
		return err
	}

	if len(objects) == 0 {
		fmt.Println("No matching objects found for composition.")
		return nil
	}

	var out string
	if viper.GetBool("compose.no-citations") || exportFormat == "" {
		// Plain composition without citation markers.
		out = composition.Compose(objects, compositionType)
	} else {
		// Citation-enriched composition (default path).
		result := composition.WithCitations(objects, compositionType)
		if exportFormat == formatJSON {
			b, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				return fmt.Errorf("marshal: %w", err)
			}
			out = string(b)
		} else {
			out = result.Content
		}
	}

	if outputFile := viper.GetString("compose.output-file"); outputFile != "" {
		if err := os.WriteFile(outputFile, []byte(out), 0600); err != nil {
			return fmt.Errorf("write file: %w", err)
		}
		fmt.Printf("Composition written to %s\n", outputFile)
		return nil
	}

	fmt.Print(out)
	return nil
}

// composeFilter selects the 100 most recently created objects the
// compose flags name. --since and --until bound created_at; a malformed
// tag list or date is a usage error naming the flag.
func composeFilter(cmd *cobra.Command) (dpkmsclient.ObjectFilter, error) {
	tags, err := parseTaggedFlag(cmd)
	if err != nil {
		return dpkmsclient.ObjectFilter{}, err
	}
	f := dpkmsclient.ObjectFilter{
		Tagged:  tags,
		Mention: viper.GetString("compose.mention"),
		Limit:   100,
	}
	if f.Since, f.Until, err = dateRange(viper.GetString("compose.since"), viper.GetString("compose.until")); err != nil {
		return dpkmsclient.ObjectFilter{}, err
	}
	return f, nil
}
