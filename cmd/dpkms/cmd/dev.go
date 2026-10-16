package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
	"gopkg.in/yaml.v3"
)

// ─── parent ──────────────────────────────────────────────────────────────────

var devCmd = &cobra.Command{
	Use:   "dev",
	Short: "Developer and maintenance utilities",
	Long:  `Developer utilities: lint a registry file and regenerate the CLI reference docs.`,
}

// ─── validate-registry ───────────────────────────────────────────────────────

type registryYAML struct {
	Name             string              `yaml:"name"`
	Description      string              `yaml:"description"`
	Version          string              `yaml:"version"`
	MinClientVersion string              `yaml:"min_client_version"`
	Capabilities     map[string]bool     `yaml:"capabilities"`
	Steps            []registryStepYAML  `yaml:"steps"`
	Taxonomy         []registryTaxonYAML `yaml:"taxonomy"`
}

type registryStepYAML struct {
	Name    string `yaml:"name"`
	Path    string `yaml:"path"`
	License string `yaml:"license"`
	Version string `yaml:"version"`
}

type registryTaxonYAML struct {
	Namespace    string            `yaml:"namespace"`
	Title        string            `yaml:"title"`
	Description  string            `yaml:"description"`
	Labels       map[string]string `yaml:"labels"`
	Descriptions map[string]string `yaml:"descriptions"`
}

// slugRE matches valid slug format: lowercase letters, digits, hyphens; 1-64 chars.
var slugRE = regexp.MustCompile(
	`^[a-z0-9][a-z0-9\-]{0,62}[a-z0-9]$|^[a-z0-9]$`)

var devValidateRegistryCmd = &cobra.Command{
	Use:   "validate-registry <file>",
	Short: "Lint a registry YAML against schema",
	Long: `Validate a registry YAML file against schema rules:
  - required fields: name, version
  - slug format: lowercase letters, digits, hyphens (namespace / step name)
  - duplicate namespace / step name detection

Examples:
  dpkms dev validate-registry path/to/registry.yaml
  dpkms dev validate-registry registry.yaml --format json`,
	Args: cobra.ExactArgs(1),
	RunE: runDevValidateRegistry,
}

func runDevValidateRegistry(cmd *cobra.Command, args []string) error {
	path := args[0]
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("validate-registry: read %q: %w", path, err)
	}

	var reg registryYAML
	if err := yaml.Unmarshal(data, &reg); err != nil {
		return fmt.Errorf("validate-registry: parse YAML: %w", err)
	}

	var issues []string

	if strings.TrimSpace(reg.Name) == "" {
		issues = append(issues, "missing required field: name")
	}
	if strings.TrimSpace(reg.Version) == "" {
		issues = append(issues, "missing required field: version")
	}

	seenNS := map[string]int{}
	for i, t := range reg.Taxonomy {
		ns := t.Namespace
		if !slugRE.MatchString(ns) {
			issues = append(issues, fmt.Sprintf(
				"taxonomy[%d]: invalid slug %q (must match [a-z0-9-])", i, ns))
		}
		seenNS[ns]++
	}
	for ns, count := range seenNS {
		if count > 1 {
			issues = append(issues, fmt.Sprintf(
				"taxonomy: duplicate namespace %q (%d occurrences)", ns, count))
		}
	}

	seenStep := map[string]int{}
	for i, s := range reg.Steps {
		if !slugRE.MatchString(s.Name) {
			issues = append(issues, fmt.Sprintf(
				"steps[%d]: invalid slug %q (must match [a-z0-9-])", i, s.Name))
		}
		seenStep[s.Name]++
	}
	for name, count := range seenStep {
		if count > 1 {
			issues = append(issues, fmt.Sprintf(
				"steps: duplicate name %q (%d occurrences)", name, count))
		}
	}

	ok := len(issues) == 0

	if isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), map[string]any{
			"valid":  ok,
			"issues": issues,
		})
	}

	if ok {
		fmt.Fprintf(cmd.OutOrStdout(), "OK: %s is valid\n", filepath.Base(path))
		return nil
	}

	fmt.Fprintf(cmd.OutOrStdout(), "INVALID: %s\n", filepath.Base(path))
	for _, issue := range issues {
		fmt.Fprintf(cmd.OutOrStdout(), "  - %s\n", issue)
	}
	return fmt.Errorf("registry validation failed: %d issue(s)", len(issues))
}

// ─── gen-docs ────────────────────────────────────────────────────────────────

var devGenDocsCmd = &cobra.Command{
	Use:   "gen-docs",
	Short: "Generate CLI reference docs from the cobra command tree",
	Long: `Generate Markdown reference documentation for all dpkms commands.

Output is written to <dir> (default: docs/cli), one file per command.
Existing files are overwritten.

Examples:
  dpkms dev gen-docs
  dpkms dev gen-docs --dir /tmp/dpkms-docs`,
	RunE: runDevGenDocs,
}

func init() {
	devGenDocsCmd.Flags().String("dir", "docs/cli",
		"output directory for generated docs")
}

func runDevGenDocs(cmd *cobra.Command, _ []string) error {
	outDir, _ := cmd.Flags().GetString("dir")

	if err := os.MkdirAll(outDir, 0o750); err != nil {
		return fmt.Errorf("gen-docs: mkdir %q: %w", outDir, err)
	}

	if err := doc.GenMarkdownTree(rootCmd, outDir); err != nil {
		return fmt.Errorf("gen-docs: %w", err)
	}

	if isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), map[string]any{
			"dir": outDir,
		})
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Docs written to: %s\n", outDir)
	return nil
}

// ─── registration ────────────────────────────────────────────────────────────

func init() {
	rootCmd.AddCommand(devCmd)
	devCmd.AddCommand(devValidateRegistryCmd)
	devCmd.AddCommand(devGenDocsCmd)

	// validate-registry only lints a YAML file on disk. Read.
	cliconv.WithSideEffect(devValidateRegistryCmd, cliconv.SideEffectRead)
	// gen-docs overwrites every file under the output directory. Local,
	// regenerable output, but prior contents are gone. Destructive.
	cliconv.WithSideEffect(devGenDocsCmd, cliconv.SideEffectDestructive)
}
