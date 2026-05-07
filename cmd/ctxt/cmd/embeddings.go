package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
	"github.com/spf13/cobra"
)

// embeddingsCmd implements the operator-facing CLI surface from ADR-071
// §"Operator-facing CLI surface". Phase 1 (T-0582) ships `list` and
// `register`; later phases ship `migrate` (T-0584), `set-default` (T-0584),
// `deprecate` / `purge` (T-0585). The verbs that are still TODO are wired as
// stubs that exit with a "not yet implemented" error so the CLI shape is
// discoverable today and the help text is honest.
var embeddingsCmd = &cobra.Command{
	Use:   "embeddings",
	Short: "Manage embedding-model registry (ADR-071)",
	Long: `Manage embedding-model registry and migrations (ADR-071).

The registry tracks every embedding model that has produced rows in the
embeddings table. Phase 1 (T-0582) ships read + register-candidate; later
phases ship migrate, set-default, deprecate, and purge.

Examples:
  # List registered models, default flag, deprecation status
  ctxt embeddings list

  # Register a candidate model from a config file
  ctxt embeddings register openai-text-embedding-3-small@2025-01-15 \
      --config ./openai-3-small.json
`,
}

var embeddingsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List registered embedding models",
	RunE:  runEmbeddingsList,
}

var (
	embeddingsRegisterConfig    string
	embeddingsRegisterProvider  string
	embeddingsRegisterDimension int
	embeddingsRegisterDefault   bool
)

var embeddingsRegisterCmd = &cobra.Command{
	Use:   "register <model_id>",
	Short: "Register a candidate embedding model",
	Long: `Register a candidate embedding model.

The model_id should follow ADR-071's "<provider-name>@<date>" convention,
for example "openai-text-embedding-3-small@2025-01-15". Configuration may
be supplied via --config <path> (JSON file) or, when --config is omitted,
$EDITOR opens with an empty JSON skeleton for the operator to fill in.

This command does NOT flip the active default — call ctxt embeddings
set-default after coverage + recall verification (Phase 3, T-0584).`,
	Args: cobra.ExactArgs(1),
	RunE: runEmbeddingsRegister,
}

// Stub-only verbs: the CLI shape is documented in ADR-071 but the
// implementation lands in later cohort tasks. Each stub exits non-zero with
// a clear pointer to the owning task so operators are not surprised.
var embeddingsMigrateCmd = &cobra.Command{
	Use:    "migrate",
	Short:  "Migrate corpus to a new embedding model (T-0584)",
	Hidden: false,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return errors.New("ctxt embeddings migrate: cmd not yet implemented (Phase 3, T-0584)")
	},
}

var embeddingsSetDefaultCmd = &cobra.Command{
	Use:    "set-default <model_id>",
	Short:  "Atomically flip the active default model (T-0584)",
	Hidden: false,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return errors.New("ctxt embeddings set-default: cmd not yet implemented (Phase 3, T-0584)")
	},
}

var embeddingsDeprecateCmd = &cobra.Command{
	Use:    "deprecate <model_id>",
	Short:  "Schedule retirement of a registered model (T-0585)",
	Hidden: false,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return errors.New("ctxt embeddings deprecate: cmd not yet implemented (Phase 4, T-0585)")
	},
}

var embeddingsPurgeCmd = &cobra.Command{
	Use:    "purge <model_id>",
	Short:  "Delete embedding rows for a deprecated model (T-0585)",
	Hidden: false,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return errors.New("ctxt embeddings purge: cmd not yet implemented (Phase 4, T-0585)")
	},
}

func init() {
	rootCmd.AddCommand(embeddingsCmd)
	embeddingsCmd.AddCommand(embeddingsListCmd)
	embeddingsCmd.AddCommand(embeddingsRegisterCmd)
	embeddingsCmd.AddCommand(embeddingsMigrateCmd)
	embeddingsCmd.AddCommand(embeddingsSetDefaultCmd)
	embeddingsCmd.AddCommand(embeddingsDeprecateCmd)
	embeddingsCmd.AddCommand(embeddingsPurgeCmd)

	embeddingsRegisterCmd.Flags().StringVar(&embeddingsRegisterConfig,
		"config", "", "path to a JSON config file (when omitted, $EDITOR opens an empty skeleton)")
	embeddingsRegisterCmd.Flags().StringVar(&embeddingsRegisterProvider,
		"provider", "", "provider name (e.g. openai, ollama, voyage)")
	embeddingsRegisterCmd.Flags().IntVar(&embeddingsRegisterDimension,
		"dimension", 0, "embedding vector dimension")
	embeddingsRegisterCmd.Flags().BoolVar(&embeddingsRegisterDefault,
		"make-default", false, "mark this model as the active default after registration (clears the previous default; coverage + recall guards do NOT run in Phase 1)")
}

// embeddingsListItem is the JSON shape of one row in `ctxt embeddings list`.
//
// The field names here are the contract surface pinned by
// `contracts/embeddings-list.eva.yaml` (T-0586). Keep them stable: they
// appear in dashboards and downstream operator scripts. Adding fields is
// fine; renaming or removing is a breaking change. `coverage` is the
// fraction of distinct objects that have an embedding row under this
// model_id (0.0 .. 1.0); on an empty corpus it is reported as 1.0.
type embeddingsListItem struct {
	ModelID      string  `json:"model_id"`
	Provider     string  `json:"provider"`
	Dimension    int     `json:"dimension"`
	IsDefault    bool    `json:"is_default"`
	RegisteredAt string  `json:"registered_at"`
	DeprecatedAt *string `json:"deprecated_at"`
	Coverage     float64 `json:"coverage"`
}

func runEmbeddingsList(cmd *cobra.Command, _ []string) error {
	r, cleanup, err := newEmbeddingsRegistry()
	if err != nil {
		return err
	}
	defer cleanup()

	models, err := r.ListWithCoverage(context.Background())
	if err != nil {
		return fmt.Errorf("list embedding models: %w", err)
	}

	out := make([]embeddingsListItem, 0, len(models))
	for _, m := range models {
		item := embeddingsListItem{
			ModelID:      m.ModelID,
			Provider:     m.Provider,
			Dimension:    m.Dimension,
			IsDefault:    m.IsDefault,
			RegisteredAt: m.RegisteredAt.Format("2006-01-02T15:04:05Z07:00"),
			Coverage:     m.Coverage,
		}
		if m.DeprecatedAt != nil {
			ts := m.DeprecatedAt.Format("2006-01-02T15:04:05Z07:00")
			item.DeprecatedAt = &ts
		}
		out = append(out, item)
	}

	if isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), map[string]any{"models": out})
	}

	if len(out) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No embedding models registered.")
		return nil
	}
	headers := []string{"Model ID", "Provider", "Dim", "Default", "Coverage", "Registered", "Deprecated"}
	rows := make([][]string, 0, len(out))
	for _, m := range out {
		def := ""
		if m.IsDefault {
			def = "*"
		}
		dep := ""
		if m.DeprecatedAt != nil {
			dep = *m.DeprecatedAt
		}
		rows = append(rows, []string{
			m.ModelID,
			m.Provider,
			fmt.Sprintf("%d", m.Dimension),
			def,
			fmt.Sprintf("%.2f", m.Coverage),
			m.RegisteredAt,
			dep,
		})
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Embedding models (%d)\n\n", len(out))
	printTable(cmd.OutOrStdout(), headers, rows)
	return nil
}

func runEmbeddingsRegister(cmd *cobra.Command, args []string) error {
	modelID := args[0]
	if modelID == "" {
		return fmt.Errorf("model_id is required")
	}

	configJSON, err := loadOrEditConfig(embeddingsRegisterConfig)
	if err != nil {
		return err
	}

	r, cleanup, err := newEmbeddingsRegistry()
	if err != nil {
		return err
	}
	defer cleanup()

	m := registry.Model{
		ModelID:    modelID,
		Provider:   embeddingsRegisterProvider,
		Dimension:  embeddingsRegisterDimension,
		ConfigJSON: configJSON,
	}
	if err := r.Register(context.Background(), m, embeddingsRegisterDefault); err != nil {
		return fmt.Errorf("register %s: %w", modelID, err)
	}
	if isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), map[string]any{
			"model_id":    modelID,
			"provider":    embeddingsRegisterProvider,
			"dimension":   embeddingsRegisterDimension,
			"is_default":  embeddingsRegisterDefault,
			"config_json": configJSON,
		})
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Registered %s\n", modelID)
	if embeddingsRegisterDefault {
		fmt.Fprintln(cmd.OutOrStdout(), "Marked as default. Note: coverage + recall guards do NOT run in Phase 1; you are responsible for verifying recall.")
	}
	return nil
}

// loadOrEditConfig returns the JSON config for a registration. When path is
// provided, the file is read verbatim. When path is empty, an empty JSON
// skeleton is opened in $EDITOR (or vi as a last resort) and the saved
// contents are returned. An empty save resolves to "{}".
func loadOrEditConfig(path string) (string, error) {
	if path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read config %s: %w", path, err)
		}
		out := strings.TrimSpace(string(b))
		if out == "" {
			out = "{}"
		}
		return out, nil
	}

	// $EDITOR fallback. Default to vi, but honour $EDITOR / $VISUAL when set.
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}

	tmp, err := os.CreateTemp("", "ctxt-embeddings-register-*.json")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	skeleton := "{\n  \"endpoint\": \"\",\n  \"model\": \"\",\n  \"api_key_env\": \"\"\n}\n"
	if _, err := tmp.WriteString(skeleton); err != nil {
		tmp.Close()
		return "", fmt.Errorf("write skeleton: %w", err)
	}
	tmp.Close()

	c := exec.Command(editor, tmpPath)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return "", fmt.Errorf("editor %s: %w", filepath.Base(editor), err)
	}
	b, err := os.ReadFile(tmpPath)
	if err != nil {
		return "", fmt.Errorf("read edited config: %w", err)
	}
	out := strings.TrimSpace(string(b))
	if out == "" {
		out = "{}"
	}
	return out, nil
}

// newEmbeddingsRegistry resolves a registry.Store via the same service-init
// path as other CLI commands. Returns a cleanup callback the caller must
// defer; it tears down the underlying driver. The registry needs a
// *sqlite.Driver so it can grab DB() — postgres support arrives when the
// postgres backend grows the equivalent CRUD wiring (out of scope for T-0582
// per the spec).
func newEmbeddingsRegistry() (*registry.Store, func(), error) {
	svc, cleanup, err := newService()
	if err != nil {
		return nil, nil, err
	}
	d, ok := svc.Store.(*sqlite.Driver)
	if !ok {
		cleanup()
		return nil, nil, fmt.Errorf("ctxt embeddings: registry currently requires the sqlite backend (have %T) — postgres support lands with the postgres registry CRUD wiring", svc.Store)
	}
	return registry.New(d.DB()), cleanup, nil
}

