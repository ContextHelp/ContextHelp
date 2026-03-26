package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	githubimporter "github.com/ideacrafterslabs/ctxt/internal/importer/github"
	"github.com/spf13/cobra"
)

const githubTokenEnv = "GITHUB_TOKEN"

// githubFetcher is the interface the command depends on; swap for testing.
type githubFetcher interface {
	Fetch(ctx context.Context, opts githubimporter.FetchOptions) ([]githubimporter.ImportedRepo, error)
}

// newGitHubClient is overridable in tests.
var newGitHubClient = func(token, baseURL string) githubFetcher {
	return githubimporter.NewClient(nil, baseURL, token)
}

// enqueueGitHubItem delegates to the shared helper; overridable in tests.
var enqueueGitHubItem = enqueueImportItem

var importGitHubCmd = &cobra.Command{
	Use:   "github",
	Short: "Import GitHub repositories",
	Long: `Import GitHub repositories a user has starred, is watching, or has contributed to.

Authentication:
  Provide a GitHub personal access token via --token or GITHUB_TOKEN.
  Public starred/watched lists work without a token, but rate limits are tight
  and private data is inaccessible.

Lists:
  --lists starred      repositories the user has starred (default)
  --lists watched      repositories the user is watching
  --lists contributed  repositories the user has contributed to (search-based)
  Multiple lists: --lists starred,watched,contributed

Examples:
  # Dry-run starred repos (default)
  ctxt import github --username octocat --dry-run

  # Import starred + watched, enqueue into dpkms
  ctxt import github --username octocat --token $GITHUB_TOKEN --lists starred,watched

  # JSON output
  ctxt import github --username octocat --dry-run --output json`,
	RunE: runImportGitHub,
}

func init() {
	importCmd.AddCommand(importGitHubCmd)

	importGitHubCmd.Flags().StringP("token", "t", "", "GitHub token (or GITHUB_TOKEN env)")
	importGitHubCmd.Flags().StringP("username", "u", "", "GitHub username (required)")
	importGitHubCmd.Flags().StringP("lists", "l", "starred", "comma-separated lists: starred,watched,contributed")
	importGitHubCmd.Flags().Bool("dry-run", false, "print what would be imported without enqueueing jobs")
	importGitHubCmd.Flags().String("output", "table", "output format: table or json")
	importGitHubCmd.Flags().String("server", "", "dpkms server URL (default http://localhost:8080)")
	importGitHubCmd.Flags().String("pipeline", "import.github", "pipeline override for enqueued jobs")

	// hidden for test/dev overrides
	importGitHubCmd.Flags().String("github-base-url", "", "override GitHub API base URL")
	_ = importGitHubCmd.Flags().MarkHidden("github-base-url")
}

func runImportGitHub(cmd *cobra.Command, args []string) error {
	token, _ := cmd.Flags().GetString("token")
	if strings.TrimSpace(token) == "" {
		token = strings.TrimSpace(os.Getenv(githubTokenEnv))
	}

	username, _ := cmd.Flags().GetString("username")
	username = strings.TrimSpace(username)
	if username == "" {
		return fmt.Errorf("--username is required")
	}

	listsRaw, _ := cmd.Flags().GetString("lists")
	lists, err := parseGitHubLists(listsRaw)
	if err != nil {
		return err
	}

	dryRun, _ := cmd.Flags().GetBool("dry-run")
	outputFmt, _ := cmd.Flags().GetString("output")
	serverURL, _ := cmd.Flags().GetString("server")
	pipelineName, _ := cmd.Flags().GetString("pipeline")
	baseURL, _ := cmd.Flags().GetString("github-base-url")

	if serverURL == "" {
		serverURL = "http://localhost:8080"
	}
	serverURL = strings.TrimRight(serverURL, "/")

	client := newGitHubClient(token, baseURL)
	repos, err := client.Fetch(context.Background(), githubimporter.FetchOptions{
		Username: username,
		Lists:    lists,
	})
	if err != nil {
		return fmt.Errorf("github fetch: %w", err)
	}

	if len(repos) == 0 {
		fmt.Fprintln(os.Stderr, "no repositories found")
		return nil
	}

	if dryRun {
		return renderGitHubOutput(os.Stdout, outputFmt, repos)
	}

	// Enqueue.
	var imported, failed int
	var firstErr error
	for _, r := range repos {
		payload := githubimporter.RenderContent(r)
		_, err := enqueueGitHubItem(serverURL, payload, "text", pipelineName, r.URL)
		if err != nil {
			failed++
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		imported++
	}

	fmt.Fprintf(os.Stdout, "Fetched: %d\nImported: %d\n", len(repos), imported)
	if failed > 0 {
		fmt.Fprintf(os.Stdout, "Failed: %d\n", failed)
	}
	if firstErr != nil {
		return fmt.Errorf("import completed with errors (first: %w)", firstErr)
	}
	return nil
}

// renderGitHubOutput renders repos to w in the requested format.
func renderGitHubOutput(w io.Writer, format string, repos []githubimporter.ImportedRepo) error {
	if format == "json" {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(repos)
	}

	// Table output.
	headers := []string{"Source", "FullName", "Stars", "Language", "Description"}
	rows := make([][]string, 0, len(repos))
	for _, r := range repos {
		desc := truncateString(r.Description, 60)
		lang := r.Language
		if lang == "" {
			lang = "-"
		}
		rows = append(rows, []string{
			string(r.Source),
			r.FullName,
			fmt.Sprintf("%d", r.Stars),
			lang,
			desc,
		})
	}
	printTable(w, headers, rows)
	return nil
}

// parseGitHubLists validates and parses the --lists flag value.
func parseGitHubLists(raw string) ([]githubimporter.ListType, error) {
	valid := map[string]githubimporter.ListType{
		"starred":     githubimporter.ListStarred,
		"watched":     githubimporter.ListWatched,
		"contributed": githubimporter.ListContributed,
	}

	parts := strings.Split(raw, ",")
	seen := map[githubimporter.ListType]struct{}{}
	out := make([]githubimporter.ListType, 0, len(parts))

	for _, p := range parts {
		p = strings.TrimSpace(strings.ToLower(p))
		if p == "" {
			continue
		}
		lt, ok := valid[p]
		if !ok {
			return nil, fmt.Errorf("unknown list type %q; valid: starred, watched, contributed", p)
		}
		if _, dup := seen[lt]; dup {
			continue
		}
		seen[lt] = struct{}{}
		out = append(out, lt)
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("--lists must not be empty")
	}
	return out, nil
}

// truncateString clips s at maxRunes runes, appending "…" if truncated.
func truncateString(s string, maxRunes int) string {
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxRunes-1]) + "…"
}
