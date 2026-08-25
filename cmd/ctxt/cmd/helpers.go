package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"github.com/ideacrafterslabs/ctxt/internal/config"
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
	pipes := builtins.Registry()
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

// clientServerURL resolves the dpkms base URL for client-side routing.
// Order: server.url (bound to --server where the flag exists, config
// otherwise) > the bridge default.
func clientServerURL() string {
	if v := viper.GetString("server.url"); v != "" {
		return v
	}
	return idxbridge.DefaultBaseURL
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
	return "", fmt.Errorf("no running dpkms instance named %q — use `dpkms ps` to list instances", nameOrPort)
}

// loadDetectors reads enabled detectors from the DB and registers them with the registry.
func loadDetectors(ctx context.Context, driver storage.StorageDriver, pipes interface {
	RegisterDetector(d pipeline.Detector)
}) error {
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

// isJSONOutput returns true when --format (or its --output alias) is "json".
func isJSONOutput() bool {
	return viper.GetString("format") == "json"
}

// outputJSON writes v as indented JSON to w.
func outputJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
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
