package cmd

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/dpkmsclient"
	ghapi "github.com/ideacrafterslabs/ctxt/internal/github"
)

type stubStars struct {
	snap  *ghapi.StarSnapshot
	owner *string
}

func (s stubStars) FetchStars(_ context.Context, owner string) (*ghapi.StarSnapshot, error) {
	if s.owner != nil {
		*s.owner = owner
	}
	return s.snap, nil
}

func starSnap() *ghapi.StarSnapshot {
	gt := ghapi.StarList{Slug: "go-tools", Name: "Go tools", Description: "CLI things"}
	return &ghapi.StarSnapshot{
		Viewer: "octo",
		Lists:  []ghapi.StarList{gt},
		Repos: []ghapi.StarRepo{
			{FullName: "a/one", URL: "https://github.com/a/one", Starred: true, Lists: []ghapi.StarList{gt}},
			{FullName: "b/two", URL: "https://github.com/b/two", Starred: true},
		},
	}
}

// starHarness wires stubs and returns the requests submitted.
type starHarness struct {
	snap   *ghapi.StarSnapshot
	sent   []dpkmsclient.AnalyzeRequest
	failOn string
	owner  string
	state  string
}

func newStarHarness(t *testing.T) *starHarness {
	t.Helper()
	h := &starHarness{snap: starSnap(), state: filepath.Join(t.TempDir(), "state.json")}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("GITHUB_TOKEN", "")
	origF, origS, origP := newStarsFetcher, newStarSubmitter, starStatePath
	newStarsFetcher = func(_ *cobra.Command, _, _ string) starsFetcher { return stubStars{snap: h.snap, owner: &h.owner} }
	newStarSubmitter = func(*cobra.Command) (starSubmitter, error) {
		return func(_ context.Context, req dpkmsclient.AnalyzeRequest) (string, error) {
			if h.failOn != "" && req.SourceKey == h.failOn {
				return "", errors.New("boom")
			}
			h.sent = append(h.sent, req)
			return "job", nil
		}, nil
	}
	starStatePath = func(_, _, _ string) (string, error) { return h.state, nil }
	t.Cleanup(func() { newStarsFetcher, newStarSubmitter, starStatePath = origF, origS, origP })
	return h
}

func (h *starHarness) run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	c := &cobra.Command{Use: "github"}
	c.Flags().String("token", "", "")
	c.Flags().String("format", "", "")
	c.Flags().Bool("dry-run", false, "")
	c.Flags().String("github-base-url", "", "")
	addGitHubStarFlags(c)
	c.SetContext(context.Background())
	require.NoError(t, c.ParseFlags(append([]string{"--star-lists"}, args...)))
	r, w, _ := os.Pipe()
	orig := os.Stdout
	os.Stdout = w
	err := runImportGitHub(c, nil)
	w.Close()
	os.Stdout = orig
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String(), err
}

func TestImportGitHubStars_SubmitsURLCapturesWithKeyHintsAndData(t *testing.T) {
	h := newStarHarness(t)
	out, err := h.run(t)
	require.NoError(t, err)
	require.Len(t, h.sent, 2)
	one := h.sent[0]
	assert.Equal(t, "https://github.com/a/one", one.Content)
	assert.Equal(t, "url", one.Type)
	assert.Equal(t, "github:a/one", one.SourceKey)
	assert.Equal(t, []string{"starred", "star-list:go-tools"}, one.Hints)
	assert.Equal(t, "octo", one.Data["account"])
	assert.NotEmpty(t, one.Data["star_lists"])
	assert.Contains(t, out, "Submitted: 2")
	assert.Contains(t, out, "Unchanged: 0")
}

func TestImportGitHubStars_RerunSubmitsOnlyChanged(t *testing.T) {
	h := newStarHarness(t)
	_, err := h.run(t)
	require.NoError(t, err)
	h.sent = nil

	out, err := h.run(t)
	require.NoError(t, err)
	assert.Empty(t, h.sent)
	assert.Contains(t, out, "Submitted: 0")
	assert.Contains(t, out, "Unchanged: 2")

	h.snap.Repos[1].Lists = []ghapi.StarList{h.snap.Lists[0]} // b/two joins a list
	out, err = h.run(t)
	require.NoError(t, err)
	require.Len(t, h.sent, 1)
	assert.Equal(t, "github:b/two", h.sent[0].SourceKey)
	assert.Contains(t, out, "Unchanged: 1")

	h.sent = nil
	_, err = h.run(t, "--full")
	require.NoError(t, err)
	assert.Len(t, h.sent, 2)
}

func TestImportGitHubStars_DryRunSubmitsNothingAndSavesNoState(t *testing.T) {
	h := newStarHarness(t)
	out, err := h.run(t, "--dry-run")
	require.NoError(t, err)
	assert.Empty(t, h.sent)
	assert.NoFileExists(t, h.state)
	assert.Contains(t, out, "Would submit: 2")
	assert.Contains(t, out, "github:a/one")
}

func TestImportGitHubStars_FailedItemRetriedNextRun(t *testing.T) {
	h := newStarHarness(t)
	h.failOn = "github:a/one"
	out, err := h.run(t)
	require.Error(t, err)
	assert.Contains(t, out, "Failed: 1")
	require.Len(t, h.sent, 1)

	h.failOn, h.sent = "", nil
	_, err = h.run(t)
	require.NoError(t, err)
	require.Len(t, h.sent, 1)
	assert.Equal(t, "github:a/one", h.sent[0].SourceKey)
}

func TestImportGitHubStars_JSONReport(t *testing.T) {
	h := newStarHarness(t)
	out, err := h.run(t, "--dry-run", "--format", "json")
	require.NoError(t, err)
	assert.Contains(t, out, `"dry_run": true`)
	assert.Contains(t, out, `"status": "new"`)
	_ = h
}

func TestResolveStarOwner(t *testing.T) {
	gh := config.GitHubConfig{Accounts: []config.GitHubAccount{
		{Name: "stars", Credential: "S", Owners: []string{"octo", "octo-org"}},
		{Name: "bare", Credential: "B"},
	}}
	for _, tc := range []struct{ in, want, err string }{
		{"", "", ""},
		{"stars", "octo", ""},
		{"STARS", "octo", ""},
		{"octo-org", "octo-org", ""},
		{"bare", "", "lists no owners"},
		{"nope", "", "unknown github account"},
	} {
		got, err := resolveStarOwner(gh, tc.in)
		if tc.err != "" {
			require.Error(t, err, tc.in)
			assert.True(t, strings.Contains(err.Error(), tc.err), err.Error())
			continue
		}
		require.NoError(t, err, tc.in)
		assert.Equal(t, tc.want, got, tc.in)
	}
}
