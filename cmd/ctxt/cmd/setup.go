package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/spf13/cobra"
	"hop.top/kit/go/console/output"
	"hop.top/kit/go/console/wizard"
)

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Interactive zero-config onboarding wizard",
	Long: `Run the ctxt setup wizard to configure your installation.

The wizard prompts for:
  - AI provider and API key (OpenAI, Anthropic, or skip)
  - Storage path (default: ~/.local/share/ctxt)
  - Default pipeline (text / url / auto)

The answers are written to ~/.config/contexthelp/ctxt.yaml ($CTXT_CONFIG
when set). Only the keys the wizard asks about change; every other key,
comment and blank line in an existing file is kept.

Answers can be piped on stdin, one line per question; an empty line
takes the default. Input that ends before the last question exits 2
and writes nothing.

Examples:
  # Interactive wizard
  ctxt setup

  # Non-interactive (CI/scripts) — writes defaults without prompting
  ctxt setup --non-interactive`,
	RunE: runSetup,
}

func init() {
	rootCmd.AddCommand(setupCmd)
	cliconv.WithSideEffect(setupCmd, cliconv.SideEffectInteractive)
	cliconv.WithExamples(setupCmd, []cliconv.Example{
		{Title: "Run the interactive wizard", Command: "ctxt setup"},
		{Title: "Write defaults non-interactively", Command: "ctxt setup --non-interactive"},
	})
	cliconv.WithNextSteps(setupCmd, []cliconv.NextStep{
		{When: "after configuration", Suggest: "ctxt doctor", Reason: "verify the resulting setup"},
		{When: "to inspect the written config", Suggest: "ctxt config show", Reason: "review the resolved values"},
	})
	// "setup" is not in kit's defaultIdempotency table; re-running the
	// wizard re-prompts (or re-writes defaults in --non-interactive),
	// setting the same keys to the same values. Mark idempotent.
	cliconv.WithIdempotency(setupCmd, cliconv.IdempotencyYes)
	setupCmd.Flags().Bool("non-interactive", false, "skip all prompts and write defaults (also triggered by CI=true)")
}

// Wizard answers: no AI provider yet, and automatic pipeline detection
// (also the inbox pipeline that choice writes).
const (
	providerSkip = "skip"
	pipelineAuto = "auto"
)

// wizardAnswers holds the values collected (or defaulted) by the wizard.
type wizardAnswers struct {
	Provider    string // "openai" | "anthropic" | "skip"
	APIKey      string
	StoragePath string
	Pipeline    string // "text" | "url" | "auto"
}

func runSetup(cmd *cobra.Command, args []string) error {
	nonInteractive, _ := cmd.Flags().GetBool("non-interactive")
	if !nonInteractive && os.Getenv("CI") != "" {
		nonInteractive = true
	}

	cfgPath := config.GetConfigPath(binName)

	var answers wizardAnswers
	if nonInteractive {
		answers = defaultAnswers()
	} else {
		_, statErr := os.Stat(cfgPath)
		a, proceed, err := runWizard(cmd, cfgPath, statErr == nil)
		if err != nil {
			return err
		}
		if !proceed {
			fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
			return nil
		}
		answers = a
	}

	err := config.EditLayer(cfgPath, func(l *config.Layer) error {
		for _, e := range setupEdits(answers) {
			if err := l.Set(e.value, e.keys...); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	printSummary(cmd, cfgPath, answers)
	return nil
}

// defaultAnswers returns safe defaults used in non-interactive mode.
func defaultAnswers() wizardAnswers {
	home, _ := os.UserHomeDir()
	return wizardAnswers{
		Provider:    providerSkip,
		APIKey:      "",
		StoragePath: filepath.Join(home, ".local", "share", "ctxt"),
		Pipeline:    pipelineAuto,
	}
}

// Wizard step keys.
const (
	stepUpdate   = "update"
	stepProvider = "provider"
	stepAPIKey   = "api_key"
	stepStorage  = "storage_path"
	stepPipeline = "pipeline"
)

// setupSteps builds the wizard's questions. With confirmUpdate, an
// existing config is confirmed first and a "no" skips every question.
func setupSteps(defaults wizardAnswers, cfgPath string, confirmUpdate bool) []wizard.Step {
	// proceed gates the questions on the update confirmation; without
	// that step its result is absent and every question is asked.
	proceed := func(v any) bool { b, ok := v.(bool); return !ok || b }
	needsKey := func(v any) bool { return v == "openai" || v == "anthropic" }

	var steps []wizard.Step
	if confirmUpdate {
		steps = append(steps, wizard.Confirm(stepUpdate,
			fmt.Sprintf("Config already exists at %s, update it?", cfgPath)).
			WithDefault(false))
	}
	return append(steps,
		wizard.Select(stepProvider, "AI provider", []wizard.Option{
			{Value: "openai", Label: "OpenAI"},
			{Value: "anthropic", Label: "Anthropic"},
			{Value: providerSkip, Label: "Skip (configure later)"},
		}).WithDefault(providerSkip).WithGroup("Step 1/3 — AI provider").
			WithWhen(stepUpdate, proceed),
		wizard.TextInput(stepAPIKey, "API key").
			WithGroup("Step 1/3 — AI provider").
			WithWhen(stepProvider, needsKey),
		wizard.TextInput(stepStorage, "Path").
			WithDefault(defaults.StoragePath).WithGroup("Step 2/3 — Storage path").
			WithWhen(stepUpdate, proceed),
		wizard.Select(stepPipeline, "Default pipeline", []wizard.Option{
			{Value: objectTypeText, Label: "text  — plain text / notes"},
			{Value: objectTypeURL, Label: "url   — web pages / links"},
			{Value: pipelineAuto, Label: "auto  — detect automatically (recommended)"},
		}).WithDefault(pipelineAuto).WithGroup("Step 3/3 — Default pipeline").
			WithWhen(stepUpdate, proceed),
	)
}

// runWizard asks the setup questions on the command's input and
// output. One kit line frontend reads every answer through one reader,
// so answers piped on stdin each reach their own question. proceed is
// false when the operator declines to update an existing config. Input
// that ends (or an interrupt) before every question is answered is a
// usage error: nothing is written.
func runWizard(cmd *cobra.Command, cfgPath string, confirmUpdate bool) (a wizardAnswers, proceed bool, err error) {
	out := cmd.OutOrStdout()
	defaults := defaultAnswers()

	w, err := wizard.New(setupSteps(defaults, cfgPath, confirmUpdate)...)
	if err != nil {
		return a, false, fmt.Errorf("setup wizard: %w", err)
	}

	fmt.Fprintln(out, "Welcome to ctxt! Let's get you set up in under 2 minutes.")

	// ForceLine: piped stdin carries answers, never a headless run.
	err = wizard.Run(cmd.Context(), w,
		wizard.ForceLine(), wizard.WithInput(cmd.InOrStdin()), wizard.WithOutput(out))
	if err != nil {
		var abort *wizard.AbortError
		if errors.As(err, &abort) {
			e := output.UsageError(
				"setup aborted before every question was answered (input ended or interrupted); nothing written")
			e.SuggestedFix = "pipe one answer per question (an empty line takes the default), or run ctxt setup --non-interactive"
			return a, false, e
		}
		return a, false, fmt.Errorf("setup wizard: %w", err)
	}
	fmt.Fprintln(out)

	r := w.Results()
	if confirmUpdate && !wizard.Bool(r, stepUpdate) {
		return a, false, nil
	}
	a = wizardAnswers{
		Provider:    wizard.Choice(r, stepProvider),
		APIKey:      wizard.String(r, stepAPIKey),
		StoragePath: wizard.String(r, stepStorage),
		Pipeline:    wizard.Choice(r, stepPipeline),
	}
	return a, true, nil
}

// setupEdit is one key the wizard writes.
type setupEdit struct {
	keys  []string
	value any
}

// setupEdits lists the keys the wizard's answers set. Only these reach
// the file: every other key keeps whatever the file already says, and
// anything the file leaves unset falls back to the built-in defaults on
// load.
func setupEdits(a wizardAnswers) []setupEdit {
	edits := []setupEdit{
		{[]string{"storage", "type"}, storageTypeSQLite},
		{[]string{"storage", "path"}, filepath.Join(a.StoragePath, "db.sqlite")},
		{[]string{"storage", "blob", "backend"}, "local"},
		{[]string{"storage", "blob", "local", "path"}, filepath.Join(a.StoragePath, "blobs")},
		{[]string{"inbox", "pipeline"}, pipelineToInboxValue(a.Pipeline)},
	}
	// A chosen provider with a key selects that LLM backend; the key
	// itself stays in the environment. Embeddings stay on the resolver
	// defaults: there is no OpenAI embedding backend.
	if a.APIKey != "" && (a.Provider == "openai" || a.Provider == "anthropic") {
		edits = append(edits, setupEdit{[]string{"providers", "llm", "backend"}, a.Provider})
	}
	return edits
}

// pipelineToInboxValue maps wizard choice to an inbox pipeline name.
func pipelineToInboxValue(p string) string {
	switch p {
	case objectTypeText:
		return pipelineTextShort
	case objectTypeURL:
		return "url.basic"
	default:
		return pipelineAuto
	}
}

// printSummary prints a concise summary after writing config.
func printSummary(cmd *cobra.Command, cfgPath string, a wizardAnswers) {
	out := cmd.OutOrStdout()
	fmt.Fprintln(out, "Configuration written successfully!")
	fmt.Fprintln(out)
	fmt.Fprintf(out, "  Config path:  %s\n", cfgPath)
	fmt.Fprintf(out, "  Storage path: %s\n", a.StoragePath)
	fmt.Fprintf(out, "  Pipeline:     %s\n", a.Pipeline)
	providerLine := a.Provider
	if a.Provider == providerSkip {
		providerLine = "none (configure later)"
	}
	fmt.Fprintf(out, "  AI provider:  %s\n", providerLine)
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Run 'ctxt --help' to explore available commands.")
}
