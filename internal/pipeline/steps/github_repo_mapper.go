package steps

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/github"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Type and subtype of an object mapped from a GitHub repository, and the
// forge identity metadata keys that make it deduplicate by repository.
const (
	repoObjectType   = "repo"
	githubSubtype    = "github"
	MetaForge        = "forge"
	MetaForgeID      = "forge_id"
	githubTagSource  = "github"
	githubTextHeader = "# "
)

// GitHubRepoMapper maps the snapshot github_repo_fetcher stored onto the
// draft:
//
//   - Type "repo", Subtype "github"; Source becomes the canonical repository
//     URL, so every spelling of it names one object.
//   - RawContent: "# owner/name", description, topics, README. Text for
//     sectioning, tagging and embedding; the structured facts below are
//     metadata, not page text.
//   - Metadata: forge, forge_id (owner/name, GitHub's casing; the content
//     hash is taken over it, see storageutil.DraftContentHash), repo_url,
//     owner, name, description, language, topics, license, homepage, stars,
//     forks, open_issues, archived, is_fork, default_branch, created_at,
//     updated_at, pushed_at (RFC 3339), releases.
//   - Tags (source "github"): lang:<language>, each topic, "archived".
type GitHubRepoMapper struct {
	pipeline.BaseContract
}

// NewGitHubRepoMapper creates the step.
func NewGitHubRepoMapper() *GitHubRepoMapper {
	return &GitHubRepoMapper{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires: []string{"Metadata"},
			Produces: []string{"Type", "Subtype", "RawContent", "ContentType", "Source", "Metadata", "Tags"},
		}),
	}
}

func (s *GitHubRepoMapper) Name() string { return "github_repo_mapper" }

func (s *GitHubRepoMapper) Run(_ context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	snap, ok := draft.Metadata[MetaGitHubSnapshot].(*github.RepoSnapshot)
	if !ok || snap == nil {
		return nil, pipeline.Permanent(fmt.Errorf("github_repo_mapper: no %s in metadata (github_repo_fetcher must run first)", MetaGitHubSnapshot))
	}
	delete(draft.Metadata, MetaGitHubSnapshot)

	draft.Type = repoObjectType
	draft.Subtype = githubSubtype
	draft.ContentType = "text/markdown"
	draft.Source = snap.URL
	draft.RawContent = repoText(snap)

	m := draft.Metadata
	m[MetaForge] = githubSubtype
	m[MetaForgeID] = snap.FullName
	m["repo_url"] = snap.URL
	m["owner"] = snap.Owner
	m["name"] = snap.Name
	m["description"] = snap.Description
	m["language"] = snap.Language
	m["topics"] = append([]string(nil), snap.Topics...)
	m["license"] = snap.License
	m["homepage"] = snap.Homepage
	m["stars"] = snap.Stars
	m["forks"] = snap.Forks
	m["open_issues"] = snap.OpenIssues
	m["archived"] = snap.IsArchived
	m["is_fork"] = snap.IsFork
	m["default_branch"] = snap.DefaultBranch
	setTime(m, "created_at", snap.CreatedAt)
	setTime(m, "updated_at", snap.UpdatedAt)
	setTime(m, "pushed_at", snap.PushedAt)
	if len(snap.Releases) > 0 {
		rels := make([]map[string]any, len(snap.Releases))
		for i, r := range snap.Releases {
			rels[i] = map[string]any{"tag": r.Tag, "name": r.Name}
			setTime(rels[i], "published_at", r.PublishedAt)
		}
		m["releases"] = rels
	}

	draft.Tags = mergeTags(draft.Tags, repoTags(snap))
	return draft, nil
}

func setTime(m map[string]any, key string, t time.Time) {
	if !t.IsZero() {
		m[key] = t.UTC().Format(time.RFC3339)
	}
}

func repoText(s *github.RepoSnapshot) string {
	var b strings.Builder
	b.WriteString(githubTextHeader + s.FullName + "\n")
	if s.Description != "" {
		b.WriteString("\n" + s.Description + "\n")
	}
	if len(s.Topics) > 0 {
		b.WriteString("\nTopics: " + strings.Join(s.Topics, ", ") + "\n")
	}
	if s.README != "" {
		b.WriteString("\n" + s.README + "\n")
	}
	return b.String()
}

func repoTags(s *github.RepoSnapshot) []storage.Tag {
	var tags []storage.Tag
	add := func(label string) {
		tags = append(tags, storage.Tag{Label: label, Weight: 1, Source: githubTagSource})
	}
	if s.Language != "" {
		add("lang:" + strings.ToLower(s.Language))
	}
	for _, t := range s.Topics {
		add(strings.ToLower(t))
	}
	if s.IsArchived {
		add("archived")
	}
	return tags
}

// mergeTags appends add to have, skipping labels already present
// (case-insensitive).
func mergeTags(have, add []storage.Tag) []storage.Tag {
	seen := make(map[string]struct{}, len(have)+len(add))
	for _, t := range have {
		seen[strings.ToLower(t.Label)] = struct{}{}
	}
	for _, t := range add {
		k := strings.ToLower(t.Label)
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		have = append(have, t)
	}
	return have
}
