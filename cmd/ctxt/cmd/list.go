package cmd

import (
	"fmt"

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
	// TODO: Implement actual list logic
	fmt.Println("Knowledge Objects:")
	fmt.Println()
	fmt.Printf("Filters: type=%s, tag=%s, mention=%s\n",
		viper.GetString("list.type"),
		viper.GetString("list.tag"),
		viper.GetString("list.mention"))
	fmt.Printf("Sort: %s %s, Limit: %d, Offset: %d\n\n",
		viper.GetString("list.sort"),
		viper.GetString("list.dir"),
		viper.GetInt("list.limit"),
		viper.GetInt("list.start"))

	fmt.Println("ID        | Type | Title                           | Created")
	fmt.Println("----------|------|----------------------------------|--------------------")
	fmt.Println("obj_001   | url  | Best UX practices for signup    | 2025-01-26 09:00:00")
	fmt.Println("obj_002   | text | Fix onboarding flow issues      | 2025-01-26 09:30:00")
	fmt.Println("obj_003   | img  | Landing page screenshot         | 2025-01-26 10:00:00")

	return nil
}
