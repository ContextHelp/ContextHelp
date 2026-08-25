//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

// makePgFTSObject builds an object whose projected FTS body contains the
// given text, mirroring the SQLite test fixture shape: a Graph with a summary
// node so ProjectIndex derives the FTS body from the graph.
func makePgFTSObject(id, typ, searchableText string) *storage.KnowledgeObject {
	now := time.Now().UTC().Truncate(time.Second)
	return &storage.KnowledgeObject{
		ID:        id,
		Type:      typ,
		CreatedAt: now,
		UpdatedAt: now,
		Graph: &pluginapi.ObjectGraph{
			Nodes: []pluginapi.GraphNode{
				{
					ID:       pluginapi.NewNodeID(id, pluginapi.NodeTypeSummary, 0),
					NodeType: pluginapi.NodeTypeSummary,
					Label:    "Summary",
					Content:  searchableText,
					Order:    0,
				},
			},
		},
	}
}

// TestPostgresFTSSearch_CreateThenSearch pins the end-to-end FTS write+read
// path: Create projects the FTS body into projected_fts_body (feeding the
// generated tsvector column), FTSSearch finds the object by term, and the
// result carries a score in Metadata["fts_score"].
func TestPostgresFTSSearch_CreateThenSearch(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	ctx := context.Background()

	obj1 := makePgFTSObject("fts-pg-1", "article", "authentication best practices guide")
	if err := drv.Objects().Create(ctx, obj1); err != nil {
		t.Fatalf("create obj1: %v", err)
	}
	obj2 := makePgFTSObject("fts-pg-2", "article", "database indexing strategies")
	if err := drv.Objects().Create(ctx, obj2); err != nil {
		t.Fatalf("create obj2: %v", err)
	}

	results, err := drv.Objects().FTSSearch(ctx, "authentication", storage.ObjectFilter{})
	if err != nil {
		t.Fatalf("FTSSearch: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results: got %d want 1", len(results))
	}
	if results[0].ID != "fts-pg-1" {
		t.Errorf("hit ID: got %q want fts-pg-1", results[0].ID)
	}
	score, ok := results[0].Metadata["fts_score"].(float64)
	if !ok {
		t.Fatalf("Metadata[fts_score] missing or not float64: %#v", results[0].Metadata["fts_score"])
	}
	if score <= 0 {
		t.Errorf("fts_score: got %v want > 0", score)
	}

	// Create must have marked the object FTS-indexed.
	got, err := drv.Objects().Get(ctx, "fts-pg-1")
	if err != nil {
		t.Fatalf("get obj1: %v", err)
	}
	if !got.FTSIndexed {
		t.Error("FTSIndexed: got false want true after Create with FTS body")
	}
}

func TestPostgresFTSSearch_EmptyQuery(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	if _, err := drv.Objects().FTSSearch(context.Background(), "", storage.ObjectFilter{}); err == nil {
		t.Fatal("FTSSearch(\"\") succeeded; want error")
	}
}

// TestPostgresFTSSearch_RankOrder pins best-first ordering: a document where
// the query term dominates must rank above one where it appears once among
// many other words. RRF upstream consumes rank order only, so this — not
// score parity with bm25 — is the cross-driver contract.
func TestPostgresFTSSearch_RankOrder(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	ctx := context.Background()

	dense := makePgFTSObject("fts-pg-dense", "note",
		"goroutine scheduling goroutine preemption goroutine latency")
	if err := drv.Objects().Create(ctx, dense); err != nil {
		t.Fatalf("create dense: %v", err)
	}
	sparse := makePgFTSObject("fts-pg-sparse", "note",
		"a long meandering note about many unrelated systems that mentions goroutine exactly once while covering deployment pipelines caching layers and various other operational topics at length")
	if err := drv.Objects().Create(ctx, sparse); err != nil {
		t.Fatalf("create sparse: %v", err)
	}

	results, err := drv.Objects().FTSSearch(ctx, "goroutine", storage.ObjectFilter{})
	if err != nil {
		t.Fatalf("FTSSearch: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results: got %d want 2", len(results))
	}
	if results[0].ID != "fts-pg-dense" {
		t.Errorf("rank 1: got %q want fts-pg-dense", results[0].ID)
	}
}

func TestPostgresFTSSearch_TypeFilter(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	ctx := context.Background()

	if err := drv.Objects().Create(ctx, makePgFTSObject("fts-pg-t1", "article", "kubernetes cluster upgrade runbook")); err != nil {
		t.Fatalf("create article: %v", err)
	}
	if err := drv.Objects().Create(ctx, makePgFTSObject("fts-pg-t2", "note", "kubernetes cluster capacity note")); err != nil {
		t.Fatalf("create note: %v", err)
	}

	results, err := drv.Objects().FTSSearch(ctx, "kubernetes", storage.ObjectFilter{Type: "note"})
	if err != nil {
		t.Fatalf("FTSSearch: %v", err)
	}
	if len(results) != 1 || results[0].ID != "fts-pg-t2" {
		t.Fatalf("type-filtered results: got %+v want single fts-pg-t2", ids(results))
	}
}

func TestPostgresFTSSearch_MetadataFacetFilter(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	ctx := context.Background()

	meeting := makePgFTSObject("fts-pg-m1", "note", "quarterly roadmap discussion")
	meeting.Metadata = map[string]any{"type": "meeting"}
	if err := drv.Objects().Create(ctx, meeting); err != nil {
		t.Fatalf("create meeting: %v", err)
	}
	memo := makePgFTSObject("fts-pg-m2", "note", "quarterly roadmap memo")
	memo.Metadata = map[string]any{"type": "memo"}
	if err := drv.Objects().Create(ctx, memo); err != nil {
		t.Fatalf("create memo: %v", err)
	}

	results, err := drv.Objects().FTSSearch(ctx, "roadmap", storage.ObjectFilter{MetadataType: "meeting"})
	if err != nil {
		t.Fatalf("FTSSearch: %v", err)
	}
	if len(results) != 1 || results[0].ID != "fts-pg-m1" {
		t.Fatalf("facet-filtered results: got %+v want single fts-pg-m1", ids(results))
	}
}

// TestPostgresFTSSearch_UpdateReindexes pins that Update rewrites
// projected_fts_body so the generated tsvector tracks the new content: the
// old term stops matching, the new term starts.
func TestPostgresFTSSearch_UpdateReindexes(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	ctx := context.Background()

	obj := makePgFTSObject("fts-pg-u1", "note", "ephemeral alpha content")
	if err := drv.Objects().Create(ctx, obj); err != nil {
		t.Fatalf("create: %v", err)
	}

	obj.Graph.Nodes[0].Content = "durable omega content"
	obj.UpdatedAt = time.Now().UTC()
	if err := drv.Objects().Update(ctx, obj); err != nil {
		t.Fatalf("update: %v", err)
	}

	if res, err := drv.Objects().FTSSearch(ctx, "alpha", storage.ObjectFilter{}); err != nil {
		t.Fatalf("FTSSearch alpha: %v", err)
	} else if len(res) != 0 {
		t.Errorf("stale term still matches after update: %+v", ids(res))
	}
	res, err := drv.Objects().FTSSearch(ctx, "omega", storage.ObjectFilter{})
	if err != nil {
		t.Fatalf("FTSSearch omega: %v", err)
	}
	if len(res) != 1 || res[0].ID != "fts-pg-u1" {
		t.Fatalf("new term results: got %+v want single fts-pg-u1", ids(res))
	}
}

// TestPostgresFTSSearchNodeAware pins node-type ALL-of filtering and the
// ReturnNodeHits DocumentView projection, mirroring the SQLite semantics.
func TestPostgresFTSSearchNodeAware(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	ctx := context.Background()

	withDecision := makePgFTSObject("fts-pg-na1", "note", "incident retrospective timeline")
	withDecision.Graph.Nodes = append(withDecision.Graph.Nodes, pluginapi.GraphNode{
		ID:       pluginapi.NewNodeID("fts-pg-na1", pluginapi.NodeTypeDecision, 1),
		NodeType: pluginapi.NodeTypeDecision,
		Label:    "Decision",
		Content:  "rotate credentials",
		Order:    1,
	})
	if err := drv.Objects().Create(ctx, withDecision); err != nil {
		t.Fatalf("create withDecision: %v", err)
	}
	summaryOnly := makePgFTSObject("fts-pg-na2", "note", "incident postmortem summary")
	if err := drv.Objects().Create(ctx, summaryOnly); err != nil {
		t.Fatalf("create summaryOnly: %v", err)
	}

	// No node filter: both match.
	all, err := drv.Objects().FTSSearchNodeAware(ctx, "incident", storage.ObjectFilter{}, pluginapi.NodeAwareFilter{})
	if err != nil {
		t.Fatalf("FTSSearchNodeAware (no filter): %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("unfiltered results: got %d want 2", len(all))
	}

	// Node-type filter: only the object carrying a decision node survives.
	filtered, err := drv.Objects().FTSSearchNodeAware(ctx, "incident", storage.ObjectFilter{},
		pluginapi.NodeAwareFilter{NodeTypes: []string{pluginapi.NodeTypeDecision}, ReturnNodeHits: true})
	if err != nil {
		t.Fatalf("FTSSearchNodeAware (decision filter): %v", err)
	}
	if len(filtered) != 1 {
		t.Fatalf("filtered results: got %d want 1", len(filtered))
	}
	if filtered[0].Object.ID != "fts-pg-na1" {
		t.Errorf("filtered hit: got %q want fts-pg-na1", filtered[0].Object.ID)
	}
	if filtered[0].DocumentView == nil {
		t.Error("DocumentView nil with ReturnNodeHits=true")
	}
}

// TestPostgresFTSSearch_RawHostileInput pins the sanitizer seam on the
// Postgres leg: FTSSearch receives RAW user text; the driver reduces it to
// the same token set the SQLite leg quotes, so hyphenated input matches
// separated words (raw websearch_to_tsquery would demand a strict <->
// phrase) and no hostile syntax errors or changes semantics.
func TestPostgresFTSSearch_RawHostileInput(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	ctx := context.Background()

	if err := drv.Objects().Create(ctx, makePgFTSObject("fts-pg-raw1", "article", "documents that are credit eligible")); err != nil {
		t.Fatalf("create: %v", err)
	}

	// The T-0565 repro shape: hyphenated query, separated words in the doc.
	results, err := drv.Objects().FTSSearch(ctx, "credit-eligible", storage.ObjectFilter{Limit: 10})
	if err != nil {
		t.Fatalf("raw hyphenated query errored: %v", err)
	}
	if len(results) != 1 || results[0].ID != "fts-pg-raw1" {
		t.Fatalf("hyphenated query results: got %+v want single fts-pg-raw1", ids(results))
	}

	// Hostile corpus: no error, implicit-AND semantics only.
	for _, q := range []string{
		`"credit eligible"`,
		"NEAR(credit, eligible)",
		"credit AND eligible",
		"credit OR nonexistent-term-xyz",
		"credit:eligible",
	} {
		if _, err := drv.Objects().FTSSearch(ctx, q, storage.ObjectFilter{Limit: 10}); err != nil {
			t.Errorf("hostile input %q errored: %v", q, err)
		}
	}

	// No usable tokens: match nothing, do not error.
	results, err = drv.Objects().FTSSearch(ctx, "!!! ???", storage.ObjectFilter{Limit: 10})
	if err != nil {
		t.Fatalf("punctuation-only query errored: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("punctuation-only query returned hits: %+v", ids(results))
	}

	// Truly empty input is still a caller bug.
	if _, err := drv.Objects().FTSSearch(ctx, "", storage.ObjectFilter{Limit: 10}); err == nil {
		t.Error("empty query succeeded; want error")
	}
}

// TestPostgresSimilarQueryEndToEnd pins the lifted similar== rejection: the
// RSQL engine auto-detects the Postgres dialect and the compiled
// websearch_to_tsquery predicate executes against the generated tsvector
// column.
func TestPostgresSimilarQueryEndToEnd(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	ctx := context.Background()

	if err := drv.Objects().Create(ctx, makePgFTSObject("fts-pg-dsl1", "note", "canary predicate body")); err != nil {
		t.Fatalf("create match: %v", err)
	}
	if err := drv.Objects().Create(ctx, makePgFTSObject("fts-pg-dsl2", "note", "unrelated body")); err != nil {
		t.Fatalf("create non-match: %v", err)
	}

	results, total, err := search.NewEngine(drv).Search(ctx, "similar==canary", 10, 0)
	if err != nil {
		t.Fatalf("engine search similar==: %v", err)
	}
	if total != 1 || len(results) != 1 || results[0].ID != "fts-pg-dsl1" {
		t.Fatalf("similar== results: total=%d got %+v want single fts-pg-dsl1", total, ids(results))
	}
}

func ids(objs []*storage.KnowledgeObject) []string {
	out := make([]string, len(objs))
	for i, o := range objs {
		out[i] = o.ID
	}
	return out
}
