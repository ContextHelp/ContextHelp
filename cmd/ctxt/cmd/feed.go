package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	gohttp "net/http"
	"net/url"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/cli/cliconv"
	"github.com/ideacrafterslabs/ctxt/internal/dpkmsclient"
	"github.com/spf13/cobra"
)

var feedCmd = &cobra.Command{
	Use:   "feed",
	Short: "Manage RSS/Atom feeds",
	Long: `Add, list, sync, and remove feed subscriptions.

Examples:
  # Add a feed
  ctxt feed add https://example.com/feed.xml

  # List all feeds
  ctxt feed list

  # Sync a feed by ID
  ctxt feed sync --id feed_12345678

  # Sync a feed by URL
  ctxt feed sync --url https://example.com/feed.xml

  # Remove a feed
  ctxt feed delete feed_12345678`,
}

var feedAddCmd = &cobra.Command{
	Use:   "add <url>",
	Short: "Subscribe to a feed",
	Long:  `Add a new RSS or Atom feed subscription.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runFeedAdd,
}

var feedListCmd = &cobra.Command{
	Use:   "list",
	Short: "List feed subscriptions",
	Long:  `List all RSS/Atom feed subscriptions with their current status.`,
	RunE:  runFeedList,
}

var feedSyncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Trigger a feed sync",
	Long:  `Trigger an immediate sync of a feed subscription.`,
	RunE:  runFeedSync,
}

var feedRemoveCmd = &cobra.Command{
	Use:   "delete <url-or-id>",
	Short: "Remove a feed subscription",
	Long:  `Remove an RSS/Atom feed subscription by URL or ID.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runFeedRemove,
}

func init() {
	rootCmd.AddCommand(feedCmd)

	feedCmd.AddCommand(feedAddCmd)
	feedCmd.AddCommand(feedListCmd)
	feedCmd.AddCommand(feedSyncCmd)
	feedCmd.AddCommand(feedRemoveCmd)

	cliconv.WithSideEffect(feedAddCmd, cliconv.SideEffectWrite)
	cliconv.WithExamples(feedAddCmd, []cliconv.Example{
		{Title: "Subscribe to a feed", Command: "ctxt feed add https://example.com/feed.xml"},
		{Title: "Use a specific dpkms server", Command: "ctxt feed add https://example.com/feed.xml --server https://dpkms.example.net"},
	})
	cliconv.WithNextSteps(feedAddCmd, []cliconv.NextStep{
		{When: "on success", Suggest: "ctxt feed sync --id <feed-id>", Reason: "trigger an immediate first sync"},
		{When: "after a few minutes", Suggest: "ctxt feed list", Reason: "confirm the new feed reached active status"},
	})
	cliconv.WithSideEffect(feedListCmd, cliconv.SideEffectRead)
	cliconv.WithExamples(feedListCmd, []cliconv.Example{
		{Title: "List feed subscriptions", Command: "ctxt feed list"},
		{Title: "Filter by status", Command: "ctxt feed list --status active"},
	})
	cliconv.WithSideEffect(feedSyncCmd, cliconv.SideEffectWrite)
	cliconv.WithExamples(feedSyncCmd, []cliconv.Example{
		{Title: "Sync by feed ID", Command: "ctxt feed sync --id feed_12345678"},
		{Title: "Sync by feed URL", Command: "ctxt feed sync --url https://example.com/feed.xml"},
	})
	cliconv.WithNextSteps(feedSyncCmd, []cliconv.NextStep{
		{When: "on success", Suggest: "ctxt job status <sync-job-id>", Reason: "follow the spawned sync job to completion"},
	})
	cliconv.WithSideEffect(feedRemoveCmd, cliconv.SideEffectDestructive)
	// 12fcc strict-gate: feed delete drops the subscription record on
	// the dpkms server; opt into kit's typed-token confirmation flow.
	cliconv.WithDestructiveToken(feedRemoveCmd)
	cliconv.WithExamples(feedRemoveCmd, []cliconv.Example{
		{Title: "Remove a feed by ID", Command: "ctxt feed delete feed_12345678 --confirm=yes"},
		{Title: "Remove a feed by URL", Command: "ctxt feed delete https://example.com/feed.xml --confirm=yes"},
	})
	cliconv.WithNextSteps(feedRemoveCmd, []cliconv.NextStep{
		{When: "after delete", Suggest: "ctxt feed list", Reason: "verify the subscription no longer appears"},
	})

	// feed add flags
	feedAddCmd.Flags().String("server", "", serverFlagUsage)

	// feed list flags
	// NOTE: --output is owned by kit's persistent global flag set; the
	// inherited flag is resolved through cmd.Flags() at read time, so
	// no local re-registration is required (12fcc local-global rule).
	feedListCmd.Flags().String("server", "", serverFlagUsage)
	feedListCmd.Flags().String("status", "", "filter by status (active|paused|error)")

	// feed sync flags
	feedSyncCmd.Flags().String("server", "", serverFlagUsage)
	feedSyncCmd.Flags().String("url", "", "feed URL to sync")
	feedSyncCmd.Flags().String("id", "", "feed ID to sync")

	// feed delete flags
	feedRemoveCmd.Flags().String("server", "", serverFlagUsage)
}

// feedsPath is the dpkms feed subscription collection.
const feedsPath = "/api/v1/feeds"

// feedResponse represents a single feed returned by the API.
type feedResponse struct {
	ID       string `json:"id"`
	URL      string `json:"url"`
	Status   string `json:"status"`
	LastSync string `json:"last_sync"`
}

func runFeedAdd(cmd *cobra.Command, args []string) error {
	feedURL := args[0]
	client, err := newDpkmsClient(cmd, 0)
	if err != nil {
		return err
	}

	var result map[string]any
	if err := client.Post(cmd.Context(), feedsPath, map[string]string{"url": feedURL}, &result); err != nil {
		return fmt.Errorf("add feed: %w", err)
	}

	fmt.Printf("Feed ID: %v\n", result["id"])
	return nil
}

func runFeedList(cmd *cobra.Command, args []string) error {
	client, err := newDpkmsClient(cmd, 0)
	if err != nil {
		return err
	}
	statusFilter, _ := cmd.Flags().GetString("status")

	feeds, err := listFeeds(cmd.Context(), client, statusFilter)
	if err != nil {
		return err
	}

	// Read --format from the cobra persistent flag directly (kit owns
	// the flag at the root). isJSONOutput() reads from viper, which
	// callers may have reset between command invocations, but the
	// cobra flag value reflects the parsed command line either way.
	formatFlag, _ := cmd.Flags().GetString("format")
	if formatFlag == "json" || isJSONOutput() {
		return outputJSON(cmd.OutOrStdout(), map[string]any{"feeds": feeds})
	}

	headers := []string{"ID", "URL", "STATUS", "LAST SYNC"}
	var rows [][]string
	for _, f := range feeds {
		rows = append(rows, []string{
			f.ID,
			f.URL,
			statusStyle(f.Status),
			f.LastSync,
		})
	}
	printTable(cmd.OutOrStdout(), headers, rows)
	return nil
}

func runFeedSync(cmd *cobra.Command, args []string) error {
	client, err := newDpkmsClient(cmd, 0)
	if err != nil {
		return err
	}
	feedURL, _ := cmd.Flags().GetString("url")
	feedID, _ := cmd.Flags().GetString("id")

	if feedID == "" && feedURL == "" {
		return fmt.Errorf("either --id or --url is required")
	}

	// If URL is given but not ID, resolve the ID first.
	if feedID == "" {
		id, err := resolveFeedID(cmd.Context(), client, feedURL)
		if err != nil {
			return err
		}
		feedID = id
	}

	var result map[string]any
	if err := client.Post(cmd.Context(), feedsPath+"/"+url.PathEscape(feedID)+"/sync", nil, &result); err != nil {
		return fmt.Errorf("sync feed %s: %w", feedID, err)
	}
	if jobID, _ := result["job_id"].(string); jobID != "" {
		fmt.Printf("Sync job: %s\n", jobID)
		return nil
	}

	fmt.Printf("Feed %s sync triggered\n", feedID)
	return nil
}

func runFeedRemove(cmd *cobra.Command, args []string) error {
	target := args[0]
	client, err := newDpkmsClient(cmd, 0)
	if err != nil {
		return err
	}

	feedID := target
	// If the arg looks like a URL, resolve the ID.
	if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
		id, err := resolveFeedID(cmd.Context(), client, target)
		if err != nil {
			return err
		}
		feedID = id
	}

	req := dpkmsclient.Request{Method: gohttp.MethodDelete, Path: feedsPath + "/" + url.PathEscape(feedID)}
	if err := client.Do(cmd.Context(), req, nil); err != nil {
		return fmt.Errorf("remove feed %s: %w", feedID, err)
	}

	fmt.Printf("Feed %s removed\n", feedID)
	return nil
}

// listFeeds fetches the feed subscriptions, optionally filtered by
// status. The list arrives bare or wrapped as {"feeds": [...]}.
func listFeeds(ctx context.Context, client *dpkmsclient.Client, status string) ([]feedResponse, error) {
	var q url.Values
	if status != "" {
		q = url.Values{"status": {status}}
	}
	var raw json.RawMessage
	if err := client.Get(ctx, feedsPath, q, &raw); err != nil {
		return nil, fmt.Errorf("list feeds: %w", err)
	}
	var feeds []feedResponse
	if err := json.Unmarshal(raw, &feeds); err != nil {
		var wrapper struct {
			Feeds []feedResponse `json:"feeds"`
		}
		if err2 := json.Unmarshal(raw, &wrapper); err2 != nil {
			return nil, fmt.Errorf("parse feed list: %w", err)
		}
		feeds = wrapper.Feeds
	}
	return feeds, nil
}

// resolveFeedID looks up a feed ID by URL from the server's feed list.
func resolveFeedID(ctx context.Context, client *dpkmsclient.Client, feedURL string) (string, error) {
	feeds, err := listFeeds(ctx, client, "")
	if err != nil {
		return "", err
	}
	for _, f := range feeds {
		if f.URL == feedURL {
			return f.ID, nil
		}
	}
	return "", fmt.Errorf("no feed found with URL %s", feedURL)
}
