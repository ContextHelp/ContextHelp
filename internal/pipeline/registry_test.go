package pipeline

import (
	"strings"
	"testing"
)

func TestRegisterAndGet(t *testing.T) {
	r := NewRegistry()
	p := &Pipeline{PipelineName: "test", Description: "test pipeline"}
	if err := r.Register("test", p); err != nil {
		t.Fatalf("register: %v", err)
	}

	got, err := r.Get("test")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.PipelineName != "test" {
		t.Errorf("name: got %q", got.PipelineName)
	}
}

func TestGetNotFound(t *testing.T) {
	r := NewRegistry()
	_, err := r.Get("nonexistent")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRegistryList(t *testing.T) {
	r := NewRegistry()
	r.Register("b", &Pipeline{PipelineName: "b"})
	r.Register("a", &Pipeline{PipelineName: "a"})

	names := r.List()
	if len(names) != 2 {
		t.Fatalf("count: got %d", len(names))
	}
	if names[0] != "a" || names[1] != "b" {
		t.Errorf("order: got %v", names)
	}
}

func TestSelectPipelineShort(t *testing.T) {
	r := NewRegistry()
	name := r.SelectPipeline("short text")
	if name != "text.short" {
		t.Errorf("got %q, want text.short", name)
	}
}

func TestSelectPipelineLong(t *testing.T) {
	r := NewRegistry()
	name := r.SelectPipeline(strings.Repeat("word ", 200))
	if name != "text.long" {
		t.Errorf("got %q, want text.long", name)
	}
}

func TestDefaultRegistry(t *testing.T) {
	r := DefaultRegistry()
	names := r.List()

	hasShort, hasLong := false, false
	for _, n := range names {
		if n == "text.short" {
			hasShort = true
		}
		if n == "text.long" {
			hasLong = true
		}
	}

	if !hasShort {
		t.Error("missing text.short")
	}
	if !hasLong {
		t.Error("missing text.long")
	}
}
