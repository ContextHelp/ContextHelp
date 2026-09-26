package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"github.com/ideacrafterslabs/ctxt/internal/cli/cliformat"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings"
	embregistry "github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
	"github.com/ideacrafterslabs/ctxt/internal/idxbridge"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pidfile"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/plugin"
	"github.com/ideacrafterslabs/ctxt/internal/projection"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
	"github.com/spf13/viper"
	"hop.top/kit/go/console/output"
	kitstyles "hop.top/kit/go/console/tui/styles"
)

// sessionSvc holds the service instance for the duration of a `ctxt shell` session.
// When non-nil, newService() returns it directly, avoiding ~50ms SQLite reinit per command.
// Set by shell.go; cleared when the shell exits.
var sessionSvc *service.Service

// newService creates a Service wired to the configured storage backend.
// Returns the service and a cleanup function that must be deferred.
// When called from within a `ctxt shell` session (sessionSvc != nil), returns
// the session-scoped service with a no-op cleanup to avoid double-close.
//
// Instance resolution order:
//  1. --instance flag (or CTXT_INSTANCE env var, bound via viper)
//  2. current-instance state file (written by `ctxt instance use`)
//  3. config storage.path (original behaviour)
func newService() (*service.Service, func(), error) {
	if sessionSvc != nil {
		return sessionSvc, func() {}, nil
	}

	storageType := cfg.Storage.Type
	if storageType == "" {
		storageType = "sqlite"
	}
	storagePath, err := resolveStoragePath()
	if err != nil {
		return nil, nil, err
	}
	if storagePath == "" {
		return nil, nil, fmt.Errorf("storage path not configured")
	}

	driver, err := storageutil.NewDriver(storageType, storagePath)
	if err != nil {
		return nil, nil, fmt.Errorf("init storage: %w", err)
	}
	ctx := context.Background()
	if err := driver.Init(ctx); err != nil {
		driver.Close(ctx)
		return nil, nil, fmt.Errorf("init storage: %w", err)
	}

	queue := jobs.NewQueue(driver.Jobs())
	pipes := builtins.ConfiguredRegistryWithOpts(embeddingBuildOpts(driver))
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

	svc := service.New(driver, queue, pipes, engine, "", nil, *cfg)
	cleanup := func() { driver.Close(context.Background()) }

	// Ensure bundled default registry is always present in the cache.
	if err := svc.EnsureDefaultRegistry(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to load default registry: %v\n", err)
	}

	return svc, cleanup, nil
}

// clientEndpoints resolves the ordered dpkms endpoint list for client-side
// routing, primary first, with per-instance credentials. Order:
// config server.urls (each entry a URL or {url, token}; server.token fills
// entries without their own) > server.url (bound to --server where the flag
// exists, config otherwise) > the bridge default. Tokens only ever come
// from config, never from flags.
func clientEndpoints() []idxbridge.Endpoint {
	def := cfg.Server.Token
	if urls := cfg.Server.URLs; len(urls) > 0 {
		eps := make([]idxbridge.Endpoint, len(urls))
		for i, u := range urls {
			tok := u.Token
			if tok == "" {
				tok = def
			}
			eps[i] = idxbridge.Endpoint{URL: u.URL, Token: tok}
		}
		return eps
	}
	if v := viper.GetString("server.url"); v != "" {
		return []idxbridge.Endpoint{{URL: v, Token: def}}
	}
	if v := cfg.Server.URL; v != "" {
		return []idxbridge.Endpoint{{URL: v, Token: def}}
	}
	return []idxbridge.Endpoint{{URL: idxbridge.DefaultBaseURL, Token: def}}
}

// pinnedEndpoint resolves an explicit --server URL to a single endpoint,
// reusing the configured token when the URL matches a server.urls entry and
// falling back to the server.token default otherwise.
func pinnedEndpoint(rawURL string) idxbridge.Endpoint {
	base := strings.TrimRight(rawURL, "/")
	for _, ep := range clientEndpoints() {
		if strings.TrimRight(ep.URL, "/") == base {
			return idxbridge.Endpoint{URL: rawURL, Token: ep.Token}
		}
	}
	return idxbridge.Endpoint{URL: rawURL, Token: cfg.Server.Token}
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
	stateFile, err := config.CurrentInstanceFile()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(stateFile)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// dbPathForInstance resolves a DB path from an instance name or port string.
// Scans live pidfiles; returns an error if no matching instance is found.
func dbPathForInstance(nameOrPort string) (string, error) {
	runDir, err := config.RunDir()
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

// embeddingBuildOpts wires the embedding write path into the pipeline
// registry: the populate set from the driver's model registry, each
// model's provider through one resolver built from config, -c and env, and
// the driver's per-model vector index. Without a readable registry, ingest
// writes no vectors.
func embeddingBuildOpts(driver storage.StorageDriver) builtins.BuildOpts {
	opts := builtins.BuildOpts{
		Resolver:   embeddings.NewProviderResolver(newEmbeddingResolver()),
		Embeddings: driver.Embeddings(),
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

// findOutputGenerator looks up a generator by format name from the plugin registry.
// Returns nil if reg is nil or no matching generator is found.
func findOutputGenerator(reg *plugin.Registry, format string) pluginapi.OutputGenerator {
	if reg == nil {
		return nil
	}
	return reg.FindOutputGenerator(format)
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

// buildObjectFilter reads common filter flags from viper and returns an ObjectFilter.
func buildObjectFilter() storage.ObjectFilter {
	filter := storage.ObjectFilter{
		Type:     viper.GetString("list.type"),
		Subtype:  viper.GetString("list.subtype"),
		Tag:      viper.GetString("list.tagged"),
		Mention:  viper.GetString("list.mention"),
		Pipeline: viper.GetString("list.pipeline"),
		Status:   viper.GetString("list.status"),
		Limit:    viper.GetInt("list.limit"),
		Offset:   viper.GetInt("list.start"),
		Sort:     viper.GetString("list.sort"),
		Dir:      viper.GetString("list.dir"),
	}
	if before := viper.GetString("list.before"); before != "" {
		if t, err := time.Parse("2006-01-02", before); err == nil {
			filter.Before = &t
		}
	}
	if after := viper.GetString("list.after"); after != "" {
		if t, err := time.Parse("2006-01-02", after); err == nil {
			filter.After = &t
		}
	}

	// Metadata facet filters (US-0407).
	filter.MetadataType = viper.GetString("list.meta-type")
	filter.MetadataTopic = viper.GetString("list.topic")
	filter.MetadataPerson = viper.GetString("list.person")
	filter.SourceType = viper.GetString("list.source-type")
	if since := viper.GetString("list.since"); since != "" {
		if t, err := time.Parse("2006-01-02", since); err == nil {
			filter.MetadataSince = &t
		}
	}
	if until := viper.GetString("list.until"); until != "" {
		if t, err := time.Parse("2006-01-02", until); err == nil {
			filter.MetadataUntil = &t
		}
	}
	return filter
}

// statusStyle renders a job/feed status string with kit's semantic palette.
// Mapping: completed→Success, failed→Error, running/processing→Accent,
// pending→Muted, fallback→Secondary.
func statusStyle(status string) string {
	st := kitstyles.NewStyles(root.Theme)
	switch strings.ToLower(status) {
	case "completed":
		return st.Success.Render(status)
	case "failed":
		return st.Error.Render(status)
	case "running", "processing":
		return st.Accent.Render(status)
	case "pending":
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
