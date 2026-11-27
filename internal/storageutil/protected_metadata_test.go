package storageutil

import (
	"reflect"
	"testing"
)

func TestRestoreProtectedMetadata(t *testing.T) {
	stored := map[string]any{
		MetadataDataKey: map[string]any{"k": []any{"v"}}, MetadataClientKey: map[string]any{"a": 1}, "title": "old",
	}
	md := map[string]any{MetadataDataKey: "step", MetadataFingerprintKey: "step", "title": "new"}
	got := RestoreProtectedMetadata(md, CloneMetadata(stored))
	want := map[string]any{MetadataDataKey: stored[MetadataDataKey], MetadataClientKey: stored[MetadataClientKey], "title": "new"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	got[MetadataDataKey].(map[string]any)["k"].([]any)[0] = "changed"
	if stored[MetadataDataKey].(map[string]any)["k"].([]any)[0] != "v" {
		t.Error("restored value shares memory with the stored one")
	}
}
