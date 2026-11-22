package sharing_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// elevatedViewer is the viewer of prn_s elevated into steward grant
// stg_1, as the resolver compiles it.
func elevatedViewer(t *testing.T) sharing.Viewer {
	t.Helper()
	v, err := sharing.Compile(sharing.Input{
		PrincipalID: "prn_s", IdentityID: "idn_s", StewardGrant: "stg_1",
		Grants: []sharing.Grant{{ID: "stg_1", Audience: "steward:stg_1", Policy: sharing.Policy{Name: "stg_1/0", Version: 1, Data: "type==contact"}}},
	})
	require.NoError(t, err)
	require.Equal(t, "stg_1", v.StewardGrantID())
	return v
}

func elevatedOp(v sharing.Viewer) sharing.Operation {
	return sharing.Operation{
		Method: "GET", Route: "GET /objects/{id}", Principal: "prn_s", Identity: "idn_s", AuthMethod: "session",
		Params: map[string][]string{"path.id": {"o1"}}, Viewer: v,
	}
}

// An elevated request's trail carries the steward_grant check right
// after authentication, its entry names the grant, and Commit writes it
// durably through AppendSteward before returning: nothing waits in the
// buffer.
func TestReceiptElevatedCommitsDurably(t *testing.T) {
	v := elevatedViewer(t)
	r := sharing.NewReceipt(elevatedOp(v))
	require.True(t, r.Elevated())
	l := sharing.NewLedger()
	l.AddObject("o1")
	l.MarkScoped()
	r.SetLedger(l)
	r.Pass(sharing.CheckScope, sharing.ReasonScopeHeld)
	r.Pass(sharing.CheckRouteAccess, sharing.ReasonRouteOpen)
	r.SettlePolicy()
	r.Pass(sharing.CheckBackstop, sharing.ReasonClean)

	store := &memAccessLog{}
	w := sharing.NewAuditWriter(store, sharing.AuditOptions{Interval: time.Hour})
	t.Cleanup(func() { _ = w.Close(context.Background()) })
	require.NoError(t, r.Commit(context.Background(), w))
	assert.Zero(t, w.Buffered(), "an elevated entry never goes through the buffer")
	require.Len(t, store.steward, 1, "committed before Commit returned")
	e := store.steward[0]
	assert.Equal(t, "stg_1", e.StewardGrantID)
	assert.Equal(t, storage.AccessDecisionAllowed, e.Decision)
	assert.Equal(t, []string{"authn.session", "steward_grant", "scope", "route_access", "share_policy", "deny_class", "backstop"}, checkIDs(e))
	assert.Equal(t, storage.AccessCheck{Check: sharing.CheckStewardGrant, Outcome: storage.AccessOutcomePass, Reason: sharing.StewardReasonGrantActive}, e.Checks[1])
	assert.Equal(t, []string{"o1"}, e.ObjectIDs)
	assert.Equal(t, v.PolicyKey(), e.PolicyVersions)
	assert.Equal(t, sharing.OperationDigest("GET", "GET /objects/{id}", map[string][]string{"path.id": {"o1"}}), e.OperationDigest)

	// Once: a second Commit or a Finish adds nothing.
	require.NoError(t, r.Commit(context.Background(), w))
	r.Finish(w)
	assert.Len(t, store.steward, 1)
}

// A store that cannot commit the entry fails the request closed: Commit
// returns the error, and the entry it records in its place (once the
// store answers again) is deferred, with the audit check UNKNOWN.
func TestReceiptElevatedCommitFailureDefers(t *testing.T) {
	store := &memAccessLog{stewardFails: 1}
	w := sharing.NewAuditWriter(store, sharing.AuditOptions{Interval: time.Hour})
	t.Cleanup(func() { _ = w.Close(context.Background()) })
	r := sharing.NewReceipt(elevatedOp(elevatedViewer(t)))
	r.Pass(sharing.CheckScope, sharing.ReasonScopeHeld)

	err := r.Commit(context.Background(), w)
	require.Error(t, err)
	assert.True(t, errors.Is(err, errStoreDown), "the cause is returned: %v", err)
	require.Len(t, store.steward, 1, "the deferral is recorded when the store takes it")
	e := store.steward[0]
	assert.Equal(t, storage.AccessDecisionDeferred, e.Decision)
	assert.Equal(t, storage.AccessCheck{Check: sharing.CheckAudit, Outcome: storage.AccessOutcomeUnknown, Reason: sharing.ReasonAuditUnavailable}, e.Checks[len(e.Checks)-1])
	assert.Zero(t, w.Buffered())

	// Down for good: the error stands and nothing is recorded.
	down := &memAccessLog{fail: errStoreDown}
	w2 := sharing.NewAuditWriter(down, sharing.AuditOptions{Interval: time.Hour})
	t.Cleanup(func() { _ = w2.Close(context.Background()) })
	r2 := sharing.NewReceipt(elevatedOp(elevatedViewer(t)))
	require.Error(t, r2.Commit(context.Background(), w2))
	assert.Empty(t, down.steward)

	// No writer at all is no store: the request fails closed too.
	r3 := sharing.NewReceipt(elevatedOp(elevatedViewer(t)))
	assert.ErrorIs(t, r3.Commit(context.Background(), nil), sharing.ErrAuditUnavailable)
}

// A refused elevation is an entry too: the steward_grant check fails
// with the resolution's code, denied, or UNKNOWN and deferred when the
// store could not be read. It names the grant the request named, and
// it is durable.
func TestReceiptStewardRefusal(t *testing.T) {
	for name, c := range map[string]struct {
		refusal  *sharing.StewardRefusal
		outcome  storage.AccessOutcome
		decision storage.AccessDecision
	}{
		"revoked":     {&sharing.StewardRefusal{Reason: sharing.StewardReasonGrantRevoked}, storage.AccessOutcomeFail, storage.AccessDecisionDenied},
		"unknown":     {&sharing.StewardRefusal{Reason: sharing.StewardReasonGrantUnknown}, storage.AccessOutcomeFail, storage.AccessDecisionDenied},
		"unavailable": {&sharing.StewardRefusal{Reason: sharing.StewardReasonGrantUnavailable, Deferred: true}, storage.AccessOutcomeUnknown, storage.AccessDecisionDeferred},
	} {
		t.Run(name, func(t *testing.T) {
			store := &memAccessLog{}
			w := sharing.NewAuditWriter(store, sharing.AuditOptions{Interval: time.Hour})
			t.Cleanup(func() { _ = w.Close(context.Background()) })
			op := elevatedOp(sharing.Viewer{})
			op.StewardGrant = "stg_named"
			r := sharing.NewReceipt(op)
			require.True(t, r.Elevated())
			r.StewardRefused(c.refusal)
			require.NoError(t, r.Commit(context.Background(), w))
			require.Len(t, store.steward, 1)
			e := store.steward[0]
			assert.Equal(t, "stg_named", e.StewardGrantID)
			assert.Equal(t, c.decision, e.Decision)
			assert.Equal(t, []storage.AccessCheck{
				{Check: "authn.session", Outcome: storage.AccessOutcomePass, Reason: sharing.ReasonAuthenticated},
				{Check: sharing.CheckStewardGrant, Outcome: c.outcome, Reason: c.refusal.Reason},
			}, e.Checks)
			assert.Empty(t, e.ObjectIDs)
			assert.Empty(t, e.PolicyVersions, "a refused elevation has no viewer")
		})
	}
}

// A receipt finished the ordinary way still records an elevated request
// durably: no code path puts a steward entry in the buffer.
func TestReceiptElevatedFinishIsDurable(t *testing.T) {
	store := &memAccessLog{}
	w := sharing.NewAuditWriter(store, sharing.AuditOptions{Interval: time.Hour})
	t.Cleanup(func() { _ = w.Close(context.Background()) })
	r := sharing.NewReceipt(elevatedOp(elevatedViewer(t)))
	r.Finish(w)
	assert.Zero(t, w.Buffered())
	assert.Len(t, store.steward, 1)
}

// An ordinary receipt is not elevated and Commit is Finish: buffered,
// never through the steward path.
func TestReceiptOrdinaryCommitIsBuffered(t *testing.T) {
	store := &memAccessLog{}
	w := sharing.NewAuditWriter(store, sharing.AuditOptions{Interval: time.Hour})
	t.Cleanup(func() { _ = w.Close(context.Background()) })
	r := newReceipt(t, colleagueViewer(t))
	assert.False(t, r.Elevated())
	require.NoError(t, r.Commit(context.Background(), w))
	assert.Equal(t, 1, w.Buffered())
	assert.Empty(t, store.steward)
	require.NoError(t, w.Flush(context.Background()))
	_, n := store.count()
	assert.Equal(t, 1, n)
	assert.NotContains(t, checkIDs(store.batches[0][0]), sharing.CheckStewardGrant)
}

// Concurrent Commits of one receipt share the first one's result, and
// write once; a refusal's entry is not asked of a store that is down a
// second time; Finish returns the commit's error.
func TestReceiptCommitIsOnceAndFinishReturnsItsError(t *testing.T) {
	store := &memAccessLog{stewardFails: 1}
	w := sharing.NewAuditWriter(store, sharing.AuditOptions{Interval: time.Hour})
	t.Cleanup(func() { _ = w.Close(context.Background()) })
	r := sharing.NewReceipt(elevatedOp(elevatedViewer(t)))
	r.Pass(sharing.CheckScope, sharing.ReasonScopeHeld)

	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = r.Commit(context.Background(), w)
		}()
	}
	wg.Wait()
	for i, err := range errs {
		assert.ErrorIs(t, err, errStoreDown, "caller %d", i)
	}
	assert.Len(t, store.steward, 1)
	assert.ErrorIs(t, r.Finish(w), errStoreDown, "Finish after a failed Commit returns the first result")

	down := &memAccessLog{stewardFails: 2}
	w2 := sharing.NewAuditWriter(down, sharing.AuditOptions{Interval: time.Hour})
	t.Cleanup(func() { _ = w2.Close(context.Background()) })
	refused := sharing.NewReceipt(elevatedOp(elevatedViewer(t)))
	refused.Refuse(sharing.CheckScope, sharing.ReasonScopeMissing)
	require.Error(t, refused.Commit(context.Background(), w2))
	assert.Equal(t, 1, 2-down.stewardFails, "one attempt for a refusal, not two")
}
