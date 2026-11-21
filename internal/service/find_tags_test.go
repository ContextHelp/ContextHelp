package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/retrieval"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/tagfilter"
)

// seedTagCorpus stores objects that all match the query "tagword" and
// differ in tags and creation time (hourly from base, oldest first).
func seedTagCorpus(t *testing.T, svc *Service) time.Time {
	t.Helper()
	base := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	for i, tags := range [][]string{
		{"a"}, {"a", "b"}, {"b"}, {"a", "b", "c"}, {"c"}, nil,
	} {
		o := makeSearchObject("ft-"+string(rune('0'+i)), []string{"tagword shared body"}, "")
		o.CreatedAt = base.Add(time.Duration(i) * time.Hour)
		o.UpdatedAt = o.CreatedAt
		for _, l := range tags {
			o.Tags = append(o.Tags, storage.Tag{Label: l})
		}
		require.NoError(t, svc.store.Objects().Create(ownerCtx(), o))
	}
	rebuildFTS(t, svc)
	return base
}

func findIDs(t *testing.T, svc *Service, req FindRequest) []string {
	t.Helper()
	req.Query = "tagword"
	if req.Limit == 0 {
		req.Limit = 20
	}
	res, err := svc.Find(ownerCtx(), req, retrieval.SemanticSource{})
	require.NoError(t, err)
	return resultIDs(res.Objects)
}

// TestFind_TaggedAndCreated: every mode filters server-side by the tagged
// grammar and the created bounds before the limit applies, so a limit
// never hides a match behind a non-match.
func TestFind_TaggedAndCreated(t *testing.T) {
	svc := newTestService(t)
	base := seedTagCorpus(t, svc)
	hour := func(h int) string { return base.Add(time.Duration(h) * time.Hour).Format(time.RFC3339) }

	cases := []struct {
		name   string
		filter FindFilter
		want   []string // as a set
	}{
		{"any", FindFilter{Tagged: "a,c"}, []string{"ft-0", "ft-1", "ft-3", "ft-4"}},
		{"all", FindFilter{Tagged: "+a,+b"}, []string{"ft-1", "ft-3"}},
		{"all three", FindFilter{Tagged: "+a,+b,+c"}, []string{"ft-3"}},
		{"all with optional", FindFilter{Tagged: "+a,b,+c"}, []string{"ft-3"}},
		{"unknown tag", FindFilter{Tagged: "nope"}, nil},
		{"since", FindFilter{Since: hour(3)}, []string{"ft-3", "ft-4", "ft-5"}},
		{"until is exclusive", FindFilter{Until: hour(1)}, []string{"ft-0"}},
		{"until", FindFilter{Until: hour(2)}, []string{"ft-0", "ft-1"}},
		{"tags and window", FindFilter{Tagged: "a,b", Since: hour(1), Until: hour(4)}, []string{"ft-1", "ft-2", "ft-3"}},
	}
	for _, mode := range []string{FindModeFTS, FindModeHybrid} {
		for _, tc := range cases {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				got := findIDs(t, svc, FindRequest{Mode: mode, Filter: tc.filter})
				assert.ElementsMatch(t, tc.want, got)
				// The limit cuts the matches, never the other way round.
				if len(tc.want) > 1 {
					assert.Len(t, findIDs(t, svc, FindRequest{Mode: mode, Filter: tc.filter, Limit: 1}), 1)
				}
			})
		}
	}
}

// TestFind_MoreMatchedTagsRankFirst: results matching more of the listed
// tags rank first, in every mode, whichever tags they are: 3 matches above
// 2 above 1, ties in relevance order. The count is a tier, not a score:
// hybrid reports it per result and Total is unchanged.
func TestFind_MoreMatchedTagsRankFirst(t *testing.T) {
	svc := newTestService(t)
	seedTagCorpus(t, svc)
	// ft-0 {a}, ft-1 {a,b}, ft-2 {b}, ft-3 {a,b,c}, ft-4 {c}, ft-5 none.
	matched := map[string]int{"ft-3": 3, "ft-1": 2, "ft-0": 1, "ft-2": 1, "ft-4": 1}

	for _, tagged := range []string{"a,b,c", "a,b,c", "+a,b,c", "c,b,a"} {
		filter := FindFilter{Tagged: tagged}
		want := map[string]int{"ft-3": 3, "ft-1": 2, "ft-0": 1, "ft-2": 1, "ft-4": 1}
		if tagged == "+a,b,c" { // a is required
			want = map[string]int{"ft-3": 3, "ft-1": 2, "ft-0": 1}
		}
		for _, mode := range []string{FindModeFTS, FindModeHybrid} {
			t.Run(mode+"/"+tagged, func(t *testing.T) {
				got := findIDs(t, svc, FindRequest{Mode: mode, Filter: filter})
				require.Len(t, got, len(want), "%v", got)
				for i := 1; i < len(got); i++ {
					assert.GreaterOrEqual(t, matched[got[i-1]], matched[got[i]], "order %v", got)
				}
				assert.Equal(t, "ft-3", got[0])
				assert.Equal(t, "ft-1", got[1])

				// A limit keeps the best matches, not the first found.
				assert.Equal(t, []string{"ft-3", "ft-1"}, findIDs(t, svc, FindRequest{Mode: mode, Filter: filter, Limit: 2}))
			})
		}
		res, err := svc.Find(ownerCtx(), FindRequest{Query: "tagword", Mode: FindModeHybrid, Limit: 20, Explain: true, Filter: filter}, retrieval.SemanticSource{})
		require.NoError(t, err)
		require.Len(t, res.Explain, len(want))
		for _, e := range res.Explain {
			assert.Equal(t, want[e.ID], e.Breakdown.TagsMatched, "%s tagged %q", e.ID, tagged)
		}
	}

	// One tag, or all mode without ?, cannot differ: no tags_matched noise
	// beyond the constant, and relevance order stands.
	res, err := svc.Find(ownerCtx(), FindRequest{Query: "tagword", Mode: FindModeHybrid, Limit: 20, Explain: true, Filter: FindFilter{Tagged: "+a,+b"}}, retrieval.SemanticSource{})
	require.NoError(t, err)
	assert.Equal(t, []string{"ft-1", "ft-3"}, resultIDs(res.Objects))
}

// MinScore reads relevance alone: the tag tier neither lifts a hit over the
// threshold nor drops one under it.
func TestFind_TagTierLeavesMinScoreAlone(t *testing.T) {
	svc := newTestService(t)
	seedTagCorpus(t, svc)
	high := 10.0
	got := findIDs(t, svc, FindRequest{Mode: FindModeHybrid, Filter: FindFilter{Tagged: "a,b,c"}, Search: FindSearch{MinScore: &high}})
	assert.Empty(t, got, "no relevance score reaches the threshold, however many tags match")

	res, err := svc.Find(ownerCtx(), FindRequest{Query: "tagword", Mode: FindModeHybrid, Limit: 20, Explain: true, Filter: FindFilter{Tagged: "a,b,c"}}, retrieval.SemanticSource{})
	require.NoError(t, err)
	plain, err := svc.Find(ownerCtx(), FindRequest{Query: "tagword", Mode: FindModeHybrid, Limit: 20, Explain: true}, retrieval.SemanticSource{})
	require.NoError(t, err)
	total := map[string]float64{}
	for _, e := range plain.Explain {
		total[e.ID] = e.Breakdown.Total
	}
	for _, e := range res.Explain {
		assert.InDelta(t, total[e.ID], e.Breakdown.Total, 1e-12, "%s: the relevance score is unchanged by tags", e.ID)
	}
}

// tierByTags orders hits by tags matched, keeping the incoming order within
// a count.
func TestTierByTags(t *testing.T) {
	obj := func(id string, tags ...string) *storage.KnowledgeObject {
		o := &storage.KnowledgeObject{ID: id}
		for _, l := range tags {
			o.Tags = append(o.Tags, storage.Tag{Label: l})
		}
		return o
	}
	f, err := tagfilter.Parse([]string{"a,b,c"})
	require.NoError(t, err)
	objs := []*storage.KnowledgeObject{obj("x", "a"), obj("y", "a", "b"), obj("z", "a", "b", "c"), obj("w", "a", "c"), obj("v", "c")}
	tierByTags(objs, f)
	assert.Equal(t, []string{"z", "y", "w", "x", "v"}, resultIDs(objs))

	one, err := tagfilter.Parse([]string{"a"})
	require.NoError(t, err)
	objs = []*storage.KnowledgeObject{obj("x", "a"), obj("y", "a", "b")}
	tierByTags(objs, one)
	assert.Equal(t, []string{"x", "y"}, resultIDs(objs), "one tag cannot differ: the order stands")
}

// Vector-only search honors the tagged filter and lets tags matched pick
// the hits a limit keeps: two objects embed identically, so only the
// tag count separates them, whichever the index returns first.
func TestSemanticSearchFiltered_TaggedMatchTier(t *testing.T) {
	f := newSemanticFixture(t)
	ctx := ownerCtx()
	tag := func(o *storage.KnowledgeObject, labels ...string) *storage.KnowledgeObject {
		o.Tags = nil
		for _, l := range labels {
			o.Tags = append(o.Tags, storage.Tag{Label: l})
		}
		return o
	}
	text := "Vectors for vec-a describe embedding migration between registered encoders."
	vecs, err := f.emb.Get(ctx, "vec-a", "sem-a")
	require.NoError(t, err)
	require.NotEmpty(t, vecs)
	require.NoError(t, f.svc.store.Objects().Create(ctx, tag(makeSearchObject("vec-c", []string{text}, ""), "common", "x")))
	require.NoError(t, f.emb.Put(ctx, "vec-c", []storage.ObjectVector{{ModelID: "sem-a", Vector: vecs[0].Vector}}))
	vecA, err := f.svc.store.Objects().Get(ctx, "vec-a")
	require.NoError(t, err)
	require.NoError(t, f.svc.store.Objects().Update(ctx, tag(vecA, "common")))

	search := func(tags string, limit int) []string {
		tf, err := tagfilter.Parse([]string{tags})
		require.NoError(t, err)
		objs, _, err := f.svc.SemanticSearchFiltered(ctx, semanticQuery, storage.ObjectFilter{Limit: limit, Tags: tf}, f.sem, semanticCfg(false))
		require.NoError(t, err)
		return resultIDs(objs)
	}
	assert.ElementsMatch(t, []string{"vec-a", "vec-c"}, search("common", 10))
	assert.Equal(t, []string{"vec-c"}, search("x", 10), "any: only the tagged hit")
	assert.Equal(t, []string{"vec-c"}, search("+common,+x", 10), "all: both tags required")
	assert.Equal(t, []string{"vec-c"}, search("+common,x", 1), "the match count decides what a limit keeps")
	assert.Equal(t, "vec-c", search("+common,x", 10)[0], "and what ranks first")
}

// TestParseInstant: a date is a whole UTC day, so an upper bound is the
// start of the next day and a lower bound its start; a timestamp is taken
// as it is.
func TestParseInstant(t *testing.T) {
	for _, tc := range []struct {
		in    string
		upper bool
		want  string
	}{
		{"2026-01-31", false, "2026-01-31T00:00:00Z"},
		{"2026-01-31", true, "2026-02-01T00:00:00Z"},
		{"2026-12-31", true, "2027-01-01T00:00:00Z"},
		{"2026-01-31T10:00:00+02:00", true, "2026-01-31T08:00:00Z"},
		{"2026-01-31T10:00:00Z", false, "2026-01-31T10:00:00Z"},
	} {
		got, err := ParseInstant(tc.in, tc.upper)
		require.NoError(t, err, tc.in)
		assert.Equal(t, tc.want, got.Format(time.RFC3339), "%s upper=%v", tc.in, tc.upper)
	}
	_, err := ParseInstant("yesterday", true)
	assert.Error(t, err)
}

// TestHybridFind_OptionalTagsDoNotCutFTSPool: the full-text leg reads its
// pool in relevance order and tiering by tags matched happens after fusion,
// so more better-matching hits than the pool holds never push out the
// most relevant hit.
func TestHybridFind_OptionalTagsDoNotCutFTSPool(t *testing.T) {
	svc := newTestService(t)
	base := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	mk := func(id, body string, tags ...string) {
		o := makeSearchObject(id, []string{body}, "")
		o.CreatedAt, o.UpdatedAt = base, base
		for _, l := range tags {
			o.Tags = append(o.Tags, storage.Tag{Label: l})
		}
		require.NoError(t, svc.store.Objects().Create(ownerCtx(), o))
	}
	mk("hit-best", "tagword tagword tagword tagword tagword", "a")
	for i := range 6 {
		mk("multi-"+string(rune('0'+i)), "tagword padding padding padding padding padding padding padding padding", "a", "b")
	}
	rebuildFTS(t, svc)

	pool := 3
	for _, tagged := range []string{"a,b", "+a,b"} {
		got := findIDs(t, svc, FindRequest{Mode: FindModeHybrid, Filter: FindFilter{Tagged: tagged},
			Search: FindSearch{FTSPool: &pool}})
		assert.Contains(t, got, "hit-best", "tagged %q: the most relevant hit must survive the pool cut: %v", tagged, got)
	}
}

// TestHybridTrace_FollowsTagTier: the search trace marks returned and
// cut_limit candidates by the order the results are in, tags matched first.
// X is the more relevant hit but matches one listed tag; Y matches two, so
// with limit 1 Y is returned and X is cut by the limit, in the results and
// in the trace alike.
func TestHybridTrace_FollowsTagTier(t *testing.T) {
	svc := newTestService(t)
	base := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	mk := func(id, body string, tags ...string) {
		o := makeSearchObject(id, []string{body}, "")
		o.CreatedAt, o.UpdatedAt = base, base
		for _, l := range tags {
			o.Tags = append(o.Tags, storage.Tag{Label: l})
		}
		require.NoError(t, svc.store.Objects().Create(ownerCtx(), o))
	}
	mk("hit-x", "tagword tagword tagword tagword tagword", "a")
	mk("hit-y", "tagword padding padding padding padding padding padding padding padding", "a", "b")
	rebuildFTS(t, svc)

	res, err := svc.Find(ownerCtx(), FindRequest{Query: "tagword", Mode: FindModeHybrid, Limit: 1, Trace: true,
		Filter: FindFilter{Tagged: "a,b"}}, retrieval.SemanticSource{})
	require.NoError(t, err)
	assert.Equal(t, []string{"hit-y"}, resultIDs(res.Objects))
	require.NotNil(t, res.Trace)
	stage := map[string]TraceStage{}
	for _, c := range res.Trace.Candidates {
		stage[c.ID] = c.Stage
	}
	assert.Equal(t, TraceStageReturned, stage["hit-y"], "trace: the better-matching hit is returned")
	assert.Equal(t, TraceStageCutLimit, stage["hit-x"], "trace: the more relevant but worse-matching hit is cut by the limit")
}
