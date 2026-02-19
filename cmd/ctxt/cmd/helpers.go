package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/builtins"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
	"github.com/spf13/viper"
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

	svc := service.New(driver, queue, pipes, engine, "")
	cleanup := func() { driver.Close(context.Background()) }
	return svc, cleanup, nil
}

// isJSONOutput returns true when --output is "json".
func isJSONOutput() bool {
	return viper.GetString("output.format") == "json"
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

// buildObjectFilter reads common filter flags from viper and returns an ObjectFilter.
func buildObjectFilter() storage.ObjectFilter {
	filter := storage.ObjectFilter{
		Type:     viper.GetString("list.type"),
		Subtype:  viper.GetString("list.subtype"),
		Tag:      viper.GetString("list.tag"),
		Mention:  viper.GetString("list.mention"),
		Pipeline: viper.GetString("list.pipeline"),
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
	return filter
}
