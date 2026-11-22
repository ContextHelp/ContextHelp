package sharechange

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"time"

	"github.com/google/uuid"

	"github.com/ideacrafterslabs/ctxt/internal/approval"
	"github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ErrNoApprovals is a widening change by a caller that is not the
// owner, on an instance with no approval mechanism: nothing can accept
// it, so nothing is written.
var ErrNoApprovals = errors.New("share change: this instance has no approval mechanism")

// Service runs sharing changes for the callers of the sharing routes.
type Service struct {
	Deps
	// Approvals files the changes that wait for an approver. nil on a
	// router built without one: a change that would wait is refused.
	Approvals *approval.Service
}

// Outcome is what Submit did with a change.
type Outcome struct {
	// Applied is set when the change was written at once.
	Applied *Applied
	// Request is set when the change was filed instead, pending, and
	// nothing was written.
	Request *storage.ApprovalRequest
	// Class is the classifier's verdict; the zero value for the
	// owner's changes, which are not classified.
	Class sharing.Classification
}

// Submit runs change c as caller (ADR-083 §10 B). The owner's change is
// written at once. Any other caller's change is classified against the
// registry and then either written, when it is neutral or narrowing, or
// filed as a share.change request, when it is widening, and so is
// anything the classifier cannot show to be neither: an unparseable
// spec, a relation it cannot compare, a state that failed to read.
// Classification and write (or filing) happen in one transaction under
// the registry lock, so a concurrent change cannot slip in between.
func (s *Service) Submit(ctx context.Context, caller *auth.Principal, c sharing.Change) (*Outcome, error) {
	if caller == nil || caller.ID == "" {
		return nil, errors.New("share change: an authenticated caller is required")
	}
	if caller.Kind == storage.PrincipalKindOwner {
		return s.Direct(ctx, caller, c)
	}
	// Notices of the request filed inside the transaction wait for its
	// commit: a change that rolled back emits nothing.
	flush := func() {}
	if s.Approvals != nil {
		ctx, flush = s.Approvals.Collect(ctx)
	}
	var out Outcome
	err := s.Sharing.InTx(ctx, func(ctx context.Context) error {
		if err := s.Sharing.LockRegistry(ctx); err != nil {
			return err
		}
		now := s.now()
		out.Class, _ = sharing.ClassifyRead(ctx, s.Sharing, c, now)
		if out.Class.Class == sharing.Widening {
			return s.file(ctx, caller, c, &out)
		}
		return s.applyRecorded(ctx, caller.ID, c, &out, now, &out.Class)
	})
	if err != nil {
		return nil, err
	}
	flush()
	return &out, nil
}

// Direct writes change c as caller at once, whatever its class: the
// owner's changes, which are never pending, and the changes that are
// not administration of another principal's view (a principal binding
// its own key, ADR-083 §10 B). It runs under the registry lock like
// Submit, so the write is ordered with every classified change.
func (s *Service) Direct(ctx context.Context, caller *auth.Principal, c sharing.Change) (*Outcome, error) {
	if caller == nil || caller.ID == "" {
		return nil, errors.New("share change: an authenticated caller is required")
	}
	var out Outcome
	err := s.Sharing.InTx(ctx, func(ctx context.Context) error {
		if err := s.Sharing.LockRegistry(ctx); err != nil {
			return err
		}
		return s.applyRecorded(ctx, caller.ID, c, &out, s.now(), nil)
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// applyRecorded writes c as actor and appends its audit entry, in the
// caller's transaction.
// class, when set, is the classifier's verdict on a non-owner's change:
// it is recorded with the entry, so the audit says why the change did
// not wait.
func (s *Service) applyRecorded(ctx context.Context, actor string, c sharing.Change, out *Outcome, now time.Time, class *sharing.Classification) error {
	var err error
	if out.Applied, err = Apply(ctx, s.Sharing, c, actor, now); err != nil {
		return err
	}
	return s.record(ctx, actor, out.Applied, now, class)
}

// maxWhy bounds the free-text detail carried into a reason.
const maxWhy = 200

// why is the classifier's verdict as one line: the class, its stable
// reason code and the detail when there is one.
func why(c sharing.Classification) string {
	out := string(c.Class) + ": " + c.Reason
	if c.Detail != "" {
		d := c.Detail
		if len(d) > maxWhy {
			d = d[:maxWhy]
		}
		out += " (" + d + ")"
	}
	return out
}

// file files c as caller's pending request.
func (s *Service) file(ctx context.Context, caller *auth.Principal, c sharing.Change, out *Outcome) error {
	if s.Approvals == nil {
		return ErrNoApprovals
	}
	f, err := Filing(caller, c, why(out.Class))
	if err != nil {
		return err
	}
	out.Request, err = s.Approvals.File(ctx, f)
	return err
}

// record appends the audit entry of a change applied at once, in the
// transaction of the change: an entry exists if and only if the change
// committed. A service built with no audit log records nothing.
func (s *Service) record(ctx context.Context, actor string, a *Applied, now time.Time, class *sharing.Classification) error {
	if s.Audit == nil {
		return nil
	}
	payload := a.Audit
	if class != nil {
		payload = maps.Clone(a.Audit)
		if payload == nil {
			payload = map[string]any{}
		}
		payload["class"], payload["class_reason"] = string(class.Class), class.Reason
		if class.Detail != "" {
			payload["class_detail"] = class.Detail
		}
	}
	entry := &storage.AuditEntry{ID: uuid.New().String(), EventType: a.Event, Actor: actor, Payload: payload, CreatedAt: now}
	if err := s.Audit.Append(ctx, entry); err != nil {
		return fmt.Errorf("audit %s: %w", a.Event, err)
	}
	return nil
}
