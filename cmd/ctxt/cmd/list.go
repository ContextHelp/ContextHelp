package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/cursor"
	"github.com/ideacrafterslabs/ctxt/internal/idxbridge"
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
  ctxt list --tagged ux,onboarding

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
	cliconv.WithSideEffect(listCmd, cliconv.SideEffectRead)
	cliconv.WithExamples(listCmd, []cliconv.Example{
		{Title: "List all objects", Command: "ctxt list"},
		{Title: "Filter by tag", Command: "ctxt list --tagged ux,onboarding"},
		{Title: "Use the query language", Command: "ctxt list --q \"type==url;tag=in=(ux,design)\""},
	})

	// Filter flags
	listCmd.Flags().String("type", "", "filter by knowledge object type")
	listCmd.Flags().String("tagged", "", "filter by tags (comma-separated)")
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

	// Named cursor (US-0409): per-viewer position over results.
	listCmd.Flags().String("cursor", "",
		"use named cursor; returns items added after cursor's last_seen_at")
	listCmd.Flags().Bool("advance", false,
		"advance the cursor after a successful list (requires --cursor)")

	// Bind flags to viper
	viper.BindPFlag("list.type", listCmd.Flags().Lookup("type"))
	viper.BindPFlag("list.tagged", listCmd.Flags().Lookup("tagged"))
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
	viper.BindPFlag("list.cursor", listCmd.Flags().Lookup("cursor"))
	viper.BindPFlag("list.advance", listCmd.Flags().Lookup("advance"))
}

func runList(cmd *cobra.Command, args []string) error {
	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx := context.Background()

	cursorName := viper.GetString("list.cursor")
	advance := viper.GetBool("list.advance")
	if advance && cursorName == "" {
		return fmt.Errorf("--advance requires --cursor <name>")
	}

	// If RSQL query is provided, use the search engine — routed via a live
	// dpkms daemon when one answers, direct local search otherwise.
	if q := viper.GetString("list.q"); q != "" {
		limit := viper.GetInt("list.limit")
		offset := viper.GetInt("list.start")
		bridge := idxbridge.New(idxbridge.Config{
			BaseURLs: clientServerURLs(),
			Fallback: svc,
		})
		objects, total, err := bridge.SearchObjects(ctx, q, limit, offset)
		if err != nil {
			return fmt.Errorf("search: %w", err)
		}
		// Cursor with --q: out of scope for predicate composition; emit warning.
		if cursorName != "" {
			fmt.Fprintln(os.Stderr, "warning: --cursor with --q does not gate by created_at; results unfiltered")
		}
		return printObjectResults(objects, total)
	}

	filter := buildObjectFilter()
	if cursorName != "" {
		return runListWithCursor(ctx, svc, filter, cursorName, advance)
	}

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

// currentSnapshot reads list flags into a QuerySnapshot for drift compare.
func currentSnapshot() cursor.QuerySnapshot {
	return cursor.QuerySnapshot{
		Mention: splitNonEmpty(viper.GetString("list.mention")),
		Tag:     splitNonEmpty(viper.GetString("list.tagged")),
		Profile: viper.GetString("profile"),
		Q:       viper.GetString("list.q"),
		Type:    viper.GetString("list.type"),
		After:   viper.GetString("list.after"),
	}
}

func splitNonEmpty(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// runListWithCursor wraps the ListObjects path with cursor gating.
//
// AC compliance:
//   - new cursor (no advance flag, never seen): exits 2 with hint
//   - new cursor + --advance: starts at epoch 0, returns full set, then advances
//   - existing cursor: gates created_at > last_seen_at; sort forced ASC
//   - empty result: cursor untouched
//   - drift: warn to stderr, continue
//   - JSON output: {cursor: ..., items: ..., advanced: bool}
func runListWithCursor(ctx context.Context, svc listService, filter storage.ObjectFilter, name string, advance bool) error {
	if err := cursor.ValidateName(name); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(2)
	}
	mgr, err := cursor.New()
	if err != nil {
		return err
	}

	c, fresh, err := mgr.GetOrInit(name)
	if err != nil {
		return err
	}
	if fresh && !advance {
		fmt.Fprintf(os.Stderr,
			"cursor %q does not exist. Run with --advance to initialize, or `ctxt cursor list` to see existing.\n", name)
		os.Exit(2)
	}

	// Drift detection — compare current flags to stored snapshot.
	snap := currentSnapshot()
	if !fresh && !c.Query.Equal(snap) {
		fmt.Fprintf(os.Stderr,
			"warning: cursor %q query snapshot differs from current flags; results may not match prior listings\n", name)
	}

	// Cursor adds time-gate predicate on created_at > last_seen_at;
	// storage's After uses `>=` against an RFC3339 (second-precision)
	// string, so bump by 1 second to strictly exclude the tail.
	// Items that share the tail's second would already have been seen
	// in the prior listing.
	if !c.LastSeenAt.IsZero() {
		gate := c.LastSeenAt.Add(time.Second)
		if filter.After == nil || gate.After(*filter.After) {
			filter.After = &gate
		}
	}
	filter.Sort = "created_at"
	filter.Dir = "asc"

	objects, total, err := svc.ListObjects(ctx, filter)
	if err != nil {
		return fmt.Errorf("list objects: %w", err)
	}

	advanced := false
	if advance && len(objects) > 0 {
		tail := objects[len(objects)-1]
		if _, err := mgr.Advance(name, tail.CreatedAt, tail.ID, snap); err != nil {
			return fmt.Errorf("advance cursor: %w", err)
		}
		advanced = true
	}

	if isJSONOutput() {
		// Refresh cursor view for output.
		latest, _ := mgr.Get(name)
		out := map[string]any{
			"cursor":   latest,
			"items":    objects,
			"total":    total,
			"advanced": advanced,
		}
		return outputJSON(os.Stdout, out)
	}
	return printObjectResults(objects, total)
}

// listService captures the subset of *service.Service used by the cursor path.
type listService interface {
	ListObjects(ctx context.Context, f storage.ObjectFilter) ([]*storage.KnowledgeObject, int, error)
}
