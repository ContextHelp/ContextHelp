// Package approvaltest is a test-only approval kind: it exercises the
// mechanism (validation, fingerprint, eligibility, beneficiaries,
// apply in the approval's transaction) before any real consumer kind
// exists. Import it from tests only.
package approvaltest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/ideacrafterslabs/ctxt/internal/approval"
	"github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Action is the one action of the test kind.
const Action = "test.change.set"

// EventApplied is the audit event the kind's Apply writes, inside the
// approval's transaction.
const EventApplied = "test.change.applied"

// ApproverScope is the scope the kind's approvers hold.
const ApproverScope = auth.ScopeApproveShares

// Payload is the test kind's payload: set Value on Target, the
// principal the change benefits.
type Payload struct {
	Target string `json:"target"`
	Value  string `json:"value"`
}

// Raw encodes a payload.
func Raw(target, value string) json.RawMessage {
	b, _ := json.Marshal(Payload{Target: target, Value: value})
	return b
}

// Kind is a test kind with knobs. Its fingerprint is a version per
// target that Bump moves; its Apply appends EventApplied to the audit
// log through the context's transaction and returns a ref and a
// once-only reveal.
type Kind struct {
	name     string
	audit    storage.AuditStore
	store    storage.ApprovalStore
	mu       sync.Mutex
	versions map[string]int
	required int
	applyErr error
	attempts int
	// seen is the number of decisions Apply saw on its request, read
	// through the store in the approval's transaction.
	seen []int
	// SubjectMayApprove sets the kind's flag.
	SubjectMayApprove bool
	// MinReason sets the kind's minimum reason.
	MinReason int
}

// New returns a test kind named name over a driver's audit log and
// approval store.
func New(name string, drv storage.StorageDriver) *Kind {
	return &Kind{name: name, audit: drv.AuditLog(), store: drv.Approvals(), versions: map[string]int{}, required: 1}
}

// Name is the kind's name.
func (k *Kind) Name() string { return k.name }

// Bump changes target's fingerprint: a pending request about it goes
// stale at its last approval.
func (k *Kind) Bump(target string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.versions[target]++
}

// SetRequired sets N for requests filed from now on.
func (k *Kind) SetRequired(n int) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.required = n
}

// FailApply makes Apply fail with err (nil restores it).
func (k *Kind) FailApply(err error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.applyErr = err
}

// Attempts is how many times Apply ran, rolled-back runs included.
func (k *Kind) Attempts() int {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.attempts
}

// Seen lists, per Apply run, how many decisions it saw on its request
// from inside the approval's transaction.
func (k *Kind) Seen() []int {
	k.mu.Lock()
	defer k.mu.Unlock()
	return append([]int(nil), k.seen...)
}

// Reveal is the once-only result of an approval of request id.
func Reveal(id string) string { return "secret-for-" + id }

// Ref is the result ref of an approval of request id.
func Ref(id string) string { return "ref_" + id }

func decode(raw json.RawMessage) (Payload, error) {
	var p Payload
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return p, fmt.Errorf("payload: %w", err)
	}
	if strings.TrimSpace(p.Target) == "" {
		return p, errors.New("payload: target is required")
	}
	return p, nil
}

// Definition is the kind to register.
func (k *Kind) Definition() approval.Kind {
	return approval.Kind{
		Name:              k.name,
		Actions:           []string{Action},
		MinReason:         k.MinReason,
		SubjectMayApprove: k.SubjectMayApprove,
		Required: func() int {
			k.mu.Lock()
			defer k.mu.Unlock()
			return k.required
		},
		Validate: func(_ context.Context, f approval.Filing) (json.RawMessage, error) {
			p, err := decode(f.Payload)
			if err != nil {
				return nil, err
			}
			return json.Marshal(p)
		},
		Fingerprint: func(_ context.Context, r *storage.ApprovalRequest) (json.RawMessage, error) {
			p, err := decode(r.Payload)
			if err != nil {
				return nil, err
			}
			k.mu.Lock()
			defer k.mu.Unlock()
			return json.Marshal(map[string]any{"target": p.Target, "version": k.versions[p.Target]})
		},
		Eligible: approval.ByScope(ApproverScope),
		Beneficiaries: func(_ context.Context, r *storage.ApprovalRequest) ([]string, error) {
			p, err := decode(r.Payload)
			if err != nil {
				return nil, err
			}
			return []string{p.Target}, nil
		},
		Apply: k.apply,
	}
}

func (k *Kind) apply(ctx context.Context, r *storage.ApprovalRequest) (approval.Result, error) {
	k.mu.Lock()
	k.attempts++
	fail := k.applyErr
	k.mu.Unlock()
	in, err := k.store.Get(ctx, r.ID)
	if err != nil {
		return approval.Result{}, err
	}
	k.mu.Lock()
	k.seen = append(k.seen, len(in.Decisions))
	k.mu.Unlock()
	if err := k.audit.Append(ctx, &storage.AuditEntry{
		ID: uuid.New().String(), EventType: EventApplied, Actor: r.RequestedBy,
		Payload: map[string]any{"request_id": r.ID}, CreatedAt: time.Now().UTC(),
	}); err != nil {
		return approval.Result{}, err
	}
	if fail != nil {
		return approval.Result{}, fail
	}
	return approval.Result{Ref: Ref(r.ID), Reveal: Reveal(r.ID)}, nil
}
