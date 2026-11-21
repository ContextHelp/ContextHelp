package config

import (
	"fmt"
	"time"
)

// DefaultApprovalPendingTTL is how long an approval request waits for
// its approvals when approvals.pending_ttl is unset (ADR-083 §9).
const DefaultApprovalPendingTTL = 72 * time.Hour

// ApprovalsConfig is the `approvals:` section: the shared approval
// mechanism (ADR-083 §9). Each kind's approver count lives with the
// kind's own settings (approvals.<kind>.required) when the kind lands.
type ApprovalsConfig struct {
	// PendingTTL is how long a request may wait for its approvals: past
	// it the request is expired, computed from the clock on every read,
	// never swept. Shared by every kind. A duration (72h by default).
	PendingTTL time.Duration `mapstructure:"pending_ttl" yaml:"pending_ttl"`
}

// EffectivePendingTTL is PendingTTL, or the default when unset.
func (a ApprovalsConfig) EffectivePendingTTL() time.Duration {
	if a.PendingTTL <= 0 {
		return DefaultApprovalPendingTTL
	}
	return a.PendingTTL
}

// Validate refuses a negative pending TTL.
func (a ApprovalsConfig) Validate() []ValidationError {
	if a.PendingTTL < 0 {
		return []ValidationError{{Field: "approvals.pending_ttl", Message: fmt.Sprintf("must be a positive duration or 0 for the default (%s); got %s", DefaultApprovalPendingTTL, a.PendingTTL)}}
	}
	return nil
}
