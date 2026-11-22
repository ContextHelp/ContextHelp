package steward

import (
	"context"
	"fmt"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// GrantView is a steward grant as one viewer sees it: the stored row,
// its state and effective expiry now (under today's duration cap), and
// who filed the request it came from. It carries no object of the
// instance: what the grant was used for is the activity report's.
type GrantView struct {
	Grant           *storage.StewardGrant
	State           storage.StewardGrantState
	EffectiveExpiry time.Time
	RequestedBy     string
}

// ListFilter narrows List. Every set field must hold.
type ListFilter struct {
	// StewardID keeps the grants of one steward.
	StewardID string
	// State keeps the grants in that state now (approved, active,
	// expired, revoked).
	State storage.StewardGrantState
	// Limit caps the result; 0 means no cap.
	Limit int
}

// Get returns grant id as viewer sees it. Its steward, the requester
// of the request it came from, a holder of approve:steward and the
// owner see it (ADR-083 §2, §5); to anyone else it is
// storage.ErrNotFound, like a grant that does not exist.
func (s *Service) Get(ctx context.Context, id string, viewer *auth.Principal) (*GrantView, error) {
	g, err := s.o.Sharing.StewardGrants().Get(ctx, id)
	if err != nil {
		return nil, err
	}
	req, err := s.o.Approvals.Get(ctx, g.RequestID)
	if err != nil {
		return nil, err
	}
	if !mayAccess(viewer, g, req) {
		return nil, fmt.Errorf("steward grant %q: %w", id, storage.ErrNotFound)
	}
	return s.view(g, req), nil
}

// List returns the grants viewer sees (Get), newest approval first,
// narrowed by f.
func (s *Service) List(ctx context.Context, viewer *auth.Principal, f ListFilter) ([]*GrantView, error) {
	grants, err := s.o.Sharing.StewardGrants().List(ctx, storage.StewardGrantFilter{StewardID: f.StewardID})
	if err != nil {
		return nil, err
	}
	out := []*GrantView{}
	for _, g := range grants {
		req, err := s.o.Approvals.Get(ctx, g.RequestID)
		if err != nil {
			return nil, fmt.Errorf("steward grant %s: request %s: %w", g.ID, g.RequestID, err)
		}
		if !mayAccess(viewer, g, req) {
			continue
		}
		v := s.view(g, req)
		if f.State != "" && v.State != f.State {
			continue
		}
		out = append(out, v)
		if f.Limit > 0 && len(out) == f.Limit {
			break
		}
	}
	return out, nil
}

// view is g, from request req, as of now.
func (s *Service) view(g *storage.StewardGrant, req *storage.ApprovalRequest) *GrantView {
	return &GrantView{Grant: g, State: s.State(g), EffectiveExpiry: s.EffectiveExpiry(g), RequestedBy: req.RequestedBy}
}
