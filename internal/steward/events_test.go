package steward_test

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/approval"
	"github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/security"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/steward"
)

// alerts records what a security emitter dispatched.
type alerts struct {
	mu   sync.Mutex
	list []security.Alert
}

func (a *alerts) record(_ context.Context, al security.Alert) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.list = append(a.list, al)
}

// wait returns the alerts once n arrived (handlers run on their own
// goroutines), failing after a deadline; a short grace then shows any
// alert beyond n.
func (a *alerts) wait(t *testing.T, n int) []security.Alert {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		a.mu.Lock()
		got := len(a.list)
		a.mu.Unlock()
		if got >= n || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]security.Alert(nil), a.list...)
}

// kinds lists the alerts' kinds, sorted.
func kinds(list []security.Alert) []string {
	out := make([]string, len(list))
	for i, a := range list {
		out[i] = a.Kind
	}
	sort.Strings(out)
	return out
}

// emitter is a security emitter with no audit log whose alerts land in
// the returned recorder.
func emitter(t *testing.T) (*security.Emitter, *alerts) {
	t.Helper()
	sec := security.New(security.DefaultConfig(), nil)
	t.Cleanup(func() { _ = sec.Close(context.Background()) })
	rec := &alerts{}
	sec.AddHandler(rec.record)
	return sec, rec
}

// Each transition of a steward.grant request reaches the security
// emitter with its actor and ids, never its reason: filed, approved
// (with the grant it wrote), rejected, withdrawn, stale. A recorded
// decision short of N is no transition.
func TestEvents_RequestTransitions(t *testing.T) {
	f := newFixture(t)
	sec, rec := emitter(t)
	svc, err := approval.NewService(f.drv.Approvals(), f.drv.AuditLog(), f.svc.Kinds(),
		approval.Options{PendingTTL: ttl, Now: f.clock.now, Notify: steward.ApprovalNotices(sec)})
	require.NoError(t, err)
	f.svc = svc
	ctx := context.Background()

	approved := f.mustFile(f.request())
	_, err = f.approve(approved, f.alice)
	require.NoError(t, err)
	grant, err := f.sharing.StewardGrants().GetByRequest(ctx, approved.ID)
	require.NoError(t, err)

	rejected := f.mustFile(f.request())
	_, err = f.svc.Reject(ctx, rejected.ID, f.alice, rejected.PayloadDigest, "no")
	require.NoError(t, err)

	withdrawn := f.mustFile(f.request())
	_, err = f.svc.Withdraw(ctx, withdrawn.ID, f.requester)
	require.NoError(t, err)

	f.required = 2
	stale := f.mustFile(f.request())
	_, err = f.approve(stale, f.alice)
	require.NoError(t, err)
	f.setRoles(f.stewardP.ID, auth.RoleReader)
	_, err = f.approve(stale, f.bob)
	requireCode(t, err, approval.CodeStale)

	got := rec.wait(t, 8)
	assert.Equal(t, []string{
		security.EventStewardRequestApproved,
		security.EventStewardRequestFiled, security.EventStewardRequestFiled, security.EventStewardRequestFiled, security.EventStewardRequestFiled,
		security.EventStewardRequestRejected, security.EventStewardRequestStale, security.EventStewardRequestWithdrawn,
	}, kinds(got), "four filings, one each of approved, rejected, withdrawn, stale; no event for a decision short of N")
	for _, a := range got {
		assert.Equal(t, f.stewardP.ID, a.Meta["steward_id"], a.Kind)
		assert.NotEmpty(t, a.Meta["request_id"], a.Kind)
		assert.NotContains(t, a.Message+a.Meta["reason"], reason, "%s carries no reason text", a.Kind)
		switch a.Kind {
		case security.EventStewardRequestFiled, security.EventStewardRequestWithdrawn:
			assert.Equal(t, f.requester.ID, a.Principal, a.Kind)
		case security.EventStewardRequestApproved:
			assert.Equal(t, f.alice.ID, a.Principal)
			assert.Equal(t, grant.ID, a.Meta["grant_id"], "the approval names the grant it wrote")
			assert.Equal(t, approved.ID, a.Meta["request_id"])
		case security.EventStewardRequestRejected:
			assert.Equal(t, f.alice.ID, a.Principal)
		case security.EventStewardRequestStale:
			assert.Equal(t, f.bob.ID, a.Principal)
		}
	}
}

// Requests of another kind are not steward events.
func TestEvents_OtherKindsAreNotStewardEvents(t *testing.T) {
	sec, rec := emitter(t)
	notify := steward.ApprovalNotices(sec)
	n := approval.Notice{Event: approval.EventFiled, Actor: "prn_admin"}
	n.Request.ID, n.Request.Kind = "apr_1", "share.change"
	notify(context.Background(), n)
	assert.Empty(t, rec.wait(t, 0))
}

// A revocation reaches the emitter once, after it commits, naming the
// revoker; a refused revocation does not.
func TestEvents_GrantRevoked(t *testing.T) {
	f := newFixture(t)
	sec, rec := emitter(t)
	var err error
	f.grants, err = steward.NewService(steward.ServiceOptions{
		Sharing: f.sharing, Approvals: f.drv.Approvals(), Audit: f.drv.AuditLog(),
		Settings: func() sharing.StewardSettings { return *f.settings }, Now: f.clock.now, Security: sec,
	})
	require.NoError(t, err)
	g := f.approved(f.request())
	_, err = f.grants.Revoke(context.Background(), g.ID, f.outsider, "not mine")
	require.Error(t, err)
	_, err = f.grants.Revoke(context.Background(), g.ID, f.alice, "matter closed")
	require.NoError(t, err)
	_, err = f.grants.Revoke(context.Background(), g.ID, f.alice, "again")
	require.Error(t, err)

	got := rec.wait(t, 1)
	require.Len(t, got, 1)
	assert.Equal(t, security.EventStewardGrantRevoked, got[0].Kind)
	assert.Equal(t, f.alice.ID, got[0].Principal)
	assert.Equal(t, map[string]string{"grant_id": g.ID, "request_id": g.RequestID, "steward_id": f.stewardP.ID}, got[0].Meta)
}

// The session hook turns each elevation start and end into its event,
// and a refusal is its own event with the code and whether it was
// deferred.
func TestEvents_Elevation(t *testing.T) {
	sec, rec := emitter(t)
	hook := steward.ElevationEvents(sec)
	ctx := context.Background()
	hook(ctx, auth.ElevationEvent{Started: true, PrincipalID: "prn_s", SessionID: "uis_1", GrantID: "stg_1"})
	hook(ctx, auth.ElevationEvent{PrincipalID: "prn_s", SessionID: "uis_1", GrantID: "stg_1", Reason: auth.ElevationEndedIdle})
	steward.RecordElevationRefused(ctx, sec, "prn_s", "stg_1", "grant_revoked", false)

	got := rec.wait(t, 3)
	require.Len(t, got, 3)
	byKind := map[string]security.Alert{}
	for _, a := range got {
		byKind[a.Kind] = a
		assert.Equal(t, "prn_s", a.Principal, a.Kind)
		assert.Equal(t, "stg_1", a.Meta["grant_id"], a.Kind)
	}
	assert.Equal(t, "uis_1", byKind[security.EventStewardElevationStarted].Meta["session_id"])
	assert.Equal(t, auth.ElevationEndedIdle, byKind[security.EventStewardElevationEnded].Meta["reason"])
	refused := byKind[security.EventStewardElevationRefused]
	assert.Equal(t, "grant_revoked", refused.Meta["reason"])
	assert.Equal(t, "false", refused.Meta["deferred"])
}
