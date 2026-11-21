package cmd

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/cli/cliformat"
	"github.com/ideacrafterslabs/ctxt/internal/cursor"
	"github.com/ideacrafterslabs/ctxt/internal/dpkmsclient"
	"github.com/ideacrafterslabs/ctxt/internal/projection"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"hop.top/kit/go/console/output"
)

var listCmd = &cobra.Command{
	Use:   "list [filters]",
	Short: "Query knowledge objects",
	Long: `Query the knowledge objects of the dpkms instance ctxt resolves
(--server, --instance, CTXT_INSTANCE, ctxt instance use, then config).

Filters combine with AND: type, subtype, tags, mention, pipeline,
status, creation dates (--since, --until), and metadata facets (type,
topic, person, source type, dates mentioned: --mentioned-since,
--mentioned-until). --q runs an RSQL query instead.

--tagged takes a comma list, a repeated flag, or both. A plain tag is
optional: with no + tag, one listed tag must match. A leading + requires a
tag: +a,+b,+c needs all three; +a,b,+c needs a and c, and b only ranks.
Results matching more of the listed tags rank first, then the sort;
--cursor and --changed-after need a fixed order and refuse a list that
ranks (two or more tags with no + tag, or a plain tag beside a + tag).
Quote a tag that holds spaces.

Examples:
  # List the most recently created objects
  ctxt list

  # Filter by type
  ctxt list --type url

  # Filter by tag: any of several (comma list or repeated flag)
  ctxt list --tagged ux,design
  ctxt list --tagged ux --tagged design --since 1w

  # Every tag required: a leading +
  ctxt list --tagged +ux,+design

  # a and c required; b optional; matches carrying b rank first
  ctxt list --tagged +a,b,+c

  # Filter by mentions
  ctxt list --mention @ui.best-practice

  # Filter by creation date: 3d, 42h, 1w, 30m (minutes), 1M or 1mo (month), yesterday, today,
  # YYYY-MM-DD or RFC 3339
  ctxt list --since 2025-01-01 --until 2025-01-31
  ctxt list --since 3d

  # Filter by metadata type
  ctxt list --meta-type observation

  # Filter by topic and source
  ctxt list --topic auth --source-type slack

  # Filter by person
  ctxt list --person alice-chen

  # Filter by dates mentioned in content
  ctxt list --mentioned-since 2026-04-01 --mentioned-until 2026-04-23

  # Most recently updated first
  ctxt list --sort updated

  # Follow changes: objects created or updated after change sequence 0,
  # oldest change first; pass next_changed_after back to continue
  ctxt list --changed-after 0 --status all -o json

  # Show facet breakdown
  ctxt list --facets

  # Use query language (several tags at once)
  ctxt list --q "type==url;tag=in=(ux,design)"`,
	RunE: runList,
}

// listSorts maps --sort values to the API's sort parameter. recent is a
// synonym for created.
var listSorts = map[string]string{
	"created": "created_at",
	"recent":  "created_at",
	"updated": "updated_at",
}

func init() {
	rootCmd.AddCommand(listCmd)
	cliconv.WithSideEffect(listCmd, cliconv.SideEffectRead)
	cliconv.WithExamples(listCmd, []cliconv.Example{
		{Title: "List all objects", Command: "ctxt list"},
		{Title: "Filter by tag", Command: "ctxt list --tagged ux"},
		{Title: "Several tags in the last week", Command: "ctxt list --tagged a --tagged b --since 1w"},
		{Title: "Require a and c, rank by b", Command: "ctxt list --tagged +a,b,+c"},
		{Title: "Use the query language", Command: "ctxt list --q \"type==url;tag=in=(ux,design)\""},
	})

	// Filter flags
	listCmd.Flags().String("type", "", "filter by knowledge object type")
	addTaggedFlag(listCmd)
	listCmd.Flags().String("mention", "", "filter by mention (@ns.slug or ctxt:// URI)")
	listCmd.Flags().String("pipeline", "", "filter by pipeline")
	listCmd.Flags().String("subtype", "", "filter by subtype")
	listCmd.Flags().String("since", "", sinceFlagUsage)
	listCmd.Flags().String("until", "", untilFlagUsage)
	listCmd.Flags().String("q", "", "RSQL query, answered by the instance's search")
	listCmd.Flags().String("status", "", "filter by status (active|inbox|discarded|raw|all)")

	// Metadata facet filters
	listCmd.Flags().String("meta-type", "", "filter by metadata type (e.g. observation, task)")
	listCmd.Flags().String("topic", "", "filter by topic in metadata")
	listCmd.Flags().String("person", "", "filter by person in metadata")
	listCmd.Flags().String("mentioned-since", "", mentionedSinceUsage)
	listCmd.Flags().String("mentioned-until", "", mentionedUntilUsage)
	listCmd.Flags().String("source-type", "", "filter by source_type")
	listCmd.Flags().Bool("facets", false, "show metadata type count breakdown")

	// Display flags
	listCmd.Flags().Int("limit", 50, "maximum results (0: every match)")
	listCmd.Flags().Int("start", 0, "pagination offset")
	listCmd.Flags().String("sort", "created", "sort by created (synonym: recent) or updated")
	listCmd.Flags().String("dir", "desc", "sort direction (asc|desc)")
	listCmd.Flags().Int64("changed-after", 0,
		"list by change order: objects created or updated after this change sequence (0: from the first), oldest change first; prints next_changed_after")

	// Named cursor: per-viewer position over results, kept in a client
	// file.
	listCmd.Flags().String("cursor", "",
		"use named cursor; returns items added after cursor's last_seen_at")
	listCmd.Flags().Bool("advance", false,
		"advance the cursor after a successful list (requires --cursor)")

	// Bind flags to viper
	for _, name := range []string{
		"type", "tagged", "mention", "pipeline", "subtype", "since", "until", "q", "status",
		"meta-type", "topic", "person", "mentioned-since", "mentioned-until", "source-type", "facets",
		"limit", "start", "sort", "dir", "changed-after", "cursor", "advance",
	} {
		_ = viper.BindPFlag("list."+name, listCmd.Flags().Lookup(name))
	}
}

func runList(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	cursorName := viper.GetString("list.cursor")
	advance := viper.GetBool("list.advance")
	if advance && cursorName == "" {
		return output.UsageError("--advance requires --cursor <name>")
	}

	if cmd.Flags().Changed("changed-after") {
		return runListChanges(ctx, cmd)
	}

	// --q is an RSQL query the instance's search engine answers
	// (GET /api/v1/search). There is no local search.
	if q := viper.GetString("list.q"); q != "" {
		client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
		if err != nil {
			return err
		}
		objects, total, err := client.Search(ctx, dpkmsclient.SearchRequest{
			Query:  q,
			Limit:  viper.GetInt("list.limit"),
			Offset: viper.GetInt("list.start"),
		})
		if err != nil {
			return err
		}
		// Cursor with --q: out of scope for predicate composition; emit warning.
		if cursorName != "" {
			fmt.Fprintln(os.Stderr, "warning: --cursor with --q does not gate by created_at; results unfiltered")
		}
		return printObjectResults(cmd, objects, total)
	}

	filter, err := listFilter(cmd)
	if err != nil {
		return err
	}
	client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
	if err != nil {
		return err
	}
	if cursorName != "" {
		return runListWithCursor(ctx, cmd, client, filter, cursorName, advance)
	}

	// --facets: show metadata type count breakdown alongside results.
	if viper.GetBool("list.facets") {
		counts, err := client.ObjectFacets(ctx, filter)
		if err != nil {
			return err
		}
		if isJSONOutput() {
			objects, total, err := client.ListObjects(ctx, filter)
			if err != nil {
				return err
			}
			return outputJSON(os.Stdout, map[string]any{
				"objects": objects,
				"total":   total,
				"facets":  counts,
			})
		}
		types := make([]string, 0, len(counts))
		for t := range counts {
			types = append(types, t)
		}
		slices.Sort(types)
		fmt.Printf("Facets (metadata type):\n")
		for _, t := range types {
			fmt.Printf("  %-20s %d\n", t, counts[t])
		}
		fmt.Println()
	}

	objects, total, err := client.ListObjects(ctx, filter)
	if err != nil {
		return err
	}
	return printObjectResults(cmd, objects, total)
}

// runListChanges lists by change order (--changed-after): the objects
// changed since a change sequence, oldest change first. The sequence of
// the next page comes back as next_changed_after; the caller keeps it,
// as --cursor keeps a created_at position. Order and position flags
// belong to other listings and are refused.
func runListChanges(ctx context.Context, cmd *cobra.Command) error {
	seq := viper.GetInt64("list.changed-after")
	if seq < 0 {
		return output.UsageError(fmt.Sprintf("--changed-after %d: want a change sequence >= 0", seq))
	}
	for _, other := range []string{"cursor", "q", "facets", "sort", "dir", "start"} {
		if cmd.Flags().Changed(other) {
			e := output.UsageError(fmt.Sprintf("--changed-after cannot be combined with --%s", other))
			e.SuggestedFix = "--changed-after lists in change order and pages by next_changed_after; drop --" + other
			return e
		}
	}
	filter, err := listFilter(cmd)
	if err != nil {
		return err
	}
	if err := refuseTagRanking("--changed-after", "change", filter.Tagged); err != nil {
		return err
	}
	filter.Sort, filter.Dir, filter.Offset = "", "", 0
	filter.ChangedAfter = &seq
	client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
	if err != nil {
		return err
	}
	page, err := client.ListObjectChanges(ctx, filter)
	if err != nil {
		return err
	}
	if isJSONOutput() {
		return outputJSON(os.Stdout, map[string]any{
			"objects":            page.Objects,
			"total":              page.Total,
			"next_changed_after": page.NextChangedAfter,
		})
	}
	// The cursor is prose here: stderr keeps stdout a table or rows.
	fmt.Fprintf(os.Stderr, "next --changed-after %d\n", page.NextChangedAfter)
	return printObjectResults(cmd, page.Objects, page.Total)
}

// listFilter reads the list flags into the API filter. It rejects a
// malformed tag list, an unknown sort and malformed dates as usage errors
// naming the flag.
func listFilter(cmd *cobra.Command) (dpkmsclient.ObjectFilter, error) {
	tags, err := parseTaggedFlag(cmd)
	if err != nil {
		return dpkmsclient.ObjectFilter{}, err
	}
	sortBy := viper.GetString("list.sort")
	apiSort, ok := listSorts[sortBy]
	if !ok {
		e := output.UsageError(fmt.Sprintf("--sort %q: want created, recent or updated", sortBy))
		e.SuggestedFix = "sort by created (the default; recent is a synonym) or updated"
		return dpkmsclient.ObjectFilter{}, e
	}
	dir := viper.GetString("list.dir")
	if dir != "asc" && dir != "desc" {
		return dpkmsclient.ObjectFilter{}, output.UsageError(fmt.Sprintf("--dir %q: want asc or desc", dir))
	}
	f := dpkmsclient.ObjectFilter{
		Type:       viper.GetString("list.type"),
		Subtype:    viper.GetString("list.subtype"),
		Tagged:     tags,
		Mention:    viper.GetString("list.mention"),
		Pipeline:   viper.GetString("list.pipeline"),
		Status:     viper.GetString("list.status"),
		MetaType:   viper.GetString("list.meta-type"),
		Topic:      viper.GetString("list.topic"),
		Person:     viper.GetString("list.person"),
		SourceType: viper.GetString("list.source-type"),
		Limit:      viper.GetInt("list.limit"),
		Offset:     viper.GetInt("list.start"),
		Sort:       apiSort,
		Dir:        dir,
	}
	if f.Since, f.Until, err = dateRange(viper.GetString("list.since"), viper.GetString("list.until")); err != nil {
		return dpkmsclient.ObjectFilter{}, err
	}
	if f.MentionedSince, f.MentionedUntil, err = mentionedRange(viper.GetString("list.mentioned-since"), viper.GetString("list.mentioned-until")); err != nil {
		return dpkmsclient.ObjectFilter{}, err
	}
	return f, nil
}

func printObjectResults(cmd *cobra.Command, objects []*storage.KnowledgeObject, total int) error {
	if isJSONOutput() {
		return outputJSON(os.Stdout, map[string]any{
			"objects": objects,
			"total":   total,
		})
	}

	// csv/text/table project from objectRow's tags through kit's
	// registry, so each renders as itself rather than falling back to
	// the human table. The "(N total)" line is deliberately omitted
	// there: it is prose, and on stdout it would sit in front of the
	// header row and break every parser.
	if cliformat.Rows() {
		return cliformat.DispatchRows(cmd, cmd.OutOrStdout(), objectRows(objects))
	}

	fmt.Printf("Knowledge Objects (%d total)\n\n", total)
	headers := []string{"ID", "Type", "Title", "Created"}
	objRows := objectRows(objects)
	rows := make([][]string, 0, len(objRows))
	for _, r := range objRows {
		rows = append(rows, []string{r.ID, r.Type, r.Title, r.Created})
	}
	printTable(os.Stdout, headers, rows)
	return nil
}

// objectRow is the row shape every tabular format projects from.
type objectRow struct {
	ID      string `table:"ID"`
	Type    string `table:"Type"`
	Title   string `table:"Title"`
	Created string `table:"Created"`
}

// objectRows projects knowledge objects onto the row shape, applying
// the same title resolution and duplicate annotation the human table
// has always shown so the formats do not disagree on what a row says.
//
// Objects with a known duplicate are annotated "(dup:<id>)". The
// duplicate_of field is set by the dedup pipeline step, or by the
// "warn"/"keep" policy when near-duplicate detection
// (duplicates.check_similar) is enabled.
func objectRows(objects []*storage.KnowledgeObject) []objectRow {
	rows := make([]objectRow, 0, len(objects))
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
		rows = append(rows, objectRow{
			ID:      obj.ID,
			Type:    obj.Type,
			Title:   title,
			Created: obj.CreatedAt.Format("2006-01-02 15:04"),
		})
	}
	return rows
}

// currentSnapshot reads list flags into a QuerySnapshot for drift compare.
func currentSnapshot(filter dpkmsclient.ObjectFilter) cursor.QuerySnapshot {
	var tag []string
	if s := filter.Tagged.String(); s != "" {
		tag = []string{s}
	}
	return cursor.QuerySnapshot{
		Mention: splitNonEmpty(viper.GetString("list.mention")),
		Tag:     tag,
		Profile: viper.GetString("profile"),
		Q:       viper.GetString("list.q"),
		Type:    viper.GetString("list.type"),
		After:   viper.GetString("list.since"),
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

// runListWithCursor lists through a named cursor, a client-side file
// (internal/cursor):
//   - new cursor (no advance flag, never seen): exits 2 with hint
//   - new cursor + --advance: starts at epoch 0, returns full set, then advances
//   - existing cursor: gates created_at > last_seen_at; sort forced ASC
//   - empty result: cursor untouched
//   - drift: warn to stderr, continue
//   - JSON output: {cursor: ..., items: ..., advanced: bool}
func runListWithCursor(ctx context.Context, cmd *cobra.Command, client *dpkmsclient.Client, filter dpkmsclient.ObjectFilter, name string, advance bool) error {
	// A cursor walks created_at order; ranking by tags matched would
	// reorder the page and break the position it saves.
	if err := refuseTagRanking("--cursor", "creation", filter.Tagged); err != nil {
		return err
	}
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
	snap := currentSnapshot(filter)
	if !fresh && !c.Query.Equal(snap) {
		fmt.Fprintf(os.Stderr,
			"warning: cursor %q query snapshot differs from current flags; results may not match prior listings\n", name)
	}

	// The cursor gates created_at > last_seen_at. The API's after bound
	// is inclusive at second precision, so the gate is one second past
	// the tail: items sharing the tail's second were in the prior
	// listing. It replaces --since when later.
	if !c.LastSeenAt.IsZero() {
		gate := c.LastSeenAt.Add(time.Second)
		since, _ := time.Parse(time.RFC3339, filter.Since) // resolved by listFilter
		if filter.Since == "" || gate.After(since) {
			filter.Since = gate.Format(time.RFC3339)
		}
	}
	filter.Sort = "created_at"
	filter.Dir = "asc"

	objects, total, err := client.ListObjects(ctx, filter)
	if err != nil {
		return err
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
	return printObjectResults(cmd, objects, total)
}
