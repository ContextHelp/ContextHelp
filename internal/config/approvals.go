package config

import (
	"fmt"
	"time"
)

// DefaultApprovalPendingTTL is how long an approval request waits for
// its approvals when approvals.pending_ttl is unset (ADR-083 §9).
const DefaultApprovalPendingTTL = 72 * time.Hour

// ApprovalsConfig is the `approvals:` section: the shared approval
// mechanism (ADR-083 §9). Each kind's approver count is its own
// setting (approvals.<kind>.required).
type ApprovalsConfig struct {
	// PendingTTL is how long a request may wait for its approvals: past
	// it the request is expired, computed from the clock on every read,
	// never swept. Shared by every kind. A duration (72h by default).
	PendingTTL time.Duration `mapstructure:"pending_ttl" yaml:"pending_ttl"`
	// Steward is the approver count of steward.grant requests
	// (approvals.steward.required).
	Steward ApprovalKindConfig `mapstructure:"steward" yaml:"steward"`
}

// ApprovalKindConfig is one kind's approval setting: how many distinct
// eligible approvers a request of the kind needs. The count is frozen
// onto each request when it is filed, so lowering it never weakens a
// pending request.
type ApprovalKindConfig struct {
	// Required is N, at least 1 (1 by default). The two-person rule is 2.
	Required int `mapstructure:"required" yaml:"required"`
}

// EffectiveRequired is Required, or 1 when unset.
func (k ApprovalKindConfig) EffectiveRequired() int {
	if k.Required <= 0 {
		return 1
	}
	return k.Required
}

// EffectivePendingTTL is PendingTTL, or the default when unset.
func (a ApprovalsConfig) EffectivePendingTTL() time.Duration {
	if a.PendingTTL <= 0 {
		return DefaultApprovalPendingTTL
	}
	return a.PendingTTL
}

// Validate refuses a negative pending TTL or approver count.
func (a ApprovalsConfig) Validate() []ValidationError {
	var errs []ValidationError
	if a.PendingTTL < 0 {
		errs = append(errs, ValidationError{Field: "approvals.pending_ttl", Message: fmt.Sprintf("must be a positive duration or 0 for the default (%s); got %s", DefaultApprovalPendingTTL, a.PendingTTL)})
	}
	if a.Steward.Required < 0 {
		errs = append(errs, ValidationError{Field: "approvals.steward.required", Message: fmt.Sprintf("must be at least 1 or 0 for the default (1); got %d", a.Steward.Required)})
	}
	return errs
}
