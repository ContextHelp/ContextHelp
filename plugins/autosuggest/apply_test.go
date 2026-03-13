package autosuggest_test

import (
	"testing"

	autosuggest "github.com/ideacrafterslabs/ctxt/plugins/autosuggest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyGenerate_AddsTags(t *testing.T) {
	obj := &storage.KnowledgeObject{
		ID:   "obj_gen_001",
		Tags: []storage.Tag{{Label: "existing", Weight: 1.0}},
	}
	tags := []string{"go", "existing", "architecture"} // "existing" is a dupe
	mentions := []string{"eng.backend"}

	require.NoError(t, autosuggest.ApplyGenerate(obj, tags, mentions))

	labels := make([]string, len(obj.Tags))
	for i, tag := range obj.Tags {
		labels[i] = tag.Label
	}
	assert.Contains(t, labels, "go")
	assert.Contains(t, labels, "architecture")
	assert.Contains(t, labels, "existing")
	assert.Equal(t, 3, len(obj.Tags), "no duplicate tags")
	assert.Contains(t, obj.Mentions, "eng.backend")
}

func TestApplyGenerate_DeduplicatesMentions(t *testing.T) {
	obj := &storage.KnowledgeObject{
		ID:       "obj_gen_002",
		Mentions: []string{"eng.frontend"},
	}
	require.NoError(t, autosuggest.ApplyGenerate(obj, nil, []string{"eng.frontend", "eng.backend"}))
	assert.Equal(t, 2, len(obj.Mentions))
}

func TestApplySelect_StoresPending(t *testing.T) {
	obj := &storage.KnowledgeObject{ID: "obj_sel_001"}
	require.NoError(t, autosuggest.ApplySelect(obj, []string{"go", "plugin"}, []string{"eng.backend"}))

	tags, mentions := autosuggest.GetPending(obj)
	assert.Equal(t, []string{"go", "plugin"}, tags)
	assert.Equal(t, []string{"eng.backend"}, mentions)
}

func TestApplySelect_ClearPending(t *testing.T) {
	obj := &storage.KnowledgeObject{ID: "obj_sel_002"}
	require.NoError(t, autosuggest.ApplySelect(obj, []string{"go"}, nil))
	autosuggest.ClearPending(obj)
	tags, mentions := autosuggest.GetPending(obj)
	assert.Empty(t, tags)
	assert.Empty(t, mentions)
}
