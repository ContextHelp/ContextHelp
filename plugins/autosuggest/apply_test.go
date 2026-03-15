package autosuggest_test

import (
	"testing"

	autosuggest "github.com/ideacrafterslabs/ctxt/plugins/autosuggest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"hop.top/uri"
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
	assert.Contains(t, obj.Mentions, uri.URI{Scheme: "ctxt", Space: "entity", ID: "eng/backend"})
}

func TestApplyGenerate_DeduplicatesMentions(t *testing.T) {
	obj := &storage.KnowledgeObject{
		ID:          "obj_gen_002",
		Mentions: []uri.URI{{Scheme: "ctxt", Space: "entity", ID: "eng/frontend"}},
	}
	require.NoError(t, autosuggest.ApplyGenerate(obj, nil, []string{"eng.frontend", "eng.backend"}))
	assert.Equal(t, 2, len(obj.Mentions))
}

func TestApplyGenerate_AcceptsURIForm(t *testing.T) {
	obj := &storage.KnowledgeObject{ID: "obj_gen_003"}
	// Pass mentions in ctxt:// URI form directly — should round-trip unchanged.
	require.NoError(t, autosuggest.ApplyGenerate(obj, nil, []string{
		"ctxt://entity/stripe/api/checkout",
		"ctxt://entity/person/alice",
	}))
	if len(obj.Mentions) != 2 {
		t.Fatalf("expected 2 mentions, got %d: %v", len(obj.Mentions), obj.Mentions)
	}
	assert.Contains(t, obj.Mentions, uri.URI{Scheme: "ctxt", Space: "entity", ID: "stripe/api/checkout"})
	assert.Contains(t, obj.Mentions, uri.URI{Scheme: "ctxt", Space: "entity", ID: "person/alice"})
}

func TestApplyGenerate_MixedForms(t *testing.T) {
	// Slug form and URI form for the same entity should produce the same URI and be deduped.
	obj := &storage.KnowledgeObject{ID: "obj_gen_004"}
	require.NoError(t, autosuggest.ApplyGenerate(obj, nil, []string{
		"@stripe.api.checkout",
		"ctxt://entity/stripe/api/checkout",
	}))
	assert.Equal(t, 1, len(obj.Mentions), "slug and URI form for same entity should deduplicate")
	assert.Equal(t, "ctxt://entity/stripe/api/checkout", obj.Mentions[0].String())
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
