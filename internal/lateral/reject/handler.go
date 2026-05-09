package reject

import (
	"context"
	"fmt"
	"time"

	"hop.top/kit/go/runtime/policy"
)

const rejectTopic = "kit.runtime.entity.pre_persisted"

// Store is the persistence boundary; concrete impls land separately
// (see T05/T10 materialize).
type Store interface {
	ReadCandidate(ctx context.Context, id string) (map[string]any, error)
	UpdateCandidate(ctx context.Context, id string, patch map[string]any) error
	RecordHardNegative(ctx context.Context, candidateID string) error
}

// Handler executes rejections whose decision rules live in CEL.
type Handler struct {
	st  Store
	eng *policy.Engine
	now func() time.Time
}

// New constructs a Handler bound to a kit policy engine. Build the engine
// via policy/withcel.New(cfg) and pass it here.
func New(st Store, eng *policy.Engine) *Handler {
	return &Handler{st: st, eng: eng, now: time.Now}
}

// Reject moves the candidate to expired + records a hard-negative label,
// iff CEL allows. A denial surfaces as policy.PolicyDeniedError (wraps
// domain.ErrConflict), so callers can errors.Is-check that and surface
// the denied policy's Message.
//
// Sibling metadata fields (kind, candidate_type, discovered_by, strategy,
// scoring, preview) are preserved across the patch — only the lifecycle
// block is mutated.
func (h *Handler) Reject(ctx context.Context, candidateID, reason string) error {
	rec, err := h.st.ReadCandidate(ctx, candidateID)
	if err != nil {
		return fmt.Errorf("read candidate: %w", err)
	}
	meta, ok := rec["metadata"].(map[string]any)
	if !ok {
		return fmt.Errorf("invalid record shape: metadata is not a map")
	}
	lc, ok := meta["lifecycle"].(map[string]any)
	if !ok {
		return fmt.Errorf("invalid record shape: metadata.lifecycle is not a map")
	}
	state, _ := lc["state"].(string)

	activation := map[string]any{
		"payload": map[string]any{
			"kind":       "lateral.reject",
			"from_state": state,
			"reason":     reason,
		},
	}
	if err := h.eng.Decide(rejectTopic, activation); err != nil {
		return fmt.Errorf("reject denied: %w", err)
	}

	// Mutate lifecycle in-place, then patch the full meta back so
	// UpdateCandidate's shallow merge preserves sibling fields.
	lc["state"] = "expired"
	lc["expired_at"] = h.now().Format(time.RFC3339)
	lc["reason"] = reason
	patch := map[string]any{"metadata": meta}
	if err := h.st.UpdateCandidate(ctx, candidateID, patch); err != nil {
		return fmt.Errorf("update candidate: %w", err)
	}
	if err := h.st.RecordHardNegative(ctx, candidateID); err != nil {
		return fmt.Errorf("record hard negative: %w", err)
	}
	return nil
}
