package rederive

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/upgrade"
)

const (
	repoURL = "https://github.com/octocat/Hello-World"
	// A README that is itself a pip manifest, so the dependency enricher
	// produces dependencies from the repository text; the same names with
	// other versions; and prose that is not a manifest.
	manifestV1 = "requests>=2.0.0\nflask==3.0.0\n"
	manifestV2 = "requests>=2.1.0\nflask==3.1.0\n"
)

func longProse() string {
	return strings.Repeat("This project does one thing and documents it in prose. ", 20)
}

// repoState is what a re-derive of a repository must leave the same as a
// fresh ingest: the text, the pending list, the count, and the dependency
// artifact nodes (IDs with the object's own id taken out).
type repoState struct {
	Raw   string
	Items any
	Deps  any
	Nodes []string
}

func repoStateOf(o *storage.KnowledgeObject) repoState {
	st := repoState{Raw: o.RawContent, Items: o.Metadata["items_to_enqueue"], Deps: o.Metadata["deps_enqueued"]}
	if o.Graph != nil {
		for _, n := range o.Graph.Nodes {
			if n.NodeType == storage.NodeTypeArtifact {
				st.Nodes = append(st.Nodes, strings.ReplaceAll(n.ID, o.ID, "<id>")+"|"+n.Label)
			}
		}
	}
	slices.Sort(st.Nodes)
	return st
}

func jobCount(t *testing.T, e *realEnv) int {
	t.Helper()
	_, total, err := jobs.NewQueue(e.driver.Jobs()).List(context.Background(), storage.JobFilter{Limit: 100})
	require.NoError(t, err)
	return total
}

// The repository pipeline re-derives in network mode: the stored body
// becomes the new README through the mapper, and the dependencies, the
// artifact nodes and the pending list come out as a fresh ingest of that
// README writes them. Nothing is enqueued, and a second run is stable.
func TestReal_RepoReDerivesWithTheFetch(t *testing.T) {
	for name, tc := range map[string]struct{ first, second string }{
		"a manifest README":        {manifestV1, manifestV2},
		"a README that is not":     {longProse(), longProse() + "More prose.\n"},
		"manifest to non-manifest": {manifestV1, longProse()},
	} {
		t.Run(name, func(t *testing.T) {
			e := newRealEnv(t)
			e.gh.set(tc.first)
			obj := e.ingest("url.github.repo", repoURL, "")
			e.record()
			jobsBefore := jobCount(t, e)
			sig := e.bump("url.github.repo", "github_repo_mapper")
			e.gh.set(tc.second)
			hits := e.gh.hits.Load()

			res, err := e.run()
			require.NoError(t, err)
			require.Equal(t, 1, res.Processed, "%+v", res)
			require.Greater(t, e.gh.hits.Load(), hits, "network mode fetches the repository")
			require.Zero(t, e.hits.Load(), "no page fetch")
			require.Equal(t, sig, e.sigOf(obj.ID))
			require.Equal(t, jobsBefore, jobCount(t, e), "a re-derive enqueues nothing")

			// A fresh ingest of the new README, in a store of its own.
			fresh := newRealEnv(t)
			fresh.gh.set(tc.second)
			want := repoStateOf(fresh.get(fresh.ingest("url.github.repo", repoURL, "").ID))
			got := repoStateOf(e.get(obj.ID))
			require.Equal(t, want, got)
			require.Contains(t, got.Raw, strings.TrimSpace(strings.SplitN(tc.second, "\n", 2)[0][:10]))

			// A second reanalysis changes nothing.
			_, _, err = e.svc.ReanalyzeObjectWith(ctxSystem(), obj.ID, service.ReanalyzeOptions{AllowNetwork: true})
			require.NoError(t, err)
			require.Equal(t, got, repoStateOf(e.get(obj.ID)))
			require.Equal(t, jobsBefore, jobCount(t, e))
		})
	}
}

// A manifest README really produces something, so the comparison above
// is not vacuous.
func TestReal_RepoManifestREADMEProducesDependencies(t *testing.T) {
	e := newRealEnv(t)
	e.gh.set(manifestV1)
	st := repoStateOf(e.get(e.ingest("url.github.repo", repoURL, "").ID))
	require.EqualValues(t, 2, st.Deps)
	require.Len(t, st.Nodes, 2, "%+v", st)
}

// Without the fetch the repository is still refused: the mapper needs it.
func TestReal_RepoIsRefusedOnStoredContent(t *testing.T) {
	e := newRealEnv(t)
	e.gh.set(manifestV1)
	obj := e.ingest("url.github.repo", repoURL, "")
	before := e.snapshot(obj.ID)
	_, _, err := e.svc.ReanalyzeObject(ctxSystem(), obj.ID)
	require.ErrorIs(t, err, errCannot())
	require.Equal(t, before, e.snapshot(obj.ID))
}

// A README that is gone must not replace a long one.
func TestReal_RepoEmptyREADMEIsRejectedByTheGuard(t *testing.T) {
	e := newRealEnv(t)
	e.gh.set(longProse())
	obj := e.ingest("url.github.repo", repoURL, "")
	e.record()
	sig := e.bump("url.github.repo", "github_repo_mapper")
	before := e.snapshot(obj.ID)
	e.gh.set("")

	res, err := e.run()
	require.NoError(t, err)
	require.Zero(t, res.Processed)
	require.Equal(t, 1, res.Transient, "%+v", res)
	require.Empty(t, e.skips(sig), "a rejected refresh is a try until the run shows the API works")
	require.Equal(t, before, e.snapshot(obj.ID), "byte-identical")
}

// A rate limit says nothing about the repository: no record, still stale.
func TestReal_RepoRateLimitIsTransient(t *testing.T) {
	cases := map[string]struct {
		status int
		header http.Header
	}{
		"429":               {http.StatusTooManyRequests, http.Header{"Retry-After": {"3600"}}},
		"403 primary limit": {http.StatusForbidden, http.Header{"X-Ratelimit-Remaining": {"0"}, "X-Ratelimit-Reset": {"4102444800"}}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newRealEnv(t)
			e.gh.set(longProse())
			obj := e.ingest("url.github.repo", repoURL, "")
			e.record()
			sig := e.bump("url.github.repo", "github_repo_mapper")
			before := e.snapshot(obj.ID)
			e.gh.fail(c.status, c.header)

			res, err := e.run()
			require.NoError(t, err)
			require.Equal(t, 1, res.Transient, "%+v", res)
			require.Empty(t, e.skips(sig))
			require.Equal(t, before, e.snapshot(obj.ID))
			require.NotEqual(t, sig, e.sigOf(obj.ID))
		})
	}
}

func errCannot() error { return upgrade.ErrCannotReanalyze }
