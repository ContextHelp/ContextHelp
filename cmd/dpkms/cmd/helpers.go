package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"github.com/ideacrafterslabs/ctxt/internal/cli/cliformat"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/registry"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/secrets"
	"github.com/ideacrafterslabs/ctxt/internal/server/stack"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
	"github.com/spf13/cobra"
	"hop.top/kit/go/storage/secret"
)

// asSystem returns ctx carrying the unrestricted viewer: a dpkms command
// at the host's terminal, and the daemon's own loops, act as the
// instance itself and read the service unscoped (ADR-080 §4). Request
// handlers never call it; their viewer comes from authentication.
func asSystem(ctx context.Context) context.Context {
	return sharing.WithViewer(ctx, sharing.Unrestricted)
}

// closeStorage closes a storage driver at the end of a command, warning
// on stderr when the close fails: the command's own result stands.
func closeStorage(d interface{ Close(context.Context) error }) {
	if err := d.Close(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to close storage: %v\n", err)
	}
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

// newService creates a Service wired to the configured storage backend.
// Returns the service and a cleanup function that must be deferred.
func newService() (*service.Service, func(), error) {
	storageType := cfg.Storage.Type
	if storageType == "" {
		storageType = storageTypeSQLite
	}
	storagePath := cfg.Storage.Path
	if storagePath == "" {
		return nil, nil, fmt.Errorf("storage path not configured")
	}

	driver, err := storageutil.Open(storageType, storagePath, cfg.Storage.Blob,
		storageutil.WithMigrateSnapshot(cfg.Storage.MigrateSnapshot))
	if err != nil {
		return nil, nil, fmt.Errorf("init storage: %w", err)
	}
	ctx := context.Background()
	if err := driver.Init(ctx); err != nil {
		return nil, nil, errors.Join(fmt.Errorf("init storage: %w", err), driver.Close(ctx))
	}

	queue := jobs.NewQueue(driver.Jobs())
	pipes := builtins.ConfiguredRegistryWithOpts(registryBuildOpts(driver))
	engine := search.NewEngine(driver)

	queue.SetPipelineValidator(func(name string) error {
		_, err := pipes.Get(name)
		return err
	})

	if err := loadDetectors(ctx, driver, pipes); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to load detectors: %v\n", err)
	}

	// Registry tokens come from secrets.backend under registry/<name>,
	// opened on the first authenticated registry request only.
	secretsCfg := cfg.Secrets
	svc := service.NewWithOptions(driver, queue, pipes, engine, "", nil,
		[]service.Option{service.WithRegistryTokens(registry.SecretTokens(func() (secret.Store, error) {
			return secrets.New(secretsCfg)
		}))}, *cfg)
	cleanup := func() { closeStorage(driver) }

	if err := svc.EnsureDefaultRegistry(asSystem(ctx)); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to load default registry: %v\n", err)
	}

	return svc, cleanup, nil
}

// registryBuildOpts is the direct-store pipeline registry's wiring, as
// serve's (stack.New): the embedding write path plus the driver's blob
// store and storage.blob.threshold, without which externalize_content
// is pruned.
func registryBuildOpts(driver storage.StorageDriver) builtins.BuildOpts {
	opts := embeddingBuildOpts(driver, newEmbeddingResolver())
	opts.BlobStore = driver.Blobs()
	if cfg != nil {
		opts.BlobThreshold = cfg.Storage.Blob.Threshold
		opts.Access = cfg.Server.EffectiveAccess()
		opts.ClassRules = builtins.ClassRulesFromConfig(cfg.Sharing)
	}
	opts.FetchGuard = config.FetchGuard(cfg)
	return opts
}

// embeddingBuildOpts is the dpkms CLI's embedding write path for the
// loaded config; see stack.EmbeddingBuildOpts.
func embeddingBuildOpts(driver storage.StorageDriver, r *embeddings.Resolver) builtins.BuildOpts {
	var dups config.DuplicatesConfig
	if cfg != nil {
		dups = cfg.Duplicates
	}
	return stack.EmbeddingBuildOpts(driver, r, dups, os.Stderr)
}

// loadDetectors reads enabled detectors from the DB and registers them.
func loadDetectors(ctx context.Context, driver storage.StorageDriver, pipes interface {
	RegisterDetector(d pipeline.Detector)
},
) error {
	t := true
	records, _, err := driver.Detectors().List(ctx, storage.DetectorFilter{Enabled: &t})
	if err != nil {
		return err
	}

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
				fmt.Fprintf(os.Stderr,
					"Warning: detector %s has invalid pattern %q: %v\n",
					rec.ID, rec.Pattern, err)
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

var (
	adminTableHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#FFFFFF")).
				Background(lipgloss.Color("#5C5CFF")).
				Padding(0, 1)

	adminTableCellStyle = lipgloss.NewStyle().Padding(0, 1)
)

func printAdminTable(w io.Writer, headers []string, rows [][]string) {
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
				return adminTableHeaderStyle
			}
			return adminTableCellStyle
		})

	fmt.Fprintln(w, t.Render())
}
