package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/spf13/cobra"
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

	// Check if config already exists.
	if _, err := os.Stat(cfgPath); err == nil && !nonInteractive {
		fmt.Fprintf(cmd.OutOrStdout(), "Config already exists at %s, update it? [y/N] ", cfgPath)
		ans := prompt(os.Stdin)
		if !strings.EqualFold(ans, "y") && !strings.EqualFold(ans, "yes") {
			fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
			return nil
		}
	}

	var answers wizardAnswers
	if nonInteractive {
		answers = defaultAnswers()
	} else {
		var err error
		answers, err = runWizard(cmd)
		if err != nil {
			return err
		}
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
		Provider:    "skip",
		APIKey:      "",
		StoragePath: filepath.Join(home, ".local", "share", "ctxt"),
		Pipeline:    "auto",
	}
}

// runWizard interactively collects answers from the user.
func runWizard(cmd *cobra.Command) (wizardAnswers, error) {
	out := cmd.OutOrStdout()
	defaults := defaultAnswers()

	fmt.Fprintln(out, "Welcome to ctxt! Let's get you set up in under 2 minutes.")
	fmt.Fprintln(out)

	// Step 1: AI provider.
	fmt.Fprintln(out, "Step 1/3 — AI provider")
	fmt.Fprintln(out, "  [1] OpenAI")
	fmt.Fprintln(out, "  [2] Anthropic")
	fmt.Fprintln(out, "  [3] Skip (configure later)")
	fmt.Fprint(out, "Choice [3]: ")
	providerChoice := prompt(os.Stdin)
	if providerChoice == "" {
		providerChoice = "3"
	}

	var provider, apiKey string
	switch providerChoice {
	case "1":
		provider = "openai"
		fmt.Fprint(out, "OpenAI API key: ")
		apiKey = prompt(os.Stdin)
	case "2":
		provider = "anthropic"
		fmt.Fprint(out, "Anthropic API key: ")
		apiKey = prompt(os.Stdin)
	default:
		provider = "skip"
	}

	fmt.Fprintln(out)

	// Step 2: Storage path.
	fmt.Fprintln(out, "Step 2/3 — Storage path")
	fmt.Fprintf(out, "Path [%s]: ", defaults.StoragePath)
	storagePath := prompt(os.Stdin)
	if storagePath == "" {
		storagePath = defaults.StoragePath
	}

	fmt.Fprintln(out)

	// Step 3: Default pipeline.
	fmt.Fprintln(out, "Step 3/3 — Default pipeline")
	fmt.Fprintln(out, "  [1] text  — plain text / notes")
	fmt.Fprintln(out, "  [2] url   — web pages / links")
	fmt.Fprintln(out, "  [3] auto  — detect automatically (recommended)")
	fmt.Fprint(out, "Choice [3]: ")
	pipelineChoice := prompt(os.Stdin)
	if pipelineChoice == "" {
		pipelineChoice = "3"
	}

	var pipeline string
	switch pipelineChoice {
	case "1":
		pipeline = "text"
	case "2":
		pipeline = "url"
	default:
		pipeline = "auto"
	}

	fmt.Fprintln(out)

	return wizardAnswers{
		Provider:    provider,
		APIKey:      apiKey,
		StoragePath: storagePath,
		Pipeline:    pipeline,
	}, nil
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
		{[]string{"storage", "type"}, "sqlite"},
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
	case "text":
		return "text.short"
	case "url":
		return "url.basic"
	default:
		return "auto"
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
	if a.Provider == "skip" {
		providerLine = "none (configure later)"
	}
	fmt.Fprintf(out, "  AI provider:  %s\n", providerLine)
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Run 'ctxt --help' to explore available commands.")
}

// prompt reads a single trimmed line from r (expected: os.Stdin).
func prompt(r *os.File) string {
	scanner := bufio.NewScanner(r)
	if scanner.Scan() {
		return strings.TrimSpace(scanner.Text())
	}
	return ""
}
