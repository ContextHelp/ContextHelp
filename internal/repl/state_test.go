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
