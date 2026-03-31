package federation

import (
	"context"
	"errors"
)

// ErrPullNotImplemented is returned by PullSyncer.Pull until Phase 3 is implemented.
// Full implementation requires a conflict resolution ADR.
// See ADR-064 Phase 3.
var ErrPullNotImplemented = errors.New("federation pull sync is not yet implemented (Phase 3)")

// PullSyncer is a placeholder for Phase 3 pull/bidirectional sync.
// Full implementation requires a conflict resolution ADR.
// See ADR-064 Phase 3.
type PullSyncer struct{}

// Pull always returns ErrPullNotImplemented until Phase 3.
func (p *PullSyncer) Pull(_ context.Context) error {
	return ErrPullNotImplemented
}
