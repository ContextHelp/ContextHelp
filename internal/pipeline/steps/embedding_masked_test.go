package steps

import (
	"context"
	"fmt"
	"testing"

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
