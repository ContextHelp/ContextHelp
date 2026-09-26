package steps

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/embeddings"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings/embeddingtest"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
	"github.com/ideacrafterslabs/ctxt/internal/projection"
	"github.com/ideacrafterslabs/ctxt/internal/providers/providertest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/pkg/pluginapi"
)

// Ollama calls replay the embedding-ollama cassettes, recorded against a
// real Ollama serving snowflake-arctic-embed2 (1024 dimensions). Re-record:
//
//	XRR_MODE=record go test -tags fts5 -count=1 -run TestEmbeddingGenerator ./internal/pipeline/steps/
const embeddingCassettes = "testdata/cassettes/embedding-ollama"

const snowflakeConfig = `{"backend":"ollama","model":"snowflake-arctic-embed2","endpoint":"http://127.0.0.1:11434"}`

func snowflake(modelID string, isDefault bool) registry.Model {
	return registry.Model{
		ModelID:    modelID,
		Provider:   "ollama",
		Dimension:  1024,
		IsDefault:  isDefault,
		ConfigJSON: snowflakeConfig,
	}
}

// notPulled names an Ollama model the recording server does not have, so
// its embed call fails for real (Ollama answers "model not found").
func notPulled(isDefault bool) registry.Model {
	return registry.Model{
		ModelID:    "not-pulled-embed@1",
		Provider:   "ollama",
		Dimension:  1024,
		IsDefault:  isDefault,
		ConfigJSON: `{"backend":"ollama","model":"ctxt-test-not-pulled-embed","endpoint":"http://127.0.0.1:11434"}`,
	}
}

func noEnv(string) (string, bool) { return "", false }

// ollamaResolver is the real ProviderResolver over replayed Ollama calls.
func ollamaResolver(t *testing.T) (embeddings.ProviderResolver, *providertest.OllamaCalls) {
	t.Helper()
	client, calls := providertest.OllamaClient(t, embeddingCassettes)
	return embeddings.NewProviderResolver(&embeddings.Resolver{LookupEnv: noEnv, HTTPClient: client}), calls
}

// captureLogs routes slog to a buffer for the test's duration.
func captureLogs(t *testing.T) *syncBuffer {
	t.Helper()
	buf := &syncBuffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return buf
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func quokkaDraft() *storage.KnowledgeObject {
	return &storage.KnowledgeObject{
		ID:        "obj-quokka",
		Summaries: []string{"quokka survey notes from rottnest island"},
	}
}

func vectorFor(t *testing.T, draft *storage.KnowledgeObject, modelID string) *pluginapi.ObjectVector {
	t.Helper()
	for i := range draft.Vectors {
		if draft.Vectors[i].ModelID == modelID {
			return &draft.Vectors[i]
		}
	}
	return nil
}

func TestEmbeddingGenerator_EveryPopulatingModelGetsAVector(t *testing.T) {
	resolver, _ := ollamaResolver(t)
	models := embeddingtest.Models{
		snowflake("snowflake-arctic-embed2@default", true),
		snowflake("snowflake-arctic-embed2@candidate", false),
	}
	step := NewEmbeddingGenerator(models, resolver)

	draft := quokkaDraft()
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(got.Vectors) != 2 {
		t.Fatalf("vectors = %d, want one per populating model (2)", len(got.Vectors))
	}
	text := projection.ProjectIndex(draft).EmbeddingText
	for _, m := range models {
		v := vectorFor(t, got, m.ModelID)
		if v == nil {
			t.Fatalf("no vector for populating model %s", m.ModelID)
		}
		if v.ChunkIdx != 0 || len(v.Vector) != 1024 || v.Text != text {
			t.Errorf("%s: chunk=%d dim=%d text=%q, want chunk 0, 1024 dims, the projected text",
				m.ModelID, v.ChunkIdx, len(v.Vector), v.Text)
		}
	}
	if !got.VectorIndexed {
		t.Error("VectorIndexed = false, want true: the default model produced a vector")
	}
	if got.Embeddings != nil {
		t.Error("step wrote the single-vector Embeddings field; the only output is Vectors")
	}
}

func TestEmbeddingGenerator_FailingModelDoesNotBlockIngest(t *testing.T) {
	logs := captureLogs(t)
	resolver, _ := ollamaResolver(t)
	failing := notPulled(false)
	step := NewEmbeddingGenerator(embeddingtest.Models{
		snowflake("snowflake-arctic-embed2@default", true),
		failing,
	}, resolver)

	got, err := step.Run(context.Background(), quokkaDraft())
	if err != nil {
		t.Fatalf("a failing model failed the step: %v", err)
	}
	if len(got.Vectors) != 1 || got.Vectors[0].ModelID != "snowflake-arctic-embed2@default" {
		t.Fatalf("vectors = %+v, want only the working model's", got.Vectors)
	}
	if !got.VectorIndexed {
		t.Error("VectorIndexed = false, want true: the default model succeeded")
	}
	if out := logs.String(); !strings.Contains(out, "model_id="+failing.ModelID) || !strings.Contains(out, "level=WARN") {
		t.Errorf("failure not recorded as a warning naming the model; logs:\n%s", out)
	}
}

func TestEmbeddingGenerator_DefaultFailureLeavesVectorIndexedFalse(t *testing.T) {
	captureLogs(t)
	resolver, _ := ollamaResolver(t)
	step := NewEmbeddingGenerator(embeddingtest.Models{
		notPulled(true),
		snowflake("snowflake-arctic-embed2@candidate", false),
	}, resolver)

	got, err := step.Run(context.Background(), quokkaDraft())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(got.Vectors) != 1 || got.Vectors[0].ModelID != "snowflake-arctic-embed2@candidate" {
		t.Fatalf("vectors = %+v, want the candidate's only", got.Vectors)
	}
	if got.VectorIndexed {
		t.Error("VectorIndexed = true, want false: the default model failed")
	}
}

// With no default, ingest still dual-writes registered candidates, never
// marks the object vector-indexed, and warns once per process.
func TestEmbeddingGenerator_NoDefaultModel(t *testing.T) {
	logs := captureLogs(t)
	noDefaultWarning = sync.Once{}
	t.Cleanup(func() { noDefaultWarning = sync.Once{} })
	resolver, _ := ollamaResolver(t)
	step := NewEmbeddingGenerator(embeddingtest.Models{
		snowflake("snowflake-arctic-embed2@candidate", false),
	}, resolver)

	for i := 0; i < 2; i++ {
		got, err := step.Run(context.Background(), quokkaDraft())
		if err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
		if len(got.Vectors) != 1 || got.Vectors[0].ModelID != "snowflake-arctic-embed2@candidate" {
			t.Fatalf("run %d: vectors = %+v, want the candidate's", i, got.Vectors)
		}
		if got.VectorIndexed {
			t.Errorf("run %d: VectorIndexed = true with no default model", i)
		}
	}
	if n := strings.Count(logs.String(), "no default embedding model"); n != 1 {
		t.Errorf("no-default warning logged %d times, want once per process; logs:\n%s", n, logs.String())
	}
}

func TestEmbeddingGenerator_NoModelsWritesNothing(t *testing.T) {
	captureLogs(t)
	resolver, calls := ollamaResolver(t)
	got, err := NewEmbeddingGenerator(embeddingtest.Models{}, resolver).Run(context.Background(), quokkaDraft())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(got.Vectors) != 0 || got.VectorIndexed {
		t.Errorf("vectors=%d indexed=%v, want none with an empty registry", len(got.Vectors), got.VectorIndexed)
	}
	if urls := calls.URLs(); len(urls) != 0 {
		t.Errorf("provider called %v with no populating model", urls)
	}
}

func TestEmbeddingGenerator_DimensionMismatchSurfaced(t *testing.T) {
	logs := captureLogs(t)
	resolver, _ := ollamaResolver(t)
	wrong := snowflake("snowflake-arctic-embed2@768", true)
	wrong.Dimension = 768
	step := NewEmbeddingGenerator(embeddingtest.Models{wrong}, resolver)

	got, err := step.Run(context.Background(), quokkaDraft())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(got.Vectors) != 0 {
		t.Fatalf("stored a %d-dim vector under a 768-dim model", len(got.Vectors[0].Vector))
	}
	if got.VectorIndexed {
		t.Error("VectorIndexed = true after a dimension mismatch")
	}
	out := logs.String()
	if !strings.Contains(out, "model_id="+wrong.ModelID) || !strings.Contains(out, storage.ErrEmbeddingDimension.Error()) ||
		!strings.Contains(out, "1024") || !strings.Contains(out, "768") {
		t.Errorf("mismatch not surfaced with model, both dimensions and ErrEmbeddingDimension; logs:\n%s", out)
	}
}

// A re-run (reanalyze, or a hop through a second pipeline) replaces the
// draft's vectors rather than appending to them.
func TestEmbeddingGenerator_ReplacesPriorVectors(t *testing.T) {
	resolver, _ := ollamaResolver(t)
	step := NewEmbeddingGenerator(embeddingtest.Models{snowflake("snowflake-arctic-embed2@default", true)}, resolver)
	draft := quokkaDraft()
	draft.Vectors = []pluginapi.ObjectVector{{ModelID: "stale@1", Vector: []float32{1}}}

	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(got.Vectors) != 1 || got.Vectors[0].ModelID != "snowflake-arctic-embed2@default" {
		t.Fatalf("vectors = %+v, want only this run's", got.Vectors)
	}
}

func TestEmbeddingGenerator_NilDependenciesAreANoOp(t *testing.T) {
	resolver, calls := ollamaResolver(t)
	models := embeddingtest.Models{snowflake("snowflake-arctic-embed2@default", true)}
	for name, step := range map[string]*EmbeddingGenerator{
		"nil models":   NewEmbeddingGenerator(nil, resolver),
		"nil resolver": NewEmbeddingGenerator(models, nil),
	} {
		t.Run(name, func(t *testing.T) {
			got, err := step.Run(context.Background(), quokkaDraft())
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			if len(got.Vectors) != 0 || got.VectorIndexed {
				t.Errorf("vectors=%d indexed=%v, want a no-op", len(got.Vectors), got.VectorIndexed)
			}
		})
	}
	if urls := calls.URLs(); len(urls) != 0 {
		t.Errorf("provider called %v", urls)
	}
}

// The embedding text is projection.ProjectIndex(draft).EmbeddingText:
// RawContent alone projects to nothing, so the step embeds nothing.
func TestEmbeddingGenerator_EmptyEmbeddingTextSkips(t *testing.T) {
	resolver, calls := ollamaResolver(t)
	step := NewEmbeddingGenerator(embeddingtest.Models{snowflake("snowflake-arctic-embed2@default", true)}, resolver)

	for name, draft := range map[string]*storage.KnowledgeObject{
		"empty":    {},
		"raw only": {RawContent: "only raw content, not indexed"},
	} {
		t.Run(name, func(t *testing.T) {
			if projection.ProjectIndex(draft).EmbeddingText != "" {
				t.Fatal("fixture projects to non-empty embedding text")
			}
			got, err := step.Run(context.Background(), draft)
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			if len(got.Vectors) != 0 || got.VectorIndexed {
				t.Errorf("vectors=%d indexed=%v, want nothing embedded", len(got.Vectors), got.VectorIndexed)
			}
		})
	}
	if urls := calls.URLs(); len(urls) != 0 {
		t.Errorf("provider called %v for empty embedding text", urls)
	}
}

// Graph-canonical drafts embed the graph projection's text.
func TestEmbeddingGenerator_EmbedsGraphProjectionText(t *testing.T) {
	resolver, _ := ollamaResolver(t)
	step := NewEmbeddingGenerator(embeddingtest.Models{snowflake("snowflake-arctic-embed2@default", true)}, resolver)
	draft := &storage.KnowledgeObject{
		Summaries: []string{"flat summary the graph path ignores"},
		Graph: &pluginapi.ObjectGraph{Nodes: []pluginapi.GraphNode{{
			ID:       pluginapi.NewNodeID("t1", pluginapi.NodeTypeSummary, 0),
			NodeType: pluginapi.NodeTypeSummary,
			Content:  "graph embedding source text",
		}}},
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(got.Vectors) != 1 || got.Vectors[0].Text != "graph embedding source text" {
		t.Fatalf("vectors = %+v, want one vector of the graph text", got.Vectors)
	}
}
