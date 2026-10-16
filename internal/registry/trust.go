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
// It only decides; the Syncer queues what it holds for approval in the
// persistent queue (storage.PendingEntityStore).
type TrustGate struct{}

// NewTrustGate returns a TrustGate.
func NewTrustGate() *TrustGate {
	return &TrustGate{}
}

// CheckWrite decides whether the entity write should proceed, be queued, or
// be blocked based on the registry's trust level.
//
//   - trusted   → proceed (returns nil, nil)
//   - untrusted → hold for approval (returns record, ErrEntityRequiresApproval)
//   - sandboxed → block (returns nil, ErrEntityRequiresApproval)
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
			"slug", e.Slug, "registry", config.RedactDSN(registryURL))
		return nil, fmt.Errorf("registry %q is sandboxed: entity %q blocked: %w",
			config.RedactDSN(registryURL), e.Slug, ErrEntityRequiresApproval)

	default: // untrusted
		slog.InfoContext(ctx, "trust gate: entity held for approval",
			"slug", e.Slug, "registry", config.RedactDSN(registryURL))
		return &ApprovalRecord{Entity: e, RegistryURL: registryURL, TrustLevel: level},
			fmt.Errorf("registry %q is untrusted: entity %q held for approval: %w",
				config.RedactDSN(registryURL), e.Slug, ErrEntityRequiresApproval)
	}
}
