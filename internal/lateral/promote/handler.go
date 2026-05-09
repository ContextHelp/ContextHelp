package promote

import (
	"context"
	"errors"
	"fmt"
	"time"

	"hop.top/kit/go/runtime/policy"

	"github.com/ideacrafterslabs/ctxt/internal/lateral/lifecycle"
)

// ErrNotFound is returned when the candidate id has no matching row.
var ErrNotFound = errors.New("candidate not found")

// Path identifies which P2/P3 promotion route triggered this call.
type Path string

// Promotion paths.
const (
	PathP2Explicit Path = "p2_explicit"
	PathP2Implicit Path = "p2_implicit"
	PathP3         Path = "p3_references"
)

// promoteTopic is the kit pre-persist topic Decide is asked about. It
// matches a rule "on:" key in policies.yaml so the engine resolves the
// correct policy set.
const promoteTopic = "kit.runtime.entity.pre_persisted"

// Store is the persistence boundary; concrete impls land separately
// (see T05/T10 materialize).
type Store interface {
	ReadCandidate(ctx context.Context, id string) (map[string]any, error)
	UpdateCandidate(ctx context.Context, id string, patch map[string]any) error
	WriteEdge(ctx context.Context, from, to, typ string) error
	KickCanonicalCapture(ctx context.Context, url string) (string, error)
}

// Handler executes promotions whose decision rules live in CEL.
type Handler struct {
	st  Store
	eng *policy.Engine
	now func() time.Time
}

// Output captures the canonical-record id created by the promotion.
type Output struct {
	NewCanonicalID string
}

// New constructs a Handler bound to a kit policy engine. Build the
// engine via policy/withcel.New(cfg) and pass it here.
func New(st Store, eng *policy.Engine) *Handler {
	return &Handler{st: st, eng: eng, now: time.Now}
}

// Promote runs CEL gates then performs the promotion writes. A denial
// surfaces as policy.PolicyDeniedError (wraps domain.ErrConflict), so
// callers can errors.Is-check that and surface the denied policy's
// Message.
func (h *Handler) Promote(ctx context.Context, candidateID string, path Path) (Output, error) {
	rec, err := h.st.ReadCandidate(ctx, candidateID)
	if err != nil {
		return Output{}, fmt.Errorf("read candidate: %w", err)
	}
	meta, ok := rec["metadata"].(map[string]any)
	if !ok {
		return Output{}, fmt.Errorf("invalid record shape: metadata is not a map")
	}
	lc, ok := meta["lifecycle"].(map[string]any)
	if !ok {
		return Output{}, fmt.Errorf("invalid record shape: metadata.lifecycle is not a map")
	}
	state, _ := lc["state"].(string)

	activation := map[string]any{
		"payload": map[string]any{
			"kind":             "lateral.promote",
			"path":             string(path),
			"from_state":       state,
			"expired_at":       lc["expired_at"],
			"now":              h.now().Format(time.RFC3339),
			"resurrect_window": "720h", // 30d in CEL duration form
		},
	}
	if err := h.eng.Decide(promoteTopic, activation); err != nil {
		return Output{}, fmt.Errorf("promote denied: %w", err)
	}

	url, ok := rec["source"].(string)
	if !ok {
		return Output{}, fmt.Errorf("invalid record shape: source is not a string")
	}
	canonicalID, err := h.st.KickCanonicalCapture(ctx, url)
	if err != nil {
		return Output{}, fmt.Errorf("kick canonical: %w", err)
	}
	// mutate lifecycle in-place — preserves all sibling metadata fields
	// (kind, candidate_type, discovered_by, strategy, scoring, preview, ...)
	// that a shallow-merge UpdateCandidate would otherwise wipe.
	lc["state"] = string(lifecycle.Promoted)
	lc["promotion_path"] = string(path)
	lc["promoted_at"] = h.now().Format(time.RFC3339)
	patch := map[string]any{"metadata": meta}
	if err := h.st.UpdateCandidate(ctx, candidateID, patch); err != nil {
		return Output{}, fmt.Errorf("update candidate: %w", err)
	}
	if err := h.st.WriteEdge(ctx, canonicalID, candidateID, "promoted_from"); err != nil {
		return Output{}, fmt.Errorf("write promoted_from edge: %w", err)
	}
	return Output{NewCanonicalID: canonicalID}, nil
}
