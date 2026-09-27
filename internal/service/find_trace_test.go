package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/retrieval"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/service/servicetest"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// traceFixture is the profile corpus on SQLite, with a default model
// when withModel is set.
func traceFixture(t *testing.T, withModel bool) *servicetest.HybridFixture {
	t.Helper()
	f := servicetest.NewHybridFixture(t, storageutil.NewTestDriver(t), withModel)
	f.SeedProfileCorpus(t)
	return f
}

// A traced find is the hybrid find plus the trace: same results, same
// diagnostics, profile scoping and min_score knob; no explain unless
// asked for.
func TestFind_TraceMatchesHybridFind(t *testing.T) {
	f := traceFixture(t, true)
	ctx := context.Background()
	minScore := 0.0
	req := service.FindRequest{
		Query: servicetest.ProfileQuery, Profile: "alpha", Limit: 1,
		Search: service.FindSearch{MinScore: &minScore},
	}
	plain, err := f.Svc.Find(ctx, req, f.Sem)
	require.NoError(t, err)
	assert.Nil(t, plain.Trace, "no trace unless asked for")

	req.Trace = true
	traced, err := f.Svc.Find(ctx, req, f.Sem)
	require.NoError(t, err)
	require.NotNil(t, traced.Trace)
	assert.Nil(t, traced.Explain, "trace does not imply explain")
	require.Len(t, traced.Objects, len(plain.Objects))
	for i, o := range traced.Objects {
		assert.Equal(t, plain.Objects[i].ID, o.ID)
	}
	assert.Equal(t, plain.Diagnostics.CandidateCount, traced.Diagnostics.CandidateCount)

	tr := traced.Trace
	assert.Equal(t, service.SearchModeHybrid, tr.Mode)
	assert.Equal(t, retrieval.SemanticOK, tr.SemanticStatus)
	assert.Equal(t, 1, tr.Limit)
	assert.Zero(t, tr.Threshold, "min_score knob reaches the trace")
	var ids []string
	for _, c := range tr.Candidates {
		ids = append(ids, c.ID)
	}
	assert.ElementsMatch(t, []string{"alpha-text", "alpha-vec"}, ids, "every candidate, cut ones included, is alpha's")
	assert.Equal(t, 1, tr.Counts.Returned)
	assert.Equal(t, 1, tr.Counts.CutLimit)

	req.Explain = true
	both, err := f.Svc.Find(ctx, req, f.Sem)
	require.NoError(t, err)
	require.NotNil(t, both.Trace)
	require.Len(t, both.Explain, len(both.Objects))
}

// A trace is always hybrid: an empty mode is hybrid, others are refused.
func TestFind_TraceRejectsOtherModesAndBadParams(t *testing.T) {
	f := traceFixture(t, true)
	ctx := context.Background()
	neg := -1.0
	for name, req := range map[string]service.FindRequest{
		"fts mode":     {Query: "x", Mode: service.FindModeFTS, Trace: true},
		"vector mode":  {Query: "x", Mode: service.FindModeVector, Trace: true},
		"no query":     {Query: "  ", Trace: true},
		"negative min": {Query: "x", Trace: true, Search: service.FindSearch{MinScore: &neg}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := f.Svc.Find(ctx, req, f.Sem)
			assert.True(t, errors.Is(err, service.ErrInvalidFind), "err = %v", err)
		})
	}
	res, err := f.Svc.Find(ctx, service.FindRequest{Query: "x", Mode: service.FindModeHybrid, Trace: true}, f.Sem)
	require.NoError(t, err)
	assert.NotNil(t, res.Trace)
}

// Without a default model the trace is the full-text-only one, not an error.
func TestFind_TraceFTSOnlyWithoutModel(t *testing.T) {
	f := traceFixture(t, false)
	res, err := f.Svc.Find(context.Background(), service.FindRequest{
		Query: servicetest.ProfileQuery, Profile: "alpha", Trace: true,
	}, f.Sem)
	require.NoError(t, err)
	require.NotNil(t, res.Trace)
	assert.Equal(t, service.SearchModeFTSOnly, res.Trace.Mode)
	assert.Equal(t, retrieval.SemanticNoDefaultModel, res.Trace.SemanticStatus)
}

func TestSearchGraphGate_SQLite(t *testing.T) {
	servicetest.RunSearchGraphGate(t, storageutil.NewTestDriver(t))
}
