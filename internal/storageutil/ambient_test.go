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
		"ambient_source": "clipboard", "fingerprint": "sha256:abc", "app_name": "Terminal",
	}
	if !reflect.DeepEqual(obj.Metadata, want) {
		t.Errorf("metadata %v, want %v", obj.Metadata, want)
	}
	if obj.SessionID != "sess_1" {
		t.Errorf("session id %q", obj.SessionID)
	}
}

// Server wins: client metadata never replaces an ambient key, a reserved
// key, or a key already on the object.
func TestApplyAmbientServerKeysWin(t *testing.T) {
	obj := &storage.KnowledgeObject{Metadata: map[string]any{
		"data": map[string]any{"k": "v"}, "title": "from-step",
	}}
	ApplyAmbient(obj, &storage.AmbientContext{
		Source: "clipboard", Fingerprint: "fp",
		Metadata: map[string]any{
			"ambient_source": "spoof", "fingerprint": "spoof", "data": "spoof",
			"next_pipeline": "evil", "forge": "x", "forge_id": "y",
			"title": "spoof", "extra": 1,
		},
	})
	want := map[string]any{
		"data": map[string]any{"k": "v"}, "title": "from-step",
		"ambient_source": "clipboard", "fingerprint": "fp", "extra": 1,
	}
	if !reflect.DeepEqual(obj.Metadata, want) {
		t.Errorf("metadata %v, want %v", obj.Metadata, want)
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
