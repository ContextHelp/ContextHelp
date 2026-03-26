package http

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestListEntities(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	now := time.Now().Truncate(time.Second)
	ctx := context.Background()
	ts.svc.Store.Entities().Upsert(ctx, &storage.Entity{
		Slug: "@ui.layout", Title: "UI Layout", CreatedAt: now, UpdatedAt: now,
	})
	ts.svc.Store.Entities().Upsert(ctx, &storage.Entity{
		Slug: "@ui.button", Title: "UI Button", CreatedAt: now, UpdatedAt: now,
	})

	resp, err := http.Get(ts.URL + "/api/v1/entities")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want 200", resp.StatusCode)
	}

	var body struct {
		Data []storage.Entity `json:"data"`
	}
	json.NewDecoder(resp.Body).Decode(&body)
	if len(body.Data) != 2 {
		t.Errorf("count: got %d, want 2", len(body.Data))
	}
}

func TestGetEntity(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	now := time.Now().Truncate(time.Second)
	ts.svc.Store.Entities().Upsert(context.Background(), &storage.Entity{
		Slug: "@ui.layout", Title: "UI Layout", CreatedAt: now, UpdatedAt: now,
	})

	resp, err := http.Get(ts.URL + "/api/v1/entities/@ui.layout")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want 200", resp.StatusCode)
	}

	var entity storage.Entity
	json.NewDecoder(resp.Body).Decode(&entity)
	if entity.Slug != "@ui.layout" {
		t.Errorf("slug: got %q", entity.Slug)
	}
}

func TestEntityBacklinks(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	now := time.Now().Truncate(time.Second)
	ctx := context.Background()

	ts.svc.Store.Objects().Create(ctx, &storage.KnowledgeObject{
		ID: "obj-1", Type: "article", CreatedAt: now, UpdatedAt: now,
	})
	ts.svc.Store.Edges().Create(ctx, &storage.Edge{
		ID:       "edge-1",
		FromType: "object", FromID: "obj-1",
		ToType: "entity", ToID: "ctxt://entity/ui/layout",
		EdgeType: "mentions", Weight: 1.0, CreatedAt: now,
	})

	resp, err := http.Get(ts.URL + "/api/v1/entities/@ui.layout/backlinks")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want 200", resp.StatusCode)
	}

	var body struct {
		Data []storage.KnowledgeObject `json:"data"`
	}
	json.NewDecoder(resp.Body).Decode(&body)
	if len(body.Data) != 1 {
		t.Errorf("backlinks: got %d, want 1", len(body.Data))
	}
}
