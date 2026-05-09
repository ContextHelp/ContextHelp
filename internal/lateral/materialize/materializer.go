package materialize

import (
	"context"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/lateral/identity"
)

// Edge is a directed relationship written alongside a materialized candidate.
// Type vocabulary lives in schemas/lateral_edges.json: "discovered_by" links
// a probationary candidate back to its parent; "refers_to_canonical" links a
// parent forward to an existing canonical entity (edge-only outcome).
type Edge struct {
	From, To, Type string
}

// Store is the subset of the canonical store the materializer writes to.
// WriteRecord persists a record and returns the assigned object ID.
// WriteEdge persists a directed edge between two object IDs.
type Store interface {
	WriteRecord(ctx context.Context, rec map[string]any) (string, error)
	WriteEdge(ctx context.Context, e Edge) error
}

// Input bundles everything the materializer needs to write one outcome.
// ParentLifespan defaults to 180d when zero. Now is injected for tests; zero
// means time.Now().
type Input struct {
	ParentID        string
	URL             string
	Title           string
	CandidateType   string
	Strategy        string
	Resolution      identity.Result
	ParentLifespan  time.Duration
	Now             time.Time
	ScoringMetadata map[string]any
	Preview         map[string]any
}

// Output reports the materialization outcome. EdgeOnly=true means no
// probationary record was written; the parent now has a refers_to_canonical
// edge pointing at CanonicalID. EdgeOnly=false means a probationary record
// was written with the returned CandidateID.
type Output struct {
	EdgeOnly    bool
	CandidateID string
	CanonicalID string
}

// Materializer turns a resolved candidate into store writes per the
// schemas/lateral_candidate.json + schemas/lateral_edges.json contracts.
type Materializer struct{ st Store }

// New wires a Materializer to its Store.
func New(st Store) *Materializer { return &Materializer{st: st} }

// Materialize executes the resolution outcome. When the resolver flagged
// EdgeOnly, writes only a refers_to_canonical edge. Otherwise writes a
// probationary lateral_candidate record + a discovered_by edge from the new
// candidate back to its parent.
func (m *Materializer) Materialize(ctx context.Context, in Input) (Output, error) {
	if in.Resolution.EdgeOnly {
		err := m.st.WriteEdge(ctx, Edge{From: in.ParentID, To: in.Resolution.CanonicalID, Type: "refers_to_canonical"})
		return Output{EdgeOnly: true, CanonicalID: in.Resolution.CanonicalID}, err
	}
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}
	lifespan := in.ParentLifespan
	if lifespan == 0 {
		lifespan = 180 * 24 * time.Hour
	}
	expiresAt := now.Add(lifespan)
	rec := map[string]any{
		"namespace": "lateral_candidate",
		"title":     in.Title,
		"source":    in.URL,
		"metadata": map[string]any{
			"kind":           "lateral_candidate",
			"candidate_type": in.CandidateType,
			"discovered_by":  in.ParentID,
			"strategy":       in.Strategy,
			"lifecycle": map[string]any{
				"state":         "probationary",
				"discovered_at": now.Format(time.RFC3339),
				"expires_at":    expiresAt.Format(time.RFC3339),
				"cold_since":    nil,
			},
			"scoring": in.ScoringMetadata,
			"preview": in.Preview,
		},
	}
	id, err := m.st.WriteRecord(ctx, rec)
	if err != nil {
		return Output{}, err
	}
	if err := m.st.WriteEdge(ctx, Edge{From: id, To: in.ParentID, Type: "discovered_by"}); err != nil {
		return Output{}, err
	}
	return Output{EdgeOnly: false, CandidateID: id}, nil
}
