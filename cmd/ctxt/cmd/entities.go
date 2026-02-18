package cmd

import (
	"context"
	"fmt"
	"os"

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
	RunE:  runEntitiesList,
}

var entitiesShowCmd = &cobra.Command{
	Use:   "show <slug>",
	Short: "Show entity details",
	Args:  cobra.ExactArgs(1),
	RunE:  runEntitiesShow,
}

var entitiesSearchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search for entities",
	Args:  cobra.ExactArgs(1),
	RunE:  runEntitiesSearch,
}

var entitiesBacklinksCmd = &cobra.Command{
	Use:   "backlink <slug>",
	Short: "Show entity backlinks",
	Args:  cobra.ExactArgs(1),
	RunE:  runEntitiesBacklinks,
}

func init() {
	rootCmd.AddCommand(entitiesCmd)

	// Add subcommands
	entitiesCmd.AddCommand(entitiesListCmd)
	entitiesCmd.AddCommand(entitiesShowCmd)
	entitiesCmd.AddCommand(entitiesSearchCmd)
	entitiesCmd.AddCommand(entitiesBacklinksCmd)

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
