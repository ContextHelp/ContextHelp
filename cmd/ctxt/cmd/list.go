package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var listCmd = &cobra.Command{
	Use:   "list [filters]",
	Short: "Query knowledge objects",
	Long: `Query knowledge objects across local storage and registries.

Supports filtering by tags, hints, mentions, entities, and types.

Examples:
  # List all objects
  ctxt list

  # Filter by type
  ctxt list --type url

  # Filter by tags
  ctxt list --tag ux,onboarding

  # Filter by mentions
  ctxt list --mention @ui.best-practice

  # Filter by date range
  ctxt list --after 2025-01-01 --before 2025-01-31

  # Use query language
  ctxt list --q "type==url;tag=in=(ux,design)"`,
	RunE: runList,
}

func init() {
	rootCmd.AddCommand(listCmd)

	// Filter flags
	listCmd.Flags().String("type", "", "filter by knowledge object type")
	listCmd.Flags().String("tag", "", "filter by tags (comma-separated)")
	listCmd.Flags().String("hint", "", "filter by hints (comma-separated)")
	listCmd.Flags().String("mention", "", "filter by mention")
	listCmd.Flags().String("pipeline", "", "filter by pipeline")
	listCmd.Flags().String("subtype", "", "filter by subtype")
	listCmd.Flags().String("before", "", "created before (ISO date)")
	listCmd.Flags().String("after", "", "created after (ISO date)")
	listCmd.Flags().String("orig-lang", "", "original language code")
	listCmd.Flags().String("q", "", "AST-based query")

	// Display flags
	listCmd.Flags().Int("limit", 50, "maximum results")
	listCmd.Flags().Int("start", 0, "pagination offset")
	listCmd.Flags().String("sort", "recent", "sort by (view|match|recent|weight|score)")
	listCmd.Flags().String("dir", "desc", "sort direction (asc|desc)")
	listCmd.Flags().Bool("no-track", false, "skip match tracking")

	// Bind flags to viper
	viper.BindPFlag("list.type", listCmd.Flags().Lookup("type"))
	viper.BindPFlag("list.tag", listCmd.Flags().Lookup("tag"))
	viper.BindPFlag("list.hint", listCmd.Flags().Lookup("hint"))
	viper.BindPFlag("list.mention", listCmd.Flags().Lookup("mention"))
	viper.BindPFlag("list.pipeline", listCmd.Flags().Lookup("pipeline"))
	viper.BindPFlag("list.subtype", listCmd.Flags().Lookup("subtype"))
	viper.BindPFlag("list.before", listCmd.Flags().Lookup("before"))
	viper.BindPFlag("list.after", listCmd.Flags().Lookup("after"))
	viper.BindPFlag("list.orig-lang", listCmd.Flags().Lookup("orig-lang"))
	viper.BindPFlag("list.q", listCmd.Flags().Lookup("q"))
	viper.BindPFlag("list.limit", listCmd.Flags().Lookup("limit"))
	viper.BindPFlag("list.start", listCmd.Flags().Lookup("start"))
	viper.BindPFlag("list.sort", listCmd.Flags().Lookup("sort"))
	viper.BindPFlag("list.dir", listCmd.Flags().Lookup("dir"))
	viper.BindPFlag("list.no-track", listCmd.Flags().Lookup("no-track"))
}

func runList(cmd *cobra.Command, args []string) error {
	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx := context.Background()

	// If RSQL query is provided, use search engine
	if q := viper.GetString("list.q"); q != "" {
		limit := viper.GetInt("list.limit")
		offset := viper.GetInt("list.start")
		objects, total, err := svc.SearchObjects(ctx, q, limit, offset)
		if err != nil {
			return fmt.Errorf("search: %w", err)
		}
		return printObjectResults(objects, total)
	}

	filter := buildObjectFilter()
	objects, total, err := svc.ListObjects(ctx, filter)
	if err != nil {
		return fmt.Errorf("list objects: %w", err)
	}
	return printObjectResults(objects, total)
}

func printObjectResults(objects []*storage.KnowledgeObject, total int) error {
	if isJSONOutput() {
		return outputJSON(os.Stdout, map[string]any{
			"objects": objects,
			"total":   total,
		})
	}

	fmt.Printf("Knowledge Objects (%d total)\n\n", total)
	headers := []string{"ID", "Type", "Title", "Created"}
	var rows [][]string
	for _, obj := range objects {
		title := obj.ID
		if len(obj.Summaries) > 0 {
			title = obj.Summaries[0]
			if len(title) > 40 {
				title = title[:37] + "..."
			}
		}
		rows = append(rows, []string{
			obj.ID,
			obj.Type,
			title,
			obj.CreatedAt.Format("2006-01-02 15:04"),
		})
	}
	printTable(os.Stdout, headers, rows)
	return nil
}
