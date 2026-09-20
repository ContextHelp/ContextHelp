package autosuggest_test

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	autosuggest "github.com/ideacrafterslabs/ctxt/plugins/autosuggest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAutoSuggestStep_GenerateMode_AppliesTags(t *testing.T) {
	cfg := autosuggest.AutoSuggestConfig{
		Mode:        "generate",
		MaxTags:     3,
		MaxMentions: 2,
		Enabled:     true,
	}
	llm := &mockLLM{response: `{"tags":["go","plugin"],"mentions":[]}`}
	step := autosuggest.NewAutoSuggestStep(cfg, llm)

	obj := &storage.KnowledgeObject{ID: "obj_step_001", RawContent: "Using Go for plugins."}
	out, err := step.Run(context.Background(), obj)
	require.NoError(t, err)
	labels := make([]string, len(out.Tags))
	for i, tag := range out.Tags {
		labels[i] = tag.Label
	}
	assert.Contains(t, labels, "go")
}

func TestAutoSuggestStep_SelectMode_StoresPending(t *testing.T) {
	cfg := autosuggest.AutoSuggestConfig{
		Mode:        "select",
		MaxTags:     3,
		MaxMentions: 2,
		Enabled:     true,
	}
	llm := &mockLLM{response: `{"tags":["architecture"],"mentions":["eng.platform"]}`}
	step := autosuggest.NewAutoSuggestStep(cfg, llm)

	obj := &storage.KnowledgeObject{ID: "obj_step_002", RawContent: "Platform architecture decisions."}
	out, err := step.Run(context.Background(), obj)
	require.NoError(t, err)
	assert.Empty(t, out.Tags, "select mode: tags NOT applied directly")
	tags, _ := autosuggest.GetPending(out)
	assert.Equal(t, []string{"architecture"}, tags)
}

func TestAutoSuggestStep_Disabled_IsNoop(t *testing.T) {
	cfg := autosuggest.AutoSuggestConfig{Enabled: false}
	llm := &mockLLM{response: `{"tags":["go"],"mentions":[]}`}
	step := autosuggest.NewAutoSuggestStep(cfg, llm)

	obj := &storage.KnowledgeObject{ID: "obj_step_003", RawContent: "content"}
	out, err := step.Run(context.Background(), obj)
	require.NoError(t, err)
	assert.Empty(t, out.Tags)
}
