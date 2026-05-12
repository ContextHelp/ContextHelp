package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var pageCmd = &cobra.Command{
	Use:   "page",
	Short: "Manage entity pages",
	Long: `View and manage persistent entity/concept pages.

Entity pages are compiled knowledge objects that aggregate information
about a single entity from all mentioning sources.

Examples:
  ctxt page list
  ctxt page show person.alice
  ctxt page refresh lang.go`,
}

var pageListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all entity pages",
	Long: `List all entity pages with a one-line summary per page (id, slug,
source count, last update).

Examples:
  ctxt page list
  ctxt page list --limit 100`,
	RunE: runPageList,
}

var pageShowCmd = &cobra.Command{
	Use:   "show <entity_slug>",
	Short: "Show entity page content",
	Long: `Show the rendered content of a single entity page along with
metadata (sources, revisions, last update).

Examples:
  ctxt page show person.alice
  ctxt page show lang.go`,
	Args: cobra.ExactArgs(1),
	RunE: runPageShow,
}

var pageRefreshCmd = &cobra.Command{
	Use:   "refresh <entity_slug>",
	Short: "Regenerate entity page from sources",
	Long: `Regenerate the entity page from all currently linked sources. The
existing page is overwritten with the freshly compiled content.

Examples:
  ctxt page refresh person.alice
  ctxt page refresh lang.go`,
	Args: cobra.ExactArgs(1),
	RunE: runPageRefresh,
}

func init() {
	rootCmd.AddCommand(pageCmd)

	pageCmd.AddCommand(pageListCmd)
	pageCmd.AddCommand(pageShowCmd)
	pageCmd.AddCommand(pageRefreshCmd)

	cliconv.WithSideEffect(pageListCmd, cliconv.SideEffectRead)
	cliconv.WithExamples(pageListCmd, []cliconv.Example{
		{Title: "List entity pages", Command: "ctxt page list"},
		{Title: "Cap the result count", Command: "ctxt page list --limit 100"},
	})
	cliconv.WithSideEffect(pageShowCmd, cliconv.SideEffectRead)
	cliconv.WithExamples(pageShowCmd, []cliconv.Example{
		{Title: "Show an entity page", Command: "ctxt page show person.alice"},
		{Title: "Render as JSON", Command: "ctxt page show lang.go --format json"},
	})
	cliconv.WithSideEffect(pageRefreshCmd, cliconv.SideEffectWrite)
	cliconv.WithIdempotency(pageRefreshCmd, cliconv.IdempotencyYes)
	cliconv.WithExamples(pageRefreshCmd, []cliconv.Example{
		{Title: "Refresh a single page", Command: "ctxt page refresh person.alice"},
		{Title: "Refresh after re-tagging", Command: "ctxt page refresh lang.go"},
	})
	cliconv.WithNextSteps(pageRefreshCmd, []cliconv.NextStep{
		{When: "on success", Suggest: "ctxt page show <entity_slug>", Reason: "review the regenerated content"},
	})

	pageListCmd.Flags().Int("limit", 50, "maximum results")
	viper.BindPFlag("page.limit", pageListCmd.Flags().Lookup("limit"))
}

func runPageList(cmd *cobra.Command, args []string) error {
	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx := context.Background()
	limit := viper.GetInt("page.limit")

	pages, err := svc.ListEntityPages(ctx, limit)
	if err != nil {
		return fmt.Errorf("list pages: %w", err)
	}

	if isJSONOutput() {
		return outputJSON(os.Stdout, pages)
	}

	fmt.Printf("Entity Pages (%d)\n\n", len(pages))
	headers := []string{"ID", "Entity", "Sources", "Updated"}
	var rows [][]string
	for _, p := range pages {
		meta, _ := service.ExtractPageMeta(p)
		rows = append(rows, []string{
			p.ID[:8],
			meta.EntitySlug,
			fmt.Sprintf("%d", len(meta.SourceIDs)),
			p.UpdatedAt.Format("2006-01-02 15:04"),
		})
	}
	printTable(os.Stdout, headers, rows)
	return nil
}

func runPageShow(cmd *cobra.Command, args []string) error {
	slug := args[0]

	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx := context.Background()
	page, err := svc.GetEntityPage(ctx, slug)
	if err != nil {
		return fmt.Errorf("get page: %w", err)
	}
	if page == nil {
		return fmt.Errorf("entity page not found: %s", slug)
	}

	if isJSONOutput() {
		return outputJSON(os.Stdout, page)
	}

	meta, _ := service.ExtractPageMeta(page)
	fmt.Printf("Entity Page: %s\n\n", meta.EntitySlug)
	fmt.Printf("ID:        %s\n", page.ID)
	fmt.Printf("Sources:   %d\n", len(meta.SourceIDs))
	fmt.Printf("Revisions: %d\n", meta.RevisionCount)
	fmt.Printf("Updated:   %s\n\n", page.UpdatedAt.Format("2006-01-02 15:04:05"))
	fmt.Println("--- Content ---")
	fmt.Println(page.RawContent)
	return nil
}

func runPageRefresh(cmd *cobra.Command, args []string) error {
	slug := args[0]

	svc, cleanup, err := newService()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx := context.Background()
	page, err := svc.RefreshEntityPage(ctx, slug)
	if err != nil {
		return fmt.Errorf("refresh page: %w", err)
	}

	fmt.Printf("Refreshed entity page: %s (ID: %s)\n", slug, page.ID)
	return nil
}
