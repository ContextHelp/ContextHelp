package http

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// TestListObjectsEmptyCollection ensures empty DB returns [] not null (T-0219).
func TestListObjectsEmptyCollection(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/objects?limit=20")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want 200", resp.StatusCode)
	}

	var raw map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if string(raw["data"]) == "null" {
		t.Errorf("data: got null, want empty array []")
	}
	if string(raw["data"]) != "[]" {
		t.Errorf("data: got %s, want []", raw["data"])
	}
}

func TestGetObject(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	now := time.Now().Truncate(time.Second)
	ts.svc.Store.Objects().Create(context.Background(), &storage.KnowledgeObject{
		ID: "obj-1", Type: "article", CreatedAt: now, UpdatedAt: now,
	})

	resp, err := http.Get(ts.URL + "/api/v1/objects/obj-1")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want 200", resp.StatusCode)
	}

	var obj storage.KnowledgeObject
	json.NewDecoder(resp.Body).Decode(&obj)
	if obj.ID != "obj-1" {
		t.Errorf("ID: got %q", obj.ID)
	}
}

func TestGetObjectNotFound(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/v1/objects/nonexistent")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status: got %d, want 404", resp.StatusCode)
	}

	var env ErrorEnvelope
	json.NewDecoder(resp.Body).Decode(&env)
	if env.Error.Code != "NOT_FOUND" {
		t.Errorf("code: got %q", env.Error.Code)
	}
}

func TestListObjects(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	now := time.Now().Truncate(time.Second)
	for _, id := range []string{"obj-a", "obj-b", "obj-c"} {
		ts.svc.Store.Objects().Create(context.Background(), &storage.KnowledgeObject{
			ID: id, Type: "article", CreatedAt: now, UpdatedAt: now,
		})
	}

	resp, err := http.Get(ts.URL + "/api/v1/objects")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d", resp.StatusCode)
	}

	var body struct {
		Data  []storage.KnowledgeObject `json:"data"`
		Total int                       `json:"total"`
	}
	json.NewDecoder(resp.Body).Decode(&body)
	if body.Total != 3 {
		t.Errorf("total: got %d, want 3", body.Total)
	}
}

func TestListObjectsWithFilter(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	now := time.Now().Truncate(time.Second)
	ts.svc.Store.Objects().Create(context.Background(), &storage.KnowledgeObject{
		ID: "obj-1", Type: "article", CreatedAt: now, UpdatedAt: now,
	})
	ts.svc.Store.Objects().Create(context.Background(), &storage.KnowledgeObject{
		ID: "obj-2", Type: "note", CreatedAt: now, UpdatedAt: now,
	})

	resp, err := http.Get(ts.URL + "/api/v1/objects?type=article")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	var body struct {
		Data  []storage.KnowledgeObject `json:"data"`
		Total int                       `json:"total"`
	}
	json.NewDecoder(resp.Body).Decode(&body)
	if body.Total != 1 {
		t.Errorf("total: got %d, want 1", body.Total)
	}
}

func TestUpdateObject(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	now := time.Now().Truncate(time.Second)
	ts.svc.Store.Objects().Create(context.Background(), &storage.KnowledgeObject{
		ID: "obj-1", Type: "article", CreatedAt: now, UpdatedAt: now,
	})

	patch := map[string]string{"type": "note"}
	body, _ := json.Marshal(patch)

	req, _ := http.NewRequest(http.MethodPatch, ts.URL+"/api/v1/objects/obj-1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want 200", resp.StatusCode)
	}

	var obj storage.KnowledgeObject
	json.NewDecoder(resp.Body).Decode(&obj)
	if obj.Type != "note" {
		t.Errorf("type: got %q, want note", obj.Type)
	}
}

func TestDeleteObject(t *testing.T) {
	ts := newTestServerBundle(t)
	defer ts.Close()

	now := time.Now().Truncate(time.Second)
	ts.svc.Store.Objects().Create(context.Background(), &storage.KnowledgeObject{
		ID: "obj-1", Type: "article", CreatedAt: now, UpdatedAt: now,
	})

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/objects/obj-1", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("status: got %d, want 204", resp.StatusCode)
	}
}
