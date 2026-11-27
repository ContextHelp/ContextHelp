package service

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/upgrade"
)

const llmDoc = "## Plan\n\nThe numbat project tracks sightings near the dryandra woodland and records each one in a ledger kept by the rangers every week.\n\n" +
	"## Decision\n\nWe decided to keep the ledger in SQLite because the rangers work offline for most of the season and need a single file."

func tagSources(tags []storage.Tag) map[string]string {
	m := map[string]string{}
	for _, t := range tags {
		m[t.Label] = t.Source
	}
	return m
}

// A provider outage during a reanalysis writes nothing and fails the
// object; a working LLM that returns less is written normally.
func TestReanalyzeRegressions_ProviderOutageWritesNothing(t *testing.T) {
	e := newRealEnv(t)
	first := e.ingest(t, "text.long", "test", llmDoc)

	// The healthy first run is the enrichment an outage must not wipe.
	require.Contains(t, tagSources(first.Tags), "llmtagone", "fixture: healthy run has llm tags")
	require.NotEmpty(t, first.Sections)
	require.NotNil(t, first.Sections[0].Metadata["summary"], "fixture: healthy run has section summaries")
	require.True(t, hasMention(first.Mentions, "@person.ada"), "fixture: healthy run has a mention")
	require.NotNil(t, first.Graph)
	var decisions int
	for _, n := range first.Graph.Nodes {
		if n.NodeType == storage.NodeTypeDecision {
			decisions++
		}
	}
	require.Positive(t, decisions, "fixture: healthy run has decision nodes")

	before := e.get(t, first.ID)
	edgesBefore := edgeTargets(t, e.driver, first.ID, storage.EdgeTypeMentions)
	require.NotEmpty(t, edgesBefore)
	headBefore, _ := e.svc.Outbox().Head(ownerCtx())

	e.llm.setMode("fail")
	_, _, err := e.svc.ReanalyzeObject(ownerCtx(), first.ID)
	require.Error(t, err, "an outage must fail the object, not count it as processed")
	assert.False(t, errors.Is(err, upgrade.ErrCannotReanalyze), "an outage is a failure, not a skip")
	assert.Contains(t, err.Error(), "model unavailable")

	assert.Equal(t, before, e.get(t, first.ID), "the row changed")
	assert.ElementsMatch(t, edgesBefore, edgeTargets(t, e.driver, first.ID, storage.EdgeTypeMentions), "mention edges changed")
	headAfter, _ := e.svc.Outbox().Head(ownerCtx())
	assert.Equal(t, headBefore, headAfter, "an event was emitted")

	// The LLM is back but has less to say: written normally.
	e.llm.setMode("few")
	_, _, err = e.svc.ReanalyzeObject(ownerCtx(), first.ID)
	require.NoError(t, err)
	got := e.get(t, first.ID)
	src := tagSources(got.Tags)
	assert.Contains(t, src, "llmtagone")
	assert.NotContains(t, src, "llmtagtwo", "tags are the new run's")
	assert.True(t, hasMention(got.Mentions, "@person.ada"))
}

const (
	promptSummary = "one-sentence summary of the following text"
	promptTags    = "concise tags"
	promptEntity  = "Identify named entities"
	promptGraph   = "knowledge extraction assistant"
)

// A reply the step cannot use is a degraded run, reported where it is
// detected: the object fails naming the step, and nothing is written.
func TestReanalyzeRegressions_UnusableReplyFailsTheObject(t *testing.T) {
	cases := []struct{ name, prompt, reply, step string }{
		{"unparseable graph", promptGraph, "not json at all", "graph_extractor"},
		{"empty summary", promptSummary, "", "sectioner"},
		{"empty tags", promptTags, "  ", "tagger"},
		{"empty entities", promptEntity, "", "entity_extractor"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newRealEnv(t)
			first := e.ingest(t, "text.long", "test", llmDoc)
			before := e.get(t, first.ID)
			headBefore, _ := e.svc.Outbox().Head(ownerCtx())

			e.llm.answer(c.prompt, c.reply)
			_, _, err := e.svc.ReanalyzeObject(ownerCtx(), first.ID)
			require.Error(t, err)
			assert.NotErrorIs(t, err, upgrade.ErrCannotReanalyze)
			assert.Contains(t, err.Error(), c.step)
			assert.Equal(t, before, e.get(t, first.ID), "the row changed")
			headAfter, _ := e.svc.Outbox().Head(ownerCtx())
			assert.Equal(t, headBefore, headAfter)
		})
	}
}

// A well-formed reply that holds nothing is a valid run: the stale values
// are gone, the object counts as processed, and the next run is too. (The
// tagger reads a reply of only separators as zero tags; it reads any other
// non-empty text, "none" included, as tags.)
func TestReanalyzeRegressions_ValidEmptyAnswersAreWritten(t *testing.T) {
	e := newRealEnv(t)
	first := e.ingest(t, "text.long", "test", llmDoc)
	before := e.get(t, first.ID)
	require.True(t, hasMention(before.Mentions, "@person.ada"), "fixture: a stored mention")
	require.True(t, hasLLMNode(before.Graph), "fixture: stored decision nodes")

	e.llm.answer(promptGraph, `{"summary":"Fixed summary.","topics":["fixedtopic"],"decisions":[]}`)
	e.llm.answer(promptEntity, "none")
	e.llm.answer(promptTags, ",")
	for run := 1; run <= 2; run++ {
		_, _, err := e.svc.ReanalyzeObject(ownerCtx(), first.ID)
		require.NoError(t, err, "run %d must be processed, not failed", run)
		got := e.get(t, first.ID)
		assert.False(t, hasLLMNode(got.Graph), "run %d: stale decision nodes kept", run)
		assert.False(t, hasMention(got.Mentions, "@person.ada"), "run %d: stale mention kept", run)
		assert.NotContains(t, tagSources(got.Tags), "llmtagone", "run %d: stale llm tag kept", run)
	}
}

func hasLLMNode(g *storage.ObjectGraph) bool {
	if g == nil {
		return false
	}
	for _, n := range g.Nodes {
		if n.NodeType == storage.NodeTypeDecision || n.NodeType == storage.NodeTypeOpenQuestion {
			return true
		}
	}
	return false
}

// An unparseable structured-metadata reply fails the object too.
func TestReanalyzeRegressions_UnusableStructuredMetadataFailsTheObject(t *testing.T) {
	e := newRealEnv(t)
	first := e.ingest(t, "text.long", "test", llmDoc)
	before := e.get(t, first.ID)
	headBefore, _ := e.svc.Outbox().Head(ownerCtx())

	e.llm.answer("metadata extraction assistant", "not json at all")
	_, _, err := e.svc.ReanalyzeObject(ownerCtx(), first.ID)
	require.Error(t, err)
	assert.NotErrorIs(t, err, upgrade.ErrCannotReanalyze)
	assert.Contains(t, err.Error(), "structured_metadata")
	assert.Equal(t, before, e.get(t, first.ID), "the row changed")
	headAfter, _ := e.svc.Outbox().Head(ownerCtx())
	assert.Equal(t, headBefore, headAfter)
}
