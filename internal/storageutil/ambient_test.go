package storageutil

import (
	"reflect"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestApplyAmbientLandsFields(t *testing.T) {
	obj := &storage.KnowledgeObject{}
	ApplyAmbient(obj, &storage.AmbientContext{
		Source: "clipboard", Fingerprint: "sha256:abc", SessionID: "sess_1",
		Metadata: map[string]any{"app_name": "Terminal"},
	})
	want := map[string]any{
		"ambient_source": "clipboard", "fingerprint": "sha256:abc",
		"client": map[string]any{"app_name": "Terminal"},
	}
	if !reflect.DeepEqual(obj.Metadata, want) {
		t.Errorf("metadata %v, want %v", obj.Metadata, want)
	}
	if obj.SessionID != "sess_1" {
		t.Errorf("session id %q", obj.SessionID)
	}
}

// Client metadata is isolated: no client key reaches the top level, so
// none can collide with a key the server or a step owns.
func TestApplyAmbientClientMetadataIsolated(t *testing.T) {
	obj := &storage.KnowledgeObject{Metadata: map[string]any{"title": "from-step"}}
	client := map[string]any{
		"ambient_source": "spoof", "fingerprint": "spoof", "title": "spoof",
		"suppress_output": true, "duplicate_of": "x", "blob_key": "k", "items_to_enqueue": 1,
	}
	ApplyAmbient(obj, &storage.AmbientContext{Source: "clipboard", Metadata: client})
	for k := range client {
		if k == "ambient_source" || k == "title" {
			continue
		}
		if _, top := obj.Metadata[k]; top {
			t.Errorf("client key %q reached top-level metadata", k)
		}
	}
	if obj.Metadata["ambient_source"] != "clipboard" || obj.Metadata["title"] != "from-step" {
		t.Errorf("server keys changed: %v", obj.Metadata)
	}
	if !reflect.DeepEqual(obj.Metadata["client"], client) {
		t.Errorf("client map %v", obj.Metadata["client"])
	}
}

func TestApplyAmbientEmptyIsNoop(t *testing.T) {
	obj := &storage.KnowledgeObject{}
	ApplyAmbient(obj, nil)
	ApplyAmbient(obj, &storage.AmbientContext{})
	if obj.Metadata != nil || obj.SessionID != "" {
		t.Errorf("changed: %+v", obj)
	}
}

func TestCarryAndStripAmbient(t *testing.T) {
	from := &storage.KnowledgeObject{SessionID: "s", Metadata: map[string]any{
		"ambient_source": "a", "fingerprint": "f", "client": map[string]any{"k": 1}, "title": "t",
	}}
	to := &storage.KnowledgeObject{}
	CarryAmbient(to, from)
	if to.SessionID != "s" || len(to.Metadata) != 3 || to.Metadata["title"] != nil {
		t.Errorf("carried %+v", to)
	}
	got := WithoutAmbient(from.Metadata)
	if !reflect.DeepEqual(got, map[string]any{"title": "t"}) {
		t.Errorf("stripped %v", got)
	}
	if len(from.Metadata) != 4 {
		t.Errorf("input modified")
	}
}
