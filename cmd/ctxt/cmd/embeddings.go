package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/postgres"
	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
	"github.com/spf13/cobra"
	"hop.top/kit/go/console/output"
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

  # Register a candidate model; its dimension is measured from the provider
  ctxt embeddings register ollama-snowflake-arctic-embed2@2026-09-26 \
      --embedding-model snowflake-arctic-embed2
`,
}

var embeddingsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List registered embedding models",
	Long: `Print every embedding model the registry has seen.

The table reports model_id, provider, vector dimension, default marker,
coverage fraction, and registered / deprecated timestamps. Coverage is
the fraction of distinct objects that have an embedding row under this
model_id (0.0..1.0); on an empty corpus it is reported as 1.0.`,
	RunE: runEmbeddingsList,
}

var (
	// embeddingsRegisterFlags holds `embeddings register`'s --embedding-* values.
	embeddingsRegisterFlags embeddings.Overrides
	// embeddingsRegisterDimension is --dimension: a check against the
	// measured dimension, 0 when unset.
	embeddingsRegisterDimension int
	// embeddingsRegisterHTTPClient, when non-nil, carries register's
	// provider calls. Tests set it to replay recorded provider traffic.
	embeddingsRegisterHTTPClient *http.Client
)

// embeddingsRegisterProbeText is the fixed text register embeds to measure
// a model's dimension.
const embeddingsRegisterProbeText = "ctxt embedding dimension probe"

// Index states `embeddings register` reports.
const (
	// registerIndexReady: the model's per-model index exists.
	registerIndexReady = "ready"
	// registerIndexUnsupported: the storage backend does not build
	// per-model indexes yet; the model is registered without one.
	registerIndexUnsupported = "unsupported"
)

var embeddingsRegisterCmd = &cobra.Command{
	Use:   "register <model_id>",
	Short: "Register a candidate embedding model, measuring its dimension",
	Long: `Register a candidate embedding model.

The model_id names the model's vector space, conventionally
"<provider>-<model>@<date>" (for example
"ollama-snowflake-arctic-embed2@2026-09-26"). It may use letters, digits
and . _ : @ / + -, start alphanumeric, and be at most 200 characters.

The provider is resolved like every embedding command: --embedding-*
flags, then CTXT_EMBEDDING_* env, then -c providers.embedding.*, then
providers.embedding in the config file, then the defaults (see ctxt
embeddings provider). Register then embeds a fixed probe string and
records the length of the returned vector as the model's dimension. The
provider must be reachable: if the probe fails, nothing is registered.

--dimension is an optional check: when the measured dimension differs,
the command fails and registers nothing.

The registry row stores the resolved backend as the provider and, in
config_json, the backend, model, endpoint and api_key_env (the variable
NAME, never the key). Backend and model are the model's fixed identity from
then on; endpoint and api_key_env stay overridable per run.

Registering starts dual-writing new ingests for the model. It does not
change the default model: promote it with ctxt embeddings set-default once
coverage and recall are verified.`,
	Args: cobra.ExactArgs(1),
	RunE: runEmbeddingsRegister,
}

// embeddingsProviderFlags holds `embeddings provider`'s --embedding-* values.
var embeddingsProviderFlags embeddings.Overrides

var embeddingsProviderCmd = &cobra.Command{
	Use:   "provider [model_id]",
	Short: "Show the resolved embedding provider and where each setting came from",
	Long: `Print the embedding provider this command line resolves to: backend,
model, endpoint, api_key_env (the variable NAME; the key is never printed)
and dimension, each with the layer that supplied it.

Layers, highest first: flag (--embedding-*), env (CTXT_EMBEDDING_*),
config-override (-c providers.embedding.*=...), registry (only when a
model_id is given: that registered model's own settings), config
(providers.embedding in the config file), default.

With a model_id, the registry entry for that model is consulted, the way a
command targeting that model resolves it: its backend, model and dimension
are fixed by the entry (labelled "fixed by registry"), and only endpoint and
api_key_env can be overridden.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runEmbeddingsProvider,
}

// Stub-only verbs: the CLI shape is documented in ADR-071 but the
// implementation lands in later cohort tasks. Each stub exits non-zero with
// a clear pointer to the owning task so operators are not surprised.
var embeddingsMigrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Migrate corpus to a new embedding model (T-0584)",
	Long: `Migrate the existing corpus to a new embedding model.

Re-embeds every object using the named model, writing rows alongside the
current default until coverage and recall guards pass. Implementation lands
in Phase 3 (T-0584); the stub exits non-zero so callers fail loudly.`,
	Hidden: false,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return errors.New("ctxt embeddings migrate: cmd not yet implemented (Phase 3, T-0584)")
	},
}

var embeddingsSetDefaultCmd = &cobra.Command{
	Use:   "set-default <model_id>",
	Short: "Atomically flip the active default model (T-0584)",
	Long: `Atomically promote a registered embedding model to the active default.

The flip is gated on coverage and recall verification (see ctxt embeddings
list). Implementation lands in Phase 3 (T-0584); the stub exits non-zero
so callers fail loudly.`,
	Hidden: false,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return errors.New("ctxt embeddings set-default: cmd not yet implemented (Phase 3, T-0584)")
	},
}

var embeddingsDeprecateCmd = &cobra.Command{
	Use:   "deprecate <model_id>",
	Short: "Schedule retirement of a registered model (T-0585)",
	Long: `Mark a registered embedding model as deprecated.

Sets the deprecated_at timestamp so the model becomes a candidate for
purge. Already-written embedding rows are preserved until purge runs.
Implementation lands in Phase 4 (T-0585); the stub exits non-zero so
callers fail loudly.`,
	Hidden: false,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return errors.New("ctxt embeddings deprecate: cmd not yet implemented (Phase 4, T-0585)")
	},
}

var embeddingsPurgeCmd = &cobra.Command{
	Use:   "purge <model_id>",
	Short: "Delete embedding rows for a deprecated model (T-0585)",
	Long: `Delete every embedding row produced by a deprecated model.

Only operates on models that have a non-null deprecated_at timestamp.
The registry record itself is retained for audit history. Implementation
lands in Phase 4 (T-0585); the stub exits non-zero so callers fail loudly.`,
	Hidden: false,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return errors.New("ctxt embeddings purge: cmd not yet implemented (Phase 4, T-0585)")
	},
}

func init() {
	rootCmd.AddCommand(embeddingsCmd)
	embeddingsCmd.AddCommand(embeddingsListCmd)
	embeddingsCmd.AddCommand(embeddingsRegisterCmd)
	embeddingsCmd.AddCommand(embeddingsProviderCmd)
	embeddingsCmd.AddCommand(embeddingsMigrateCmd)
	embeddingsCmd.AddCommand(embeddingsSetDefaultCmd)
	embeddingsCmd.AddCommand(embeddingsDeprecateCmd)
	embeddingsCmd.AddCommand(embeddingsPurgeCmd)

	// 12fcc conformance: side-effect + idempotency annotations.
	// list is read; register / migrate / set-default mutate registry
	// state (write-shared, runs against the dpkms DB); deprecate /
	// purge are destructive (purge especially deletes embedding rows).
	cliconv.WithSideEffect(embeddingsListCmd, cliconv.SideEffectRead)
	cliconv.WithSideEffect(embeddingsProviderCmd, cliconv.SideEffectRead)
	cliconv.WithIdempotency(embeddingsProviderCmd, cliconv.IdempotencyYes)
	cliconv.WithSideEffect(embeddingsRegisterCmd, cliconv.SideEffectWriteShared)
	cliconv.WithSideEffect(embeddingsMigrateCmd, cliconv.SideEffectWriteShared)
	cliconv.WithSideEffect(embeddingsSetDefaultCmd, cliconv.SideEffectWriteShared)
	cliconv.WithSideEffect(embeddingsDeprecateCmd, cliconv.SideEffectDestructiveShared)
	cliconv.WithSideEffect(embeddingsPurgeCmd, cliconv.SideEffectDestructiveShared)

	// 12fcc strict-gate: deprecate flips a model's lifecycle bit and
	// purge deletes embedding rows for a deprecated model. Both opt
	// into kit's typed-token confirmation flow.
	cliconv.WithDestructiveToken(embeddingsDeprecateCmd)
	cliconv.WithDestructiveToken(embeddingsPurgeCmd)

	// Kit verb defaults only cover "list" here. Tag the rest:
	// register is non-idempotent (creates a candidate record);
	// migrate / set-default / deprecate / purge are non-idempotent
	// in the strict sense (replay re-runs the mutation against
	// changed state).
	cliconv.WithIdempotency(embeddingsRegisterCmd, cliconv.IdempotencyNo)
	cliconv.WithIdempotency(embeddingsMigrateCmd, cliconv.IdempotencyNo)
	cliconv.WithIdempotency(embeddingsSetDefaultCmd, cliconv.IdempotencyYes)
	cliconv.WithIdempotency(embeddingsDeprecateCmd, cliconv.IdempotencyYes)
	cliconv.WithIdempotency(embeddingsPurgeCmd, cliconv.IdempotencyYes)

	// 12fcc strict-gate: examples + next-steps on every leaf.
	cliconv.WithExamples(embeddingsListCmd, []cliconv.Example{
		{Title: "List registered models", Command: "ctxt embeddings list"},
		{Title: "JSON for scripting", Command: "ctxt embeddings list --format json"},
	})
	cliconv.WithExamples(embeddingsProviderCmd, []cliconv.Example{
		{Title: "Show the resolved provider", Command: "ctxt embeddings provider"},
		{Title: "Check a one-run override", Command: "ctxt embeddings provider --embedding-endpoint http://127.0.0.1:11555 --format json"},
		{Title: "Resolve a registered model", Command: "ctxt embeddings provider ollama-snowflake-arctic-embed2@2026-09-26"},
	})
	cliconv.WithExamples(embeddingsRegisterCmd, []cliconv.Example{
		{Title: "Register the configured provider's model", Command: "ctxt embeddings register ollama-nomic-embed-text@2026-09-26"},
		{Title: "Register a specific model and check its dimension", Command: "ctxt embeddings register ollama-snowflake-arctic-embed2@2026-09-26 --embedding-model snowflake-arctic-embed2 --dimension 1024"},
		{Title: "Probe through a tunnel, JSON output", Command: "ctxt embeddings register ollama-snowflake-arctic-embed2@2026-09-26 --embedding-model snowflake-arctic-embed2 --embedding-endpoint http://127.0.0.1:11555 --format json"},
	})
	cliconv.WithNextSteps(embeddingsRegisterCmd, []cliconv.NextStep{
		{When: "on success", Suggest: "ctxt embeddings list", Reason: "confirm coverage + default marker after registration"},
		{When: "after coverage + recall verification", Suggest: "ctxt embeddings set-default <model_id>", Reason: "promote the candidate once recall guards pass"},
	})
	cliconv.WithExamples(embeddingsMigrateCmd, []cliconv.Example{
		{Title: "Migrate corpus to a new model", Command: "ctxt embeddings migrate voyage-3-large@2025-08-01"},
		{Title: "Dry-run a migration", Command: "ctxt embeddings migrate voyage-3-large@2025-08-01 --dry-run"},
	})
	cliconv.WithNextSteps(embeddingsMigrateCmd, []cliconv.NextStep{
		{When: "on success", Suggest: "ctxt embeddings list", Reason: "check coverage reached 1.0 before flipping the default"},
	})
	cliconv.WithExamples(embeddingsSetDefaultCmd, []cliconv.Example{
		{Title: "Promote a registered model to default", Command: "ctxt embeddings set-default voyage-3-large@2025-08-01"},
		{Title: "JSON output", Command: "ctxt embeddings set-default voyage-3-large@2025-08-01 --format json"},
	})
	cliconv.WithNextSteps(embeddingsSetDefaultCmd, []cliconv.NextStep{
		{When: "on success", Suggest: "ctxt embeddings list", Reason: "verify the new default marker landed"},
		{When: "after verification", Suggest: "ctxt embeddings deprecate <old_model_id>", Reason: "schedule the previous default for retirement"},
	})
	cliconv.WithExamples(embeddingsDeprecateCmd, []cliconv.Example{
		{Title: "Mark a model deprecated", Command: "ctxt embeddings deprecate openai-text-embedding-ada-002@2022-12-15"},
	})
	cliconv.WithNextSteps(embeddingsDeprecateCmd, []cliconv.NextStep{
		{When: "after grace period", Suggest: "ctxt embeddings purge <model_id>", Reason: "delete the embedding rows once no consumer is reading them"},
	})
	cliconv.WithExamples(embeddingsPurgeCmd, []cliconv.Example{
		{Title: "Delete rows for a deprecated model", Command: "ctxt embeddings purge openai-text-embedding-ada-002@2022-12-15"},
	})
	cliconv.WithNextSteps(embeddingsPurgeCmd, []cliconv.NextStep{
		{When: "on success", Suggest: "ctxt embeddings list", Reason: "confirm coverage for remaining models is unaffected"},
	})

	embeddings.AddFlags(embeddingsProviderCmd.Flags(), &embeddingsProviderFlags)

	embeddings.AddFlags(embeddingsRegisterCmd.Flags(), &embeddingsRegisterFlags)
	embeddingsRegisterCmd.Flags().IntVar(&embeddingsRegisterDimension,
		"dimension", 0, "expected vector dimension; fails when the measured dimension differs (the stored dimension is always measured)")
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

// embeddingsProviderDoc is the JSON shape of `ctxt embeddings provider`.
type embeddingsProviderDoc struct {
	ModelID   string            `json:"model_id"`
	Backend   string            `json:"backend"`
	Model     string            `json:"model"`
	Endpoint  string            `json:"endpoint"`
	APIKeyEnv string            `json:"api_key_env"`
	Dimension int               `json:"dimension"`
	Sources   map[string]string `json:"sources"`
	// Fixed lists the settings a targeted registered model fixes; runtime
	// overrides cannot change them.
	Fixed []string `json:"fixed"`
}

func runEmbeddingsProvider(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	r := newEmbeddingResolver()
	req := embeddings.Request{Overrides: embeddingsProviderFlags}
	if len(args) == 1 {
		req.ModelID = args[0]
		reg, cleanup, err := newEmbeddingsRegistry()
		if err != nil {
			return err
		}
		defer cleanup()
		r.Registry = reg
	}
	res, err := r.Resolve(ctx, req)
	if err != nil {
		return err
	}

	doc := embeddingsProviderDoc{
		ModelID: res.ModelID, Backend: res.Backend, Model: res.Model, Endpoint: res.Endpoint,
		APIKeyEnv: res.APIKeyEnv, Dimension: res.Dimension, Sources: map[string]string{},
		Fixed: []string{},
	}
	rows := make([][]string, 0, len(embeddings.Fields))
	for _, e := range res.Explain() {
		doc.Sources[string(e.Field)] = string(e.Layer)
		value := e.Value
		switch {
		case e.Field == embeddings.FieldDimension && res.Dimension == 0:
			value = "(unknown)"
		case value == "":
			value = "(unset)"
		}
		source := string(e.Layer)
		if e.Fixed {
			doc.Fixed = append(doc.Fixed, string(e.Field))
			source = "fixed by registry"
		}
		rows = append(rows, []string{string(e.Field), value, source})
	}

	if isJSONOutput() {
		if err := outputJSON(cmd.OutOrStdout(), doc); err != nil {
			return err
		}
	} else {
		if res.ModelID != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "Embedding provider for %s\n\n", res.ModelID)
		} else {
			fmt.Fprintln(cmd.OutOrStdout(), "Embedding provider")
			fmt.Fprintln(cmd.OutOrStdout())
		}
		printTable(cmd.OutOrStdout(), []string{"Setting", "Value", "Source"}, rows)
	}

	// An unusable resolution (unsupported backend) is reported after the
	// table so the operator sees which layer supplied it, and fails the run.
	if _, err := res.Provider(); err != nil {
		return err
	}
	return nil
}

// embeddingsRegisterDoc is the JSON shape of `ctxt embeddings register`.
type embeddingsRegisterDoc struct {
	ModelID   string `json:"model_id"`
	Provider  string `json:"provider"`
	Dimension int    `json:"dimension"`
	IsDefault bool   `json:"is_default"`
	// ConfigJSON is the stored config_json document.
	ConfigJSON json.RawMessage `json:"config_json"`
	// Sources maps each setting to where it came from: a resolver layer
	// for backend, model, endpoint and api_key_env, "measured" for the
	// dimension.
	Sources map[string]string `json:"sources"`
	// Index is registerIndexReady or registerIndexUnsupported.
	Index string `json:"index"`
}

// dimensionSourceMeasured is the source register reports for the dimension.
const dimensionSourceMeasured = "measured"

func runEmbeddingsRegister(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	modelID := args[0]
	if err := storage.ValidateEmbeddingModelID(modelID); err != nil {
		return output.UsageError(err.Error()).Retaining(err)
	}
	if embeddingsRegisterDimension < 0 {
		return output.UsageError(fmt.Sprintf("--dimension %d must not be negative", embeddingsRegisterDimension))
	}

	reg, driver, cleanup, err := openEmbeddingsBackend()
	if err != nil {
		return err
	}
	defer cleanup()

	// Fail a duplicate before probing: the provider is not consulted for
	// a model_id that cannot be registered.
	switch _, err := reg.Get(ctx, modelID); {
	case err == nil:
		e := output.ConflictError(fmt.Sprintf("register %s: %v", modelID, registry.ErrModelAlreadyRegistered))
		e.SuggestedFix = "a model_id names one vector space; register the new model under a new model_id (for example a later date), or run `ctxt embeddings list`"
		return e.Retaining(registry.ErrModelAlreadyRegistered)
	case !errors.Is(err, registry.ErrModelNotFound):
		return fmt.Errorf("register %s: %w", modelID, err)
	}

	r := newEmbeddingResolver()
	r.Flags = embeddingsRegisterFlags
	r.HTTPClient = embeddingsRegisterHTTPClient
	// Resolve reports where each setting came from; ForRegistration builds
	// the provider and the config_json from the same layers.
	res, err := r.Resolve(ctx, embeddings.Request{Overrides: embeddingsRegisterFlags})
	if err != nil {
		return err
	}
	prov, configJSON, err := embeddings.NewProviderResolver(r).ForRegistration(ctx)
	if err != nil {
		return err
	}
	var mc embeddings.ModelConfig
	if err := json.Unmarshal(configJSON, &mc); err != nil {
		return fmt.Errorf("register %s: config_json: %w", modelID, err)
	}

	dim, err := probeEmbeddingDimension(ctx, prov, mc)
	if err != nil {
		e := output.PrerequisiteError(fmt.Sprintf("register %s: %v", modelID, err))
		e.SuggestedFix = probeFixHint(mc)
		return e.Retaining(err)
	}
	if embeddingsRegisterDimension != 0 && embeddingsRegisterDimension != dim {
		e := output.ConflictError(fmt.Sprintf(
			"register %s: --dimension %d does not match the measured dimension %d of %s model %q; nothing was registered",
			modelID, embeddingsRegisterDimension, dim, mc.Backend, mc.Model,
		))
		e.SuggestedFix = fmt.Sprintf("drop --dimension to register the measured %d, or select the model that produces %d vectors with --embedding-model",
			dim, embeddingsRegisterDimension)
		return e
	}

	m := registry.Model{
		ModelID:    modelID,
		Provider:   mc.Backend,
		Dimension:  dim,
		ConfigJSON: string(configJSON),
	}
	if err := reg.Register(ctx, m, false); err != nil {
		return fmt.Errorf("register %s: %w", modelID, err)
	}

	index := registerIndexReady
	if err := driver.Embeddings().EnsureIndex(ctx, embeddings.SpecFor(m)); err != nil {
		if !errors.Is(err, errors.ErrUnsupported) {
			return fmt.Errorf(
				"registered %s (dimension %d), but building its index failed: %w; the index is rebuilt the next time the database opens",
				modelID, dim, err,
			)
		}
		index = registerIndexUnsupported
	}

	doc := embeddingsRegisterDoc{
		ModelID: modelID, Provider: m.Provider, Dimension: dim,
		ConfigJSON: configJSON, Sources: map[string]string{}, Index: index,
	}
	rows := make([][]string, 0, len(embeddings.Fields))
	for _, e := range res.Explain() {
		value, source := e.Value, string(e.Layer)
		if e.Field == embeddings.FieldDimension {
			value, source = strconv.Itoa(dim), dimensionSourceMeasured
		}
		doc.Sources[string(e.Field)] = source
		if value == "" {
			value = "(unset)"
		}
		rows = append(rows, []string{string(e.Field), value, source})
	}

	if isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), doc)
	}
	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "Registered %s\n\n", modelID)
	printTable(w, []string{"Setting", "Value", "Source"}, rows)
	switch index {
	case registerIndexReady:
		fmt.Fprintln(w, "Index: ready")
	default:
		fmt.Fprintln(w, "Index: not built (this storage backend does not build per-model indexes yet)")
	}
	fmt.Fprintf(w, "Not the default model. Promote it with: ctxt embeddings set-default %s\n", modelID)
	return nil
}

// probeEmbeddingDimension embeds the fixed probe text and returns the
// vector length. Errors name the endpoint and a fix.
func probeEmbeddingDimension(ctx context.Context, p providers.EmbeddingProvider, mc embeddings.ModelConfig) (int, error) {
	vec, err := p.Embed(ctx, embeddingsRegisterProbeText)
	if err != nil {
		return 0, fmt.Errorf("probe %s model %q at %s failed: %w; nothing was registered. %s",
			mc.Backend, mc.Model, mc.Endpoint, err, probeFixHint(mc))
	}
	if len(vec) == 0 {
		return 0, fmt.Errorf("probe %s model %q returned an empty vector, so there is no dimension to measure; nothing was registered. %s",
			mc.Backend, mc.Model, probeFixHint(mc))
	}
	return len(vec), nil
}

func probeFixHint(mc embeddings.ModelConfig) string {
	switch mc.Backend {
	case embeddings.BackendOllama:
		return fmt.Sprintf("Check that Ollama is running at %s and the model is pulled (ollama pull %s), or point at another Ollama with --embedding-endpoint.",
			mc.Endpoint, mc.Model)
	case embeddings.BackendStub:
		return "The stub backend produces no vectors; choose a real backend with --embedding-provider."
	default:
		return "Check the provider settings with ctxt embeddings provider, or override them with --embedding-provider, --embedding-model and --embedding-endpoint."
	}
}

// newEmbeddingsRegistry resolves a registry.Store via the same service-init
// path as other CLI commands. Returns a cleanup callback the caller must
// defer; it tears down the underlying driver. The registry is
// dialect-aware, so `ctxt embeddings` works on both the sqlite and postgres
// backends (hosted instances included).
func newEmbeddingsRegistry() (*registry.Store, func(), error) {
	reg, _, cleanup, err := openEmbeddingsBackend()
	return reg, cleanup, err
}

// openEmbeddingsBackend is newEmbeddingsRegistry plus the storage driver the
// registry lives in, for commands that also build per-model indexes.
func openEmbeddingsBackend() (*registry.Store, storage.StorageDriver, func(), error) {
	svc, cleanup, err := newService()
	if err != nil {
		return nil, nil, nil, err
	}
	switch d := svc.Store.(type) {
	case *sqlite.Driver:
		return registry.NewFor(d.DB(), "sqlite"), d, cleanup, nil
	case *postgres.Driver:
		return registry.NewFor(d.DB(), d.SQLDialect()), d, cleanup, nil
	default:
		cleanup()
		return nil, nil, nil, fmt.Errorf("ctxt embeddings: unsupported storage backend %T", svc.Store)
	}
}
