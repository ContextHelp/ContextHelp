package cmd

import (
	"errors"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/dpkmsclient"
	"github.com/spf13/cobra"
	"hop.top/kit/go/console/output"
)

var pageCmd = &cobra.Command{
	Use:   "page",
	Short: "Manage entity pages",
	Long: `View and manage persistent entity/concept pages.

Entity pages are compiled knowledge objects that aggregate information
about a single entity from all mentioning sources.

Every subcommand calls the dpkms API of the resolved instance (--server,
--instance, CTXT_INSTANCE, the current instance, then config). list and
show need the read:objects scope; refresh needs write:objects.

Examples:
  ctxt page list
  ctxt page show person.alice
  ctxt page refresh lang.go`,
}

var pageListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all entity pages",
	Long: `List all entity pages with a one-line summary per page (id, slug,
source count, last update), newest first.

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
	for _, c := range []*cobra.Command{pageListCmd, pageShowCmd, pageRefreshCmd} {
		c.Flags().String("server", "", serverFlagUsage)
	}
}

// pageLookupError gives a dpkms 404 for slug the recovery a caller
// needs; every other failure keeps the class the client gave it.
func pageLookupError(slug string, err error) error {
	var ke *output.Error
	if !errors.As(err, &ke) || ke.ExitCode != output.ExitNotFound {
		return fmt.Errorf("entity page %s: %w", slug, err)
	}
	e := output.NotFoundError(fmt.Sprintf("no entity page for %q", slug))
	e.SuggestedFix = "run `ctxt page list` to see the entity pages"
	return e
}

func runPageList(cmd *cobra.Command, _ []string) error {
	limit, _ := cmd.Flags().GetInt("limit")
	client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
	if err != nil {
		return err
	}
	pages, err := client.ListPages(cmd.Context(), limit)
	if err != nil {
		return fmt.Errorf("list pages: %w", err)
	}

	out := cmd.OutOrStdout()
	if isJSONOutput() {
		return outputJSON(out, pages)
	}

	fmt.Fprintf(out, "Entity Pages (%d)\n\n", len(pages))
	headers := []string{"ID", "Entity", "Sources", "Updated"}
	var rows [][]string
	for _, p := range pages {
		meta, _ := dpkmsclient.PageMeta(p)
		id := p.ID
		if len(id) > 8 {
			id = id[:8]
		}
		rows = append(rows, []string{
			id,
			meta.EntitySlug,
			fmt.Sprintf("%d", len(meta.SourceIDs)),
			p.UpdatedAt.Format("2006-01-02 15:04"),
		})
	}
	printTable(out, headers, rows)
	return nil
}

func runPageShow(cmd *cobra.Command, args []string) error {
	slug := args[0]
	client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
	if err != nil {
		return err
	}
	page, err := client.GetPage(cmd.Context(), slug)
	if err != nil {
		return pageLookupError(slug, err)
	}

	out := cmd.OutOrStdout()
	if isJSONOutput() {
		return outputJSON(out, page)
	}

	meta, _ := dpkmsclient.PageMeta(page)
	fmt.Fprintf(out, "Entity Page: %s\n\n", meta.EntitySlug)
	fmt.Fprintf(out, "ID:        %s\n", page.ID)
	fmt.Fprintf(out, "Sources:   %d\n", len(meta.SourceIDs))
	fmt.Fprintf(out, "Revisions: %d\n", meta.RevisionCount)
	fmt.Fprintf(out, "Updated:   %s\n\n", page.UpdatedAt.Format("2006-01-02 15:04:05"))
	fmt.Fprintln(out, "--- Content ---")
	fmt.Fprintln(out, page.RawContent)
	return nil
}

func runPageRefresh(cmd *cobra.Command, args []string) error {
	slug := args[0]
	client, err := newDpkmsClient(cmd, dpkmsclient.DefaultTimeout)
	if err != nil {
		return err
	}
	page, err := client.RefreshPage(cmd.Context(), slug)
	if err != nil {
		return pageLookupError(slug, err)
	}

	out := cmd.OutOrStdout()
	if isJSONOutput() {
		return outputJSON(out, page)
	}
	fmt.Fprintf(out, "Refreshed entity page: %s (ID: %s)\n", slug, page.ID)
	return nil
}
