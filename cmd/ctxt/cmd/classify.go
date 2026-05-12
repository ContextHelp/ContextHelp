package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/spf13/cobra"
)

var classifyCmd = &cobra.Command{
	Use:   "classify <text|object_id>",
	Short: "Classify content using c12n signals",
	Long: `Run c12n classification on text or a knowledge object.

If the argument starts with "o-", it is treated as an object ID and the
object's content is fetched from storage. Otherwise the argument is
classified as raw text.

Examples:
  # Classify raw text
  ctxt classify "The server crashed after deploying v2.3"

  # Classify a stored knowledge object
  ctxt classify o-abc123

  # JSON output
  ctxt classify "some text" --format json

  # Select a named pipeline (reserved for future use)
  ctxt classify "some text" --pipeline default`,
	Args: cobra.ExactArgs(1),
	RunE: runClassify,
}

func init() {
	rootCmd.AddCommand(classifyCmd)
	cliconv.WithSideEffect(classifyCmd, cliconv.SideEffectWrite)
	cliconv.WithExamples(classifyCmd, []cliconv.Example{
		{Title: "Classify raw text", Command: "ctxt classify \"The server crashed after deploying v2.3\""},
		{Title: "Classify a stored object", Command: "ctxt classify o-abc123"},
		{Title: "JSON output", Command: "ctxt classify \"some text\" --format json"},
	})
	cliconv.WithNextSteps(classifyCmd, []cliconv.NextStep{
		{When: "on success", Suggest: "ctxt show <object_id>", Reason: "inspect persisted classification signals"},
	})
	// "classify" is not in kit's defaultIdempotency table; classifying the
	// same input twice yields the same signals but persists fresh
	// classification rows on stored objects, so naïve idempotency is no.
	cliconv.WithIdempotency(classifyCmd, cliconv.IdempotencyConditional)

	classifyCmd.Flags().String("pipeline", "", "pipeline name (reserved)")
}

// classifyResult mirrors c12n.PipelineResult for display without importing c12n.
type classifyResult struct {
	Results []classifySignal `json:"results"`
	Errors  []json.RawMessage `json:"errors,omitempty"`
}

type classifySignal struct {
	Name       string   `json:"name"`
	Type       string   `json:"signal_type"`
	Confidence float64  `json:"confidence"`
	Labels     []string `json:"labels"`
}

// classifyFunc is the classification backend. Overridden in classify_cgo.go
// when cgo is available; defaults to a stub returning an error.
var classifyFunc = func(text string) (*classifyResult, error) {
	return nil, fmt.Errorf(
		"classification unavailable: binary built without cgo support",
	)
}

func runClassify(cmd *cobra.Command, args []string) error {
	input := args[0]
	if input == "" {
		return fmt.Errorf("classify requires non-empty input")
	}

	text, err := resolveClassifyInput(input)
	if err != nil {
		return err
	}

	result, err := classifyFunc(text)
	if err != nil {
		return err
	}

	if isJSONOutput() {
		return outputJSON(os.Stdout, result)
	}

	printClassifyTable(result)
	return nil
}

// resolveClassifyInput returns the text to classify. When input starts with
// "o-", the object is fetched from storage; otherwise input is returned
// verbatim.
func resolveClassifyInput(input string) (string, error) {
	if !strings.HasPrefix(input, "o-") {
		return input, nil
	}

	svc, cleanup, err := newService()
	if err != nil {
		return "", fmt.Errorf("open storage: %w", err)
	}
	defer cleanup()

	obj, err := svc.GetObject(context.Background(), input)
	if err != nil {
		return "", fmt.Errorf("get object %s: %w", input, err)
	}

	if len(obj.Summaries) > 0 {
		return obj.Summaries[0], nil
	}
	if obj.TextContent != "" {
		return obj.TextContent, nil
	}
	if obj.RawContent != "" {
		return obj.RawContent, nil
	}
	return "", fmt.Errorf("object %s has no classifiable content", input)
}

func printClassifyTable(r *classifyResult) {
	headers := []string{"SIGNAL", "TYPE", "CONFIDENCE", "LABELS"}
	var rows [][]string
	for _, s := range r.Results {
		rows = append(rows, []string{
			s.Name,
			s.Type,
			fmt.Sprintf("%.2f", s.Confidence),
			fmt.Sprintf("%v", s.Labels),
		})
	}
	printTable(os.Stdout, headers, rows)

	if len(r.Errors) > 0 {
		fmt.Fprintf(os.Stderr, "\n%d signal error(s) during classification\n",
			len(r.Errors))
	}
}
