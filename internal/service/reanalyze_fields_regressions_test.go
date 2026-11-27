package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/mentions"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
	"github.com/ideacrafterslabs/ctxt/internal/upgrade"
)

// A field is cleared only when a step of the pipeline produces it: with a
// producing step it is replaced, with one that does not it stays as stored.
func TestReanalyzeRegressions_DerivedFieldsFollowTheContract(t *testing.T) {
	cases := []struct {
		field string
		stale func(*storage.KnowledgeObject) bool // still the stored value
		fresh func(*storage.KnowledgeObject) bool // the step's value
	}{
		{"Summaries", func(o *storage.KnowledgeObject) bool {
			return len(o.Summaries) == 1 && o.Summaries[0] == "Patched Title"
		},
			func(o *storage.KnowledgeObject) bool {
				return len(o.Summaries) == 1 && o.Summaries[0] == "fresh summary"
			}},
		{"Sections", func(o *storage.KnowledgeObject) bool {
			return len(o.Sections) == 1 && o.Sections[0].Title == "old section"
		},
			func(o *storage.KnowledgeObject) bool {
				return len(o.Sections) == 1 && o.Sections[0].Title == "fresh section"
			}},
		{"Decisions", func(o *storage.KnowledgeObject) bool {
			return len(o.Decisions) == 1 && o.Decisions[0].Title == "old decision"
		},
			func(o *storage.KnowledgeObject) bool {
				return len(o.Decisions) == 1 && o.Decisions[0].Title == "fresh decision"
			}},
		{"Tasks", func(o *storage.KnowledgeObject) bool { return len(o.Tasks) == 1 && o.Tasks[0].Title == "old task" },
			func(o *storage.KnowledgeObject) bool { return len(o.Tasks) == 1 && o.Tasks[0].Title == "fresh task" }},
		{"TextContent", func(o *storage.KnowledgeObject) bool { return o.TextContent == "old text" },
			func(o *storage.KnowledgeObject) bool { return o.TextContent == "derived:numbat sightings" }},
	}
	for _, c := range cases {
		t.Run(c.field+"/not produced", func(t *testing.T) {
			svc, driver, runs := regrService(t, identity, "Metadata")
			regrSeed(t, driver, "f-1", "numbat sightings")
			_, _, err := svc.ReanalyzeObject(ownerCtx(), "f-1")
			require.NoError(t, err)
			assert.Equal(t, 1, *runs)
			assert.True(t, c.stale(regrGet(t, driver, "f-1")), "%s changed although no step produces it", c.field)
		})
		t.Run(c.field+"/produced", func(t *testing.T) {
			svc, driver, _ := regrService(t, identity, c.field, "RawContent")
			regrSeed(t, driver, "f-2", "numbat sightings")
			_, _, err := svc.ReanalyzeObject(ownerCtx(), "f-2")
			require.NoError(t, err)
			assert.True(t, c.fresh(regrGet(t, driver, "f-2")), "%s not re-derived by the step that produces it", c.field)
		})
	}
}

// A pipeline whose only step is a no-op leaves the object as stored.
func TestReanalyzeRegressions_NoOpPipelineKeepsEverything(t *testing.T) {
	svc, driver, _ := regrService(t, identity, "Metadata")
	seeded := regrSeed(t, driver, "noop-1", "numbat sightings")
	graphed := *seeded
	graphed.Graph = &storage.ObjectGraph{Nodes: append(append([]storage.GraphNode(nil), seeded.Graph.Nodes...),
		storage.GraphNode{ID: storage.NewNodeID("noop-1", storage.NodeTypeSection, 0), NodeType: storage.NodeTypeSection, Label: "imported node"})}
	require.NoError(t, driver.Objects().Update(ownerCtx(), &graphed))
	seeded = &graphed
	_, _, err := svc.ReanalyzeObject(ownerCtx(), "noop-1")
	require.NoError(t, err)
	got := regrGet(t, driver, "noop-1")
	assert.Equal(t, seeded.Graph.Nodes, got.Graph.Nodes, "graph changed although no step produces it")
	assert.Equal(t, seeded.Graph.Edges, got.Graph.Edges)
	assert.True(t, got.Graph.Stamped, "the run did not mark the graph")
	assert.Equal(t, "old text", got.TextContent)
	assert.Len(t, got.Tags, len(seeded.Tags), "tags changed although no step produces them")
	assert.Len(t, got.Mentions, len(seeded.Mentions))
}

// A step that produces tags (not mentions) rebuilds the graph but keeps
// the entity-mention nodes: they hold the provenance of the mentions that stay.
func TestReanalyzeRegressions_KeepsMentionProvenanceNodes(t *testing.T) {
	svc, driver, _ := regrService(t, identity, "Tags")
	seeded := regrSeed(t, driver, "prov-1", "numbat sightings")
	stale := *seeded
	stale.Graph = &storage.ObjectGraph{Nodes: append(append([]storage.GraphNode(nil), seeded.Graph.Nodes...),
		storage.GraphNode{ID: storage.NewNodeID("prov-1", storage.NodeTypeTag, 0), NodeType: storage.NodeTypeTag, Label: "stale-tag-node"})}
	require.NoError(t, driver.Objects().Update(ownerCtx(), &stale))
	_, _, err := svc.ReanalyzeObject(ownerCtx(), "prov-1")
	require.NoError(t, err)
	got := regrGet(t, driver, "prov-1")
	require.NotNil(t, got.Graph)
	assert.Equal(t, seeded.Graph.Nodes, got.Graph.Nodes)
}

// networkStep is a step with a fetch step's name that reads a server.
type networkStep struct {
	pipeline.BaseContract
	name string
	url  string
	hits *atomic.Int32
}

func (s networkStep) Name() string { return s.name }

func (s networkStep) Run(_ context.Context, d *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if s.url != "" {
		resp, err := http.Get(s.url) //nolint:noctx // test server
		if err != nil {
			return nil, err
		}
		resp.Body.Close()
	}
	s.hits.Add(1)
	d.RawContent = "LIVE PAGE NOW"
	return d, nil
}

func TestReanalyzeRegressions_NetworkStepsDoNotRunByDefault(t *testing.T) {
	var reqs atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reqs.Add(1) }))
	defer srv.Close()
	var hits atomic.Int32
	runs := new(int)
	driver := storageutil.NewTestDriver(t)
	mk := func(name string) pipeline.PipelineStep {
		return networkStep{pipeline.NewBaseContract(pipeline.StepContract{Produces: []string{"RawContent"}}), name, srv.URL, &hits}
	}
	steps := append([]pipeline.PipelineStep{mk("url_fetcher"), mk("ibr_fetcher")}, regrPipe(runs, identity, "Sections", "Tags")...)
	svc := regrServiceSteps(t, driver, steps)
	regrSeed(t, driver, "net-1", "stored snapshot")

	_, _, err := svc.ReanalyzeObject(ownerCtx(), "net-1")
	require.NoError(t, err)
	got := regrGet(t, driver, "net-1")
	assert.Zero(t, reqs.Load(), "a reanalysis fetched the live URL")
	assert.Equal(t, "stored snapshot", got.RawContent)
	assert.Equal(t, "fresh section", got.Sections[0].Title, "sections are re-derived from the stored text")
	assert.Contains(t, labels(got.Tags), "auto-new")

	_, _, err = svc.ReanalyzeObjectWith(ownerCtx(), "net-1", ReanalyzeOptions{AllowNetwork: true})
	require.NoError(t, err)
	assert.Equal(t, int32(2), reqs.Load(), "AllowNetwork must run both fetch steps")
	assert.Equal(t, "LIVE PAGE NOW", regrGet(t, driver, "net-1").RawContent)
}

// A pipeline whose later step needs a fetch's output cannot run on stored
// content: refused, nothing run, row unchanged.
func TestReanalyzeRegressions_FetchDependentPipelineIsRefused(t *testing.T) {
	var hits atomic.Int32
	driver := storageutil.NewTestDriver(t)
	mk := func(name string) pipeline.PipelineStep {
		return networkStep{pipeline.NewBaseContract(pipeline.StepContract{}), name, "", &hits}
	}
	svc := regrServiceSteps(t, driver, []pipeline.PipelineStep{mk("github_repo_fetcher"), mk("github_repo_mapper")})
	regrSeed(t, driver, "gh-1", "# owner/name")
	before := regrGet(t, driver, "gh-1")
	headBefore, _ := svc.Outbox().Head(context.Background())

	_, _, err := svc.ReanalyzeObject(ownerCtx(), "gh-1")
	require.ErrorIs(t, err, upgrade.ErrCannotReanalyze)
	assert.Zero(t, hits.Load())
	assert.Equal(t, before, regrGet(t, driver, "gh-1"))
	headAfter, _ := svc.Outbox().Head(context.Background())
	assert.Equal(t, headBefore, headAfter)
}

// With fan-out enabled a reanalysis writes the reverse edges and entity
// pages as ingest does; disabled, only the forward edges exist.
func TestReanalyzeRegressions_FanOut(t *testing.T) {
	for _, on := range []bool{true, false} {
		svc, driver, _ := regrService(t, identity)
		svc.Cfg.FanOut = config.FanOutConfig{Enabled: on, Entities: on}
		regrSeed(t, driver, "fo-1", "numbat sightings")
		_, _, err := svc.ReanalyzeObject(ownerCtx(), "fo-1")
		require.NoError(t, err)
		target := mentions.ParseSlice([]string{"@auto.new"})[0].String()
		back, err := driver.Edges().ListFrom(ownerCtx(), storage.EdgeEndpointEntity, target)
		require.NoError(t, err)
		var reverse int
		for _, e := range back {
			if e.EdgeType == storage.EdgeTypeMentionedIn && e.ToID == "fo-1" {
				reverse++
			}
		}
		if on {
			assert.Equal(t, 1, reverse, "fan-out on: reverse edge missing")
		} else {
			assert.Zero(t, reverse, "fan-out off: reverse edge written")
		}
	}
}

type failingEdges struct{ storage.EdgeStore }

func (failingEdges) CreateMissing(context.Context, []*storage.Edge) (int, error) {
	return 0, errors.New("edge store down")
}
func (failingEdges) DeleteMatching(context.Context, []storage.EdgeMatch) (int, error) {
	return 0, errors.New("edge store down")
}

type edgeFailDriver struct{ storage.StorageDriver }

func (d edgeFailDriver) Edges() storage.EdgeStore { return failingEdges{d.StorageDriver.Edges()} }

// Mention-edge failures after the row is written are logged: the object
// counts as re-analyzed and the live event still goes out.
func TestReanalyzeRegressions_EdgeFailureAfterWriteIsNotAnError(t *testing.T) {
	base := storageutil.NewTestDriver(t)
	runs := new(int)
	driver := edgeFailDriver{base}
	svc := regrServiceSteps(t, driver, regrPipe(runs, identity, regrAll...))
	rb := &recordingBus{}
	svc.Bus = rb
	regrSeed(t, base, "ef-1", "numbat sightings")

	_, _, err := svc.ReanalyzeObject(ownerCtx(), "ef-1")
	require.NoError(t, err)
	assert.Contains(t, labels(regrGet(t, base, "ef-1").Tags), "auto-new", "row not written")
	var published int
	for _, topic := range rb.take() {
		if topic == "ctxt.runtime.object.updated" {
			published++
		}
	}
	assert.Equal(t, 1, published, "live event skipped")
}

// countingStep is a step that only counts its runs.
type countingStep struct {
	pipeline.BaseContract
	name string
	runs *int
}

func (s countingStep) Name() string { return s.name }
func (s countingStep) Run(_ context.Context, d *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	*s.runs++
	return d, nil
}
