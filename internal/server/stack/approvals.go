package stack

import (
	"github.com/ideacrafterslabs/ctxt/internal/approval"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/steward"
)

// wireApprovals builds the shared approval mechanism (ADR-083 §9) with
// the kinds of ApprovalKinds, and what the steward.grant kind (§2)
// needs beside it: the grant half (revocation, state) and the viewer
// resolver's steward branch (§3). Steward access is off by default
// (steward.enabled): the kind then refuses every filing and the
// resolver every elevation. The settings are read on every use, so they
// are the config's as it stands. Each transition of a steward.grant
// request and each revocation is reported to the security emitter,
// which Build creates first.
func (s *Stack) wireApprovals(in Inputs, cfg *config.Config) error {
	settings := stewardSettings(cfg)
	kinds, err := ApprovalKinds(in.Driver, cfg)
	if err != nil {
		return err
	}
	s.Approvals, err = approval.NewService(in.Driver.Approvals(), in.Driver.AuditLog(), kinds,
		approval.Options{PendingTTL: cfg.Approvals.EffectivePendingTTL(), Notify: steward.ApprovalNotices(s.Security)})
	if err != nil {
		return err
	}
	s.Stewards, err = steward.NewService(steward.ServiceOptions{
		Sharing: in.Driver.Sharing(), Approvals: in.Driver.Approvals(), Audit: in.Driver.AuditLog(),
		Settings: settings, Security: s.Security,
	})
	if err != nil {
		return err
	}
	// A principal that names a steward grant is resolved to that grant's
	// viewer alone, under these settings (ADR-083 §3).
	if viewers := s.Service.Viewers(); viewers != nil {
		viewers.WithSteward(steward.ResolverOptions(settings))
	}
	return nil
}
