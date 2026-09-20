package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"github.com/ideacrafterslabs/ctxt/internal/cli/cliformat"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// newService creates a Service wired to the configured storage backend.
// Returns the service and a cleanup function that must be deferred.
func newService() (*service.Service, func(), error) {
	storageType := cfg.Storage.Type
	if storageType == "" {
		storageType = "sqlite"
	}
	storagePath := cfg.Storage.Path
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

	queue.SetPipelineValidator(func(name string) error {
		_, err := pipes.Get(name)
		return err
	})

	if err := loadDetectors(ctx, driver, pipes); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to load detectors: %v\n", err)
	}

	svc := service.New(driver, queue, pipes, engine, "", nil, *cfg)
	cleanup := func() { driver.Close(context.Background()) }

	if err := svc.EnsureDefaultRegistry(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to load default registry: %v\n", err)
	}

	return svc, cleanup, nil
}

// loadDetectors reads enabled detectors from the DB and registers them.
func loadDetectors(ctx context.Context, driver storage.StorageDriver, pipes interface {
	RegisterDetector(d pipeline.Detector)
}) error {
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
