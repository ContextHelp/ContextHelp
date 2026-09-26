package steps

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/plugins/ica"
)

func TestICAProcessorSuccess(t *testing.T) {
	respItem := ica.NormalizedItem{
		Embedding: []float32{0.1, 0.2, 0.3},
		Title:     "Enriched Title",
		StructuredAnalysis: &ica.StructuredAnalysis{
			Chapters: []ica.Chapter{
				{
					ID:               "ch1",
					Phase:            "intro",
					VoiceoverSummary: "opening",
				},
			},
		},
	}

	srv := httptest.NewServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/process" {
					t.Errorf("path: got %s", r.URL.Path)
				}
				if ct := r.Header.Get("Content-Type"); ct !=
					"application/json" {
					t.Errorf("content-type: got %s", ct)
				}
				resp := processorResponse{
					Item:       respItem,
					IsNewStory: true,
					Status:     "ok",
				}
				json.NewEncoder(w).Encode(resp)
			},
		),
	)
	defer srv.Close()

	step := NewICAProcessor(srv.URL, srv.Client())
	draft := &storage.KnowledgeObject{
		RawContent: "some raw content",
		Metadata:   map[string]any{"existing": "kept"},
	}

	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	// ICA's vector is from a model outside the registry: not merged.
	if len(got.Vectors) != 0 {
		t.Errorf("vectors: got %d, want none", len(got.Vectors))
	}

	// existing metadata preserved
	if got.Metadata["existing"] != "kept" {
		t.Error("existing metadata lost")
	}

	// enriched metadata merged
	if got.Metadata["title"] != "Enriched Title" {
		t.Errorf(
			"title metadata: got %v",
			got.Metadata["title"],
		)
	}

	// is_new_story flag
	if got.Metadata["ica_is_new_story"] != true {
		t.Errorf(
			"ica_is_new_story: got %v",
			got.Metadata["ica_is_new_story"],
		)
	}

	// sections appended
	if len(got.Sections) != 1 {
		t.Fatalf(
			"sections: got %d, want 1", len(got.Sections),
		)
	}
	if got.Sections[0].Title != "intro" {
		t.Errorf(
			"section title: got %s", got.Sections[0].Title,
		)
	}
}

// The processor re-embeds every item with its own model and never reads
// an incoming vector, so the draft's registry vectors stay local.
func TestICAProcessorRequestCarriesNoVector(t *testing.T) {
	srv := httptest.NewServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				var sent ica.NormalizedItem
				if err := json.NewDecoder(r.Body).Decode(&sent); err != nil {
					t.Errorf("decode request: %v", err)
				}
				if len(sent.Embedding) != 0 {
					t.Errorf("request embedding: got %v, want none", sent.Embedding)
				}
				json.NewEncoder(w).Encode(processorResponse{Status: "ok"})
			},
		),
	)
	defer srv.Close()

	draft := &storage.KnowledgeObject{
		RawContent: "some raw content",
		Vectors: []storage.ObjectVector{
			{ModelID: "registry-model", Vector: []float32{0.4, 0.5}},
		},
	}
	if _, err := NewICAProcessor(srv.URL, srv.Client()).Run(
		context.Background(), draft,
	); err != nil {
		t.Fatalf("run: %v", err)
	}
}

func TestICAProcessorRetryOn500(t *testing.T) {
	var calls atomic.Int32

	srv := httptest.NewServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, _ *http.Request) {
				n := calls.Add(1)
				if n == 1 {
					w.WriteHeader(
						http.StatusInternalServerError,
					)
					w.Write([]byte("boom"))
					return
				}
				resp := processorResponse{
					Item:   ica.NormalizedItem{},
					Status: "ok",
				}
				json.NewEncoder(w).Encode(resp)
			},
		),
	)
	defer srv.Close()

	step := NewICAProcessor(srv.URL, srv.Client())
	draft := &storage.KnowledgeObject{
		RawContent: "content",
	}

	_, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("expected success after retry: %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("calls: got %d, want 2", got)
	}
}

func TestICAProcessorPermanentOn400(t *testing.T) {
	srv := httptest.NewServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte("bad input"))
			},
		),
	)
	defer srv.Close()

	step := NewICAProcessor(srv.URL, srv.Client())
	draft := &storage.KnowledgeObject{
		RawContent: "content",
	}

	_, err := step.Run(context.Background(), draft)
	if err == nil {
		t.Fatal("expected error for 400")
	}
	if !pipeline.IsPermanent(err) {
		t.Errorf("expected permanent error, got: %v", err)
	}
}

func TestICAProcessorNetworkTimeout(t *testing.T) {
	srv := httptest.NewServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, _ *http.Request) {
				time.Sleep(2 * time.Second)
			},
		),
	)
	defer srv.Close()

	client := srv.Client()
	client.Timeout = 10 * time.Millisecond

	step := NewICAProcessor(srv.URL, client)
	draft := &storage.KnowledgeObject{
		RawContent: "content",
	}

	_, err := step.Run(context.Background(), draft)
	if err == nil {
		t.Fatal("expected error for timeout")
	}
}

func TestICAProcessorEmptyDraft(t *testing.T) {
	srv := httptest.NewServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, _ *http.Request) {
				resp := processorResponse{
					Item:   ica.NormalizedItem{},
					Status: "ok",
				}
				json.NewEncoder(w).Encode(resp)
			},
		),
	)
	defer srv.Close()

	step := NewICAProcessor(srv.URL, srv.Client())
	draft := &storage.KnowledgeObject{}

	got, err := step.Run(context.Background(), draft)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil draft")
	}
}
