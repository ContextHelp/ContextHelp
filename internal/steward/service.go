package steward

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/security"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ServiceOptions configures a Service.
type ServiceOptions struct {
	// Sharing holds the steward grants. Required.
	Sharing storage.SharingStore
	// Approvals is the approval store: the request a grant came from
	// (who may revoke it), and the transaction a revocation and its
	// audit entry share. Required.
	Approvals storage.ApprovalStore
	// Audit is the driver's audit log. Required.
	Audit storage.AuditStore
	// Settings returns the steward settings; the duration cap decides
	// whether a grant is still in force. Required.
	Settings func() sharing.StewardSettings
	// Security records each revocation once it commits, for the
	// notification hooks; nil records none.
	Security *security.Emitter
	// Now is the clock; nil is time.Now.
	Now func() time.Time
}

// Service is the grant half of the steward state machine (ADR-083 §2):
// a grant is approved by the final approval of its request (the kind's
// Apply), is active or expired as the clock and the cap say, and ends
// once by revocation. There is no sweeper and no edit.
type Service struct{ o ServiceOptions }

// NewService returns a Service over o.
func NewService(o ServiceOptions) (*Service, error) {
	if o.Sharing == nil || o.Approvals == nil || o.Audit == nil || o.Settings == nil {
		return nil, errors.New("steward service: the sharing registry, the approval store, the audit log and the settings are required")
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	return &Service{o: o}, nil
}

func (s *Service) now() time.Time { return clockMS(s.o.Now()) }

// Settings returns the steward settings as they stand.
func (s *Service) Settings() sharing.StewardSettings { return s.o.Settings() }

// State is g's state now, under today's duration cap: lowering
// steward.max_duration expires a live grant here exactly as it does at
// the viewer.
func (s *Service) State(g *storage.StewardGrant) storage.StewardGrantState {
	return g.StateAt(s.now(), s.o.Settings().MaxDuration)
}

// EffectiveExpiry is when g ends under today's duration cap.
func (s *Service) EffectiveExpiry(g *storage.StewardGrant) time.Time {
	return g.EffectiveExpiry(s.o.Settings().MaxDuration)
}

// Revoke ends grant id as actor, with a note. Any holder of
// approve:steward (the owner included, unless narrowed below it), the
// request's requester or the steward may; to anyone else the grant does not exist. Any grant whose stored
// status is approved is revoked, whatever the clock or the cap says:
// expired is computed, and raising steward.max_duration brings back a
// grant the cap had expired, so revocation is the one end an operator
// can force. A revoked grant is terminal (ErrStewardGrantNotApproved).
// The revocation and its audit entry commit together, and it is
// effective on the steward's next request, since every request reads
// the grant.
func (s *Service) Revoke(ctx context.Context, id string, actor *auth.Principal, note string) (*storage.StewardGrant, error) {
	var out *storage.StewardGrant
	err := s.o.Approvals.InTx(ctx, func(ctx context.Context) error {
		g, err := s.o.Sharing.StewardGrants().Get(ctx, id)
		if err != nil {
			return err
		}
		req, err := s.o.Approvals.Get(ctx, g.RequestID)
		if err != nil {
			return err
		}
		if !mayAccess(actor, g, req) {
			return fmt.Errorf("steward grant %q: %w", id, storage.ErrNotFound)
		}
		if g.Status != storage.StewardGrantApproved {
			return fmt.Errorf("steward grant %q is %s: %w", id, g.Status, storage.ErrStewardGrantNotApproved)
		}
		now := s.now()
		state := g.StateAt(now, s.o.Settings().MaxDuration)
		if err := s.o.Sharing.StewardGrants().Revoke(ctx, id, actor.ID, note, now); err != nil {
			return err
		}
		if err := s.o.Audit.Append(ctx, &storage.AuditEntry{
			ID: uuid.New().String(), EventType: EventGrantRevoked, Actor: actor.ID, CreatedAt: now,
			Payload: map[string]any{
				"grant_id": g.ID, "request_id": g.RequestID, "steward_id": g.StewardID, "note": strings.TrimSpace(note), "state": string(state),
			},
		}); err != nil {
			return fmt.Errorf("audit %s: %w", EventGrantRevoked, err)
		}
		out, err = s.o.Sharing.StewardGrants().Get(ctx, id)
		return err
	})
	if err != nil {
		return nil, err
	}
	recordRevoked(ctx, s.o.Security, actor.ID, out)
	return out, nil
}

// mayAccess reports whether actor may see and revoke g: a holder of
// approve:steward (the owner holds it by role, a key or session of the
// owner narrowed below it does not), or the request's requester or the
// steward, who each also hold request:steward on the key or session
// they act with: one narrowed below it has no reach over the grant.
func mayAccess(actor *auth.Principal, g *storage.StewardGrant, req *storage.ApprovalRequest) bool {
	switch {
	case actor == nil || actor.ID == "":
		return false
	case actor.HasScope(auth.ScopeApproveSteward):
		return true
	}
	return (actor.ID == g.StewardID || actor.ID == req.RequestedBy) && actor.HasScope(auth.ScopeRequestSteward)
}
