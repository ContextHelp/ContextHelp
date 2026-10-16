package stack

import (
	"github.com/ideacrafterslabs/ctxt/internal/approval"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/sharechange"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/steward"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// stewardSettings returns the steward settings as the config stands: a
// function, read on every use, so a change to cfg applies at once.
func stewardSettings(cfg *config.Config) func() sharing.StewardSettings {
	return func() sharing.StewardSettings { return steward.Settings(cfg.Steward) }
}

// ApprovalKinds returns the kinds of approval request the instance
// accepts, each with its approver count from the config
// (approvals.<kind>.required): steward.grant (ADR-083 §2), off by
// default (steward.enabled), which then refuses every filing; and
// share.change (ADR-083 §10 B), the widening sharing change of a
// non-owner administrator.
func ApprovalKinds(drv storage.StorageDriver, cfg *config.Config) (*approval.Registry, error) {
	reg := approval.NewRegistry()
	k, err := steward.Kind(steward.KindOptions{
		Settings: stewardSettings(cfg),
		// Read live, like Settings: a change to cfg applies to the next filing.
		Required:       func() int { return cfg.Approvals.Steward.EffectiveRequired() },
		MinReasonChars: func() int { return cfg.Steward.EffectiveMinReasonChars() },
		Sharing:        drv.Sharing(),
		Auth:           drv.Auth(),
		Audit:          drv.AuditLog(),
	})
	if err != nil {
		return nil, err
	}
	if err := reg.Register(k); err != nil {
		return nil, err
	}
	if err := sharechange.Register(reg, sharechange.Deps{
		Sharing: drv.Sharing(), Auth: drv.Auth(), Audit: drv.AuditLog(),
		Required: cfg.Approvals.Share.EffectiveRequired,
	}); err != nil {
		return nil, err
	}
	return reg, nil
}
