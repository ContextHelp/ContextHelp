package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
	"gopkg.in/yaml.v3"
)

// ─── parent ──────────────────────────────────────────────────────────────────

var devCmd = &cobra.Command{
	Use:   "dev",
	Short: "Developer and maintenance utilities",
	Long:  `Developer utilities for inspecting, maintaining, and scaffolding ctxt plugins.`,
}

// ─── reindex-vectors ─────────────────────────────────────────────────────────

var devReindexVectorsCmd = &cobra.Command{
	Use:   "reindex-vectors",
	Short: "Re-embed objects that are missing vector embeddings",
	Long: `Re-embed all active knowledge objects that currently lack a stored embedding
vector. Uses the configured embedding provider (Ollama by default).

Objects with no raw content or summaries are skipped and counted as failed.

Examples:
  # Re-index all objects missing embeddings
  dpkms dev reindex-vectors

  # JSON output (indexed/failed counts + object IDs)
  dpkms dev reindex-vectors --format json`,
	RunE: runDevReindexVectors,
}

func runDevReindexVectors(cmd *cobra.Command, _ []string) error {
	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx := context.Background()

	factory := providers.NewFactory(cfg.Providers, nil)
	ep := factory.Embedding()

	fmt.Fprintln(cmd.OutOrStdout(), "Starting vector re-indexing...")

	indexed, failed, err := svc.ReindexVectors(ctx, ep)
	if err != nil {
		return fmt.Errorf("reindex-vectors: %w", err)
	}

	if isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), map[string]any{
			"indexed": indexed,
			"failed":  failed,
		})
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Done. indexed=%d  failed=%d\n", indexed, failed)
	return nil
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

// ─── init-plugin ─────────────────────────────────────────────────────────────

var devInitPluginCmd = &cobra.Command{
	Use:   "init-plugin <slug>",
	Short: "Scaffold a new plugin with manifest, Go stub, and README",
	Long: `Scaffold a new ctxt plugin directory under plugins/<slug>/:
  - manifest.yaml   — plugin manifest (name, version, description)
  - plugin.go       — Go stub implementing pluginapi.Plugin
  - README.md       — minimal plugin documentation

Examples:
  dpkms dev init-plugin my-plugin
  dpkms dev init-plugin sentiment --dir ./custom-plugins`,
	Args: cobra.ExactArgs(1),
	RunE: runDevInitPlugin,
}

func init() {
	devInitPluginCmd.Flags().String("dir", "plugins",
		"parent directory for the new plugin")
}

const pluginManifestTmpl = `name: {{ .Slug }}
version: "0.1.0"
description: "TODO: describe {{ .Slug }}"
author: ""
`

const pluginGoTmpl = `// Package {{ .PkgName }} is a ctxt plugin scaffold.
package {{ .PkgName }}

import (
	"context"

	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

// Plugin implements pluginapi.Plugin.
type Plugin struct{}

// New returns an uninitialised Plugin for registration.
func New() *Plugin { return &Plugin{} }

func (p *Plugin) Name() string    { return "{{ .Slug }}" }
func (p *Plugin) Version() string { return "0.1.0" }

func (p *Plugin) Init(_ context.Context, _ map[string]interface{}, _ pluginapi.Deps) error {
	return nil
}

func (p *Plugin) PipelineSteps() []pluginapi.PipelineStep { return nil }

func (p *Plugin) Close(_ context.Context) error { return nil }
`

const pluginReadmeTmpl = `# {{ .Slug }}

A ctxt plugin.

## Overview

TODO: describe what this plugin does.

## Configuration

` + "```yaml" + `
plugins:
  {{ .Slug }}:
    enabled: true
` + "```" + `

## Development

See [ctxt plugin development guide](../../docs/plugins.md).
`

type pluginTmplData struct {
	Slug    string
	PkgName string
}

func runDevInitPlugin(cmd *cobra.Command, args []string) error {
	slug := args[0]
	if !slugRE.MatchString(slug) {
		return fmt.Errorf("init-plugin: invalid slug %q (must match [a-z0-9-])", slug)
	}

	parentDir, _ := cmd.Flags().GetString("dir")
	pluginDir := filepath.Join(parentDir, slug)

	if _, err := os.Stat(pluginDir); err == nil {
		return fmt.Errorf("init-plugin: directory %q already exists", pluginDir)
	}

	if err := os.MkdirAll(pluginDir, 0o750); err != nil {
		return fmt.Errorf("init-plugin: mkdir %q: %w", pluginDir, err)
	}

	pkgName := strings.ReplaceAll(slug, "-", "")
	data := pluginTmplData{Slug: slug, PkgName: pkgName}

	files := []struct {
		name string
		tmpl string
	}{
		{"manifest.yaml", pluginManifestTmpl},
		{"plugin.go", pluginGoTmpl},
		{"README.md", pluginReadmeTmpl},
	}

	for _, f := range files {
		path := filepath.Join(pluginDir, f.name)
		t, err := template.New(f.name).Parse(f.tmpl)
		if err != nil {
			return fmt.Errorf("init-plugin: parse template %s: %w", f.name, err)
		}
		fh, err := os.Create(path)
		if err != nil {
			return fmt.Errorf("init-plugin: create %s: %w", path, err)
		}
		if err := t.Execute(fh, data); err != nil {
			fh.Close()
			return fmt.Errorf("init-plugin: render %s: %w", f.name, err)
		}
		fh.Close()
	}

	if isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), map[string]any{
			"slug":    slug,
			"dir":     pluginDir,
			"created": []string{"manifest.yaml", "plugin.go", "README.md"},
		})
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Created plugin scaffold: %s\n", pluginDir)
	fmt.Fprintf(cmd.OutOrStdout(), "  %s/manifest.yaml\n", pluginDir)
	fmt.Fprintf(cmd.OutOrStdout(), "  %s/plugin.go\n", pluginDir)
	fmt.Fprintf(cmd.OutOrStdout(), "  %s/README.md\n", pluginDir)
	return nil
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
	devCmd.AddCommand(devReindexVectorsCmd)
	devCmd.AddCommand(devValidateRegistryCmd)
	devCmd.AddCommand(devInitPluginCmd)
	devCmd.AddCommand(devGenDocsCmd)

	// validate-registry only lints a YAML file on disk. Read.
	cliconv.WithSideEffect(devValidateRegistryCmd, cliconv.SideEffectRead)
	// reindex-vectors adds missing embeddings; existing vectors are left
	// alone, so re-running converges. Write.
	cliconv.WithSideEffect(devReindexVectorsCmd, cliconv.SideEffectWrite)
	// init-plugin scaffolds a new directory; it refuses to clobber an
	// existing slug. Write.
	cliconv.WithSideEffect(devInitPluginCmd, cliconv.SideEffectWrite)
	// gen-docs overwrites every file under the output directory. Local,
	// regenerable output, but prior contents are gone. Destructive.
	cliconv.WithSideEffect(devGenDocsCmd, cliconv.SideEffectDestructive)
}
