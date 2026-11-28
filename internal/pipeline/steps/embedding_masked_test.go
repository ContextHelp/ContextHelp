package steps

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/embeddings"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings/embeddingtest"
	"github.com/ideacrafterslabs/ctxt/internal/embeddings/registry"
	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// textProvider embeds a text as a vector that encodes its length, so two
// texts yield two vectors and the test can tell which text each row
// holds.
type textProvider struct{ calls []string }

func (p *textProvider) Embed(_ context.Context, text string) ([]float32, error) {
	p.calls = append(p.calls, text)
	return []float32{float32(len(text)), 1}, nil
}
func (p *textProvider) Dimensions() int { return 2 }
func (p *textProvider) Name() string    { return "text" }

// fixedResolver hands every model the same provider.
type fixedResolver struct {
	embeddings.ProviderResolver
	p providers.EmbeddingProvider
}

func (r fixedResolver) ForModel(context.Context, registry.Model) (providers.EmbeddingProvider, error) {
	return r.p, nil
}

func maskedModels() embeddingtest.Models {
	return embeddingtest.Models{{ModelID: "fixture@1", Provider: "fixture", Dimension: 2, IsDefault: true}}
}

// classedDraft has a plain section and a personal one: its masked
// projection drops the personal section.
func classedDraft() *storage.KnowledgeObject {
	d := &storage.KnowledgeObject{ID: "x", Type: "note", RawContent: "card", Graph: &storage.ObjectGraph{Nodes: []storage.GraphNode{
		{ID: "x/section/0", NodeType: storage.NodeTypeSection, Content: "Sarah leads ops at Acme"},
		{ID: "x/section/1", NodeType: storage.NodeTypeSection, Content: "kids Leo and Mia, moving to Lisbon"},
	}}}
	storage.SetNodeClasses(&d.Graph.Nodes[1], []string{"personal"})
	return d
}

func variants(vs []storage.ObjectVector) string {
	s := ""
	for _, v := range vs {
		s += fmt.Sprintf("%s:%s|", v.EffectiveVariant(), v.Text)
	}
	return s
}

// With the masked variant on, an object with a classed part gets a raw
// and a masked vector per model, the masked one embedding the text
// without that part; the raw vector is unchanged.
func TestEmbeddingGenerator_MaskedVariant(t *testing.T) {
	p := &textProvider{}
	step := NewEmbeddingGenerator(maskedModels(), fixedResolver{p: p}, WithMaskedVariant(true))
	got, err := step.Run(context.Background(), classedDraft())
	if err != nil {
		t.Fatal(err)
	}
	want := "raw:Sarah leads ops at Acme\nkids Leo and Mia, moving to Lisbon|masked:Sarah leads ops at Acme|"
	if variants(got.Vectors) != want {
		t.Fatalf("vectors = %q, want %q", variants(got.Vectors), want)
	}
	if got.Vectors[0].Vector[0] == got.Vectors[1].Vector[0] {
		t.Error("masked vector embeds the raw text")
	}
	if len(p.calls) != 2 {
		t.Errorf("provider called %d times, want 2", len(p.calls))
	}
}

// Nothing withheld: one vector serves both variants, so only the raw
// one is produced.
func TestEmbeddingGenerator_MaskedVariantNotNeeded(t *testing.T) {
	p := &textProvider{}
	step := NewEmbeddingGenerator(maskedModels(), fixedResolver{p: p}, WithMaskedVariant(true))
	plain := &storage.KnowledgeObject{ID: "y", Type: "note", RawContent: "nothing to hide"}
	got, err := step.Run(context.Background(), plain)
	if err != nil {
		t.Fatal(err)
	}
	if variants(got.Vectors) != "raw:nothing to hide|" {
		t.Fatalf("vectors = %q", variants(got.Vectors))
	}
}

// Off (private instances), a classed part changes nothing.
func TestEmbeddingGenerator_MaskedVariantOff(t *testing.T) {
	p := &textProvider{}
	got, err := NewEmbeddingGenerator(maskedModels(), fixedResolver{p: p}).Run(context.Background(), classedDraft())
	if err != nil {
		t.Fatal(err)
	}
	if n := len(got.Vectors); n != 1 || got.Vectors[0].Variant != "" {
		t.Fatalf("vectors = %q", variants(got.Vectors))
	}
}

// failResolver cannot build any model's provider.
type failResolver struct{ embeddings.ProviderResolver }

func (failResolver) ForModel(context.Context, registry.Model) (providers.EmbeddingProvider, error) {
	return nil, fmt.Errorf("no such backend")
}

// secondCallFails embeds the first text and fails the next ones: the raw
// vector works, the masked one does not.
type secondCallFails struct{ n int }

func (p *secondCallFails) Embed(_ context.Context, text string) ([]float32, error) {
	p.n++
	if p.n > 1 {
		return nil, fmt.Errorf("model gone away")
	}
	return []float32{float32(len(text)), 1}, nil
}
func (p *secondCallFails) Dimensions() int { return 2 }
func (p *secondCallFails) Name() string    { return "flaky" }

// A model whose provider cannot be built is unusable, not a failed call:
// it is skipped at reanalysis as at ingest, and reports nothing. A failed
// masked vector falls back to the raw one, as at ingest.
func TestEmbeddingGenerator_OnlyAFailedCallOfABuiltProviderIsReported(t *testing.T) {
	ctx, calls := providers.WithCallRecorder(context.Background())
	step := NewEmbeddingGenerator(maskedModels(), failResolver{}, WithMaskedVariant(true))
	got, err := step.Run(ctx, classedDraft())
	if err != nil || len(got.Vectors) != 0 || calls.Err() != nil {
		t.Fatalf("unbuildable model: vectors %d, err %v, recorder %v", len(got.Vectors), err, calls.Err())
	}

	ctx, calls = providers.WithCallRecorder(context.Background())
	step = NewEmbeddingGenerator(maskedModels(), fixedResolver{p: &secondCallFails{}}, WithMaskedVariant(true))
	got, err = step.Run(ctx, classedDraft())
	if err != nil || len(got.Vectors) != 1 || got.Vectors[0].EffectiveVariant() == storage.EmbeddingVariantMasked || calls.Err() != nil {
		t.Fatalf("failed masked vector: vectors %s, err %v, recorder %v", variants(got.Vectors), err, calls.Err())
	}
}

// brokenModels cannot say which models to populate.
type brokenModels struct{ embeddingtest.Models }

func (brokenModels) Populating(context.Context, time.Time) ([]registry.Model, error) {
	return nil, fmt.Errorf("registry unreadable")
}

// A registry that cannot be read is a failed embedding at reanalysis (the
// object fails, nothing is written); at ingest, with no recorder, the step
// still returns no vectors and no error.
func TestEmbeddingGenerator_UnreadableRegistryIsReportedAtReanalysisOnly(t *testing.T) {
	step := NewEmbeddingGenerator(brokenModels{}, fixedResolver{p: &textProvider{}})
	ctx, calls := providers.WithCallRecorder(context.Background())
	got, err := step.Run(ctx, classedDraft())
	var ce *providers.CallError
	if err != nil || len(got.Vectors) != 0 || !errors.As(calls.Err(), &ce) || !strings.HasPrefix(ce.Provider, "embedding") {
		t.Fatalf("with a recorder: vectors %d, err %v, recorder %v", len(got.Vectors), err, calls.Err())
	}
	got, err = step.Run(context.Background(), classedDraft())
	if err != nil || len(got.Vectors) != 0 {
		t.Fatalf("without a recorder: vectors %d, err %v", len(got.Vectors), err)
	}
}

// A completed embed call is noted on the recorder; an object with nothing to
// embed makes no call, so it notes none.
func TestEmbeddingGenerator_NotesCompletedCalls(t *testing.T) {
	step := NewEmbeddingGenerator(maskedModels(), fixedResolver{p: &textProvider{}})
	ctx, calls := providers.WithCallRecorder(context.Background())
	if _, err := step.Run(ctx, classedDraft()); err != nil || calls.Calls()["embedding"] != 1 {
		t.Fatalf("with text: calls %v, err %v", calls.Calls(), err)
	}
	ctx, calls = providers.WithCallRecorder(context.Background())
	if _, err := step.Run(ctx, &storage.KnowledgeObject{ID: "e", Type: "note"}); err != nil || calls.Calls()["embedding"] != 0 {
		t.Fatalf("empty text: calls %v, err %v", calls.Calls(), err)
	}
}
