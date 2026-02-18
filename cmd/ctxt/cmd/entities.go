package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var entitiesCmd = &cobra.Command{
	Use:   "entities",
	Short: "Query and inspect entities",
	Long: `Query and inspect canonical entities (concepts).

Entities represent canonical concepts that can be referenced across
knowledge objects using the @entity.slug notation.

Examples:
  # List all entities
  ctxt entities list

  # Show entity details
  ctxt entities show ui.best-practice

  # Search for entities
  ctxt entities search "checkout"

  # Show entity backlinks
  ctxt entities backlinks ui.best-practice`,
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
	Use:   "backlinks <slug>",
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
	// TODO: Implement actual entities list logic
	limit := viper.GetInt("entities.limit")
	namespace := viper.GetString("entities.namespace")

	fmt.Printf("Entities (limit: %d", limit)
	if namespace != "" {
		fmt.Printf(", namespace: %s", namespace)
	}
	fmt.Println(")")
	fmt.Println()
	fmt.Println("Slug                      | Title                    | Namespace")
	fmt.Println("--------------------------|--------------------------|----------")
	fmt.Println("ui.best-practice          | UI Best Practice         | ui")
	fmt.Println("ux.onboarding             | UX Onboarding            | ux")
	fmt.Println("auth.jwt                  | JWT Authentication       | auth")

	return nil
}

func runEntitiesShow(cmd *cobra.Command, args []string) error {
	slug := args[0]

	// TODO: Implement actual entities show logic
	fmt.Printf("Entity: %s\n\n", slug)
	fmt.Println("Title:       UI Best Practice")
	fmt.Println("Namespace:   ui")
	fmt.Println("Description: Established patterns and guidelines for user interface design")
	fmt.Println()
	fmt.Println("Aliases:")
	fmt.Println("  - ui-best-practice")
	fmt.Println("  - ui-pattern")
	fmt.Println()
	fmt.Println("Translations:")
	fmt.Println("  fr: Meilleure pratique UI")
	fmt.Println("  es: Mejor práctica de UI")
	fmt.Println()
	fmt.Println("Registry Source: uxpatterns")
	fmt.Println("Backlinks:       23 knowledge objects")

	return nil
}

func runEntitiesSearch(cmd *cobra.Command, args []string) error {
	query := args[0]

	// TODO: Implement actual entities search logic
	fmt.Printf("Searching entities for: %s\n\n", query)
	fmt.Println("Slug                      | Title                    | Relevance")
	fmt.Println("--------------------------|--------------------------|----------")
	fmt.Println("checkout.flow             | Checkout Flow            | 0.95")
	fmt.Println("checkout.optimization     | Checkout Optimization    | 0.87")
	fmt.Println("payment.gateway           | Payment Gateway          | 0.72")

	return nil
}

func runEntitiesBacklinks(cmd *cobra.Command, args []string) error {
	slug := args[0]

	// TODO: Implement actual entities backlinks logic
	fmt.Printf("Backlinks for entity: %s\n\n", slug)
	fmt.Println("Knowledge objects that mention this entity:")
	fmt.Println()
	fmt.Println("ID        | Title                           | Type | Created")
	fmt.Println("----------|----------------------------------|------|--------------------")
	fmt.Println("obj_001   | Best UX practices for signup    | url  | 2025-01-26 09:00:00")
	fmt.Println("obj_005   | Mobile UI patterns collection   | url  | 2025-01-26 08:30:00")
	fmt.Println("obj_012   | Design system documentation     | text | 2025-01-25 15:00:00")

	return nil
}
