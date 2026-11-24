// Package representationtest holds test helpers for code that produces
// representation nodes: fake providers with fixed output, and the provenance
// invariants every media object's graph must hold. Import from _test.go
// files only.
package representationtest

import (
	"context"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/providers"
	"github.com/ideacrafterslabs/ctxt/internal/providers/vision"
	"github.com/ideacrafterslabs/ctxt/internal/representation"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob"
)

// RequireProvenance fails t unless g holds the stored original as artifact 0
// with a blob key, at least one representation node, unique node IDs, and
// for every representation node exactly one derives_from edge, to that
// original, starting at the node.
func RequireProvenance(t *testing.T, objectID string, g *storage.ObjectGraph) {
	t.Helper()
	if g == nil {
		t.Fatal("no graph")
	}
	seen := map[string]bool{}
	for _, n := range g.Nodes {
		if seen[n.ID] {
			t.Errorf("duplicate node id %q", n.ID)
		}
		seen[n.ID] = true
	}
	artifactID := storage.NewNodeID(objectID, storage.NodeTypeArtifact, 0)
	art := g.FindNode(artifactID)
	if art == nil {
		t.Fatalf("no artifact/0 %q", artifactID)
	}
	if k, _ := art.Metadata[blob.MetaBlobKey].(string); k == "" {
		t.Errorf("artifact/0 has no blob_key: %v", art.Metadata)
	}
	reps := 0
	for _, n := range g.Nodes {
		if _, ok := representation.MetaOf(n); !ok {
			continue
		}
		reps++
		edges := 0
		for _, e := range g.Edges {
			if e.EdgeType != storage.EdgeTypeDerivedFrom || e.FromID != n.ID {
				continue
			}
			edges++
			if e.ToID != artifactID {
				t.Errorf("%s derives_from %s, want the original %s", n.ID, e.ToID, artifactID)
			}
		}
		if edges != 1 {
			t.Errorf("%s has %d derives_from edges, want 1", n.ID, edges)
		}
	}
	if reps == 0 {
		t.Error("no representation nodes")
	}
}

// OCR returns fixed text.
type OCR struct{}

func (OCR) Name() string { return "fakeocr" }
func (OCR) Extract(context.Context, []byte, string) (*providers.OCRResult, error) {
	return &providers.OCRResult{Text: "text in the image", Confidence: 0.9}, nil
}

// Vision returns a fixed caption.
type Vision struct{}

func (Vision) Name() string { return "fakevision" }
func (Vision) Analyze(context.Context, []byte, string) (*vision.Result, error) {
	return &vision.Result{Description: "a cat on a chair", Confidence: 0.8}, nil
}

// Transcriber returns two fixed segments.
type Transcriber struct{}

func (Transcriber) Name() string { return "faketranscriber" }
func (Transcriber) Transcribe(context.Context, string, providers.TranscribeOptions) (*providers.TranscriptResult, error) {
	return &providers.TranscriptResult{
		FullText: "first words second words",
		Segments: []providers.TranscriptSegment{
			{StartTime: 0, EndTime: 2 * time.Second, Text: "first words"},
			{StartTime: 2 * time.Second, EndTime: 4 * time.Second, Text: "second words"},
		},
	}, nil
}
