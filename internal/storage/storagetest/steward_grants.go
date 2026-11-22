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

// stewardT0 is the steward grants clock origin, on a millisecond
// boundary.
var stewardT0 = time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)

// StewardGrantsConformance pins the steward grant store contract every
// driver implements (ADR-083 §2): one grant per approved request, a
// scope and digest kept verbatim, a status that is stored as approved
// or revoked and never as the computed active or expired, a revocation
// that only an approved grant takes, and writes that join the
// transaction of the approval that makes them.
func StewardGrantsConformance(t *testing.T, drv storage.StorageDriver) {
	t.Helper()
	if drv.Sharing().StewardGrants() == nil {
		t.Fatal("Sharing().StewardGrants() returned nil")
	}
	for _, c := range []struct {
		name string
		fn   func(*testing.T, storage.StorageDriver)
	}{
		{"create and get round trip", stewardConfRoundTrip},
		{"renewal and the clock", stewardConfRenewalAndClock},
		{"scope and digest are kept verbatim", stewardConfVerbatim},
		{"create refusals", stewardConfCreateRefusals},
		{"one grant per request", stewardConfOneGrantPerRequest},
		{"list filters newest first", stewardConfList},
		{"revoke keeps the first revocation", stewardConfRevoke},
		{"concurrent revokes: exactly one wins", stewardConfConcurrentRevoke},
		{"transaction commits and rolls back with the approval", stewardConfInTx},
	} {
		t.Run(c.name, func(t *testing.T) { c.fn(t, drv) })
	}
}

// stewardFixture is a steward and an approver, plus requests to hang
// grants on, named after the test so cases never collide.
type stewardFixture struct {
	drv              storage.StorageDriver
	tag              string
	steward, approve string
	n                int
}

func newStewardFixture(t *testing.T, drv storage.StorageDriver, tag string) *stewardFixture {
	t.Helper()
	f := &stewardFixture{drv: drv, tag: tag, steward: "prn_stg_steward_" + tag, approve: "prn_stg_appr_" + tag}
	for _, id := range []string{f.steward, f.approve} {
		newPrincipal(t, drv.Auth(), id, stewardT0)
	}
	return f
}

// request files an approval request a grant can come from.
func (f *stewardFixture) request(t *testing.T) *storage.ApprovalRequest {
	t.Helper()
	f.n++
	at := stewardT0.Add(time.Duration(f.n) * time.Minute)
	r := &storage.ApprovalRequest{
		Kind: "steward.grant", Action: "steward.grant.request", RequestedBy: f.approve, SubjectID: f.steward, Reason: "conformance",
		Payload: json.RawMessage(`{"a":1}`), PayloadDigest: "v1:sha256:aa", ApprovalsRequired: 1, RequestedAt: at, ExpiresAt: at.Add(72 * time.Hour),
	}
	if err := f.drv.Approvals().Create(context.Background(), r); err != nil {
		t.Fatalf("create request: %v", err)
	}
	return r
}

// grant is a valid new grant on a fresh request, approved at the given
// offset from the origin.
func (f *stewardFixture) grant(t *testing.T, approvedAfter time.Duration) *storage.StewardGrant {
	t.Helper()
	return f.grantOn(f.request(t), approvedAfter)
}

// grantOn is a valid new grant on request r.
func (f *stewardFixture) grantOn(r *storage.ApprovalRequest, approvedAfter time.Duration) *storage.StewardGrant {
	return &storage.StewardGrant{
		RequestID: r.ID, StewardID: f.steward, MatterRef: "CASE-" + f.tag,
		Scope: json.RawMessage(`{"data":"type==contact"}`), ScopeDigest: "v1:sha256:bb",
		NotBefore: stewardT0.Add(time.Hour), ExpiresAt: stewardT0.Add(25 * time.Hour), ApprovedAt: stewardT0.Add(approvedAfter),
	}
}

func createSteward(t *testing.T, drv storage.StorageDriver, g *storage.StewardGrant) *storage.StewardGrant {
	t.Helper()
	if err := drv.Sharing().StewardGrants().Create(context.Background(), g); err != nil {
		t.Fatalf("Create: %v", err)
	}
	return g
}

func stewardConfRoundTrip(t *testing.T, drv storage.StorageDriver) {
	ctx := context.Background()
	st := drv.Sharing().StewardGrants()
	f := newStewardFixture(t, drv, "rt")
	g := f.grant(t, 0)
	createSteward(t, drv, g)
	if len(g.ID) != len("stg_")+26 || g.ID[:4] != "stg_" || g.Status != storage.StewardGrantApproved {
		t.Fatalf("created = %+v, want stg_<ULID> in approved", g)
	}
	got, err := st.Get(ctx, g.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	checkStewardRoundTrip(t, got, g, f)
	byRequest, err := st.GetByRequest(ctx, g.RequestID)
	if err != nil || byRequest.ID != g.ID {
		t.Errorf("GetByRequest = %+v, %v; want %s", byRequest, err, g.ID)
	}

	for _, miss := range []string{"stg_missing", ""} {
		if _, err := st.Get(ctx, miss); !errors.Is(err, storage.ErrNotFound) {
			t.Errorf("Get(%q) = %v, want ErrNotFound", miss, err)
		}
	}
	if _, err := st.GetByRequest(ctx, "apr_missing"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("GetByRequest(missing) = %v, want ErrNotFound", err)
	}
}

// checkStewardRoundTrip compares a stored grant with what f.grant filed.
func checkStewardRoundTrip(t *testing.T, got, filed *storage.StewardGrant, f *stewardFixture) {
	t.Helper()
	fields := got.RequestID == filed.RequestID && got.StewardID == f.steward && got.MatterRef == "CASE-rt" &&
		string(got.Scope) == `{"data":"type==contact"}` && got.ScopeDigest == "v1:sha256:bb" && got.Status == storage.StewardGrantApproved
	times := got.NotBefore.Equal(stewardT0.Add(time.Hour)) && got.ExpiresAt.Equal(stewardT0.Add(25*time.Hour)) && got.ApprovedAt.Equal(stewardT0)
	open := got.RevokedAt == nil && got.RevokedBy == "" && got.RevokeReason == "" && got.RenewsID == ""
	if !fields || !times || !open {
		t.Errorf("Get = %+v", got)
	}
}

// A renewal names its predecessor, a missing matter ref is "", and the
// clock decides active and expired while the row stays approved.
func stewardConfRenewalAndClock(t *testing.T, drv storage.StorageDriver) {
	ctx := context.Background()
	st := drv.Sharing().StewardGrants()
	f := newStewardFixture(t, drv, "renew")
	first := createSteward(t, drv, f.grant(t, 0))
	next := f.grant(t, time.Hour)
	next.MatterRef, next.RenewsID = "", first.ID
	createSteward(t, drv, next)
	got, err := st.Get(ctx, next.ID)
	if err != nil || got.RenewsID != first.ID || got.MatterRef != "" || got.Status != storage.StewardGrantApproved {
		t.Fatalf("renewal = %+v, %v", got, err)
	}
	if s := got.StateAt(got.NotBefore.Add(-time.Millisecond), 0); s != storage.StewardStatePending {
		t.Errorf("before not_before: %s", s)
	}
	if s := got.StateAt(got.NotBefore, 0); s != storage.StewardStateActive {
		t.Errorf("at not_before: %s", s)
	}
	if s := got.StateAt(got.ExpiresAt, 0); s != storage.StewardStateExpired {
		t.Errorf("at expires_at: %s", s)
	}
}

// The scope and its digest come back as they went in: the reader's
// comparison of the two is the tamper check, so the store must neither
// re-encode the one nor recompute the other.
func stewardConfVerbatim(t *testing.T, drv storage.StorageDriver) {
	ctx := context.Background()
	f := newStewardFixture(t, drv, "verb")
	g := f.grant(t, 0)
	g.Scope = json.RawMessage("{ \"fields\": {\"contact\": [\"name\"]},\n  \"data\":\"type==contact\", \"x\": 1.0 }")
	g.ScopeDigest = "not-a-real-digest"
	createSteward(t, drv, g)
	got, err := drv.Sharing().StewardGrants().Get(ctx, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if string(got.Scope) != "{ \"fields\": {\"contact\": [\"name\"]},\n  \"data\":\"type==contact\", \"x\": 1.0 }" || got.ScopeDigest != "not-a-real-digest" {
		t.Errorf("scope %q digest %q came back changed", got.Scope, got.ScopeDigest)
	}
}

func stewardConfCreateRefusals(t *testing.T, drv storage.StorageDriver) {
	ctx := context.Background()
	f := newStewardFixture(t, drv, "ref")
	for name, c := range map[string]struct {
		mutate   func(*storage.StewardGrant)
		sentinel error
	}{
		"no request":         {func(g *storage.StewardGrant) { g.RequestID = " " }, nil},
		"no steward":         {func(g *storage.StewardGrant) { g.StewardID = "" }, nil},
		"no digest":          {func(g *storage.StewardGrant) { g.ScopeDigest = "" }, nil},
		"scope not JSON":     {func(g *storage.StewardGrant) { g.Scope = json.RawMessage(`{`) }, nil},
		"scope not object":   {func(g *storage.StewardGrant) { g.Scope = json.RawMessage(`["a"]`) }, nil},
		"no scope":           {func(g *storage.StewardGrant) { g.Scope = nil }, nil},
		"window empty":       {func(g *storage.StewardGrant) { g.ExpiresAt = g.NotBefore }, nil},
		"window inverted":    {func(g *storage.StewardGrant) { g.ExpiresAt = g.NotBefore.Add(-time.Hour) }, nil},
		"no window":          {func(g *storage.StewardGrant) { g.NotBefore, g.ExpiresAt = time.Time{}, time.Time{} }, nil},
		"filed revoked":      {func(g *storage.StewardGrant) { g.Status = storage.StewardGrantRevoked }, nil},
		"unknown request":    {func(g *storage.StewardGrant) { g.RequestID = "apr_missing" }, storage.ErrNotFound},
		"unknown steward":    {func(g *storage.StewardGrant) { g.StewardID = "prn_nobody" }, storage.ErrNotFound},
		"unknown renewed id": {func(g *storage.StewardGrant) { g.RenewsID = "stg_missing" }, storage.ErrNotFound},
	} {
		g := f.grant(t, 0)
		c.mutate(g)
		err := drv.Sharing().StewardGrants().Create(ctx, g)
		if err == nil || (c.sentinel != nil && !errors.Is(err, c.sentinel)) {
			t.Errorf("%s: Create = %v, want a refusal (%v)", name, err, c.sentinel)
		}
	}
	if err := drv.Sharing().StewardGrants().Create(ctx, nil); err == nil {
		t.Error("Create(nil) accepted")
	}
	list, err := drv.Sharing().StewardGrants().List(ctx, storage.StewardGrantFilter{StewardID: f.steward})
	if err != nil || len(list) != 0 {
		t.Errorf("after refusals: %d grants, %v; want none", len(list), err)
	}
}

func stewardConfOneGrantPerRequest(t *testing.T, drv storage.StorageDriver) {
	ctx := context.Background()
	f := newStewardFixture(t, drv, "one")
	g := createSteward(t, drv, f.grant(t, 0))
	again := f.grant(t, time.Minute)
	again.RequestID = g.RequestID
	if err := drv.Sharing().StewardGrants().Create(ctx, again); !errors.Is(err, storage.ErrAlreadyExists) {
		t.Errorf("second grant of one request: %v, want ErrAlreadyExists", err)
	}
	dup := f.grant(t, time.Minute)
	dup.ID = g.ID
	if err := drv.Sharing().StewardGrants().Create(ctx, dup); !errors.Is(err, storage.ErrAlreadyExists) {
		t.Errorf("duplicate id: %v, want ErrAlreadyExists", err)
	}
}

func stewardConfList(t *testing.T, drv storage.StorageDriver) {
	ctx := context.Background()
	st := drv.Sharing().StewardGrants()
	f := newStewardFixture(t, drv, "list")
	other := newStewardFixture(t, drv, "list2")
	first := createSteward(t, drv, f.grant(t, 0))
	second := createSteward(t, drv, f.grant(t, time.Minute))
	third := createSteward(t, drv, f.grant(t, 2*time.Minute))
	foreign := createSteward(t, drv, other.grant(t, 3*time.Minute))
	if err := st.Revoke(ctx, third.ID, f.approve, "done", stewardT0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	ids := func(f storage.StewardGrantFilter) []string {
		t.Helper()
		list, err := st.List(ctx, f)
		if err != nil {
			t.Fatalf("List(%+v): %v", f, err)
		}
		out := make([]string, len(list))
		for i, g := range list {
			out[i] = g.ID
		}
		return out
	}
	for name, c := range map[string]struct {
		f    storage.StewardGrantFilter
		want []string
	}{
		"steward, newest first": {storage.StewardGrantFilter{StewardID: f.steward}, []string{third.ID, second.ID, first.ID}},
		"status":                {storage.StewardGrantFilter{StewardID: f.steward, Status: storage.StewardGrantApproved}, []string{second.ID, first.ID}},
		"revoked":               {storage.StewardGrantFilter{StewardID: f.steward, Status: storage.StewardGrantRevoked}, []string{third.ID}},
		"limit":                 {storage.StewardGrantFilter{StewardID: f.steward, Limit: 2}, []string{third.ID, second.ID}},
		"other steward":         {storage.StewardGrantFilter{StewardID: other.steward}, []string{foreign.ID}},
		"no match":              {storage.StewardGrantFilter{StewardID: "prn_nobody"}, []string{}},
	} {
		if got := ids(c.f); !slices.Equal(got, c.want) {
			t.Errorf("%s: %v, want %v", name, got, c.want)
		}
	}
}

func stewardConfRevoke(t *testing.T, drv storage.StorageDriver) {
	ctx := context.Background()
	st := drv.Sharing().StewardGrants()
	f := newStewardFixture(t, drv, "rev")
	g := createSteward(t, drv, f.grant(t, 0))
	at := stewardT0.Add(2 * time.Hour)
	if err := st.Revoke(ctx, g.ID, " "+f.approve+" ", " no longer needed ", at); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	got, err := st.Get(ctx, g.ID)
	if err != nil || got.Status != storage.StewardGrantRevoked || got.RevokedAt == nil || !got.RevokedAt.Equal(at) ||
		got.RevokedBy != f.approve || got.RevokeReason != "no longer needed" {
		t.Fatalf("after Revoke = %+v, %v", got, err)
	}
	if got.StateAt(g.NotBefore.Add(time.Minute), 0) != storage.StewardStateRevoked {
		t.Error("a revoked grant is revoked inside its window")
	}
	if err := st.Revoke(ctx, g.ID, f.steward, "again", at.Add(time.Hour)); !errors.Is(err, storage.ErrStewardGrantNotApproved) {
		t.Errorf("second Revoke: %v, want ErrStewardGrantNotApproved", err)
	}
	if again, _ := st.Get(ctx, g.ID); again.RevokedBy != f.approve || !again.RevokedAt.Equal(at) || again.RevokeReason != "no longer needed" {
		t.Errorf("a refused revocation changed the row: %+v", again)
	}
	if err := st.Revoke(ctx, "stg_missing", f.approve, "", at); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Revoke(missing) = %v, want ErrNotFound", err)
	}
	open := createSteward(t, drv, f.grant(t, time.Minute))
	if err := st.Revoke(ctx, open.ID, " ", "", at); err == nil {
		t.Error("Revoke without a revoker accepted")
	}
	if still, _ := st.Get(ctx, open.ID); still.Status != storage.StewardGrantApproved {
		t.Errorf("a refused revocation moved the grant to %s", still.Status)
	}
}

// Revokers of one grant race; under concurrency exactly one revokes it
// and the others see it closed.
func stewardConfConcurrentRevoke(t *testing.T, drv storage.StorageDriver) {
	ctx := context.Background()
	st := drv.Sharing().StewardGrants()
	f := newStewardFixture(t, drv, "conc")
	g := createSteward(t, drv, f.grant(t, 0))
	const revokers = 6
	var wg sync.WaitGroup
	var mu sync.Mutex
	won, closed := 0, 0
	for i := range revokers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := st.Revoke(ctx, g.ID, f.approve, "race", stewardT0.Add(time.Duration(i+1)*time.Hour))
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				won++
			case errors.Is(err, storage.ErrStewardGrantNotApproved):
				closed++
			default:
				t.Errorf("revoker %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	if won != 1 || closed != revokers-1 {
		t.Errorf("won %d, saw it closed %d; want exactly one winner", won, closed)
	}
}

// The approval kind writes the grant in the final approval's
// transaction: it commits with the request's transition and the audit
// entry, and a failure after it takes the grant away too.
func stewardConfInTx(t *testing.T, drv storage.StorageDriver) {
	ctx := context.Background()
	f := newStewardFixture(t, drv, "tx")
	approvals, st := drv.Approvals(), drv.Sharing().StewardGrants()

	// Requests are filed first: a write outside the transaction would
	// wait for its lock.
	committed, rolledRequest := f.request(t), f.request(t)
	var kept *storage.StewardGrant
	if err := approvals.InTx(ctx, func(ctx context.Context) error {
		kept = f.grantOn(committed, 0)
		if err := st.Create(ctx, kept); err != nil {
			return err
		}
		// Reads in the transaction see its own writes.
		if _, err := st.Get(ctx, kept.ID); err != nil {
			return err
		}
		if err := approvals.Transition(ctx, committed.ID, storage.ApprovalApproved, stewardT0, kept.ID); err != nil {
			return err
		}
		return txAudit(ctx, drv, "conformance.steward.committed")
	}); err != nil {
		t.Fatalf("InTx: %v", err)
	}
	if got, err := st.GetByRequest(ctx, committed.ID); err != nil || got.ID != kept.ID {
		t.Errorf("committed grant = %+v, %v", got, err)
	}
	if n := len(auditOf(t, drv, "conformance.steward.committed")); n != 1 {
		t.Errorf("committed audit entries: %d, want 1", n)
	}

	boom := errors.New("boom")
	var rolled *storage.StewardGrant
	err := approvals.InTx(ctx, func(ctx context.Context) error {
		rolled = f.grantOn(rolledRequest, time.Minute)
		if err := st.Create(ctx, rolled); err != nil {
			return err
		}
		if err := st.Revoke(ctx, kept.ID, f.approve, "in the rolled-back transaction", stewardT0.Add(time.Hour)); err != nil {
			return err
		}
		if err := approvals.Transition(ctx, rolledRequest.ID, storage.ApprovalApproved, stewardT0, rolled.ID); err != nil {
			return err
		}
		if err := txAudit(ctx, drv, "conformance.steward.rolled"); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("InTx = %v, want fn's error", err)
	}
	if _, err := st.Get(ctx, rolled.ID); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("a grant created in a rolled-back transaction: %v, want ErrNotFound", err)
	}
	if got, _ := st.Get(ctx, kept.ID); got.Status != storage.StewardGrantApproved || got.RevokedAt != nil {
		t.Errorf("a revocation in a rolled-back transaction survived: %+v", got)
	}
	if r, _ := approvals.Get(ctx, rolledRequest.ID); r.Status != storage.ApprovalRequested {
		t.Errorf("the request moved to %s in a rolled-back transaction", r.Status)
	}
	if n := len(auditOf(t, drv, "conformance.steward.rolled")); n != 0 {
		t.Errorf("an audit entry of a rolled-back transaction survived (%d)", n)
	}
}
