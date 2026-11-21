package approval

import (
	"context"
	"fmt"
	"slices"

	"github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Item is a request as one viewer sees it.
type Item struct {
	Request *storage.ApprovalRequest
	// State is the request's state now (expired computed).
	State State
	// CanDecide reports whether the viewer may approve or reject it now:
	// pending, and the viewer passes every separation check.
	CanDecide bool
}

// ListFilter narrows List.
type ListFilter struct {
	// Kind keeps requests of that kind; "" keeps all.
	Kind string
	// State keeps requests in that state (expired included); "" keeps
	// all.
	State State
	// Limit caps the result; 0 means no cap.
	Limit int
}

// Get returns request id as viewer sees it. A request the viewer may
// not see is storage.ErrNotFound, like one that does not exist.
func (s *Service) Get(ctx context.Context, id string, viewer *auth.Principal) (*Item, error) {
	r, err := s.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	k, _ := s.kinds.Lookup(r.Kind)
	if ok, err := s.visible(ctx, k, r, viewer); err != nil || !ok {
		return nil, notVisible(id, err)
	}
	return s.item(ctx, k, r, viewer)
}

// listPage is how many requests List reads from the store at a time.
const listPage = 100

// List returns the requests viewer filed, is the subject of, or may
// decide (the owner: every request), newest first. The store narrows
// them in SQL (kind, state, expiry and, for a viewer that is not the
// acting owner, the requests it filed, is the subject of or that are of
// a registered kind) and List pages through them, stopping at the
// limit; only the kinds' own eligibility is decided here.
func (s *Service) List(ctx context.Context, viewer *auth.Principal, f ListFilter) ([]*Item, error) {
	if f.State != "" && !slices.Contains(States, f.State) {
		return nil, refuse(CodeInvalid, "unknown state %q", f.State)
	}
	if viewer == nil || viewer.ID == "" {
		return []*Item{}, nil
	}
	now := s.clock()
	sf := storage.ApprovalFilter{Kind: f.Kind}
	switch f.State {
	case "":
	case StateRequested:
		sf.Status, sf.ExpiresAfter = storage.ApprovalRequested, now
	case StateExpired:
		sf.Status, sf.ExpiresAtOrBefore = storage.ApprovalRequested, now
	default:
		sf.Status = storage.ApprovalStatus(f.State)
	}
	acting := ownerActs(viewer)
	if !acting {
		sf.Involving, sf.InvolvingKinds = viewer.ID, s.kinds.Names()
	}
	out := []*Item{}
	sf.Limit = listPage
	if acting && f.Limit > 0 {
		// Nothing is filtered after the store: it applies the limit.
		sf.Limit = f.Limit
	}
	for {
		reqs, err := s.store.List(ctx, sf)
		if err != nil {
			return nil, err
		}
		for _, r := range reqs {
			k, _ := s.kinds.Lookup(r.Kind)
			ok, err := s.visible(ctx, k, r, viewer)
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
			it, err := s.item(ctx, k, r, viewer)
			if err != nil {
				return nil, err
			}
			out = append(out, it)
			if f.Limit > 0 && len(out) == f.Limit {
				return out, nil
			}
		}
		if len(reqs) < sf.Limit {
			return out, nil
		}
		sf.Offset += len(reqs)
	}
}

// item builds viewer's Item of r.
func (s *Service) item(ctx context.Context, k *Kind, r *storage.ApprovalRequest, viewer *auth.Principal) (*Item, error) {
	now := s.clock()
	it := &Item{Request: r, State: StateOf(r, now)}
	if k == nil || it.State != StateRequested {
		return it, nil
	}
	err := s.separate(ctx, k, r, viewer)
	switch CodeOf(err) {
	case "":
		if err != nil {
			return nil, err
		}
		it.CanDecide = true
	case CodeRequesterCannotDecide, CodeBeneficiaryCannotDecide, CodeNotEligible, CodeAlreadyDecided:
	default:
		return nil, err
	}
	return it, nil
}

// visible reports whether viewer may see r: the owner, its requester,
// its subject, or an eligible approver of its kind. A request of an
// unregistered kind is visible to the first three only.
func (s *Service) visible(ctx context.Context, k *Kind, r *storage.ApprovalRequest, viewer *auth.Principal) (bool, error) {
	if viewer == nil || viewer.ID == "" {
		return false, nil
	}
	if ownerActs(viewer) || viewer.ID == r.RequestedBy || (r.SubjectID != "" && viewer.ID == r.SubjectID) {
		return true, nil
	}
	if k == nil {
		return false, nil
	}
	ok, err := k.Eligible(ctx, r, viewer)
	if err != nil {
		return false, fmt.Errorf("approval kind %s: eligibility: %w", k.Name, err)
	}
	return ok, nil
}
