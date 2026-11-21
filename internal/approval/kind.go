// Package approval is the shared approval mechanism of ADR-082 §9: one
// state machine for every change that needs someone other than its
// requester to accept it. A consumer (a steward grant, a widening
// sharing change, a credential for another principal) registers a Kind;
// the mechanism freezes the kind's payload with a digest and the state
// it was frozen against, collects N decisions from eligible approvers
// who are never the requester nor a beneficiary, computes expiry from
// the clock, ends a request stale when its preconditions moved, and
// runs the kind's apply step in the same transaction as the last
// approval. Every transition is written to the audit log in that
// transaction too.
package approval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Filing is what a consumer route files: the requester, the operation
// and why.
type Filing struct {
	Kind   string
	Action string
	// Requester is the authenticated caller filing the request.
	Requester *auth.Principal
	// SubjectID is the principal the request is about, "" when none.
	SubjectID string
	Reason    string
	// Payload is the operation as the route received it; the kind's
	// Validate returns the form to freeze. It never holds a secret.
	Payload json.RawMessage
}

// Result is what a kind's Apply produced.
type Result struct {
	// Ref names the row the approval wrote (a grant, membership,
	// identity or key id); it is stored as the request's result_ref.
	Ref string
	// Reveal is handed to the final approver in its response, once, and
	// never stored or logged: a credential.issue kind's secret. nil for
	// every kind that produces none.
	Reveal any
}

// Kind is one consumer of the mechanism. The four functions are
// required; the store and the state machine are the mechanism's and
// know no kind.
type Kind struct {
	// Name is the kind's discriminator ("steward.grant").
	Name string
	// Actions are the operations the kind files ("share.grant.create");
	// a filing naming another is refused.
	Actions []string
	// MinReason is the shortest reason a filing may give, in
	// characters; 0 makes the reason optional.
	MinReason int
	// Required returns N for a new request: the kind's own setting
	// (approvals.<kind>.required) or a fixed value. It is frozen onto the
	// request when filed. nil means 1.
	Required func() int
	// SubjectMayApprove admits the request's subject as an approver: the
	// one kind whose subject is its recipient by design (identity.bind).
	SubjectMayApprove bool

	// Validate (a) checks the filing and returns the payload to freeze.
	// It runs in the filing's transaction. An error refuses the filing:
	// an *Error keeps its code, any other error is CodeInvalid.
	Validate func(ctx context.Context, f Filing) (json.RawMessage, error)
	// Fingerprint (b) returns the current state the request's payload
	// depends on (a policy's version, a membership's expiry, a
	// principal's status). It runs when the request is filed, to record
	// its preconditions, and again inside the last approval's
	// transaction: a different answer ends the request stale and
	// nothing is applied.
	Fingerprint func(ctx context.Context, r *storage.ApprovalRequest) (json.RawMessage, error)
	// Eligible (c) reports whether approver may decide r at all: it
	// holds the kind's approver scope, or is the owner where the kind
	// admits the owner (ByScope, OwnerOnly). The mechanism refuses the
	// requester, the subject and the beneficiaries before asking.
	Eligible func(ctx context.Context, r *storage.ApprovalRequest, approver *auth.Principal) (bool, error)
	// Apply (d) applies r's payload. It runs in the last approval's
	// transaction, after the preconditions held: it writes only through
	// stores that join the transaction of ctx, and an error rolls back
	// the approval with it.
	Apply func(ctx context.Context, r *storage.ApprovalRequest) (Result, error)

	// Beneficiaries returns the principals r would benefit, computed at
	// decision time; none of them may decide it. nil means none beyond
	// the subject.
	Beneficiaries func(ctx context.Context, r *storage.ApprovalRequest) ([]string, error)
}

// required returns the N to freeze onto a new request.
func (k *Kind) required() (int, error) {
	if k.Required == nil {
		return 1, nil
	}
	n := k.Required()
	if n < 1 {
		return 0, fmt.Errorf("approval kind %s: required approvals must be at least 1, got %d", k.Name, n)
	}
	return n, nil
}

// ByScope is the eligibility of a kind whose approvers hold scope, the
// owner included (it holds every approval scope).
func ByScope(scope auth.Scope) func(context.Context, *storage.ApprovalRequest, *auth.Principal) (bool, error) {
	return func(_ context.Context, _ *storage.ApprovalRequest, p *auth.Principal) (bool, error) {
		return isOwner(p) || p.HasScope(scope), nil
	}
}

// OwnerOnly is the eligibility of a kind only the owner approves.
func OwnerOnly(_ context.Context, _ *storage.ApprovalRequest, p *auth.Principal) (bool, error) {
	return isOwner(p), nil
}

// isOwner reports whether p is the instance owner.
func isOwner(p *auth.Principal) bool {
	return p != nil && p.Kind == storage.PrincipalKindOwner
}

// Registry holds the registered kinds.
type Registry struct {
	mu    sync.RWMutex
	kinds map[string]*Kind
}

// NewRegistry returns a registry with no kind.
func NewRegistry() *Registry { return &Registry{kinds: map[string]*Kind{}} }

// Register adds k. A kind without a name, an action or one of its four
// functions, or a name already registered, is an error.
func (g *Registry) Register(k Kind) error {
	k.Name = strings.TrimSpace(k.Name)
	switch {
	case k.Name == "":
		return errors.New("approval kind: name is required")
	case len(k.Actions) == 0 || slices.Contains(k.Actions, ""):
		return fmt.Errorf("approval kind %s: at least one action, none empty", k.Name)
	case k.Validate == nil || k.Fingerprint == nil || k.Eligible == nil || k.Apply == nil:
		return fmt.Errorf("approval kind %s: Validate, Fingerprint, Eligible and Apply are required", k.Name)
	case k.MinReason < 0:
		return fmt.Errorf("approval kind %s: negative minimum reason", k.Name)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, taken := g.kinds[k.Name]; taken {
		return fmt.Errorf("approval kind %s: already registered", k.Name)
	}
	k.Actions = slices.Clone(k.Actions)
	g.kinds[k.Name] = &k
	return nil
}

// Lookup returns the kind named name.
func (g *Registry) Lookup(name string) (*Kind, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	k, ok := g.kinds[name]
	return k, ok
}

// Names lists the registered kinds, sorted.
func (g *Registry) Names() []string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	out := make([]string, 0, len(g.kinds))
	for n := range g.kinds {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
