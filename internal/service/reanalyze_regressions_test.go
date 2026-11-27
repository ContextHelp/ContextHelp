package service

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	uri "hop.top/cite/scheme"

	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/mentions"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
	"github.com/ideacrafterslabs/ctxt/internal/upgrade"
)

// regrStep stands in for a pipeline: it writes only the fields it
// declares in its contract, like a well-behaved step, and tries to
// overwrite protected metadata keys.
type regrStep struct {
	pipeline.BaseContract
	name     string
	produces map[string]bool
	rewrite  func(string) string
	runs     *int
}

func (s regrStep) Name() string { return s.name }

func (s regrStep) Run(_ context.Context, d *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	*s.runs++
	p := s.produces
	if p["RawContent"] {
		d.RawContent = s.rewrite(d.RawContent)
	}
	if p["TextContent"] {
		d.TextContent = "derived:" + d.RawContent
	}
	if p["Summaries"] {
		d.Summaries = []string{"fresh summary"}
	}
	if p["Sections"] {
		d.Sections = []storage.Section{{Title: "fresh section"}}
	}
	if p["Decisions"] {
		d.Decisions = []storage.Decision{{Title: "fresh decision"}}
	}
	if p["Tasks"] {
		d.Tasks = []storage.Task{{Title: "fresh task"}}
	}
	if p["Tags"] {
		d.Tags = append(d.Tags, storage.Tag{Label: "auto-new", Source: "auto", Weight: 0.5})
	}
	if p["Mentions"] {
		auto := mentions.ParseSlice([]string{"@auto.new"})[0]
		d.Mentions = append(d.Mentions, auto)
		if d.Graph == nil {
			d.Graph = &storage.ObjectGraph{}
		}
		d.Graph.Nodes = append(d.Graph.Nodes, storage.GraphNode{
			ID: storage.NewNodeID(d.ID, storage.NodeTypeEntityMention, 99), NodeType: storage.NodeTypeEntityMention,
			Label: auto.String(), Metadata: map[string]any{storage.ProvenanceSourceField: storage.MentionSourceRawContent},
		})
	}
	if p["Metadata"] {
		if d.Metadata == nil {
			d.Metadata = map[string]any{}
		}
		d.Metadata["title"] = "step title"
		d.Metadata[storageutil.MetadataDataKey] = "STEP OVERWRITE"
		d.Metadata["fingerprint"] = "STEP OVERWRITE"
		d.Metadata["client"] = "STEP OVERWRITE"
	}
	return d, nil
}

// regrAll is the field set the default test pipeline produces.
var regrAll = []string{"RawContent", "TextContent", "Tags", "Mentions", "Metadata"}

func regrPipe(runs *int, rewrite func(string) string, produces ...string) []pipeline.PipelineStep {
	p := map[string]bool{}
	for _, f := range produces {
		p[f] = true
	}
	return []pipeline.PipelineStep{regrStep{pipeline.NewBaseContract(pipeline.StepContract{Produces: produces}), "regr", p, rewrite, runs}}
}

func regrServiceSteps(t *testing.T, driver storage.StorageDriver, steps []pipeline.PipelineStep) *Service {
	t.Helper()
	pipes := pipeline.DefaultRegistry()
	pipes.Upsert("regr", &pipeline.Pipeline{PipelineName: "regr", Steps: steps})
	return New(driver, jobs.NewQueue(driver.Jobs()), pipes, search.NewEngine(driver), "", nil)
}

func regrService(t *testing.T, rewrite func(string) string, produces ...string) (*Service, storage.StorageDriver, *int) {
	t.Helper()
	if len(produces) == 0 {
		produces = regrAll
	}
	driver := storageutil.NewTestDriver(t)
	runs := new(int)
	return regrServiceSteps(t, driver, regrPipe(runs, rewrite, produces...)), driver, runs
}

func identity(s string) string { return s }

var (
	regrNow    = time.Now().UTC().Truncate(time.Second)
	regrRemind = regrNow.Add(48 * time.Hour)
	regrExpire = regrNow.Add(72 * time.Hour)
)

// regrSeed stores an object carrying every field a reanalysis must not
// lose, plus an auto tag and an auto mention it must replace.
func regrSeed(t *testing.T, driver storage.StorageDriver, id, raw string) *storage.KnowledgeObject {
	t.Helper()
	userM := mentions.ParseSlice([]string{"@user.one"})[0]
	bareM := mentions.ParseSlice([]string{"@bare.one"})[0] // no graph node
	oldAuto := mentions.ParseSlice([]string{"@old.auto"})[0]
	obj := &storage.KnowledgeObject{
		ID: id, Type: "text", RawContent: raw, Pipeline: "regr@v0", CreatedAt: regrNow, UpdatedAt: regrNow,
		ContentHash: storageutil.DraftContentHash(raw, "", nil),
		Metadata: map[string]any{
			storageutil.MetadataDataKey: map[string]any{"k": "v"},
			"ambient_source":            "clipboard", "fingerprint": "sha256:f",
			"client": map[string]any{"app_name": "Terminal"},
			"title":  "old title",
		},
		InboxNote: "a note", RemindAt: &regrRemind, ExpiresAt: &regrExpire, ExpiryScope: "pii",
		Classes: []string{"internal"}, SessionID: "sess_1",
		Summaries: []string{"Patched Title"}, TextContent: "old text",
		Sections:  []storage.Section{{Title: "old section"}},
		Decisions: []storage.Decision{{Title: "old decision"}},
		Tasks:     []storage.Task{{Title: "old task"}},
		Tags: []storage.Tag{
			{Label: "keep-me", Source: "user", Weight: 1}, {Label: "stale-auto", Source: "auto", Weight: 0.4},
			{Label: "stale-llm", Source: "llm", Weight: 0.4},
			{Label: "stale-ocr", Source: "ocr"}, {Label: "stale-gh", Source: "github"}, {Label: "stale-op", Source: "step"},
			{Label: "imported", Source: ""}, {Label: "patched", Source: "manual", Weight: 1},
			{Label: "bookmark", Source: "registry"},
		},
		Mentions: []uri.URI{userM, bareM, oldAuto},
		Graph: &storage.ObjectGraph{Nodes: []storage.GraphNode{
			{
				ID: storage.NewNodeID(id, storage.NodeTypeEntityMention, 0), NodeType: storage.NodeTypeEntityMention,
				Label: userM.String(), Metadata: map[string]any{storage.ProvenanceSourceField: storage.MentionSourceMentions},
			},
			{
				ID: storage.NewNodeID(id, storage.NodeTypeEntityMention, 1), NodeType: storage.NodeTypeEntityMention,
				Label: oldAuto.String(), Metadata: map[string]any{storage.ProvenanceSourceField: storage.MentionSourceRawContent},
			},
		}},
	}
	require.NoError(t, driver.Objects().Create(ownerCtx(), obj))
	for _, m := range obj.Mentions {
		require.NoError(t, storage.EnsureMentionEntities(ownerCtx(), driver.Entities(), []uri.URI{m}, regrNow))
		_, err := driver.Edges().CreateMissing(ownerCtx(), storage.MentionEdges(obj, []uri.URI{m}, true, regrNow))
		require.NoError(t, err)
	}
	return obj
}

func regrGet(t *testing.T, driver storage.StorageDriver, id string) *storage.KnowledgeObject {
	t.Helper()
	got, err := driver.Objects().Get(ownerCtx(), id)
	require.NoError(t, err)
	require.NotNil(t, got)
	return got
}

func labels(tags []storage.Tag) []string {
	var out []string
	for _, t := range tags {
		out = append(out, t.Label)
	}
	return out
}

func hasMention(ms []uri.URI, slug string) bool {
	w := mentions.ParseSlice([]string{slug})[0].String()
	for _, m := range ms {
		if m.String() == w {
			return true
		}
	}
	return false
}

// Each field a capture recorded and no step derives survives a reanalysis.
func TestReanalyzeRegressions_KeepsCaptureFields(t *testing.T) {
	svc, driver, _ := regrService(t, identity)
	regrSeed(t, driver, "keep-1", "numbat sightings")
	_, _, err := svc.ReanalyzeObject(ownerCtx(), "keep-1")
	require.NoError(t, err)
	got := regrGet(t, driver, "keep-1")

	cases := []struct {
		name string
		ok   bool
	}{
		{"metadata.data", reflect.DeepEqual(got.Metadata[storageutil.MetadataDataKey], map[string]any{"k": "v"})},
		{"metadata.ambient_source", got.Metadata["ambient_source"] == "clipboard"},
		{"metadata.fingerprint", got.Metadata["fingerprint"] == "sha256:f"},
		{"metadata.client", reflect.DeepEqual(got.Metadata["client"], map[string]any{"app_name": "Terminal"})},
		{"inbox_note", got.InboxNote == "a note"},
		{"remind_at", got.RemindAt != nil && got.RemindAt.Equal(regrRemind)},
		{"expires_at", got.ExpiresAt != nil && got.ExpiresAt.Equal(regrExpire)},
		{"expiry_scope", got.ExpiryScope == "pii"},
		{"classes", reflect.DeepEqual(got.Classes, []string{"internal"})},
		{"session_id", got.SessionID == "sess_1"},
		{"summaries (patched title)", reflect.DeepEqual(got.Summaries, []string{"Patched Title"})},
		{"decisions", len(got.Decisions) == 1 && got.Decisions[0].Title == "old decision"},
		{"tasks", len(got.Tasks) == 1 && got.Tasks[0].Title == "old task"},
		{"sections", len(got.Sections) == 1 && got.Sections[0].Title == "old section"},
		{"user mention", hasMention(got.Mentions, "@user.one")},
		{"mention without provenance", hasMention(got.Mentions, "@bare.one")},
	}
	for _, c := range cases {
		assert.True(t, c.ok, "%s was lost: %+v", c.name, got)
	}
	// Every tag whose source no step emits is kept, whatever the source.
	for _, want := range []string{"keep-me", "imported", "patched", "bookmark"} {
		assert.Contains(t, labels(got.Tags), want, "tag %q lost", want)
	}
}

// A step cannot overwrite a protected key; an unprotected key it writes
// is updated. Auto tags and mentions come from the new run.
func TestReanalyzeRegressions_ProtectedMetadataAndAutoReplacement(t *testing.T) {
	svc, driver, _ := regrService(t, identity)
	regrSeed(t, driver, "prot-1", "numbat sightings")
	_, _, err := svc.ReanalyzeObject(ownerCtx(), "prot-1")
	require.NoError(t, err)
	got := regrGet(t, driver, "prot-1")

	assert.Equal(t, map[string]any{"k": "v"}, got.Metadata[storageutil.MetadataDataKey], "data key overwritten by a step")
	assert.Equal(t, "sha256:f", got.Metadata["fingerprint"], "ambient key overwritten by a step")
	assert.Equal(t, map[string]any{"app_name": "Terminal"}, got.Metadata["client"], "ambient key overwritten by a step")
	assert.Equal(t, "step title", got.Metadata["title"], "unprotected key not updated by the step")

	assert.ElementsMatch(t, []string{"keep-me", "imported", "patched", "bookmark", "auto-new"}, labels(got.Tags),
		"step-sourced tags (auto, llm) must go, all others stay")
	assert.True(t, hasMention(got.Mentions, "@auto.new"))
	assert.False(t, hasMention(got.Mentions, "@old.auto"), "stale auto mention kept")
}

// The content hash follows the derived text; unchanged text keeps it.
func TestReanalyzeRegressions_ContentHash(t *testing.T) {
	t.Run("changed text", func(t *testing.T) {
		svc, driver, _ := regrService(t, func(s string) string { return s + " plus more" })
		seeded := regrSeed(t, driver, "hash-2", "numbat sightings")
		_, _, err := svc.ReanalyzeObject(ownerCtx(), "hash-2")
		require.NoError(t, err)
		got := regrGet(t, driver, "hash-2")
		assert.NotEqual(t, seeded.ContentHash, got.ContentHash)
		assert.Equal(t, storageutil.DraftContentHash("numbat sightings plus more", "", got.Metadata), got.ContentHash)
	})
	t.Run("unchanged text", func(t *testing.T) {
		svc, driver, _ := regrService(t, identity)
		seeded := regrSeed(t, driver, "hash-3", "numbat sightings")
		_, _, err := svc.ReanalyzeObject(ownerCtx(), "hash-3")
		require.NoError(t, err)
		assert.Equal(t, seeded.ContentHash, regrGet(t, driver, "hash-3").ContentHash)
	})
}

// A new hash another object of the profile holds: both stay, the
// re-derived one takes the new text, holds no hash (so a later capture of
// its old text cannot reinforce it) and names the other. A run that does
// not collide drops a stale duplicate_of.
func TestReanalyzeRegressions_HashCollision(t *testing.T) {
	svc, driver, _ := regrService(t, func(s string) string {
		if s == "alpha text" {
			return "beta text"
		}
		return s
	})
	regrSeed(t, driver, "col-a", "alpha text")
	require.NoError(t, driver.Objects().Create(ownerCtx(), &storage.KnowledgeObject{
		ID: "col-b", Type: "text", RawContent: "beta text", Pipeline: "regr@v0", CreatedAt: regrNow, UpdatedAt: regrNow,
		ContentHash: storageutil.DraftContentHash("beta text", "", nil),
	}))
	bBefore := regrGet(t, driver, "col-b")

	_, _, err := svc.ReanalyzeObject(ownerCtx(), "col-a")
	require.NoError(t, err)

	gotA := regrGet(t, driver, "col-a")
	assert.Equal(t, "beta text", gotA.RawContent, "re-derived object takes its new text")
	assert.Empty(t, gotA.ContentHash, "the old hash no longer matches the text")
	assert.Equal(t, "col-b", gotA.Metadata["duplicate_of"])
	assert.Equal(t, bBefore, regrGet(t, driver, "col-b"), "the other object is untouched")

	// A later run that no longer collides drops the stale marker.
	require.NoError(t, driver.Objects().Delete(ownerCtx(), "col-b"))
	_, _, err = svc.ReanalyzeObject(ownerCtx(), "col-a")
	require.NoError(t, err)
	gotA = regrGet(t, driver, "col-a")
	assert.NotContains(t, gotA.Metadata, "duplicate_of", "stale duplicate_of survived")
	assert.Equal(t, storageutil.DraftContentHash("beta text", "", gotA.Metadata), gotA.ContentHash)
}

// Encrypted and held objects are refused with the sentinel, run no step
// and are left exactly as stored.
func TestReanalyzeRegressions_RefusesEncryptedAndHeld(t *testing.T) {
	for _, kind := range []string{"encrypted", "held"} {
		t.Run(kind, func(t *testing.T) {
			svc, driver, runs := regrService(t, strings.ToUpper)
			seeded := regrSeed(t, driver, "ref-1", "numbat sightings")
			if kind == "encrypted" {
				seeded.Encrypted, seeded.EncryptScope = true, "all"
				require.NoError(t, driver.Objects().Update(ownerCtx(), seeded))
			} else {
				require.NoError(t, driver.Holds().Place(ownerCtx(), &storage.ObjectHold{
					ObjectID: "ref-1", HoldID: "h1", Source: "bos", PlacedBy: "bos", PlacedAt: regrNow,
				}))
			}
			before := regrGet(t, driver, "ref-1")
			headBefore, _ := svc.Outbox().Head(context.Background())

			_, _, err := svc.ReanalyzeObject(ownerCtx(), "ref-1")
			require.ErrorIs(t, err, upgrade.ErrCannotReanalyze)
			assert.Zero(t, *runs, "a step ran on a refused object")
			assert.Equal(t, before, regrGet(t, driver, "ref-1"), "refused object changed")
			headAfter, _ := svc.Outbox().Head(context.Background())
			assert.Equal(t, headBefore, headAfter, "refused object emitted an event")
		})
	}
}

// One object.updated event, and mention edges that match the new mentions.
func TestReanalyzeRegressions_EventAndMentionEdges(t *testing.T) {
	svc, driver, _ := regrService(t, identity)
	regrSeed(t, driver, "evt-1", "numbat sightings")
	headBefore, err := svc.Outbox().Head(context.Background())
	require.NoError(t, err)

	_, _, err = svc.ReanalyzeObject(ownerCtx(), "evt-1")
	require.NoError(t, err)

	rs, err := svc.Outbox().Since(context.Background(), headBefore, 0)
	require.NoError(t, err)
	n := 0
	for _, r := range rs {
		if r.Topic == "ctxt.runtime.object.updated" && r.ObjectID == "evt-1" {
			n++
		}
	}
	assert.Equal(t, 1, n, "object.updated count; rows: %+v", rs)

	assert.ElementsMatch(t, []string{
		mentions.ParseSlice([]string{"@user.one"})[0].String(),
		mentions.ParseSlice([]string{"@bare.one"})[0].String(),
		mentions.ParseSlice([]string{"@auto.new"})[0].String(),
	}, edgeTargets(t, driver, "evt-1", storage.EdgeTypeMentions), "mention edges must match the new mentions")
}

func edgeTargets(t *testing.T, driver storage.StorageDriver, id, edgeType string) []string {
	t.Helper()
	edges, err := driver.Edges().ListFrom(ownerCtx(), storage.EdgeEndpointObject, id)
	require.NoError(t, err)
	var targets []string
	for _, e := range edges {
		if e.EdgeType == edgeType {
			targets = append(targets, e.ToID)
		}
	}
	return targets
}

func mentionsOf(slugs ...string) []uri.URI { return mentions.ParseSlice(slugs) }
