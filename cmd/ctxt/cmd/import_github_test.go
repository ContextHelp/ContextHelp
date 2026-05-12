package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"

	githubimporter "github.com/ideacrafterslabs/ctxt/internal/importer/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubGitHubFetcher returns a fixed set of repos.
type stubGitHubFetcher struct {
	repos []githubimporter.ImportedRepo
	err   error
}

func (s *stubGitHubFetcher) Fetch(_ context.Context, _ githubimporter.FetchOptions) ([]githubimporter.ImportedRepo, error) {
	return s.repos, s.err
}

func (s *stubGitHubFetcher) Enrich(_ context.Context, _ []githubimporter.ImportedRepo) {}

func sampleRepos() []githubimporter.ImportedRepo {
	return []githubimporter.ImportedRepo{
		{
			Source:      githubimporter.ListStarred,
			FullName:    "torvalds/linux",
			Description: "Linux kernel source tree",
			Language:    "C",
			Stars:       180000,
			URL:         "https://github.com/torvalds/linux",
		},
		{
			Source:      githubimporter.ListStarred,
			FullName:    "golang/go",
			Description: "The Go programming language",
			Language:    "Go",
			Stars:       120000,
			URL:         "https://github.com/golang/go",
		},
	}
}

func TestParseGitHubLists(t *testing.T) {
	tests := []struct {
		raw     string
		want    []githubimporter.ListType
		wantErr string
	}{
		{"starred", []githubimporter.ListType{githubimporter.ListStarred}, ""},
		{"watched", []githubimporter.ListType{githubimporter.ListWatched}, ""},
		{"contributed", []githubimporter.ListType{githubimporter.ListContributed}, ""},
		{"starred,watched", []githubimporter.ListType{githubimporter.ListStarred, githubimporter.ListWatched}, ""},
		{"starred,starred", []githubimporter.ListType{githubimporter.ListStarred}, ""}, // dedup
		{"STARRED", []githubimporter.ListType{githubimporter.ListStarred}, ""},         // case-insensitive
		{"", nil, "--lists must not be empty"},
		{"bogus", nil, "unknown list type"},
	}
	for _, tc := range tests {
		t.Run(tc.raw, func(t *testing.T) {
			got, err := parseGitHubLists(tc.raw)
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestTruncateString(t *testing.T) {
	assert.Equal(t, "hello", truncateString("hello", 10))
	assert.Equal(t, "hel…", truncateString("hello", 4))
	assert.Equal(t, "hello", truncateString("hello", 5))
}

func TestRenderGitHubOutputTable(t *testing.T) {
	var buf bytes.Buffer
	err := renderGitHubOutput(&buf, "table", sampleRepos())
	require.NoError(t, err)
	out := buf.String()
	assert.Contains(t, out, "torvalds/linux")
	assert.Contains(t, out, "golang/go")
}

func TestRenderGitHubOutputJSON(t *testing.T) {
	var buf bytes.Buffer
	err := renderGitHubOutput(&buf, "json", sampleRepos())
	require.NoError(t, err)
	out := buf.String()
	assert.Contains(t, out, `"full_name"`)
	assert.Contains(t, out, "torvalds/linux")
}

func TestRunImportGitHubDryRun(t *testing.T) {
	stub := &stubGitHubFetcher{repos: sampleRepos()}
	origClient := newGitHubClient
	newGitHubClient = func(token, baseURL string) githubFetcher { return stub }
	defer func() { newGitHubClient = origClient }()

	cmd := importGitHubCmd
	cmd.ResetFlags()
	// re-register flags
	importGitHubCmd.Flags().StringP("token", "t", "", "")
	importGitHubCmd.Flags().StringP("username", "u", "", "")
	importGitHubCmd.Flags().StringP("lists", "l", "starred", "")
	importGitHubCmd.Flags().Bool("dry-run", false, "")
	importGitHubCmd.Flags().String("format", "table", "")
	importGitHubCmd.Flags().String("server", "", "")
	importGitHubCmd.Flags().String("pipeline", "url.github.repo", "")
	importGitHubCmd.Flags().String("github-base-url", "", "")

	err := cmd.ParseFlags([]string{"--username", "octocat", "--dry-run"})
	require.NoError(t, err)

	err = runImportGitHub(cmd, nil)
	require.NoError(t, err)
}

func TestRunImportGitHubMissingUsername(t *testing.T) {
	cmd := importGitHubCmd
	cmd.ResetFlags()
	importGitHubCmd.Flags().StringP("token", "t", "", "")
	importGitHubCmd.Flags().StringP("username", "u", "", "")
	importGitHubCmd.Flags().StringP("lists", "l", "starred", "")
	importGitHubCmd.Flags().Bool("dry-run", false, "")
	importGitHubCmd.Flags().String("format", "table", "")
	importGitHubCmd.Flags().String("server", "", "")
	importGitHubCmd.Flags().String("pipeline", "url.github.repo", "")
	importGitHubCmd.Flags().String("github-base-url", "", "")

	err := runImportGitHub(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--username is required")
}

func TestRunImportGitHubEnqueue(t *testing.T) {
	stub := &stubGitHubFetcher{repos: sampleRepos()}
	origClient := newGitHubClient
	newGitHubClient = func(token, baseURL string) githubFetcher { return stub }
	defer func() { newGitHubClient = origClient }()

	var enqueuedSources []string
	origEnqueue := enqueueGitHubItem
	enqueueGitHubItem = func(serverURL, content, contentType, pipelineName, source string) (string, error) {
		enqueuedSources = append(enqueuedSources, source)
		return "job-id", nil
	}
	defer func() { enqueueGitHubItem = origEnqueue }()

	cmd := importGitHubCmd
	cmd.ResetFlags()
	importGitHubCmd.Flags().StringP("token", "t", "", "")
	importGitHubCmd.Flags().StringP("username", "u", "", "")
	importGitHubCmd.Flags().StringP("lists", "l", "starred", "")
	importGitHubCmd.Flags().Bool("dry-run", false, "")
	importGitHubCmd.Flags().String("format", "table", "")
	importGitHubCmd.Flags().String("server", "", "")
	importGitHubCmd.Flags().String("pipeline", "url.github.repo", "")
	importGitHubCmd.Flags().String("github-base-url", "", "")

	err := cmd.ParseFlags([]string{"--username", "octocat"})
	require.NoError(t, err)

	err = runImportGitHub(cmd, nil)
	require.NoError(t, err)
	assert.Len(t, enqueuedSources, 2)
	assert.True(t, strings.HasPrefix(enqueuedSources[0], "https://github.com/"))
}
