// Package audit builds and exports audit log entries.
package audit

import (
	"time"

	"github.com/google/uuid"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// DedupActor is the actor recorded on every dedup decision.
const DedupActor = "system"

// Dedup match kinds; each audits under event type dedup.<kind>.
const (
	DedupExact     = "exact"
	DedupSourceKey = "source_key"
	DedupSimilar   = "similar"
)

// DedupDecision is one dedup outcome: ObjectID was judged a duplicate of
// DuplicateOf. It holds IDs and scores only; content never reaches the
// audit log.
type DedupDecision struct {
	ObjectID    string
	DuplicateOf string
	Similarity  float64
	// Kind is the match kind: exact, source_key or similar.
	Kind string
	// Policy is the duplicates policy applied: warn, keep or drop.
	Policy string
	// ModelID is the embedding model a similar match was scored under;
	// empty for hash and source-key matches.
	ModelID string
}

// DedupEntry returns the audit entry for d, with event type dedup.<kind>.
func DedupEntry(d DedupDecision) *storage.AuditEntry {
	payload := map[string]any{
		"duplicate_of": d.DuplicateOf,
		"similarity":   d.Similarity,
		"kind":         d.Kind,
		"policy":       d.Policy,
	}
	if d.ModelID != "" {
		payload["model_id"] = d.ModelID
	}
	return &storage.AuditEntry{
		ID:        uuid.New().String(),
		EventType: "dedup." + d.Kind,
		ObjectID:  d.ObjectID,
		Actor:     DedupActor,
		Payload:   payload,
		CreatedAt: time.Now().UTC(),
	}
}
