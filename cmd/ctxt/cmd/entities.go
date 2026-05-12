package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var entitiesCmd = &cobra.Command{
	Use:   "entity",
	Short: "Query and inspect entities",
	Long: `Query and inspect canonical entities (concepts).

Entities represent canonical concepts that can be referenced across
knowledge objects using the @entity.slug notation.

Examples:
  # List all entities
  ctxt entity list

  # Show entity details
  ctxt entity show ui.best-practice

  # Search for entities
  ctxt entity search "checkout"

  # Show entity backlinks
  ctxt entity backlink ui.best-practice`,
}

var entitiesListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all entities",
	Long: `Print every canonical entity in the local store.

The table reports slug, title, and namespace. Use --namespace to filter and
--limit to bound the result set. JSON output (--format json) emits the
full entity record for each row.`,
	RunE: runEntitiesList,
}

var entitiesShowCmd = &cobra.Command{
	Use:   "show <slug>",
	Short: "Show entity details",
	Long: `Print the full record for a single canonical entity.

Resolves <slug> via the local entity store and prints title, namespace,
description, creation time, and the list of aliases. JSON output emits
the raw entity object.`,
	Args: cobra.ExactArgs(1),
	RunE: runEntitiesShow,
}

var entitiesSearchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search for entities",
	Long: `Search entities by free-text query.

Returns up to 50 entities whose title, slug, or aliases match <query>.
The result table reports slug, title, and namespace; JSON output emits
each hit as a full entity record.`,
	Args: cobra.ExactArgs(1),
	RunE: runEntitiesSearch,
}

var entitiesBacklinksCmd = &cobra.Command{
	Use:   "backlink <slug>",
	Short: "Show entity backlinks",
	Long: `List every knowledge object that references the named entity.

Returns the set of objects whose body or metadata mentions @<slug> (or one of
its aliases). The table reports object ID, type, and creation time; JSON
output emits the full object records.`,
	Args: cobra.ExactArgs(1),
	RunE: runEntitiesBacklinks,
}

func init() {
	rootCmd.AddCommand(entitiesCmd)

	// Add subcommands
	entitiesCmd.AddCommand(entitiesListCmd)
	entitiesCmd.AddCommand(entitiesShowCmd)
	entitiesCmd.AddCommand(entitiesSearchCmd)
	entitiesCmd.AddCommand(entitiesBacklinksCmd)

	// 12fcc conformance: side-effect annotations. All four entity
	// leaves are pure read operations (list, show, search, backlink:
	// the latter is an inbound-edge query, not a write).
	cliconv.WithSideEffect(entitiesListCmd, cliconv.SideEffectRead)
	cliconv.WithSideEffect(entitiesShowCmd, cliconv.SideEffectRead)
	cliconv.WithSideEffect(entitiesSearchCmd, cliconv.SideEffectRead)
	cliconv.WithSideEffect(entitiesBacklinksCmd, cliconv.SideEffectRead)

	// Kit verb defaults cover list/show/search (Yes). "backlink" is
	// not in the default table; tag explicitly.
	cliconv.WithIdempotency(entitiesBacklinksCmd, cliconv.IdempotencyYes)

	// 12fcc strict-gate: examples on every leaf (all four are reads,
	// so no next-steps required).
	cliconv.WithExamples(entitiesListCmd, []cliconv.Example{
		{Title: "List all entities", Command: "ctxt entity list"},
		{Title: "Filter by namespace", Command: "ctxt entity list --namespace ui --limit 100"},
	})
	cliconv.WithExamples(entitiesShowCmd, []cliconv.Example{
		{Title: "Show entity details", Command: "ctxt entity show ui.best-practice"},
		{Title: "JSON dump", Command: "ctxt entity show ui.best-practice --format json"},
	})
	cliconv.WithExamples(entitiesSearchCmd, []cliconv.Example{
		{Title: "Search by free-text", Command: "ctxt entity search \"checkout\""},
		{Title: "JSON for scripting", Command: "ctxt entity search \"checkout\" --format json"},
	})
	cliconv.WithExamples(entitiesBacklinksCmd, []cliconv.Example{
		{Title: "List objects referencing the entity", Command: "ctxt entity backlink ui.best-practice"},
		{Title: "JSON for scripting", Command: "ctxt entity backlink ui.best-practice --format json"},
	})

	// List flags
	entitiesListCmd.Flags().Int("limit", 50, "maximum results")
	entitiesListCmd.Flags().String("namespace", "", "filter by namespace")

	// Bind flags to viper
	viper.BindPFlag("entities.limit", entitiesListCmd.Flags().Lookup("limit"))
	viper.BindPFlag("entities.namespace", entitiesListCmd.Flags().Lookup("namespace"))
}

func runEntitiesList(cmd *cobra.Command, args []string) error {
	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx := context.Background()
	filter := storage.EntityFilter{
		Namespace: viper.GetString("entities.namespace"),
		Limit:     viper.GetInt("entities.limit"),
	}
	entities, err := svc.ListEntities(ctx, filter)
	if err != nil {
		return fmt.Errorf("list entities: %w", err)
	}

	if isJSONOutput() {
		return outputJSON(os.Stdout, entities)
	}

	fmt.Printf("Entities (%d)\n\n", len(entities))
	headers := []string{"Slug", "Title", "Namespace"}
	var rows [][]string
	for _, e := range entities {
		rows = append(rows, []string{e.Slug, e.Title, e.Namespace})
	}
	printTable(os.Stdout, headers, rows)
	return nil
}

func runEntitiesShow(cmd *cobra.Command, args []string) error {
	slug := args[0]

	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx := context.Background()
	entity, err := svc.GetEntity(ctx, slug)
	if err != nil {
		return fmt.Errorf("get entity: %w", err)
	}

	if isJSONOutput() {
		return outputJSON(os.Stdout, entity)
	}

	fmt.Printf("Entity: %s\n\n", entity.Slug)
	fmt.Printf("Title:       %s\n", entity.Title)
	fmt.Printf("Namespace:   %s\n", entity.Namespace)
	if entity.Description != "" {
		fmt.Printf("Description: %s\n", entity.Description)
	}
	fmt.Printf("Created:     %s\n", entity.CreatedAt.Format("2006-01-02 15:04:05"))
	if len(entity.Aliases) > 0 {
		fmt.Println("\nAliases:")
		for _, a := range entity.Aliases {
			fmt.Printf("  - %s\n", a)
		}
	}
	return nil
}

func runEntitiesSearch(cmd *cobra.Command, args []string) error {
	query := args[0]

	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx := context.Background()
	results, err := svc.SearchEntities(ctx, query, 50)
	if err != nil {
		return fmt.Errorf("search entities: %w", err)
	}

	if isJSONOutput() {
		return outputJSON(os.Stdout, results)
	}

	fmt.Printf("Entity search: %q (%d results)\n\n", query, len(results))
	headers := []string{"Slug", "Title", "Namespace"}
	var rows [][]string
	for _, e := range results {
		rows = append(rows, []string{e.Slug, e.Title, e.Namespace})
	}
	printTable(os.Stdout, headers, rows)
	return nil
}

func runEntitiesBacklinks(cmd *cobra.Command, args []string) error {
	slug := args[0]

	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx := context.Background()
	objects, err := svc.EntityBacklinks(ctx, slug)
	if err != nil {
		return fmt.Errorf("backlinks: %w", err)
	}

	if isJSONOutput() {
		return outputJSON(os.Stdout, objects)
	}

	fmt.Printf("Backlinks for %s (%d objects)\n\n", slug, len(objects))
	headers := []string{"ID", "Type", "Created"}
	var rows [][]string
	for _, obj := range objects {
		rows = append(rows, []string{
			obj.ID,
			obj.Type,
			obj.CreatedAt.Format("2006-01-02 15:04"),
		})
	}
	printTable(os.Stdout, headers, rows)
	return nil
}
