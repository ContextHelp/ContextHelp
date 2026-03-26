package integration

// US-0300: Importer interface contract.
//
// Verifies that importer packages follow a consistent API shape:
//   - typed output records with ExternalID / Source / renderable content
//   - source attribution preserved end-to-end through analyze + store pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	gohttp "net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/importer/bookmarks"
	"github.com/ideacrafterslabs/ctxt/internal/importer/slack"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// TestUS0300_BookmarkImporterContract verifies bookmarks importer returns
// typed records with URL (externalID proxy) and renderable content.
func TestUS0300_BookmarkImporterContract(t *testing.T) {
	t.Parallel()

	html := []byte(`<!DOCTYPE NETSCAPE-Bookmark-file-1>
<TITLE>Bookmarks</TITLE><H1>Bookmarks</H1>
<DL><p>
<DT><A HREF="https://example.com" ADD_DATE="1700000001">Example Site</A>
<DT><A HREF="https://go.dev" ADD_DATE="1700000002">Go Lang</A>
</DL>`)

	bmarks, err := bookmarks.ParseBookmarks(html)
	require.NoError(t, err)
	require.Len(t, bmarks, 2, "must parse 2 bookmarks from fixture")

	for _, b := range bmarks {
		assert.NotEmpty(t, b.URL, "URL (externalID) must be set")
		assert.NotEmpty(t, b.Title, "Title must be set")
	}
	assert.Equal(t, "https://example.com", bmarks[0].URL)
	assert.Equal(t, "Example Site", bmarks[0].Title)
}

// TestUS0300_SlackImporterContract verifies Slack importer returns
// typed records with ExternalID, Source attribution, and renderable content.
func TestUS0300_SlackImporterContract(t *testing.T) {
	t.Parallel()

	raw := []byte(`[
	  {"type":"message","user":"U001","username":"alice",
	   "text":"Hello world","ts":"1736946000.000001"},
	  {"type":"message","user":"U002","username":"bob",
	   "text":"Hi alice","ts":"1736946060.000002"}
	]`)

	msgs, err := slack.ParseMessagesJSON(raw, "general")
	require.NoError(t, err)
	require.Len(t, msgs, 2, "must parse 2 messages")

	for _, m := range msgs {
		assert.NotEmpty(t, m.ExternalID, "ExternalID must be set")
		assert.Equal(t, "slack:general", m.Source, "Source must be 'slack:<channel>'")
		rendered := slack.RenderContent(m)
		assert.NotEmpty(t, rendered, "RenderContent must produce non-empty string")
		assert.Contains(t, rendered, m.Text, "rendered content must include message text")
	}
}

// TestUS0300_SourceAttributionEndToEnd verifies objects stored via the service
// preserve the importer-supplied source field.
func TestUS0300_SourceAttributionEndToEnd(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	const importerSource = "chrome:bookmarks"
	ctx := context.Background()

	jobID, err := env.svc.Analyze(ctx, service.AnalyzeRequest{
		Content: "Bookmark: https://go.dev/doc — Go Documentation",
		Type:    "bookmark",
		Source:  importerSource,
	})
	require.NoError(t, err)

	job := waitForJob(t, env.URL, jobID, storage.JobCompleted)
	require.NotEmpty(t, job.ResultID)

	resp, err := gohttp.Get(fmt.Sprintf("%s/api/v1/objects/%s", env.URL, job.ResultID))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, gohttp.StatusOK, resp.StatusCode)

	var obj storage.KnowledgeObject
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&obj))
	assert.Equal(t, importerSource, obj.Source, "source attribution must be preserved")
}
