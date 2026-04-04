package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Interactive zero-config onboarding wizard",
	Long: `Run the ctxt setup wizard to configure your installation.

The wizard prompts for:
  - AI provider and API key (OpenAI, Anthropic, or skip)
  - Storage path (default: ~/.local/share/ctxt)
  - Default pipeline (text / url / auto)

The resulting config is written to ~/.config/contexthelp/config.yaml.

Examples:
  # Interactive wizard
  ctxt init

  # Non-interactive (CI/scripts) — writes defaults without prompting
  ctxt init --non-interactive`,
	RunE: runInit,
}

func init() {
	rootCmd.AddCommand(initCmd)
	initCmd.Flags().Bool("non-interactive", false, "skip all prompts and write defaults (also triggered by CI=true)")
}

// wizardAnswers holds the values collected (or defaulted) by the wizard.
type wizardAnswers struct {
	Provider    string // "openai" | "anthropic" | "skip"
	APIKey      string
	StoragePath string
	Pipeline    string // "text" | "url" | "auto"
}

func runInit(cmd *cobra.Command, args []string) error {
	nonInteractive, _ := cmd.Flags().GetBool("non-interactive")
	if !nonInteractive && os.Getenv("CI") != "" {
		nonInteractive = true
	}

	cfgPath := config.GetConfigPath()

	// Check if config already exists.
	if _, err := os.Stat(cfgPath); err == nil && !nonInteractive {
		fmt.Fprintf(cmd.OutOrStdout(), "Config already exists at %s, overwrite? [y/N] ", cfgPath)
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

	newCfg, err := buildConfig(answers)
	if err != nil {
		return fmt.Errorf("build config: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(cfgPath), 0750); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	if err := config.WriteBack(newCfg, cfgPath); err != nil {
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

// buildConfig constructs a Config from wizard answers.
func buildConfig(a wizardAnswers) (*config.Config, error) {
	// Start from a clean Config; setDefaults will not run here — we set
	// only what the wizard asked about and leave everything else at zero
	// so that config.Load() fills in viper defaults on next run.
	dbPath := filepath.Join(a.StoragePath, "db.sqlite")
	blobPath := filepath.Join(a.StoragePath, "blobs")

	c := &config.Config{
		Version: 1,
		Storage: config.StorageConfig{
			Type: "sqlite",
			Path: dbPath,
			Blob: config.BlobConfig{
				Backend: "local",
				Local:   config.BlobLocalConfig{Path: blobPath},
			},
		},
		Inbox: config.InboxConfig{
			Pipeline: pipelineToInboxValue(a.Pipeline),
		},
	}

	// Embed API key as env-var reference when the user chose a provider.
	switch a.Provider {
	case "openai":
		if a.APIKey != "" {
			c.Providers.LLM = config.ProviderBackendConfig{Backend: "openai"}
			c.Providers.Embedding = config.ProviderBackendConfig{Backend: "openai"}
		}
	case "anthropic":
		if a.APIKey != "" {
			c.Providers.LLM = config.ProviderBackendConfig{Backend: "anthropic"}
		}
	}

	return c, nil
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
