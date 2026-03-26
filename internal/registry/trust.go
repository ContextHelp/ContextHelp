package registry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ErrEntityRequiresApproval is returned when an entity from an untrusted
// registry attempts to write to the local graph without explicit approval.
var ErrEntityRequiresApproval = errors.New("entity requires approval before writing to local graph")

// ApprovalRecord captures a pending entity write from an untrusted registry.
type ApprovalRecord struct {
	Entity      *storage.Entity
	RegistryURL string
	TrustLevel  config.RegistryTrustLevel
}

// TrustGate enforces registry trust-level rules before entity writes.
type TrustGate struct {
	// pendingApprovals holds entities queued for user approval.
	pendingApprovals []*ApprovalRecord
}

// NewTrustGate returns an initialised TrustGate.
func NewTrustGate() *TrustGate {
	return &TrustGate{}
}

// CheckWrite decides whether the entity write should proceed, be queued, or
// be blocked based on the registry's trust level.
//
//   - trusted   → proceed (returns nil, nil)
//   - untrusted → queue for approval (returns record, ErrEntityRequiresApproval)
//   - sandboxed → block silently (returns nil, ErrEntityRequiresApproval after logging)
func (g *TrustGate) CheckWrite(
	ctx context.Context,
	e *storage.Entity,
	registryURL string,
	level config.RegistryTrustLevel,
) (*ApprovalRecord, error) {
	switch level {
	case config.RegistryTrustLevelTrusted:
		return nil, nil

	case config.RegistryTrustLevelSandboxed:
		slog.InfoContext(ctx, "trust gate: sandboxed registry entity blocked",
			"slug", e.Slug, "registry", registryURL)
		return nil, fmt.Errorf("registry %q is sandboxed: entity %q blocked: %w",
			registryURL, e.Slug, ErrEntityRequiresApproval)

	default: // untrusted
		rec := &ApprovalRecord{
			Entity:      e,
			RegistryURL: registryURL,
			TrustLevel:  level,
		}
		g.pendingApprovals = append(g.pendingApprovals, rec)
		slog.InfoContext(ctx, "trust gate: entity queued for approval",
			"slug", e.Slug, "registry", registryURL)
		return rec, fmt.Errorf("registry %q is untrusted: entity %q queued for approval: %w",
			registryURL, e.Slug, ErrEntityRequiresApproval)
	}
}

// PendingApprovals returns all entities awaiting user approval.
func (g *TrustGate) PendingApprovals() []*ApprovalRecord {
	out := make([]*ApprovalRecord, len(g.pendingApprovals))
	copy(out, g.pendingApprovals)
	return out
}

// Approve marks the record as approved and returns it for writing.
// After approval the record is removed from the pending queue.
func (g *TrustGate) Approve(registryURL, slug string) (*ApprovalRecord, error) {
	for i, r := range g.pendingApprovals {
		if r.RegistryURL == registryURL && r.Entity.Slug == slug {
			g.pendingApprovals = append(g.pendingApprovals[:i], g.pendingApprovals[i+1:]...)
			return r, nil
		}
	}
	return nil, fmt.Errorf("no pending approval for slug %q from registry %q", slug, registryURL)
}
