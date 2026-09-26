package repl

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionState_ResolveIndex(t *testing.T) {
	objs := []*storage.KnowledgeObject{
		{ID: "aaa"},
		{ID: "bbb"},
		{ID: "ccc"},
	}

	s := &SessionState{}
	s.SetResults(objs)

	t.Run("valid index 1", func(t *testing.T) {
		got, err := s.ResolveIndex(1)
		require.NoError(t, err)
		assert.Equal(t, "aaa", got.ID)
	})

	t.Run("valid index 3", func(t *testing.T) {
		got, err := s.ResolveIndex(3)
		require.NoError(t, err)
		assert.Equal(t, "ccc", got.ID)
	})

	t.Run("index 0 is out of range", func(t *testing.T) {
		_, err := s.ResolveIndex(0)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no result 0")
	})

	t.Run("index beyond length", func(t *testing.T) {
		_, err := s.ResolveIndex(4)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no result 4")
	})

	t.Run("empty state", func(t *testing.T) {
		empty := &SessionState{}
		_, err := empty.ResolveIndex(1)
		assert.Error(t, err)
	})
}

func TestSessionState_SetResults_Replaces(t *testing.T) {
	s := &SessionState{}
	s.SetResults([]*storage.KnowledgeObject{{ID: "x"}})
	assert.Len(t, s.LastResults, 1)
	s.SetResults([]*storage.KnowledgeObject{{ID: "y"}, {ID: "z"}})
	assert.Len(t, s.LastResults, 2)
	assert.Equal(t, "y", s.LastResults[0].ID)
}

func TestSessionState_PushQueryVector_SlidingWindow(t *testing.T) {
	s := &SessionState{}

	for i := 0; i < 7; i++ {
		s.PushQueryVector("m1", []float32{float32(i), 0})
	}

	assert.Len(t, s.QueryHistory, 5, "window must cap at QueryHistoryMax (5)")
	// Oldest remaining should be i=2 (7 pushes, keep last 5: 2,3,4,5,6).
	assert.Equal(t, float32(2), s.QueryHistory[0][0])
	assert.Equal(t, float32(6), s.QueryHistory[4][0])
}

func TestSessionState_SessionContextVector_NilWhenFewEntries(t *testing.T) {
	s := &SessionState{}
	assert.Nil(t, s.SessionContextVector("m1"), "nil when empty")

	s.PushQueryVector("m1", []float32{1, 0})
	assert.Nil(t, s.SessionContextVector("m1"), "nil with only 1 entry")
}

func TestSessionState_SessionContextVector_WeightsMostRecent(t *testing.T) {
	s := &SessionState{}
	// Push 3 orthogonal-ish vectors.
	s.PushQueryVector("m1", []float32{1, 0, 0}) // oldest
	s.PushQueryVector("m1", []float32{0, 1, 0})
	s.PushQueryVector("m1", []float32{0, 0, 1}) // most recent → weight 0.4

	ctx := s.SessionContextVector("m1")
	require.NotNil(t, ctx)
	require.Len(t, ctx, 3)

	// Most recent dim (index 2) should dominate; its weight is 0.4.
	// The unnormalized contribution to dim 2 is 0.4 (from vec [0,0,1]).
	// dim 0 contributes 0.2 (from vec [1,0,0] with weight 0.2).
	// dim 1 contributes 0.3 (from vec [0,1,0] with weight 0.3).
	// After normalization the largest component should be dim 2.
	assert.Greater(t, ctx[2], ctx[0], "most-recent dim should dominate after blending")
	assert.Greater(t, ctx[2], ctx[1], "most-recent dim should dominate after blending")
}

// Query vectors are kept per embedding model: a vector from a different
// model is never blended, and switching models starts a fresh history.
func TestSessionState_QueryHistoryIsPerModel(t *testing.T) {
	s := &SessionState{}
	s.PushQueryVector("m1", []float32{1, 0, 0})
	s.PushQueryVector("m1", []float32{0, 1, 0})
	require.NotNil(t, s.SessionContextVector("m1"))
	assert.Nil(t, s.SessionContextVector("m2"), "m1 vectors must not blend into an m2 query")

	s.PushQueryVector("m2", []float32{0, 0, 1, 0})
	assert.Equal(t, "m2", s.QueryModelID)
	assert.Len(t, s.QueryHistory, 1, "switching models drops the other model's vectors")
	assert.Nil(t, s.SessionContextVector("m1"))
}
