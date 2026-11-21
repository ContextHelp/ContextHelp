package storagetest

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// approvalT0 is the approvals clock origin, on a millisecond boundary.
var approvalT0 = time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)

// approvalNobody is a principal id no fixture creates.
const approvalNobody = "prn_nobody"

// ApprovalsConformance pins the approval store contract every driver
// implements (ADR-082 §9): requests with a frozen payload and N, the
// one-decision-per-approver rule, transitions out of requested only,
// the newest-first listing, and transactions that the store and the
// audit log join and roll back together.
func ApprovalsConformance(t *testing.T, drv storage.StorageDriver) {
	t.Helper()
	if drv.Approvals() == nil {
		t.Fatal("Approvals() returned nil")
	}
	for _, c := range []struct {
		name string
		fn   func(*testing.T, storage.StorageDriver)
	}{
		{"create and get round trip", approvalConfRoundTrip},
		{"create refusals", approvalConfCreateRefusals},
		{"list filters newest first", approvalConfList},
		{"decisions one per approver, oldest first", approvalConfDecisions},
		{"decision refusals", approvalConfDecisionRefusals},
		{"transition out of requested only", approvalConfTransition},
		{"transaction commits and rolls back with the audit log", approvalConfInTx},
		{"nested transaction joins", approvalConfNestedTx},
		{"concurrent transitions: exactly one wins", approvalConfConcurrent},
	} {
		t.Run(c.name, func(t *testing.T) { c.fn(t, drv) })
	}
}

// approvalFixture is a requester and two approvers named after the
// test, so cases never collide.
type approvalFixture struct {
	requester, alice, bob string
}

func newApprovalFixture(t *testing.T, drv storage.StorageDriver, tag string) approvalFixture {
	t.Helper()
	f := approvalFixture{requester: "prn_apr_req_" + tag, alice: "prn_apr_alice_" + tag, bob: "prn_apr_bob_" + tag}
	for _, id := range []string{f.requester, f.alice, f.bob} {
		newPrincipal(t, drv.Auth(), id, approvalT0)
	}
	return f
}

// request is a valid new request by f.requester about f.alice.
func (f approvalFixture) request(kind string, at time.Time) *storage.ApprovalRequest {
	return &storage.ApprovalRequest{
		Kind: kind, Action: kind + ".do", RequestedBy: f.requester, SubjectID: f.alice, Reason: "conformance",
		Payload: json.RawMessage(`{"a":1}`), PayloadDigest: "v1:sha256:aa", Preconditions: json.RawMessage(`{"v":1}`),
		ApprovalsRequired: 2, RequestedAt: at, ExpiresAt: at.Add(72 * time.Hour),
	}
}

func createApproval(t *testing.T, drv storage.StorageDriver, r *storage.ApprovalRequest) *storage.ApprovalRequest {
	t.Helper()
	if err := drv.Approvals().Create(context.Background(), r); err != nil {
		t.Fatalf("Create: %v", err)
	}
	return r
}

func approvalConfRoundTrip(t *testing.T, drv storage.StorageDriver) {
	ctx := context.Background()
	f := newApprovalFixture(t, drv, "rt")
	r := createApproval(t, drv, f.request("test.rt", approvalT0))
	if len(r.ID) != len("apr_")+26 || r.ID[:4] != "apr_" || r.Status != storage.ApprovalRequested {
		t.Fatalf("created = %+v, want apr_<ULID> in requested", r)
	}
	got, err := drv.Approvals().Get(ctx, r.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	checkRoundTrip(t, got, f)

	// No subject and no preconditions: stored as NULL and {}.
	bare := f.request("test.rt", approvalT0)
	bare.SubjectID, bare.Preconditions = "", nil
	createApproval(t, drv, bare)
	got, err = drv.Approvals().Get(ctx, bare.ID)
	if err != nil || got.SubjectID != "" || string(got.Preconditions) != `{}` {
		t.Errorf("bare = %+v, %v", got, err)
	}
	if !got.ExpiredAt(approvalT0.Add(72*time.Hour)) || got.ExpiredAt(approvalT0.Add(72*time.Hour-time.Millisecond)) {
		t.Error("ExpiredAt: expired from expires_at on, not before")
	}
	if _, err := drv.Approvals().Get(ctx, "apr_missing"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Get(missing) = %v, want ErrNotFound", err)
	}
}

// checkRoundTrip compares a stored request with what f.request filed.
func checkRoundTrip(t *testing.T, got *storage.ApprovalRequest, f approvalFixture) {
	t.Helper()
	filed := got.Kind == "test.rt" && got.Action == "test.rt.do" && got.RequestedBy == f.requester &&
		got.SubjectID == f.alice && got.Reason == "conformance"
	frozen := string(got.Payload) == `{"a":1}` && got.PayloadDigest == "v1:sha256:aa" &&
		string(got.Preconditions) == `{"v":1}` && got.ApprovalsRequired == 2
	times := got.RequestedAt.Equal(approvalT0) && got.ExpiresAt.Equal(approvalT0.Add(72*time.Hour)) && got.DecidedAt == nil
	pending := got.Status == storage.ApprovalRequested && got.ResultRef == "" && got.Decisions != nil && len(got.Decisions) == 0
	if !filed || !frozen || !times || !pending {
		t.Errorf("Get = %+v", got)
	}
}

func approvalConfCreateRefusals(t *testing.T, drv storage.StorageDriver) {
	ctx := context.Background()
	f := newApprovalFixture(t, drv, "refuse")
	for name, mutate := range map[string]func(*storage.ApprovalRequest){
		"no kind":            func(r *storage.ApprovalRequest) { r.Kind = "" },
		"no action":          func(r *storage.ApprovalRequest) { r.Action = " " },
		"no requester":       func(r *storage.ApprovalRequest) { r.RequestedBy = "" },
		"no digest":          func(r *storage.ApprovalRequest) { r.PayloadDigest = "" },
		"payload not JSON":   func(r *storage.ApprovalRequest) { r.Payload = json.RawMessage(`{`) },
		"no payload":         func(r *storage.ApprovalRequest) { r.Payload = nil },
		"preconditions bad":  func(r *storage.ApprovalRequest) { r.Preconditions = json.RawMessage(`nope`) },
		"N zero":             func(r *storage.ApprovalRequest) { r.ApprovalsRequired = 0 },
		"expires at filing":  func(r *storage.ApprovalRequest) { r.ExpiresAt = r.RequestedAt },
		"filed approved":     func(r *storage.ApprovalRequest) { r.Status = storage.ApprovalApproved },
		"unknown requester":  func(r *storage.ApprovalRequest) { r.RequestedBy = approvalNobody },
		"unknown subject":    func(r *storage.ApprovalRequest) { r.SubjectID = approvalNobody },
		"expires before now": func(r *storage.ApprovalRequest) { r.ExpiresAt = r.RequestedAt.Add(-time.Hour) },
	} {
		r := f.request("test.refuse", approvalT0)
		mutate(r)
		err := drv.Approvals().Create(ctx, r)
		if err == nil {
			t.Errorf("%s: Create accepted %+v", name, r)
			continue
		}
		if name == "unknown requester" || name == "unknown subject" {
			if !errors.Is(err, storage.ErrNotFound) {
				t.Errorf("%s: %v, want ErrNotFound", name, err)
			}
		}
	}
	if err := drv.Approvals().Create(ctx, nil); err == nil {
		t.Error("Create(nil) accepted")
	}
	r := createApproval(t, drv, f.request("test.refuse", approvalT0))
	dup := f.request("test.refuse", approvalT0)
	dup.ID = r.ID
	if err := drv.Approvals().Create(ctx, dup); !errors.Is(err, storage.ErrAlreadyExists) {
		t.Errorf("duplicate id: %v, want ErrAlreadyExists", err)
	}
	list, err := drv.Approvals().List(ctx, storage.ApprovalFilter{Kind: "test.refuse"})
	if err != nil || len(list) != 1 {
		t.Errorf("after refusals: %d requests, %v; want only the valid one", len(list), err)
	}
}

func approvalConfList(t *testing.T, drv storage.StorageDriver) {
	ctx := context.Background()
	f := newApprovalFixture(t, drv, "list")
	first := createApproval(t, drv, f.request("test.list", approvalT0))
	second := f.request("test.list", approvalT0.Add(time.Minute))
	second.RequestedBy, second.SubjectID = f.bob, ""
	createApproval(t, drv, second)
	third := createApproval(t, drv, f.request("test.list", approvalT0.Add(2*time.Minute)))
	createApproval(t, drv, f.request("test.list.other", approvalT0.Add(3*time.Minute)))
	if err := drv.Approvals().Transition(ctx, third.ID, storage.ApprovalWithdrawn, approvalT0.Add(4*time.Minute), ""); err != nil {
		t.Fatal(err)
	}
	ids := func(f storage.ApprovalFilter) []string {
		t.Helper()
		list, err := drv.Approvals().List(ctx, f)
		if err != nil {
			t.Fatalf("List(%+v): %v", f, err)
		}
		out := make([]string, len(list))
		for i, r := range list {
			out[i] = r.ID
			if r.Decisions == nil {
				t.Errorf("List left Decisions nil on %s", r.ID)
			}
		}
		return out
	}
	for name, c := range map[string]struct {
		f    storage.ApprovalFilter
		want []string
	}{
		"kind, newest first": {storage.ApprovalFilter{Kind: "test.list"}, []string{third.ID, second.ID, first.ID}},
		"status":             {storage.ApprovalFilter{Kind: "test.list", Status: storage.ApprovalRequested}, []string{second.ID, first.ID}},
		"requested by":       {storage.ApprovalFilter{Kind: "test.list", RequestedBy: f.bob}, []string{second.ID}},
		"subject":            {storage.ApprovalFilter{Kind: "test.list", SubjectID: f.alice}, []string{third.ID, first.ID}},
		"limit":              {storage.ApprovalFilter{Kind: "test.list", Limit: 2}, []string{third.ID, second.ID}},
		"no match":           {storage.ApprovalFilter{Kind: "test.list.none"}, []string{}},
	} {
		if got := ids(c.f); !slices.Equal(got, c.want) {
			t.Errorf("%s: %v, want %v", name, got, c.want)
		}
	}
}

func approvalConfDecisions(t *testing.T, drv storage.StorageDriver) {
	ctx := context.Background()
	f := newApprovalFixture(t, drv, "dec")
	r := createApproval(t, drv, f.request("test.dec", approvalT0))
	add := func(approver string, v storage.ApprovalVerdict, at time.Time) error {
		return drv.Approvals().AddDecision(ctx, &storage.ApprovalDecision{
			RequestID: r.ID, ApproverID: approver, Decision: v, PayloadDigest: r.PayloadDigest, Note: "n " + approver, DecidedAt: at,
		})
	}
	if err := add(f.bob, storage.ApprovalVerdictApprove, approvalT0.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := add(f.alice, storage.ApprovalVerdictReject, approvalT0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := add(f.bob, storage.ApprovalVerdictReject, approvalT0.Add(3*time.Minute)); !errors.Is(err, storage.ErrAlreadyExists) {
		t.Errorf("second decision by one approver: %v, want ErrAlreadyExists", err)
	}
	got, err := drv.Approvals().Get(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Decisions) != 2 {
		t.Fatalf("decisions = %+v, want 2", got.Decisions)
	}
	a, b := got.Decisions[0], got.Decisions[1]
	if a.ApproverID != f.alice || a.Decision != storage.ApprovalVerdictReject || a.Note != "n "+f.alice ||
		a.PayloadDigest != r.PayloadDigest || !a.DecidedAt.Equal(approvalT0.Add(time.Minute)) || a.RequestID != r.ID {
		t.Errorf("first decision = %+v", a)
	}
	if b.ApproverID != f.bob || b.Decision != storage.ApprovalVerdictApprove {
		t.Errorf("second decision = %+v", b)
	}
	list, err := drv.Approvals().List(ctx, storage.ApprovalFilter{Kind: "test.dec"})
	if err != nil || len(list) != 1 || len(list[0].Decisions) != 2 {
		t.Errorf("List carries the decisions: %+v, %v", list, err)
	}
}

func approvalConfDecisionRefusals(t *testing.T, drv storage.StorageDriver) {
	ctx := context.Background()
	f := newApprovalFixture(t, drv, "decref")
	r := createApproval(t, drv, f.request("test.decref", approvalT0))
	valid := func() *storage.ApprovalDecision {
		return &storage.ApprovalDecision{RequestID: r.ID, ApproverID: f.alice, Decision: storage.ApprovalVerdictApprove, PayloadDigest: "d"}
	}
	for name, c := range map[string]struct {
		mutate   func(*storage.ApprovalDecision)
		sentinel error
	}{
		"no request":       {func(d *storage.ApprovalDecision) { d.RequestID = "" }, nil},
		"no approver":      {func(d *storage.ApprovalDecision) { d.ApproverID = "" }, nil},
		"no digest":        {func(d *storage.ApprovalDecision) { d.PayloadDigest = "" }, nil},
		"unknown verdict":  {func(d *storage.ApprovalDecision) { d.Decision = "maybe" }, nil},
		"unknown request":  {func(d *storage.ApprovalDecision) { d.RequestID = "apr_missing" }, storage.ErrNotFound},
		"unknown approver": {func(d *storage.ApprovalDecision) { d.ApproverID = approvalNobody }, storage.ErrNotFound},
	} {
		d := valid()
		c.mutate(d)
		err := drv.Approvals().AddDecision(ctx, d)
		if err == nil || (c.sentinel != nil && !errors.Is(err, c.sentinel)) {
			t.Errorf("%s: %v, want a refusal (%v)", name, err, c.sentinel)
		}
	}
	if err := drv.Approvals().AddDecision(ctx, nil); err == nil {
		t.Error("AddDecision(nil) accepted")
	}
	got, err := drv.Approvals().Get(ctx, r.ID)
	if err != nil || len(got.Decisions) != 0 {
		t.Errorf("after refusals: %+v, %v; want no decision", got, err)
	}
}

func approvalConfTransition(t *testing.T, drv storage.StorageDriver) {
	ctx := context.Background()
	f := newApprovalFixture(t, drv, "tr")
	r := createApproval(t, drv, f.request("test.tr", approvalT0))
	at := approvalT0.Add(time.Hour)
	for _, bad := range []storage.ApprovalStatus{storage.ApprovalRequested, "expired", ""} {
		if err := drv.Approvals().Transition(ctx, r.ID, bad, at, ""); err == nil {
			t.Errorf("Transition to %q accepted", bad)
		}
	}
	if err := drv.Approvals().Transition(ctx, r.ID, storage.ApprovalApproved, at, "res_1"); err != nil {
		t.Fatalf("Transition: %v", err)
	}
	got, err := drv.Approvals().Get(ctx, r.ID)
	if err != nil || got.Status != storage.ApprovalApproved || got.ResultRef != "res_1" || got.DecidedAt == nil || !got.DecidedAt.Equal(at) {
		t.Fatalf("after Transition = %+v, %v", got, err)
	}
	if got.ExpiredAt(approvalT0.Add(100 * time.Hour)) {
		t.Error("a decided request is never expired")
	}
	for _, to := range []storage.ApprovalStatus{storage.ApprovalRejected, storage.ApprovalWithdrawn, storage.ApprovalStale, storage.ApprovalApproved} {
		if err := drv.Approvals().Transition(ctx, r.ID, to, at.Add(time.Hour), "res_2"); !errors.Is(err, storage.ErrApprovalNotPending) {
			t.Errorf("second Transition to %s: %v, want ErrApprovalNotPending", to, err)
		}
	}
	got, _ = drv.Approvals().Get(ctx, r.ID)
	if got.Status != storage.ApprovalApproved || got.ResultRef != "res_1" || !got.DecidedAt.Equal(at) {
		t.Errorf("a refused transition changed the row: %+v", got)
	}
	if err := drv.Approvals().Transition(ctx, "apr_missing", storage.ApprovalRejected, at, ""); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Transition(missing) = %v, want ErrNotFound", err)
	}
}

// auditOf returns the audit entries of event type kind.
func auditOf(t *testing.T, drv storage.StorageDriver, kind string) []*storage.AuditEntry {
	t.Helper()
	entries, _, err := drv.AuditLog().List(context.Background(), storage.AuditFilter{EventType: kind, Limit: 100})
	if err != nil {
		t.Fatalf("audit list: %v", err)
	}
	return entries
}

// txAudit appends one audit entry of event in ctx's transaction.
func txAudit(ctx context.Context, drv storage.StorageDriver, event string) error {
	return drv.AuditLog().Append(ctx, &storage.AuditEntry{ID: "aud_" + event, EventType: event, Actor: storage.OwnerPrincipal, Payload: map[string]any{}, CreatedAt: approvalT0})
}

func approvalConfInTx(t *testing.T, drv storage.StorageDriver) {
	ctx := context.Background()
	f := newApprovalFixture(t, drv, "tx")
	st := drv.Approvals()
	committed := approvalConfCommit(t, drv, f)
	approvalConfRollback(t, drv, f, committed)

	// A panic rolls back too.
	func() {
		defer func() { _ = recover() }()
		_ = st.InTx(ctx, func(ctx context.Context) error {
			if err := st.Transition(ctx, committed.ID, storage.ApprovalStale, approvalT0, ""); err != nil {
				return err
			}
			panic("boom")
		})
	}()
	if got, _ := st.Get(ctx, committed.ID); got.Status != storage.ApprovalRequested {
		t.Errorf("after a panic the request is %s, want requested", got.Status)
	}
}

// approvalConfCommit writes a request, a decision and an audit entry in
// one transaction and checks all three committed.
func approvalConfCommit(t *testing.T, drv storage.StorageDriver, f approvalFixture) *storage.ApprovalRequest {
	t.Helper()
	ctx := context.Background()
	st := drv.Approvals()
	var committed *storage.ApprovalRequest
	if err := st.InTx(ctx, func(ctx context.Context) error {
		committed = f.request("test.tx", approvalT0)
		if err := st.Create(ctx, committed); err != nil {
			return err
		}
		// Reads in the transaction see its own writes.
		if _, err := st.GetForUpdate(ctx, committed.ID); err != nil {
			return err
		}
		if err := st.AddDecision(ctx, &storage.ApprovalDecision{RequestID: committed.ID, ApproverID: f.alice, Decision: storage.ApprovalVerdictApprove, PayloadDigest: "d"}); err != nil {
			return err
		}
		return txAudit(ctx, drv, "conformance.tx.committed")
	}); err != nil {
		t.Fatalf("InTx: %v", err)
	}
	got, err := st.Get(ctx, committed.ID)
	if err != nil || len(got.Decisions) != 1 {
		t.Errorf("committed request = %+v, %v", got, err)
	}
	if n := len(auditOf(t, drv, "conformance.tx.committed")); n != 1 {
		t.Errorf("committed audit entries: %d, want 1", n)
	}
	return committed
}

// approvalConfRollback writes in a transaction whose fn fails and checks
// nothing of it, the audit entry included, survived.
func approvalConfRollback(t *testing.T, drv storage.StorageDriver, f approvalFixture, committed *storage.ApprovalRequest) {
	t.Helper()
	ctx := context.Background()
	st := drv.Approvals()
	boom := errors.New("boom")
	var rolled *storage.ApprovalRequest
	err := st.InTx(ctx, func(ctx context.Context) error {
		rolled = f.request("test.tx", approvalT0)
		if err := st.Create(ctx, rolled); err != nil {
			return err
		}
		if err := st.AddDecision(ctx, &storage.ApprovalDecision{RequestID: committed.ID, ApproverID: f.bob, Decision: storage.ApprovalVerdictApprove, PayloadDigest: "d"}); err != nil {
			return err
		}
		if err := st.Transition(ctx, committed.ID, storage.ApprovalApproved, approvalT0, "res"); err != nil {
			return err
		}
		if err := txAudit(ctx, drv, "conformance.tx.rolled"); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("InTx = %v, want fn's error", err)
	}
	if _, err := st.Get(ctx, rolled.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("a request created in a rolled-back transaction: %v, want ErrNotFound", err)
	}
	got, err := st.Get(ctx, committed.ID)
	if err != nil || got.Status != storage.ApprovalRequested || len(got.Decisions) != 1 || got.ResultRef != "" {
		t.Errorf("after rollback = %+v, %v; want the committed state", got, err)
	}
	if n := len(auditOf(t, drv, "conformance.tx.rolled")); n != 0 {
		t.Errorf("an audit entry of a rolled-back transaction survived (%d)", n)
	}
}

func approvalConfNestedTx(t *testing.T, drv storage.StorageDriver) {
	ctx := context.Background()
	f := newApprovalFixture(t, drv, "nest")
	st := drv.Approvals()
	r := createApproval(t, drv, f.request("test.nest", approvalT0))
	inner := errors.New("inner")
	err := st.InTx(ctx, func(ctx context.Context) error {
		if err := st.Transition(ctx, r.ID, storage.ApprovalRejected, approvalT0, ""); err != nil {
			return err
		}
		// The inner InTx joins: its writes belong to the outer
		// transaction and its error rolls the whole of it back.
		return st.InTx(ctx, func(ctx context.Context) error {
			if err := st.AddDecision(ctx, &storage.ApprovalDecision{RequestID: r.ID, ApproverID: f.alice, Decision: storage.ApprovalVerdictReject, PayloadDigest: "d"}); err != nil {
				return err
			}
			return inner
		})
	})
	if !errors.Is(err, inner) {
		t.Fatalf("InTx = %v, want the inner error", err)
	}
	got, err := st.Get(ctx, r.ID)
	if err != nil || got.Status != storage.ApprovalRequested || len(got.Decisions) != 0 {
		t.Errorf("after the rollback = %+v, %v", got, err)
	}
	if err := st.InTx(ctx, func(ctx context.Context) error {
		return st.InTx(ctx, func(ctx context.Context) error {
			return st.Transition(ctx, r.ID, storage.ApprovalRejected, approvalT0, "")
		})
	}); err != nil {
		t.Fatalf("nested commit: %v", err)
	}
	if got, _ := st.Get(ctx, r.ID); got.Status != storage.ApprovalRejected {
		t.Errorf("nested commit left %s", got.Status)
	}
}

// Deciders of one request each lock it, check it is pending and close
// it; under concurrency exactly one closes it and the others see it
// closed, never both "won".
func approvalConfConcurrent(t *testing.T, drv storage.StorageDriver) {
	ctx := context.Background()
	f := newApprovalFixture(t, drv, "conc")
	st := drv.Approvals()
	r := createApproval(t, drv, f.request("test.conc", approvalT0))
	const deciders = 6
	var wg sync.WaitGroup
	var mu sync.Mutex
	won, closed := 0, 0
	for i := range deciders {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := st.InTx(ctx, func(ctx context.Context) error {
				got, err := st.GetForUpdate(ctx, r.ID)
				if err != nil {
					return err
				}
				if got.Status != storage.ApprovalRequested {
					return storage.ErrApprovalNotPending
				}
				return st.Transition(ctx, r.ID, storage.ApprovalApproved, approvalT0, "res")
			})
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				won++
			case errors.Is(err, storage.ErrApprovalNotPending):
				closed++
			default:
				t.Errorf("decider %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	if won != 1 || closed != deciders-1 {
		t.Errorf("won %d, saw it closed %d; want exactly one winner", won, closed)
	}
}
