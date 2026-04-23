package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/ideacrafterslabs/ctxt/internal/projection"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var listCmd = &cobra.Command{
	Use:   "list [filters]",
	Short: "Query knowledge objects",
	Long: `Query knowledge objects across local storage and registries.

Supports filtering by tags, hints, mentions, entities, types,
and metadata facets (type, topic, person, dates, source).

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

  # Filter by metadata type
  ctxt list --meta-type observation

  # Filter by topic and source
  ctxt list --topic auth --source-type slack

  # Filter by person
  ctxt list --person alice-chen

  # Filter by dates mentioned in content
  ctxt list --since 2026-04-01 --until 2026-04-23

  # Show facet breakdown
  ctxt list --facets

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
	listCmd.Flags().String("status", "", "filter by status (active|inbox|discarded|raw|all)")

	// Metadata facet filters (US-0407)
	listCmd.Flags().String("meta-type", "", "filter by metadata type (e.g. observation, task)")
	listCmd.Flags().String("topic", "", "filter by topic in metadata")
	listCmd.Flags().String("person", "", "filter by person in metadata")
	listCmd.Flags().String("since", "", "filter by dates_mentioned >= (ISO date)")
	listCmd.Flags().String("until", "", "filter by dates_mentioned <= (ISO date)")
	listCmd.Flags().String("source-type", "", "filter by source_type")
	listCmd.Flags().Bool("facets", false, "show metadata type count breakdown")

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
	viper.BindPFlag("list.status", listCmd.Flags().Lookup("status"))
	viper.BindPFlag("list.meta-type", listCmd.Flags().Lookup("meta-type"))
	viper.BindPFlag("list.topic", listCmd.Flags().Lookup("topic"))
	viper.BindPFlag("list.person", listCmd.Flags().Lookup("person"))
	viper.BindPFlag("list.since", listCmd.Flags().Lookup("since"))
	viper.BindPFlag("list.until", listCmd.Flags().Lookup("until"))
	viper.BindPFlag("list.source-type", listCmd.Flags().Lookup("source-type"))
	viper.BindPFlag("list.facets", listCmd.Flags().Lookup("facets"))
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

	// --facets: show metadata type count breakdown alongside results.
	if viper.GetBool("list.facets") {
		counts, err := svc.FacetCounts(ctx, filter)
		if err != nil {
			return fmt.Errorf("facet counts: %w", err)
		}
		if isJSONOutput() {
			objects, total, err := svc.ListObjects(ctx, filter)
			if err != nil {
				return fmt.Errorf("list objects: %w", err)
			}
			return outputJSON(os.Stdout, map[string]any{
				"objects": objects,
				"total":   total,
				"facets":  counts,
			})
		}
		fmt.Printf("Facets (metadata type):\n")
		for t, c := range counts {
			fmt.Printf("  %-20s %d\n", t, c)
		}
		fmt.Println()
	}

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
	// Objects with a known duplicate are annotated with "(duplicate of <id>)".
	// The duplicate_of field is set by the dedup pipeline step or by the "warn"/"keep"
	// policy when near-duplicate detection (duplicates.check_similar) is enabled.
	headers := []string{"ID", "Type", "Title", "Created"}
	var rows [][]string
	for _, obj := range objects {
		// Use projection for title; fall back to first summary then ID.
		docProj := projection.ProjectDocument(obj)
		title := docProj.Title
		if title == "" && len(obj.Summaries) > 0 {
			title = obj.Summaries[0]
		}
		if title == "" {
			title = obj.ID
		}
		if len(title) > 40 {
			title = title[:37] + "..."
		}
		dupOf, _ := obj.Metadata["duplicate_of"].(string)
		if dupOf != "" {
			title += " (dup:" + dupOf + ")"
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
