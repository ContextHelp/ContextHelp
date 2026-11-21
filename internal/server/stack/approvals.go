package stack

import (
	"github.com/ideacrafterslabs/ctxt/internal/approval"
	"github.com/ideacrafterslabs/ctxt/internal/config"
)

// wireApprovals builds the shared approval mechanism (ADR-083 §9). Its
// kinds register with their consumers; none is registered yet, so the
// queue only lists and decides what a kind filed.
func (s *Stack) wireApprovals(in Inputs, cfg *config.Config) error {
	kinds := approval.NewRegistry()
	svc, err := approval.NewService(in.Driver.Approvals(), in.Driver.AuditLog(), kinds,
		approval.Options{PendingTTL: cfg.Approvals.EffectivePendingTTL()})
	if err != nil {
		return err
	}
	s.Approvals = svc
	return nil
}
