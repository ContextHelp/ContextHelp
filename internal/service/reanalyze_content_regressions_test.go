package service

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// From stored content a url object's cleaned text is not cleaned again:
// escapes, generics and comparisons in prose stay as they are, and the
// page is not fetched. With the network allowed the whole chain runs.
func TestReanalyzeRegressions_CleanedTextIsNotCleanedAgain(t *testing.T) {
	e := newRealEnv(t)
	e.page.Store(`<html><body><nav>menus</nav><p>Write &lt;script&gt;alert(1)&lt;/script&gt; and Vec&lt;String&gt;, a&lt;b. ` +
		`Escapes: &amp;lt;b&amp;gt; and AT&amp;amp;T</p><script>evil()</script></body></html>`)
	first := e.ingest(t, "url.generic", e.srv.URL+"/p", "")
	stored := e.get(t, first.ID)
	require.Contains(t, stored.RawContent, "Escapes: &lt;b&gt; and AT&amp;T", "fixture: first fetch cleaned the page")
	hits := e.hits.Load()

	_, _, err := e.svc.ReanalyzeObject(ownerCtx(), first.ID)
	require.NoError(t, err)
	got := e.get(t, first.ID)
	assert.Equal(t, stored.RawContent, got.RawContent, "cleaned text was cleaned again")
	assert.Equal(t, hits, e.hits.Load(), "the page was fetched")

	_, _, err = e.svc.ReanalyzeObjectWith(ownerCtx(), first.ID, ReanalyzeOptions{AllowNetwork: true})
	require.NoError(t, err)
	assert.Equal(t, hits+1, e.hits.Load(), "AllowNetwork fetches")
	assert.Equal(t, stored.RawContent, e.get(t, first.ID).RawContent, "same page, same cleaned text")
}

// After a step changes the content, text, sections and hash follow the
// new content: nothing derived from the old text is written back.
func TestReanalyzeRegressions_TextFollowsChangedContent(t *testing.T) {
	e := newRealEnv(t)
	e.page.Store("<html><body><h1>Old</h1><p>old body words that the first fetch saw, long enough to be sectioned.</p></body></html>")
	first := e.ingest(t, "url.generic", e.srv.URL+"/p", "")
	require.Contains(t, strings.ToLower(e.get(t, first.ID).RawContent), "old body words")

	e.page.Store("<html><body><h1>New</h1><p>new page words that the refresh sees, long enough to be sectioned.</p></body></html>")
	_, _, err := e.svc.ReanalyzeObjectWith(ownerCtx(), first.ID, ReanalyzeOptions{AllowNetwork: true})
	require.NoError(t, err)
	got := e.get(t, first.ID)

	assert.Contains(t, got.RawContent, "new page words")
	assert.NotContains(t, strings.ToLower(got.RawContent), "old body")
	assert.NotContains(t, strings.ToLower(got.TextContent), "old body", "stale text written back")
	assert.Contains(t, got.TextContent, "new page words")
	require.NotEmpty(t, got.Sections)
	for _, s := range got.Sections {
		assert.NotContains(t, strings.ToLower(s.Content), "old body", "stale section")
	}
	assert.Equal(t, storageutil.DraftContentHash(got.RawContent, got.Source, got.Metadata), got.ContentHash)
}

// A mention with no provenance node that the stored text names is
// re-derived by the extractor, so it is not kept; one the text does not
// name was set by a person or a client and stays.
func TestReanalyzeRegressions_MentionWithoutNodeIsKeptOnlyIfNotInText(t *testing.T) {
	svc, driver, _ := regrService(t, identity)
	seeded := regrSeed(t, driver, "mn-1", "numbat sightings with @in.text noted")
	seeded.Mentions = append(seeded.Mentions, mentionsOf("@in.text")...)
	require.NoError(t, driver.Objects().Update(ownerCtx(), seeded))

	_, _, err := svc.ReanalyzeObject(ownerCtx(), "mn-1")
	require.NoError(t, err)
	got := regrGet(t, driver, "mn-1")
	assert.True(t, hasMention(got.Mentions, "@bare.one"), "a mention the text does not name was dropped")
	assert.False(t, hasMention(got.Mentions, "@in.text"), "a mention the text names was kept as if asserted")
}

// A duplicate_of the dedup step wrote is not this package's marker and
// is left alone; the one a hash collision wrote is removed with its kind.
func TestReanalyzeRegressions_ForeignDuplicateMarkerSurvives(t *testing.T) {
	svc, driver, _ := regrService(t, identity, "Metadata")
	seeded := regrSeed(t, driver, "dm-1", "numbat sightings")
	seeded.Metadata["duplicate_of"] = "other-object"
	seeded.Metadata["duplicate_kind"] = "similar"
	seeded.Metadata["duplicate_similarity"] = 0.97
	require.NoError(t, driver.Objects().Update(ownerCtx(), seeded))

	_, _, err := svc.ReanalyzeObject(ownerCtx(), "dm-1")
	require.NoError(t, err)
	got := regrGet(t, driver, "dm-1")
	assert.Equal(t, "other-object", got.Metadata["duplicate_of"], "the dedup step's marker was deleted")
	assert.Equal(t, "similar", got.Metadata["duplicate_kind"])
	assert.InDelta(t, 0.97, got.Metadata["duplicate_similarity"], 1e-9)
}

// sneakyStep rewrites the content without declaring it.
type sneakyStep struct{ pipeline.BaseContract }

func (sneakyStep) Name() string { return "sneaky" }
func (sneakyStep) Run(_ context.Context, d *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	d.RawContent = "NEW BODY WORDS"
	return d, nil
}

// Whatever a step declares, text derived before it changed the content is
// never written back with the new content.
func TestReanalyzeRegressions_StaleTextIsNotWrittenBack(t *testing.T) {
	driver := storageutil.NewTestDriver(t)
	svc := regrServiceSteps(t, driver, []pipeline.PipelineStep{sneakyStep{pipeline.NewBaseContract(pipeline.StepContract{})}})
	seeded := regrSeed(t, driver, "st-1", "OLD BODY WORDS")
	seeded.TextContent = "old body words"
	require.NoError(t, driver.Objects().Update(ownerCtx(), seeded))

	_, _, err := svc.ReanalyzeObject(ownerCtx(), "st-1")
	require.NoError(t, err)
	got := regrGet(t, driver, "st-1")
	assert.Equal(t, "NEW BODY WORDS", got.RawContent)
	assert.Equal(t, "NEW BODY WORDS", got.TextContent, "text derived from the old content was written back")
	assert.Equal(t, storageutil.DraftContentHash("NEW BODY WORDS", "", got.Metadata), got.ContentHash)
}

// With the network allowed, the fresh response decides how it is routed
// and cleaned: an edited content type does not keep a refreshed HTML page
// raw, and a URL that now serves another type is routed by the new one.
func TestReanalyzeRegressions_RefreshIgnoresTheStoredContentType(t *testing.T) {
	e := newRealEnv(t)
	e.page.Store("<html><body><h1>First</h1><p>first body words, long enough to be kept as a section.</p></body></html>")
	first := e.ingest(t, "url.generic", e.srv.URL+"/p", "")
	stored := e.get(t, first.ID)
	stored.Type, stored.Subtype, stored.ContentType = "decision", "adr", "text/x-edited"
	require.NoError(t, e.driver.Objects().Update(ownerCtx(), stored))

	e.page.Store("<html><body><nav>menus</nav><h1>Second</h1><p>second body words, long enough to be kept as a section.</p></body></html>")
	_, _, err := e.svc.ReanalyzeObjectWith(ownerCtx(), first.ID, ReanalyzeOptions{AllowNetwork: true})
	require.NoError(t, err)
	got := e.get(t, first.ID)
	assert.Contains(t, got.RawContent, "second body words")
	assert.NotContains(t, got.RawContent, "<h1>", "the refreshed page was stored as raw HTML")
	assert.NotContains(t, got.RawContent, "menus", "the refreshed page was not cleaned")
	assert.NotEqual(t, "text/x-edited", got.ContentType)
}

// A URL that now serves plain text is routed as text, not cleaned as HTML.
func TestReanalyzeRegressions_RefreshRoutesByTheNewContentType(t *testing.T) {
	e := newRealEnv(t)
	e.page.Store("<html><body><p>an html page that said so in its header, long enough to matter.</p></body></html>")
	first := e.ingest(t, "url.generic", e.srv.URL+"/p", "")

	e.contentType.Store("text/plain; charset=utf-8")
	e.page.Store("plain <b>text</b> that is no longer a page, with Vec<String> in it")
	_, _, err := e.svc.ReanalyzeObjectWith(ownerCtx(), first.ID, ReanalyzeOptions{AllowNetwork: true})
	require.NoError(t, err)
	got := e.get(t, first.ID)
	assert.Contains(t, got.RawContent, "<b>text</b>", "plain text was cleaned as HTML")
	assert.Equal(t, "text/plain; charset=utf-8", got.Metadata["content_type"])
}
