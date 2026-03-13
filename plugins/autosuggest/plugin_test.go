package autosuggest_test

import (
	"context"
	"testing"

	autosuggest "github.com/ideacrafterslabs/ctxt/plugins/autosuggest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPluginE2E_SelectMode simulates the full pipeline step + pending retrieve flow.
func TestPluginE2E_SelectMode(t *testing.T) {
	cfg := autosuggest.AutoSuggestConfig{
		Mode:        "select",
		MaxTags:     3,
		MaxMentions: 2,
		Enabled:     true,
	}
	llm := &mockLLM{response: `{"tags":["golang","testing"],"mentions":["eng.qa"]}`}
	step := autosuggest.NewAutoSuggestStep(cfg, llm)

	obj := &storage.KnowledgeObject{
		ID:         "e2e_001",
		RawContent: "Integration testing strategy for Go services in the eng.qa team.",
	}

	// Run step.
	out, err := step.Run(context.Background(), obj)
	require.NoError(t, err)

	// In select mode: tags NOT applied directly.
	assert.Empty(t, out.Tags, "select mode must not mutate Tags directly")

	// Pending suggestions stored.
	tags, mentions := autosuggest.GetPending(out)
	assert.Equal(t, []string{"golang", "testing"}, tags)
	assert.Equal(t, []string{"eng.qa"}, mentions)

	// Approve: apply and clear.
	require.NoError(t, autosuggest.ApplyGenerate(out, tags, mentions))
	autosuggest.ClearPending(out)

	labels := make([]string, len(out.Tags))
	for i, tag := range out.Tags {
		labels[i] = tag.Label
	}
	assert.Contains(t, labels, "golang")
	assert.Contains(t, labels, "testing")
	t2, m2 := autosuggest.GetPending(out)
	assert.Empty(t, t2)
	assert.Empty(t, m2)
}

// TestPluginE2E_GenerateMode simulates auto-apply flow.
func TestPluginE2E_GenerateMode(t *testing.T) {
	cfg := autosuggest.AutoSuggestConfig{
		Mode:        "generate",
		MaxTags:     5,
		MaxMentions: 3,
		Enabled:     true,
	}
	llm := &mockLLM{response: `{"tags":["plugin","architecture"],"mentions":[]}`}
	step := autosuggest.NewAutoSuggestStep(cfg, llm)

	obj := &storage.KnowledgeObject{
		ID:         "e2e_002",
		RawContent: "Plugin architecture for extending dPKMS.",
	}

	out, err := step.Run(context.Background(), obj)
	require.NoError(t, err)

	labels := make([]string, len(out.Tags))
	for i, tag := range out.Tags {
		labels[i] = tag.Label
	}
	assert.Contains(t, labels, "plugin")
	assert.Contains(t, labels, "architecture")
}
