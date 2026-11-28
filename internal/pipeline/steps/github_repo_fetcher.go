package steps

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/ideacrafterslabs/ctxt/internal/github"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// MetaGitHubSnapshot is the draft.Metadata key that carries the fetched
// *github.RepoSnapshot from github_repo_fetcher to github_repo_mapper. The
// mapper removes it: only the mapped fields persist.
const MetaGitHubSnapshot = "github_snapshot"

// defaultREADMEBytes caps the README text kept per repository.
const defaultREADMEBytes = 64 << 10

// GitHubRepoFetcher resolves a github.com repository URL to its API
// snapshot (description, topics, language, stars, license, pushed_at,
// archived, README) through the extraction core.
//
// Reads draft.Source, else draft.RawContent, as the repository URL.
// Writes draft.Metadata[MetaGitHubSnapshot].
//
// Errors: a repository that is not found, forbidden or unauthorized is
// permanent (no job retry); rate limits and server errors are transient.
type GitHubRepoFetcher struct {
	pipeline.BaseContract
	client      *github.Client
	readmeBytes int
}

// GitHubRepoFetcherOption configures a GitHubRepoFetcher.
type GitHubRepoFetcherOption func(*GitHubRepoFetcher)

// WithGitHubREADMEBytes sets the README byte cap; 0 skips the README call.
func WithGitHubREADMEBytes(n int) GitHubRepoFetcherOption {
	return func(f *GitHubRepoFetcher) { f.readmeBytes = n }
}

// NewGitHubRepoFetcher creates the step over client.
func NewGitHubRepoFetcher(client *github.Client, opts ...GitHubRepoFetcherOption) *GitHubRepoFetcher {
	f := &GitHubRepoFetcher{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires:  []string{"Source"},
			Produces:  []string{"Metadata"},
			Resources: []string{pipeline.ResourceNetwork},
		}),
		client:      client,
		readmeBytes: defaultREADMEBytes,
	}
	for _, o := range opts {
		o(f)
	}
	return f
}

func (s *GitHubRepoFetcher) Name() string { return "github_repo_fetcher" }

func (s *GitHubRepoFetcher) Run(ctx context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	owner, name, ok := github.ParseRepoURL(draft.Source)
	if !ok {
		owner, name, ok = github.ParseRepoURL(draft.RawContent)
	}
	if !ok {
		return nil, pipeline.Permanent(fmt.Errorf("github_repo_fetcher: no github.com repository URL (source %q)", draft.Source))
	}

	var opts []github.FetchOption
	if s.readmeBytes > 0 {
		opts = append(opts, github.WithREADME(s.readmeBytes))
	}
	snap, err := s.client.FetchRepo(ctx, owner, name, opts...)
	if err != nil {
		slog.Debug("github_repo_fetcher: fetch failed", "repo", owner+"/"+name, "err", err)
		wrapped := error(&pipeline.FetchError{
			Host: s.client.Host(), Status: githubStatus(err), RateLimited: errors.Is(err, github.ErrRateLimited),
			Err: fmt.Errorf("github_repo_fetcher: %s/%s: %w", owner, name, err),
		})
		if github.IsPermanent(err) {
			return nil, pipeline.Permanent(wrapped)
		}
		return nil, wrapped
	}

	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	draft.Metadata[MetaGitHubSnapshot] = snap
	providers.ReportFetched(ctx, s.client.Host())
	return draft, nil
}

// githubStatus is the HTTP status of a failed API call, 0 when the API did
// not answer.
func githubStatus(err error) int {
	var api *github.APIError
	if errors.As(err, &api) {
		return api.Status
	}
	var limited *github.RateLimitError
	if errors.As(err, &limited) {
		return limited.Status
	}
	return 0
}
