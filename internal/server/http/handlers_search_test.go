package http

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestSearch(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	now := time.Now().Truncate(time.Second)
	ts.svc.Store.Objects().Create(context.Background(), &storage.KnowledgeObject{
		ID: "obj-1", Type: "article", CreatedAt: now, UpdatedAt: now,
	})
	ts.svc.Store.Objects().Create(context.Background(), &storage.KnowledgeObject{
		ID: "obj-2", Type: "note", CreatedAt: now, UpdatedAt: now,
	})

	resp, err := http.Get(ts.URL + "/api/v1/search?q=type==article")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want 200", resp.StatusCode)
	}

	var body struct {
		Data  []storage.KnowledgeObject `json:"data"`
		Total int                       `json:"total"`
	}
	json.NewDecoder(resp.Body).Decode(&body)
	if body.Total != 1 {
		t.Errorf("total: got %d, want 1", body.Total)
	}
}

func TestSearchInvalidQuery(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/search?q===bad")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", resp.StatusCode)
	}
}

func TestSearchPagination(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	now := time.Now().Truncate(time.Second)
	for i := 0; i < 5; i++ {
		ts.svc.Store.Objects().Create(context.Background(), &storage.KnowledgeObject{
			ID: "obj-" + string(rune('a'+i)), Type: "article", CreatedAt: now, UpdatedAt: now,
		})
	}

	resp, err := http.Get(ts.URL + "/api/v1/search?q=type==article&limit=2&offset=1")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	var body struct {
		Data  []storage.KnowledgeObject `json:"data"`
		Total int                       `json:"total"`
	}
	json.NewDecoder(resp.Body).Decode(&body)
	if body.Total != 5 {
		t.Errorf("total: got %d, want 5", body.Total)
	}
	if len(body.Data) != 2 {
		t.Errorf("data len: got %d, want 2", len(body.Data))
	}
}
