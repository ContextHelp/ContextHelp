package storagetest

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

// SearchCapabilities declares which search legs a driver claims to support.
// A false flag renders the corresponding assertions as loud skips — the
// parity gap stays visible in every test run instead of silently passing.
type SearchCapabilities struct {
	// FTS: full-text search (FTSSearch / FTSSearchNodeAware) implemented.
	FTS bool
	// Vectors: vector search (EmbeddingStore + VectorSearch) implemented.
	Vectors bool
}

// searchFixtureObject builds an object whose projected FTS body carries the
// given text (graph summary node — the canonical projection path).
func searchFixtureObject(id, typ, text string) *storage.KnowledgeObject {
	return &storage.KnowledgeObject{
		ID:        id,
		Type:      typ,
		Status:    "active",
		CreatedAt: fixtureTime(),
		UpdatedAt: fixtureTime(),
		Graph: &pluginapi.ObjectGraph{
			Nodes: []pluginapi.GraphNode{
				{
					ID:       pluginapi.NewNodeID(id, pluginapi.NodeTypeSummary, 0),
					NodeType: pluginapi.NodeTypeSummary,
					Label:    "Summary",
					Content:  text,
					Order:    0,
				},
			},
		},
	}
}

// ftsRankCorpus is the golden FTS corpus: same query term at strictly
// decreasing frequency in documents of comparable length, so bm25 and
// ts_rank_cd agree on the order. Rank order — never score values — is the
// cross-driver contract; RRF upstream consumes ranks only.
var ftsRankCorpus = []struct {
	ID   string
	Text string
}{
	{"ftsr-heavy", "raft raft raft consensus consensus log"},
	{"ftsr-mid", "raft raft consensus election vote term"},
	{"ftsr-light", "raft consensus snapshot compaction wal"},
	{"ftsr-decoy", "paxos quorum ballot acceptor promise"},
}

var ftsRankOrder = []string{"ftsr-heavy", "ftsr-mid", "ftsr-light"}

// RunSearchConformance runs the cross-driver search assertions against an
// initialized driver on a FRESH database. Subtests run in declaration order
// and share the database; each seeds its own uniquely-prefixed rows. The
// vector subtests index under VectorFixtureModelID.
func RunSearchConformance(t *testing.T, drv storage.StorageDriver, caps SearchCapabilities) {
	t.Helper()
	ctx := context.Background()

	// Vector golden rank first: its assertion counts every object carrying
	// an embedding, so it must precede the filtered-recall corpus.
	t.Run("VectorRankOrder", func(t *testing.T) {
		if !caps.Vectors {
			t.Skip("PARITY GAP: driver declares Vectors=false — vector rank contract unverified")
		}
		RunVectorRankFixture(t, drv)
	})

	t.Run("FTSRankOrder", func(t *testing.T) {
		if !caps.FTS {
			t.Skip("PARITY GAP: driver declares FTS=false — FTS rank contract unverified")
		}
		for _, d := range ftsRankCorpus {
			if err := drv.Objects().Create(ctx, searchFixtureObject(d.ID, "note", d.Text)); err != nil {
				t.Fatalf("seed %s: %v", d.ID, err)
			}
		}
		results, err := drv.Objects().FTSSearch(ctx, "raft", storage.ObjectFilter{Limit: 10})
		if err != nil {
			t.Fatalf("FTSSearch: %v", err)
		}
		if len(results) != len(ftsRankOrder) {
			t.Fatalf("results: got %d (%v) want %d", len(results), resultIDs(results), len(ftsRankOrder))
		}
		for i, want := range ftsRankOrder {
			if results[i].ID != want {
				t.Errorf("rank %d: got %s want %s (best-first is the cross-driver contract)", i, results[i].ID, want)
			}
			if _, ok := results[i].Metadata["fts_score"].(float64); !ok {
				t.Errorf("rank %d (%s): Metadata[fts_score] missing or not float64: %#v",
					i, results[i].ID, results[i].Metadata["fts_score"])
			}
		}
	})

	t.Run("FTSHostileInput", func(t *testing.T) {
		if !caps.FTS {
			t.Skip("PARITY GAP: driver declares FTS=false — sanitizer contract unverified")
		}
		if err := drv.Objects().Create(ctx,
			searchFixtureObject("ftsh-doc", "note", "documents that are credit eligible")); err != nil {
			t.Fatalf("seed: %v", err)
		}

		// The hyphen class: raw user text must match separated words on
		// every driver — no operator semantics, no phrase strictness.
		res, err := drv.Objects().FTSSearch(ctx, "credit-eligible", storage.ObjectFilter{Limit: 10})
		if err != nil {
			t.Fatalf("hyphenated query errored: %v", err)
		}
		if len(res) != 1 || res[0].ID != "ftsh-doc" {
			t.Errorf("hyphenated query: got %v want [ftsh-doc]", resultIDs(res))
		}

		// Hostile corpus: no driver may error or leak operator semantics.
		for _, q := range []string{
			`"credit eligible"`,
			"NEAR(credit, eligible)",
			"credit AND eligible",
			"credit OR zqxjkv",
			"credit:eligible",
			"credit*",
			"-credit",
		} {
			if _, err := drv.Objects().FTSSearch(ctx, q, storage.ObjectFilter{Limit: 10}); err != nil {
				t.Errorf("hostile input %q errored: %v", q, err)
			}
		}

		// No usable tokens: match nothing, do not error.
		res, err = drv.Objects().FTSSearch(ctx, "!!! ???", storage.ObjectFilter{Limit: 10})
		if err != nil {
			t.Errorf("punctuation-only query errored: %v", err)
		}
		if len(res) != 0 {
			t.Errorf("punctuation-only query matched: %v", resultIDs(res))
		}

		// Truly empty input is a caller bug on every driver.
		if _, err := drv.Objects().FTSSearch(ctx, "", storage.ObjectFilter{Limit: 10}); err == nil {
			t.Error("empty query succeeded; want error")
		}
	})

	t.Run("FilteredKNNRecall", func(t *testing.T) {
		if !caps.Vectors {
			t.Skip("PARITY GAP: driver declares Vectors=false — filtered recall unverified")
		}
		SeedVectorModel(t, drv)
		// 12 near-query objects of the majority type, 3 far objects of the
		// selective type: a selective filter + limit must return ALL
		// qualifying neighbors even though every unfiltered near neighbor
		// ranks above them.
		for i := 0; i < 12; i++ {
			obj := searchFixtureObject(fkID("fkr-hay", i), "fkr-hay", "haystack filler")
			if err := drv.Objects().Create(ctx, obj); err != nil {
				t.Fatalf("seed hay %d: %v", i, err)
			}
			putFixtureVector(t, drv, obj.ID, []float32{1, float32(i) * 0.001, 0, 0})
		}
		for i := 0; i < 3; i++ {
			obj := searchFixtureObject(fkID("fkr-needle", i), "fkr-needle", "needle fixture")
			if err := drv.Objects().Create(ctx, obj); err != nil {
				t.Fatalf("seed needle %d: %v", i, err)
			}
			putFixtureVector(t, drv, obj.ID, []float32{0, 0, 1, float32(i) * 0.01})
		}

		results, err := drv.Objects().VectorSearch(ctx, fixtureQuery([]float32{1, 0, 0, 0}),
			storage.ObjectFilter{Type: "fkr-needle", Limit: 3})
		if err != nil {
			t.Fatalf("filtered VectorSearch: %v", err)
		}
		if len(results) != 3 {
			t.Fatalf("filtered recall: got %d (%v) want all 3 qualifying neighbors",
				len(results), resultIDs(results))
		}
		for _, r := range results {
			if r.Type != "fkr-needle" {
				t.Errorf("non-qualifying hit %s (type %s) leaked through filter", r.ID, r.Type)
			}
		}
	})

	t.Run("FTSProfileScope", func(t *testing.T) {
		if !caps.FTS {
			t.Skip("PARITY GAP: driver declares FTS=false — FTS profile scoping unverified")
		}
		seedProfileScopeCorpus(t, drv, "ftsp", "note", nil)
		assertProfileScope(t, "FTSSearch", func(profileID string) ([]*storage.KnowledgeObject, error) {
			return drv.Objects().FTSSearch(ctx, profileScopeToken,
				storage.ObjectFilter{ProfileID: profileID, Limit: 10})
		}, "ftsp")

		// List is the reference the search legs mirror; it must persist
		// and filter the profile the same way on every driver.
		listed, _, err := drv.Objects().List(ctx, storage.ObjectFilter{ProfileID: "alpha"})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(listed) != 1 || listed[0].ID != "ftsp-alpha" || listed[0].ProfileID != "alpha" {
			t.Errorf("List profile alpha: got %v want [ftsp-alpha]", resultIDs(listed))
		}
	})

	t.Run("VectorProfileScope", func(t *testing.T) {
		if !caps.Vectors {
			t.Skip("PARITY GAP: driver declares Vectors=false — vector profile scoping unverified")
		}
		SeedVectorModel(t, drv)
		seedProfileScopeCorpus(t, drv, "vecp", "vecp-scope", []float32{0, 0, 0, 1})
		assertProfileScope(t, "VectorSearch", func(profileID string) ([]*storage.KnowledgeObject, error) {
			return drv.Objects().VectorSearch(ctx, fixtureQuery([]float32{0, 0, 0, 1}),
				storage.ObjectFilter{Type: "vecp-scope", ProfileID: profileID, Limit: 10})
		}, "vecp")
	})

	t.Run("UnindexedModel", func(t *testing.T) {
		if !caps.Vectors {
			t.Skip("PARITY GAP: driver declares Vectors=false — index-missing contract unverified")
		}
		SeedVectorModel(t, drv)
		_, err := drv.Objects().VectorSearch(ctx, storage.VectorQuery{
			ModelID: "fixture-unindexed", Vector: vectorRankQuery,
		}, storage.ObjectFilter{Limit: 10})
		if !errors.Is(err, storage.ErrEmbeddingIndexMissing) {
			t.Fatalf("VectorSearch on an unindexed model: err = %v, want ErrEmbeddingIndexMissing", err)
		}
	})
}

// profileScopeToken is the FTS term every profile-scope fixture shares, so
// only the profile filter can tell the rows apart.
const profileScopeToken = "zephyrscope"

// profileScopeProfiles are the owners of the profile-scope fixture rows;
// "" is the global scope.
var profileScopeProfiles = []string{"alpha", "beta", ""}

// seedProfileScopeCorpus stores one object per profileScopeProfiles entry,
// all with the same text and, when vec is non-nil, the same vector under
// the fixture model. IDs are "<prefix>-<profile>" ("<prefix>-global" for "").
func seedProfileScopeCorpus(t *testing.T, drv storage.StorageDriver, prefix, typ string, vec []float32) {
	t.Helper()
	for _, p := range profileScopeProfiles {
		obj := searchFixtureObject(profileScopeID(prefix, p), typ, profileScopeToken+" shared fixture text")
		obj.ProfileID = p
		if err := drv.Objects().Create(context.Background(), obj); err != nil {
			t.Fatalf("seed %s: %v", obj.ID, err)
		}
		if vec != nil {
			putFixtureVector(t, drv, obj.ID, vec)
		}
	}
}

func profileScopeID(prefix, profile string) string {
	if profile == "" {
		return prefix + "-global"
	}
	return prefix + "-" + profile
}

// assertProfileScope pins the ObjectFilter.ProfileID contract for a search
// leg, the same one List and the query-language engine honor: a non-empty
// ProfileID returns only that profile's objects (each carrying its
// ProfileID); an empty one does not scope.
func assertProfileScope(t *testing.T, leg string, search func(profileID string) ([]*storage.KnowledgeObject, error), prefix string) {
	t.Helper()
	for _, p := range []string{"alpha", "beta"} {
		res, err := search(p)
		if err != nil {
			t.Fatalf("%s profile %q: %v", leg, p, err)
		}
		want := profileScopeID(prefix, p)
		if len(res) != 1 || res[0].ID != want {
			t.Errorf("%s profile %q: got %v want [%s] (another profile's objects leaked)", leg, p, resultIDs(res), want)
			continue
		}
		if res[0].ProfileID != p {
			t.Errorf("%s profile %q: result %s has ProfileID %q", leg, p, res[0].ID, res[0].ProfileID)
		}
	}
	res, err := search("")
	if err != nil {
		t.Fatalf("%s unscoped: %v", leg, err)
	}
	if len(res) != len(profileScopeProfiles) {
		t.Errorf("%s unscoped: got %v want all %d profiles' objects", leg, resultIDs(res), len(profileScopeProfiles))
	}
}

func fkID(prefix string, i int) string {
	return fmt.Sprintf("%s-%02d", prefix, i)
}

func resultIDs(objs []*storage.KnowledgeObject) []string {
	out := make([]string, len(objs))
	for i, o := range objs {
		out[i] = o.ID
	}
	return out
}
