//go:build openai_vcr

package steps

import (
	"context"
	"os"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/dnaeon/go-vcr.v4/pkg/cassette"
	"gopkg.in/dnaeon/go-vcr.v4/pkg/recorder"
)

const cassettePath = "testdata/fixtures/graph_extractor_openai"

// TestGraphExtractorOpenAI runs GraphExtractor against the real OpenAI API,
// recording the interaction on first run and replaying it on subsequent runs.
//
// To record a fresh cassette (requires OPENAI_API_KEY):
//
//	go test -tags openai_vcr -run TestGraphExtractorOpenAI -count=1 ./internal/pipeline/steps/
//
// To replay (no API key needed):
//
//	go test -tags openai_vcr -run TestGraphExtractorOpenAI ./internal/pipeline/steps/
func TestGraphExtractorOpenAI(t *testing.T) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	mode := recorder.ModeRecordOnce
	if apiKey == "" {
		mode = recorder.ModeReplayOnly
		// Dummy key so the provider doesn't short-circuit; VCR intercepts the request.
		apiKey = "sk-vcr-replay"
	}
	t.Setenv("OPENAI_API_KEY", apiKey)

	// Strip Authorization before saving so the cassette is safe to commit.
	scrubAuth := func(i *cassette.Interaction) error {
		delete(i.Request.Headers, "Authorization")
		return nil
	}

	rec, err := recorder.New(
		cassettePath,
		recorder.WithMode(mode),
		recorder.WithSkipRequestLatency(true),
		recorder.WithHook(scrubAuth, recorder.BeforeSaveHook),
		// Ignore Authorization differences when replaying (key was scrubbed from cassette).
		recorder.WithMatcher(cassette.NewDefaultMatcher(cassette.WithIgnoreAuthorization())),
	)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, rec.Stop()) })

	llm := providers.NewOpenAILLMProvider("gpt-4o-mini").WithHTTPClient(rec.GetDefaultClient())
	step := NewGraphExtractorWithLLM(llm)

	ko := &storage.KnowledgeObject{
		ID:         "vcr-test-obj",
		RawContent: "We decided to use SQLite as the default storage backend. The main open question is which authentication provider to integrate. Artifacts include go.mod and the Makefile.",
	}

	got, err := step.Run(context.Background(), ko)
	require.NoError(t, err)
	require.NotNil(t, got.Graph, "expected graph nodes from LLM")

	types := make(map[string]int)
	for _, n := range got.Graph.Nodes {
		types[n.NodeType]++
	}

	assert.Greater(t, types["summary"], 0, "expected at least one summary node")
	assert.Greater(t, len(got.Graph.Nodes), 1, "expected multiple nodes")
	assert.Equal(t, len(got.Graph.Nodes), len(got.Graph.Edges), "each node must have a contains-edge")
}
