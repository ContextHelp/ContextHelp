package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	gohttp "net/http"
	"strings"

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
  ctxt feed remove feed_12345678`,
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
	Use:   "remove <url-or-id>",
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

	// feed add flags
	feedAddCmd.Flags().String("server", "", "dpkms server URL (default http://localhost:8080)")

	// feed list flags
	feedListCmd.Flags().String("server", "", "dpkms server URL (default http://localhost:8080)")
	feedListCmd.Flags().String("output", "", "output format (text|json)")
	feedListCmd.Flags().String("status", "", "filter by status (active|paused|error)")

	// feed sync flags
	feedSyncCmd.Flags().String("server", "", "dpkms server URL (default http://localhost:8080)")
	feedSyncCmd.Flags().String("url", "", "feed URL to sync")
	feedSyncCmd.Flags().String("id", "", "feed ID to sync")

	// feed remove flags
	feedRemoveCmd.Flags().String("server", "", "dpkms server URL (default http://localhost:8080)")
}

// feedServerURL returns the server URL from the command flag or the default.
func feedServerURL(cmd *cobra.Command) string {
	url, _ := cmd.Flags().GetString("server")
	if url == "" {
		url = "http://localhost:8080"
	}
	return strings.TrimRight(url, "/")
}

// feedResponse represents a single feed returned by the API.
type feedResponse struct {
	ID       string `json:"id"`
	URL      string `json:"url"`
	Status   string `json:"status"`
	LastSync string `json:"last_sync"`
}

func runFeedAdd(cmd *cobra.Command, args []string) error {
	feedURL := args[0]
	serverURL := feedServerURL(cmd)

	payload := map[string]string{"url": feedURL}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	resp, err := gohttp.Post(serverURL+"/api/v1/feeds", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != gohttp.StatusCreated && resp.StatusCode != gohttp.StatusOK {
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var result map[string]string
	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}

	fmt.Printf("Feed ID: %s\n", result["id"])
	return nil
}

func runFeedList(cmd *cobra.Command, args []string) error {
	serverURL := feedServerURL(cmd)
	outputFmt, _ := cmd.Flags().GetString("output")
	statusFilter, _ := cmd.Flags().GetString("status")

	reqURL := serverURL + "/api/v1/feeds"
	if statusFilter != "" {
		reqURL += "?status=" + statusFilter
	}

	resp, err := gohttp.Get(reqURL)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != gohttp.StatusOK {
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var feeds []feedResponse
	if err := json.Unmarshal(respBody, &feeds); err != nil {
		// Try unwrapping {"feeds": [...]}
		var wrapper struct {
			Feeds []feedResponse `json:"feeds"`
		}
		if err2 := json.Unmarshal(respBody, &wrapper); err2 != nil {
			return fmt.Errorf("parse response: %w", err)
		}
		feeds = wrapper.Feeds
	}

	if outputFmt == "json" || isJSONOutput() {
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
	serverURL := feedServerURL(cmd)
	feedURL, _ := cmd.Flags().GetString("url")
	feedID, _ := cmd.Flags().GetString("id")

	if feedID == "" && feedURL == "" {
		return fmt.Errorf("either --id or --url is required")
	}

	// If URL is given but not ID, resolve the ID first.
	if feedID == "" {
		id, err := resolveFeedID(serverURL, feedURL)
		if err != nil {
			return err
		}
		feedID = id
	}

	resp, err := gohttp.Post(serverURL+"/api/v1/feeds/"+feedID+"/sync", "application/json", gohttp.NoBody)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != gohttp.StatusAccepted && resp.StatusCode != gohttp.StatusOK {
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var result map[string]string
	if err := json.Unmarshal(respBody, &result); err == nil {
		if jobID := result["job_id"]; jobID != "" {
			fmt.Printf("Sync job: %s\n", jobID)
			return nil
		}
	}

	fmt.Printf("Feed %s sync triggered\n", feedID)
	return nil
}

func runFeedRemove(cmd *cobra.Command, args []string) error {
	target := args[0]
	serverURL := feedServerURL(cmd)

	feedID := target
	// If the arg looks like a URL, resolve the ID.
	if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
		id, err := resolveFeedID(serverURL, target)
		if err != nil {
			return err
		}
		feedID = id
	}

	req, err := gohttp.NewRequest(gohttp.MethodDelete, serverURL+"/api/v1/feeds/"+feedID, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	client := &gohttp.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != gohttp.StatusOK && resp.StatusCode != gohttp.StatusNoContent {
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	fmt.Printf("Feed %s removed\n", feedID)
	return nil
}

// resolveFeedID looks up a feed ID by URL from the server's feed list.
func resolveFeedID(serverURL, feedURL string) (string, error) {
	resp, err := gohttp.Get(serverURL + "/api/v1/feeds")
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != gohttp.StatusOK {
		return "", fmt.Errorf("server returned %d", resp.StatusCode)
	}

	var feeds []feedResponse
	if err := json.Unmarshal(respBody, &feeds); err != nil {
		var wrapper struct {
			Feeds []feedResponse `json:"feeds"`
		}
		if err2 := json.Unmarshal(respBody, &wrapper); err2 != nil {
			return "", fmt.Errorf("parse response: %w", err)
		}
		feeds = wrapper.Feeds
	}

	for _, f := range feeds {
		if f.URL == feedURL {
			return f.ID, nil
		}
	}

	return "", fmt.Errorf("no feed found with URL %s", feedURL)
}
