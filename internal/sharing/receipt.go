package sharing

import (
	"context"
	"sync"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Check ids of a read audit entry's trail (ADR-080 §9), in the order a
// request meets them. The transports differ on one step: REST applies
// view_as before scope, gRPC after.
const (
	// CheckScope: the principal holds the scope the route needs.
	CheckScope = "scope"
	// CheckViewAs: the owner's X-Ctxt-View-As swap, or a non-owner's
	// attempt at it.
	CheckViewAs = "view_as"
	// CheckRouteAccess: the capability table opens the route to the
	// viewer, with the capability it needs.
	CheckRouteAccess = "route_access"
	// CheckSharePolicy: the viewer's grants applied to the scoped reads.
	CheckSharePolicy = "share_policy"
	// CheckDenyClass: the principal's deny-class ceiling.
	CheckDenyClass = "deny_class"
	// CheckBackstop: the fail-closed response check (ADR-080 §8).
	CheckBackstop = "backstop"
)

// CheckAuthn is the check id of the authentication method that
// admitted the request, for example "authn.api_key".
func CheckAuthn(method string) string { return "authn." + method }

// Reason codes of a check. They are part of the audit's contract:
// stable snake_case, never reworded.
const (
	ReasonAuthenticated      = "authenticated"
	ReasonScopeHeld          = "scope_held"
	ReasonScopeMissing       = "scope_missing"
	ReasonScopeUndeclared    = "scope_undeclared"
	ReasonOwnerOnly          = "owner_only"
	ReasonPreviewResolved    = "preview_resolved"
	ReasonRouteOpen          = "route_open"
	ReasonCapabilityHeld     = "capability_held"
	ReasonCapabilityMissing  = "capability_missing"
	ReasonRouteUnclassified  = "route_unclassified"
	ReasonNoViewer           = "no_viewer"
	ReasonRowsScoped         = "rows_scoped"
	ReasonNoApplicableGrant  = "no_applicable_grant"
	ReasonNoScopedRead       = "no_scoped_read"
	ReasonCeilingApplied     = "ceiling_applied"
	ReasonNoCeiling          = "no_ceiling"
	ReasonClean              = "clean"
	ReasonNothingReleased    = "nothing_released"
	ReasonHitStripped        = "hit_stripped"
	ReasonHitRefused         = "hit_refused"
	ReasonResponseUnreadable = "response_unreadable"
	// ReasonStreamFiltered: a stream's frames are filtered one by one for
	// the viewer by the stream's handler, so the share policy has no
	// single verdict to record.
	ReasonStreamFiltered = "stream_filtered"
	// ReasonStewardNoStream: a steward-elevated request asked for a
	// stream (the event stream), which a steward grant never opens
	// (ADR-083 §3).
	ReasonStewardNoStream = "steward_no_stream"
	// The authority's guard on the /bos/v1 provisioning routes
	// (CheckRouteAccess): the caller is the configured authority, is
	// not, or none is configured.
	ReasonAuthorityMatched      = "authority_matched"
	ReasonNotAuthority          = "not_authority"
	ReasonAuthorityUnconfigured = "authority_unconfigured"
	// The owner-only guard on credential delivery (CheckRouteAccess): a
	// key, sign-in link, invite or identity for a principal other than
	// the caller, asked for by a caller that is not the owner.
	ReasonCredentialOwnerOnly = "credential_owner_only"
	// The owner guard on the owner principal's own credentials
	// (CheckRouteAccess): an administrator acting on the owner.
	ReasonOwnerProtected = "owner_protected"
	// View-as refusals (CheckViewAs): the route writes, the target is
	// neither a principal nor a share policy, the target failed to
	// resolve.
	ReasonViewAsReadOnly         = "read_only"
	ReasonViewAsUnknownTarget    = "unknown_target"
	ReasonViewAsResolutionFailed = "resolution_failed"
)

// Operation is what a transport knows of a request before any check
// runs.
type Operation struct {
	// Method and Route are the HTTP method and the route template, or
	// the gRPC full method name (Method "" for it); Route is what the
	// entry shows ("GET /objects/{id}").
	Method string
	Route  string
	// Params are the consequential params, namespaced ("query.q",
	// "path.id", "req.limit"); see OperationDigest.
	Params map[string][]string
	// Principal, Identity and AuthMethod are the authenticated caller,
	// its credential and the method that admitted it. A request with no
	// Principal is not attributable and is never recorded.
	Principal  string
	Identity   string
	AuthMethod string
	// Viewer is the caller's viewer when the request arrived.
	Viewer Viewer
	// At is when the request began; the entry is stamped with it, not
	// with when the receipt finished, which for a stream is when it
	// ended. Zero is now.
	At time.Time
	// StewardGrant is the steward grant the request named (ADR-083 §3):
	// the elevated viewer's grant, or the grant of a refused elevation,
	// whose Viewer is the zero viewer. "" for an ordinary request; when
	// left empty it is taken from Viewer.
	StewardGrant string
}

// Receipt collects the audit record of one request by a restricted
// viewer (ADR-080 §9): the checks it met in the order they ran, and how
// the request ended. The transport creates it before the first check
// (BeginReceipt in each transport), carries it on the request context
// (WithReceipt), every check appends to it, and the transport finishes
// it once, on every way out including early refusals, which hands one
// entry to the AuditWriter.
//
// A check the request never reached is absent from the trail, never
// PASS: a refusal ends the trail at the check that refused. NOT_APPLICABLE
// is for a check that ran and had nothing to decide.
//
// Methods are nil-safe, so a check records without asking whether
// auditing is on, and safe for concurrent use.
type Receipt struct {
	op Operation

	mu        sync.Mutex
	checks    []storage.AccessCheck
	refused   bool
	refusals  map[string]bool // check ids a refusal recorded
	viewer    Viewer
	preview   *Preview
	ledger    *Ledger
	discarded bool
	done      bool
	// auditFailed is set once a steward entry could not be committed:
	// the request failed closed and its decision is deferred.
	auditFailed bool
	// commitErr is what the first Commit returned.
	commitErr  error
	commitOnce sync.Once
}

// NewReceipt starts the receipt of op. When the request was
// authenticated, the trail opens with the authn check that admitted it;
// a request elevated into a steward grant (ADR-083 §5) carries the
// steward_grant check next, passed when op.Viewer is that grant's
// elevation. A refused elevation records its code with StewardRefused.
func NewReceipt(op Operation) *Receipt {
	if op.At.IsZero() {
		op.At = time.Now()
	}
	if op.StewardGrant == "" {
		op.StewardGrant = op.Viewer.StewardGrantID()
	}
	r := &Receipt{op: op, viewer: op.Viewer}
	if op.Principal != "" && op.AuthMethod != "" {
		r.add(CheckAuthn(op.AuthMethod), storage.AccessOutcomePass, ReasonAuthenticated)
	}
	if op.StewardGrant != "" && op.Viewer.StewardGrantID() == op.StewardGrant {
		r.add(CheckStewardGrant, storage.AccessOutcomePass, StewardReasonGrantActive)
	}
	return r
}

// Pass records a check the request met.
func (r *Receipt) Pass(check, reason string) { r.record(check, storage.AccessOutcomePass, reason) }

// NotApplicable records a check that ran and had nothing to decide.
func (r *Receipt) NotApplicable(check, reason string) {
	r.record(check, storage.AccessOutcomeNotApplicable, reason)
}

// Fail records a check the request failed that did not stop it, such as
// a backstop hit it stripped. The decision stays allowed.
func (r *Receipt) Fail(check, reason string) { r.record(check, storage.AccessOutcomeFail, reason) }

// Refuse records the check that refused the request: the decision is
// denied.
func (r *Receipt) Refuse(check, reason string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.refuse(check, reason)
}

// refuse records a refusing FAIL, which no later result of the same
// check replaces, so a denied entry always holds its FAIL. Callers hold
// r.mu.
func (r *Receipt) refuse(check, reason string) {
	if r.refusals == nil {
		r.refusals = map[string]bool{}
	}
	if !r.refusals[check] {
		r.set(check, storage.AccessOutcomeFail, reason)
	}
	r.refusals[check] = true
	r.refused = true
}

// Defer records a check that could not decide and made the request fail
// closed: the decision is deferred, unless a check also refused. The
// UNKNOWN it records is what makes the entry deferred (the decision is
// derived from the trail, see decision), and it outranks a stripped hit
// on the same check id: a stream that stripped one message and could not
// read a later one is deferred, whichever came first.
func (r *Receipt) Defer(check, reason string) { r.record(check, storage.AccessOutcomeUnknown, reason) }

func (r *Receipt) record(check string, o storage.AccessOutcome, reason string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.add(check, o, reason)
}

// add appends a check; one already recorded under the same id (a stream
// checks every message) is replaced only by a worse result, and never
// once a refusal holds it.
func (r *Receipt) add(check string, o storage.AccessOutcome, reason string) {
	if r.refusals[check] {
		return
	}
	for i := range r.checks {
		if r.checks[i].Check == check && rank(o) <= rank(r.checks[i].Outcome) {
			return
		}
	}
	r.set(check, o, reason)
}

// set puts the check in the trail, in place when its id is there.
func (r *Receipt) set(check string, o storage.AccessOutcome, reason string) {
	c := storage.AccessCheck{Check: check, Outcome: o, Reason: reason}
	for i := range r.checks {
		if r.checks[i].Check == check {
			r.checks[i] = c
			return
		}
	}
	r.checks = append(r.checks, c)
}

// rank orders the results of one check id, worst last: a result that
// could not decide (UNKNOWN) outranks a FAIL that did not stop the
// request, since the request then failed closed.
func rank(o storage.AccessOutcome) int {
	switch o {
	case storage.AccessOutcomeUnknown:
		return 2
	case storage.AccessOutcomeFail:
		return 1
	}
	return 0
}

// Refused reports whether a check has refused the request: it never
// reached the handler, or the handler's reads left it nothing.
func (r *Receipt) Refused() bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.refused
}

// SetLedger gives the receipt the request's ledger, whose released rows
// become the entry's counts and object ids.
func (r *Receipt) SetLedger(l *Ledger) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.ledger = l
	r.mu.Unlock()
}

// SetPreview records that the owner is previewing another viewer: the
// entry is the owner's request, the checks and policy versions are the
// previewed viewer's.
func (r *Receipt) SetPreview(p Preview, viewer Viewer) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.preview, r.viewer = &p, viewer
	r.add(CheckViewAs, storage.AccessOutcomePass, ReasonPreviewResolved)
	r.mu.Unlock()
}

// SettlePolicy records the share-policy and deny-class checks once the
// handler has run, from what the request's reads did. A handler that
// made no scoped read leaves both NOT_APPLICABLE; a viewer no grant
// applies to is refused by the share policy.
func (r *Receipt) SettlePolicy() { r.settle(false) }

// SettleStream is SettlePolicy for a stream, which ends it: the
// handler filters every frame for the viewer, so the share policy is
// NOT_APPLICABLE with reason stream_filtered and never refuses the
// request; the ceiling is recorded as for any call. On gRPC the
// stream's backstop checks come first, since they run as each message
// is sent (ADR-080 §9).
func (r *Receipt) SettleStream() { r.settle(true) }

func (r *Receipt) settle(stream bool) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if stream {
		r.add(CheckSharePolicy, storage.AccessOutcomeNotApplicable, ReasonStreamFiltered)
		r.settleCeiling()
		return
	}
	if !r.ledger.Scoped() {
		r.add(CheckSharePolicy, storage.AccessOutcomeNotApplicable, ReasonNoScopedRead)
		r.add(CheckDenyClass, storage.AccessOutcomeNotApplicable, ReasonNoScopedRead)
		return
	}
	if r.viewer.MatchesNothing() {
		r.refuse(CheckSharePolicy, ReasonNoApplicableGrant)
	} else {
		r.add(CheckSharePolicy, storage.AccessOutcomePass, ReasonRowsScoped)
	}
	r.settleCeiling()
}

// settleCeiling records the deny-class check from what the reads did.
// Callers hold r.mu.
func (r *Receipt) settleCeiling() {
	if !r.ledger.Scoped() {
		r.add(CheckDenyClass, storage.AccessOutcomeNotApplicable, ReasonNoScopedRead)
		return
	}
	if len(r.viewer.Plan().DenyClasses) == 0 {
		r.add(CheckDenyClass, storage.AccessOutcomeNotApplicable, ReasonNoCeiling)
		return
	}
	r.add(CheckDenyClass, storage.AccessOutcomePass, ReasonCeilingApplied)
}

// Discard drops the receipt: nothing is recorded. The owner's own
// refused requests end this way, since owner requests are not logged.
func (r *Receipt) Discard() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.discarded = true
	r.mu.Unlock()
}

// Finish hands the entry of the request to w, once, stamped with the
// time the receipt began; later calls do nothing. A discarded receipt, and a request with no attributable
// principal, record nothing: an unauthenticated flood must not grow the
// table. w may be nil.
//
// An elevated request's entry never goes to the buffer: Finish commits
// it durably (Commit), after the fact, for a path that did not commit
// it before its answer, and returns the commit's error, which a caller
// must not discard. The transports commit it before the answer.
func (r *Receipt) Finish(w *AuditWriter) error {
	return r.Commit(context.Background(), w)
}

// entry builds the audit entry; nil when nothing is to be recorded.
// Callers hold r.mu.
func (r *Receipt) entry() *storage.AccessEntry {
	if r.discarded {
		return nil
	}
	e := &storage.AccessEntry{
		At:              r.op.At.UTC(),
		PrincipalID:     r.op.Principal,
		IdentityID:      r.op.Identity,
		Route:           r.op.Route,
		Decision:        r.decision(),
		Checks:          append([]storage.AccessCheck{}, r.checks...),
		OperationDigest: OperationDigest(r.op.Method, r.op.Route, r.op.Params),
		PolicyVersions:  r.viewer.PolicyKey(),
		ObjectIDs:       r.ledger.Objects(),
		StewardGrantID:  r.op.StewardGrant,
	}
	e.ObjectCount = len(e.ObjectIDs)
	e.EntityCount = len(r.ledger.Entities())
	if r.preview != nil {
		e.PrincipalID, e.IdentityID = r.preview.Principal, r.preview.Identity
		e.PolicyVersions = "preview=" + r.preview.Target + ";" + e.PolicyVersions
	}
	if e.PrincipalID == "" {
		return nil
	}
	return e
}

// decision derives the verdict from the trail (ADR-080 §9), so an entry
// is valid by construction: denied when a check refused (its FAIL is
// held), else deferred when any check is UNKNOWN, else allowed.
func (r *Receipt) decision() storage.AccessDecision {
	switch {
	case r.auditFailed:
		// The answer was replaced by AUDIT_UNAVAILABLE: no verdict was
		// released, whatever the checks had decided.
		return storage.AccessDecisionDeferred
	case r.refused:
		return storage.AccessDecisionDenied
	}
	for _, c := range r.checks {
		if c.Outcome == storage.AccessOutcomeUnknown {
			return storage.AccessDecisionDeferred
		}
	}
	return storage.AccessDecisionAllowed
}

type receiptKey struct{}

// WithReceipt returns a context carrying r, for the checks the request
// meets in later middleware.
func WithReceipt(ctx context.Context, r *Receipt) context.Context {
	return context.WithValue(ctx, receiptKey{}, r)
}

// ReceiptFrom returns the receipt attached to ctx, or nil: the request
// is not audited (the owner's, or an unattributed one) and recording
// into it is a no-op.
func ReceiptFrom(ctx context.Context) *Receipt {
	r, _ := ctx.Value(receiptKey{}).(*Receipt)
	return r
}
