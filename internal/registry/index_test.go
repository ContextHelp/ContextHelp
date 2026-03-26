package registry_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/registry"
)

func makeTestIndex() *registry.RegistryIndex {
	return &registry.RegistryIndex{
		Version: "1.0",
		Registries: []registry.RegistryIndexEntry{
			{
				Name:        "AI Registry",
				URL:         "https://ai.example.com",
				Description: "Machine learning and AI entities",
				Namespaces:  []string{"ai", "ml"},
				Tags:        []string{"ai", "ml", "llm"},
				TrustLevel:  "trusted",
				EntityCount: 500,
			},
			{
				Name:        "Frontend Patterns",
				URL:         "https://frontend.example.com",
				Description: "React, Vue, and web UI patterns",
				Namespaces:  []string{"ui", "react", "frontend"},
				Tags:        []string{"web", "frontend", "ui"},
				TrustLevel:  "trusted",
				EntityCount: 200,
			},
			{
				Name:        "DevOps Tools",
				URL:         "https://devops.example.com",
				Description: "CI/CD, containers, and orchestration",
				Namespaces:  []string{"devops", "k8s", "docker"},
				Tags:        []string{"devops", "cicd", "containers"},
				TrustLevel:  "untrusted",
				EntityCount: 150,
			},
		},
	}
}

func TestSearchIndex_NamespacePrefix_ReturnsMatch(t *testing.T) {
	idx := makeTestIndex()
	results := registry.SearchIndex(idx, "ai")

	if len(results) == 0 {
		t.Fatal("expected results for 'ai' query")
	}
	if results[0].Entry.Name != "AI Registry" {
		t.Errorf("top result: got %q, want %q", results[0].Entry.Name, "AI Registry")
	}
}

func TestSearchIndex_TagMatch_ReturnsMatch(t *testing.T) {
	idx := makeTestIndex()
	results := registry.SearchIndex(idx, "frontend")

	if len(results) == 0 {
		t.Fatal("expected results for 'frontend' query")
	}
	found := false
	for _, r := range results {
		if r.Entry.URL == "https://frontend.example.com" {
			found = true
		}
	}
	if !found {
		t.Error("Frontend Patterns registry not found in results")
	}
}

func TestSearchIndex_DescriptionMatch_ReturnsMatch(t *testing.T) {
	idx := makeTestIndex()
	results := registry.SearchIndex(idx, "orchestration")

	if len(results) == 0 {
		t.Fatal("expected results for 'orchestration' query")
	}
	if results[0].Entry.Name != "DevOps Tools" {
		t.Errorf("expected DevOps Tools, got %q", results[0].Entry.Name)
	}
}

func TestSearchIndex_NoMatch_ReturnsEmpty(t *testing.T) {
	idx := makeTestIndex()
	results := registry.SearchIndex(idx, "xyznomatch")
	if len(results) != 0 {
		t.Errorf("expected empty results, got %d", len(results))
	}
}

func TestSearchIndex_EmptyQuery_ReturnsNil(t *testing.T) {
	idx := makeTestIndex()
	results := registry.SearchIndex(idx, "")
	if results != nil {
		t.Error("empty query should return nil")
	}
}

func TestSearchIndex_NilIndex_ReturnsNil(t *testing.T) {
	results := registry.SearchIndex(nil, "ai")
	if results != nil {
		t.Error("nil index should return nil")
	}
}

func TestFetchIndex_HappyPath(t *testing.T) {
	idx := makeTestIndex()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(idx)
	}))
	defer srv.Close()

	fetched, err := registry.FetchIndex(context.Background(), nil, srv.URL)
	if err != nil {
		t.Fatalf("FetchIndex: %v", err)
	}
	if len(fetched.Registries) != 3 {
		t.Errorf("registry count: got %d, want 3", len(fetched.Registries))
	}
	if fetched.FetchedAt.IsZero() {
		t.Error("FetchedAt should be set after fetch")
	}
}

func TestFetchIndex_ServerError_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := registry.FetchIndex(context.Background(), nil, srv.URL)
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}
