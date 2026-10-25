package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/cli/cliformat"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/directopen"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings"
	embregistry "github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pidfile"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/projection"
	"github.com/ideacrafterslabs/ctxt/internal/registry"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"hop.top/kit/go/console/output"
	kitstyles "hop.top/kit/go/console/tui/styles"
)

// storageTypeSQLite is the storage.type value naming the SQLite backend,
// the default when the config leaves it empty.
const storageTypeSQLite = "sqlite"

// Output format names: --format, compose.export and import payloads.
const (
	formatJSON     = output.JSON
	formatMarkdown = "markdown"
)

// sessionSvc holds the service instance for the duration of a `ctxt shell` session.
// When non-nil, newService() returns it directly, avoiding ~50ms SQLite reinit per command.
// Set by shell.go; cleared when the shell exits.
var sessionSvc *service.Service

// asOwner returns ctx carrying the unrestricted viewer: a ctxt command
// that opens the store directly runs at the owner's terminal and reads
// it unscoped (ADR-080 §4). Everything else reaches dpkms over the API,
// where the transport attaches the caller's viewer.
func asOwner(ctx context.Context) context.Context {
	return sharing.WithViewer(ctx, sharing.Unrestricted)
}

// directLabel names a ctxt command in the database lock body.
const directLabel = "ctxt cli"

// newService creates a Service wired to the configured storage backend,
// opened for cmd through the direct-open gates (internal/directopen), as
// dpkms opens it: refused on a protected or public instance (the
// stricter of server.access and the access marker); read-only, with no
// lock and no migration, for a command that declares the read side
// effect; under the database lock, migrated, for anything else. A lock
// the daemon holds refuses the write at once. Returns the service and a
// cleanup function that must be deferred; it closes the store and
// releases the lock.
//
// When called from within a `ctxt shell` session (sessionSvc != nil), returns
// the session-scoped service with a no-op cleanup to avoid double-close.
//
// Instance resolution order:
//  1. --instance flag (or CTXT_INSTANCE env var, bound via viper)
//  2. current-instance state file (written by `ctxt instance use`)
//  3. config storage.path
func newService(cmd *cobra.Command) (*service.Service, func(), error) {
	if sessionSvc != nil {
		return sessionSvc, func() {}, nil
	}

	storagePath, err := resolveStoragePath()
	if err != nil {
		return nil, nil, err
	}
	t := directopen.Target{
		Type:            cfg.Storage.Type,
		Path:            storagePath,
		Blob:            cfg.Storage.Blob,
		MigrateSnapshot: cfg.Storage.MigrateSnapshot,
		Label:           directLabel,
		Access:          cfg.Server.EffectiveAccess(),
	}
	ctx := context.Background()
	readOnly := cliconv.ReadsOnly(cmd)
	open := directopen.Write
	if readOnly {
		open = directopen.Read
	}
	h, err := open(ctx, t)
	if err != nil {
		return nil, nil, explainDaemonHeld(err, cmd, storagePath)
	}
	driver := h.Driver

	queue := jobs.NewQueue(driver.Jobs())
	pipes := builtins.ConfiguredRegistryWithOpts(registryBuildOpts(driver))
	engine := search.NewEngine(driver)

	// Wire pipeline preflight validation into the queue.
	queue.SetPipelineValidator(func(name string) error {
		_, err := pipes.Get(name)
		return err
	})

	// Load persisted detectors into the pipeline registry.
	if err := loadDetectors(ctx, driver, pipes); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to load detectors: %v\n", err)
	}

	// Registry requests carry the token `ctxt registry login` stored.
	svc := service.NewWithOptions(driver, queue, pipes, engine, "", nil,
		[]service.Option{service.WithRegistryTokens(registry.KeychainTokens())}, *cfg)
	cleanup := func() {
		if err := h.Close(context.Background()); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to close storage: %v\n", err)
		}
	}

	// Caching the bundled default registry writes; a read command leaves it.
	if !readOnly {
		if err := svc.EnsureDefaultRegistry(asOwner(ctx)); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to load default registry: %v\n", err)
		}
	}

	return svc, cleanup, nil
}

// resolveStoragePath returns the DB path to open, applying instance routing.
// Resolution order: --instance flag / CTXT_INSTANCE env > state file > config.
func resolveStoragePath() (string, error) {
	instanceTarget := activeInstanceName()
	if instanceTarget != "" {
		path, err := dbPathForInstance(instanceTarget)
		if err != nil {
			return "", err
		}
		return path, nil
	}
	return cfg.Storage.Path, nil
}

// activeInstanceName returns the active instance selector, if any.
// Priority: --instance flag (or CTXT_INSTANCE env, bound in root.go) > state file.
func activeInstanceName() string {
	if v := viper.GetString("instance"); v != "" {
		return v
	}
	return currentInstance()
}

// dbPathForInstance resolves a DB path from an instance name or port string.
// Scans live pidfiles; returns an error if no matching instance is found.
func dbPathForInstance(nameOrPort string) (string, error) {
	runDir, err := config.RunDirPath(dataDir())
	if err != nil {
		return "", fmt.Errorf("instance routing: run dir: %w", err)
	}
	infos, err := pidfile.Scan(runDir)
	if err != nil {
		return "", fmt.Errorf("instance routing: scan pidfiles: %w", err)
	}
	for _, info := range infos {
		if info.Name == nameOrPort || fmt.Sprintf("%d", info.Port) == nameOrPort {
			return info.DBPath, nil
		}
	}
	// A declared dependency ctxt could not reach: the instance is named
	// and routable, nothing is listening. kit's PREREQUISITE (exit 70)
	// is the class — the invocation was correct and ctxt's own logic
	// never ran, so the caller repairs the environment and re-runs the
	// identical command rather than backing off or changing the input.
	// The recovery command moves out of the prose and into the
	// envelope's SuggestedFix, where a machine reader can act on it.
	e := output.PrerequisiteError(fmt.Sprintf("no running dpkms instance named %q", nameOrPort))
	e.SuggestedFix = "run `dpkms ps` to list running instances, or start one with `dpkms serve`"
	return "", e
}

// registryBuildOpts is the direct-store pipeline registry's wiring, as
// serve's (stack.New): the embedding write path plus the driver's blob
// store and storage.blob.threshold, without which externalize_content
// is pruned.
func registryBuildOpts(driver storage.StorageDriver) builtins.BuildOpts {
	opts := embeddingBuildOpts(driver)
	opts.BlobStore = driver.Blobs()
	if cfg != nil {
		opts.BlobThreshold = cfg.Storage.Blob.Threshold
		opts.Access = cfg.Server.EffectiveAccess()
		opts.ClassRules = builtins.ClassRulesFromConfig(cfg.Sharing)
	}
	opts.FetchGuard = config.FetchGuard(cfg)
	return opts
}

// embeddingBuildOpts wires the embedding write path into the pipeline
// registry: the populate set from the driver's model registry, each
// model's provider through one resolver built from config, -c and env, and
// the driver's per-model vector index. Without a readable registry, ingest
// writes no vectors. The duplicates config rides along for dedup, which
// runs on those vectors.
func embeddingBuildOpts(driver storage.StorageDriver) builtins.BuildOpts {
	opts := builtins.BuildOpts{
		Resolver:   embeddings.NewProviderResolver(newEmbeddingResolver()),
		Embeddings: driver.Embeddings(),
		Audit:      driver.AuditLog(),
	}
	if cfg != nil {
		opts.Duplicates = cfg.Duplicates
	}
	reg, err := embregistry.ForDriver(driver)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: embedding model registry unavailable; ingest writes no vectors: %v\n", err)
		return opts
	}
	opts.Models = reg
	return opts
}

// loadDetectors reads enabled detectors from the DB and registers them with the registry.
func loadDetectors(ctx context.Context, driver storage.StorageDriver, pipes interface {
	RegisterDetector(d pipeline.Detector)
},
) error {
	t := true
	records, _, err := driver.Detectors().List(ctx, storage.DetectorFilter{Enabled: &t})
	if err != nil {
		return err
	}

	// Sort by priority ascending (lower number = higher priority).
	sort.Slice(records, func(i, j int) bool {
		return records[i].Priority < records[j].Priority
	})

	for _, rec := range records {
		switch rec.Kind {
		case storage.DetectorKindExtension:
			pipes.RegisterDetector(pipeline.NewExtensionDetector(map[string]string{
				rec.Pattern: rec.PipelineName,
			}))
		case storage.DetectorKindURLPattern:
			pat, err := regexp.Compile(rec.Pattern)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: detector %s has invalid pattern %q: %v\n", rec.ID, rec.Pattern, err)
				continue
			}
			pipes.RegisterDetector(pipeline.NewURLPatternDetector(rec.PipelineName, pat))
		}
	}
	return nil
}

// isJSONOutput returns true when --format asks for a machine-readable
// document (json or yaml) rather than the human table.
//
// The name is historical: every call site branches "structured vs
// human", and the branch is now honored for yaml too instead of
// falling through to the table. Pair it with outputJSON, which renders
// in whichever of the two the caller actually asked for.
func isJSONOutput() bool {
	return cliformat.Structured()
}

// outputJSON writes v to w in the active machine format (json or yaml),
// normalising empty collections so they serialize as [] rather than
// null.
func outputJSON(w io.Writer, v any) error {
	// EncodeTo rather than Encode: -o names a destination file, and
	// every caller here passes os.Stdout, so encoding straight to w
	// would print the document and leave the requested file uncreated.
	// nil cmd resolves to the command Bind recorded for this run.
	return cliformat.EncodeTo(nil, w, v)
}

// printTable writes a text table with headers and rows to w.
var (
	tableHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#FFFFFF")).
				Background(lipgloss.Color("#5C5CFF")).
				Padding(0, 1)

	tableCellStyle = lipgloss.NewStyle().Padding(0, 1)
)

func printTable(w io.Writer, headers []string, rows [][]string) {
	if len(rows) == 0 {
		emptyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
		fmt.Fprintf(w, "%s\n", emptyStyle.Render("No results"))
		return
	}

	borderColor := lipgloss.Color("238")
	t := table.New().
		Headers(headers...).
		Rows(rows...).
		Border(lipgloss.RoundedBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(borderColor)).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return tableHeaderStyle
			}
			return tableCellStyle
		})

	fmt.Fprintln(w, t.Render())
}

// koLabel returns a short display label for a KO. Uses DocumentProjection.Title when
// available, then falls back to the first summary, then the object ID.
// maxLen caps the label length; truncation appends "...".
func koLabel(ko *storage.KnowledgeObject) string {
	const maxLen = 60
	docProj := projection.ProjectDocument(ko)
	label := docProj.Title
	if label == "" && len(ko.Summaries) > 0 {
		label = ko.Summaries[0]
	}
	if label == "" {
		return ko.ID
	}
	out := fmt.Sprintf("%s  %s", ko.ID, label)
	if len(out) > maxLen+len(ko.ID)+2 {
		label = label[:maxLen-3] + "..."
		out = fmt.Sprintf("%s  %s", ko.ID, label)
	}
	return out
}

// statusStyle renders a job/feed status string with kit's semantic palette.
// Mapping: completed→Success, failed→Error, running/processing→Accent,
// pending→Muted, fallback→Secondary.
func statusStyle(status string) string {
	st := kitstyles.NewStyles(root.Theme)
	switch strings.ToLower(status) {
	case string(storage.JobCompleted):
		return st.Success.Render(status)
	case string(storage.JobFailed):
		return st.Error.Render(status)
	case string(storage.JobRunning), "processing":
		return st.Accent.Render(status)
	case string(storage.JobPending):
		return st.Muted.Render(status)
	default:
		return st.Secondary.Render(status)
	}
}

// truncate clips s to n runes, appending "..." if truncated.
func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	if n <= 3 {
		return string(runes[:n])
	}
	return string(runes[:n-3]) + "..."
}

// markFlagsRequired marks each named flag on cmd required. Cobra only
// fails here when no such flag is defined, which is a wiring bug in the
// command's init — so it panics at startup instead of silently leaving
// the flag optional.
func markFlagsRequired(cmd *cobra.Command, names ...string) {
	for _, name := range names {
		if err := cmd.MarkFlagRequired(name); err != nil {
			panic(fmt.Sprintf("%s: mark --%s required: %v", cmd.CommandPath(), name, err))
		}
	}
}
