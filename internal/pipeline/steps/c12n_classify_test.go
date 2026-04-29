package steps

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestC12nClassifierName(t *testing.T) {
	step := NewC12nClassifier()
	if step.Name() != "c12n_classify" {
		t.Errorf("name: got %q, want %q", step.Name(), "c12n_classify")
	}
}

func TestC12nClassifierContract(t *testing.T) {
	step := NewC12nClassifier()
	c := step.Contract()
	if len(c.Requires) == 0 || c.Requires[0] != "RawContent" {
		t.Errorf("requires: got %v, want [RawContent]", c.Requires)
	}
	if len(c.Produces) == 0 || c.Produces[0] != "Metadata" {
		t.Errorf("produces: got %v, want [Metadata]", c.Produces)
	}
}

func TestC12nClassifierGracefulFallback(t *testing.T) {
	// Whatever the build environment (cgo on/off, Rust lib present/absent),
	// Run must never return an error and must always set a c12n_status.
	// When the pipeline cannot classify (unavailable/error), the enrichment
	// key must NOT be set so downstream steps don't pick up stale data.
	step := NewC12nClassifier()
	draft := &storage.KnowledgeObject{
		RawContent: "Some content to classify",
	}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	status, ok := got.Metadata["c12n_status"]
	if !ok {
		t.Fatal("expected c12n_status in metadata")
	}
	switch status {
	case "complete":
		if _, exists := got.Metadata["enrichment.c12n_signals"]; !exists {
			t.Error("enrichment.c12n_signals must be set when c12n_status=complete")
		}
	case "unavailable", "error":
		if _, exists := got.Metadata["enrichment.c12n_signals"]; exists {
			t.Errorf("enrichment.c12n_signals must not be set when c12n_status=%v", status)
		}
	default:
		t.Errorf("unexpected c12n_status %v (want one of complete/unavailable/error)", status)
	}
}

func TestC12nClassifierEmptyContent(t *testing.T) {
	step := NewC12nClassifier()
	draft := &storage.KnowledgeObject{RawContent: "   "}
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Empty content path is reached only if pipeline init succeeds.
	// Without cgo, we hit "unavailable" first. Either status is acceptable.
	status := got.Metadata["c12n_status"]
	if status != "skipped" && status != "unavailable" {
		t.Errorf("expected c12n_status skipped or unavailable, got %v", status)
	}
}

func TestC12nClassifierNilMetadata(t *testing.T) {
	step := NewC12nClassifier()
	draft := &storage.KnowledgeObject{RawContent: "test"}
	// Metadata starts nil; step must initialize it.
	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Metadata == nil {
		t.Error("metadata should be initialized, not nil")
	}
}

func TestC12nClassifierIdempotent(t *testing.T) {
	step := NewC12nClassifier()
	draft := &storage.KnowledgeObject{RawContent: "content"}
	got1, _ := step.Run(context.Background(), draft)
	status1 := got1.Metadata["c12n_status"]

	draft.Metadata = nil
	got2, _ := step.Run(context.Background(), draft)
	status2 := got2.Metadata["c12n_status"]

	if status1 != status2 {
		t.Errorf("idempotency: status mismatch %v vs %v", status1, status2)
	}
}
