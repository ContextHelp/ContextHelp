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

Filters combine with AND: type, subtype, one tag, mention, pipeline,
status, creation dates, and metadata facets (type, topic, person,
source type, dates mentioned). --q runs an RSQL query instead.

Examples:
  # List the most recently created objects
  ctxt list

  # Filter by type
  ctxt list --type url

  # Filter by one tag
  ctxt list --tagged ux

  # Filter by mentions
  ctxt list --mention @ui.best-practice

  # Filter by creation date (YYYY-MM-DD or RFC 3339)
  ctxt list --after 2025-01-01 --before 2025-01-31

  # Filter by metadata type
  ctxt list --meta-type observation

  # Filter by topic and source
  ctxt list --topic auth --source-type slack

  # Filter by person
  ctxt list --person alice-chen

  # Filter by dates mentioned in content
  ctxt list --since 2026-04-01 --until 2026-04-23

  # Most recently updated first
  ctxt list --sort updated

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
		{Title: "Use the query language", Command: "ctxt list --q \"type==url;tag=in=(ux,design)\""},
	})

	// Filter flags
	listCmd.Flags().String("type", "", "filter by knowledge object type")
	listCmd.Flags().String("tagged", "", "filter by one tag")
	listCmd.Flags().String("mention", "", "filter by mention (@ns.slug or ctxt:// URI)")
	listCmd.Flags().String("pipeline", "", "filter by pipeline")
	listCmd.Flags().String("subtype", "", "filter by subtype")
	listCmd.Flags().String("before", "", "created on or before (YYYY-MM-DD or RFC 3339)")
	listCmd.Flags().String("after", "", "created on or after (YYYY-MM-DD or RFC 3339)")
	listCmd.Flags().String("q", "", "RSQL query, answered by the instance's search")
	listCmd.Flags().String("status", "", "filter by status (active|inbox|discarded|raw|all)")

	// Metadata facet filters
	listCmd.Flags().String("meta-type", "", "filter by metadata type (e.g. observation, task)")
	listCmd.Flags().String("topic", "", "filter by topic in metadata")
	listCmd.Flags().String("person", "", "filter by person in metadata")
	listCmd.Flags().String("since", "", "filter by dates_mentioned >= (YYYY-MM-DD)")
	listCmd.Flags().String("until", "", "filter by dates_mentioned <= (YYYY-MM-DD)")
	listCmd.Flags().String("source-type", "", "filter by source_type")
	listCmd.Flags().Bool("facets", false, "show metadata type count breakdown")

	// Display flags
	listCmd.Flags().Int("limit", 50, "maximum results (0: every match)")
	listCmd.Flags().Int("start", 0, "pagination offset")
	listCmd.Flags().String("sort", "created", "sort by created (synonym: recent) or updated")
	listCmd.Flags().String("dir", "desc", "sort direction (asc|desc)")

	// Named cursor: per-viewer position over results, kept in a client
	// file.
	listCmd.Flags().String("cursor", "",
		"use named cursor; returns items added after cursor's last_seen_at")
	listCmd.Flags().Bool("advance", false,
		"advance the cursor after a successful list (requires --cursor)")
	listCmd.Flags().String("server", "", serverFlagUsage)

	// Bind flags to viper
	for _, name := range []string{
		"type", "tagged", "mention", "pipeline", "subtype", "before", "after", "q", "status",
		"meta-type", "topic", "person", "since", "until", "source-type", "facets",
		"limit", "start", "sort", "dir", "cursor", "advance",
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

	filter, err := listFilter()
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

// listFilter reads the list flags into the API filter. It rejects what
// the instance cannot select by (several tags, an unknown sort) and
// malformed dates as usage errors naming the flag.
func listFilter() (dpkmsclient.ObjectFilter, error) {
	tag, err := oneTag(viper.GetString("list.tagged"))
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
		Tag:        tag,
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
	for _, d := range []struct {
		flag string
		dst  *string
		day  bool
	}{
		{"after", &f.After, false}, {"before", &f.Before, false},
		{"since", &f.Since, true}, {"until", &f.Until, true},
	} {
		v := viper.GetString("list." + d.flag)
		if v == "" {
			continue
		}
		if _, err := parseDateFlag(d.flag, v, d.day); err != nil {
			return dpkmsclient.ObjectFilter{}, err
		}
		*d.dst = v
	}
	return f, nil
}

// oneTag returns v when it names at most one tag. The instance filters
// by a single tag label; several go through --q.
func oneTag(v string) (string, error) {
	v = strings.TrimSpace(v)
	if strings.Contains(v, ",") {
		e := output.UsageError(fmt.Sprintf("--tagged %q: takes one tag", v))
		e.SuggestedFix = "filter by one tag, or match several with --q \"tag=in=(a,b)\""
		return "", e
	}
	return v, nil
}

// parseDateFlag parses a date flag: YYYY-MM-DD, or also an RFC 3339
// timestamp unless dayOnly. A malformed value is a usage error naming
// the flag.
func parseDateFlag(flag, v string, dayOnly bool) (time.Time, error) {
	if t, err := time.Parse(time.DateOnly, v); err == nil {
		return t, nil
	}
	if !dayOnly {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return t, nil
		}
		return time.Time{}, output.UsageError(fmt.Sprintf("--%s %q: want YYYY-MM-DD or an RFC 3339 timestamp", flag, v))
	}
	return time.Time{}, output.UsageError(fmt.Sprintf("--%s %q: want YYYY-MM-DD", flag, v))
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

// runListWithCursor lists through a named cursor, a client-side file
// (internal/cursor):
//   - new cursor (no advance flag, never seen): exits 2 with hint
//   - new cursor + --advance: starts at epoch 0, returns full set, then advances
//   - existing cursor: gates created_at > last_seen_at; sort forced ASC
//   - empty result: cursor untouched
//   - drift: warn to stderr, continue
//   - JSON output: {cursor: ..., items: ..., advanced: bool}
func runListWithCursor(ctx context.Context, cmd *cobra.Command, client *dpkmsclient.Client, filter dpkmsclient.ObjectFilter, name string, advance bool) error {
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

	// The cursor gates created_at > last_seen_at. The API's after bound
	// is inclusive at second precision, so the gate is one second past
	// the tail: items sharing the tail's second were in the prior
	// listing. It replaces --after when later.
	if !c.LastSeenAt.IsZero() {
		gate := c.LastSeenAt.Add(time.Second)
		after, _ := parseDateFlag("after", filter.After, false) // validated by listFilter
		if filter.After == "" || gate.After(after) {
			filter.After = gate.Format(time.RFC3339)
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
