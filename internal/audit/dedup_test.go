package audit

import (
	"maps"
	"testing"
)

func TestDedupEntry(t *testing.T) {
	for name, tc := range map[string]struct {
		d         DedupDecision
		wantEvent string
		want      map[string]any
	}{
		"similar carries the model": {
			d:         DedupDecision{ObjectID: "new", DuplicateOf: "old", Similarity: 0.97, Kind: DedupSimilar, Policy: "warn", ModelID: "m@1"},
			wantEvent: "dedup.similar",
			want:      map[string]any{"duplicate_of": "old", "similarity": 0.97, "kind": "similar", "policy": "warn", "model_id": "m@1"},
		},
		"exact has no model": {
			d:         DedupDecision{ObjectID: "old", DuplicateOf: "old", Similarity: 1, Kind: DedupExact, Policy: "drop"},
			wantEvent: "dedup.exact",
			want:      map[string]any{"duplicate_of": "old", "similarity": 1.0, "kind": "exact", "policy": "drop"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			e := DedupEntry(tc.d)
			if e.ID == "" || e.CreatedAt.IsZero() {
				t.Errorf("entry id=%q created_at=%v, want both set", e.ID, e.CreatedAt)
			}
			if e.EventType != tc.wantEvent || e.ObjectID != tc.d.ObjectID || e.Actor != DedupActor {
				t.Errorf("event=%q object=%q actor=%q, want %q %q %q", e.EventType, e.ObjectID, e.Actor, tc.wantEvent, tc.d.ObjectID, DedupActor)
			}
			if !maps.Equal(e.Payload, tc.want) {
				t.Errorf("payload = %v, want %v", e.Payload, tc.want)
			}
		})
	}
}
