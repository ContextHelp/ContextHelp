package approval_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/approval"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// noticeLog records the notices a service reports.
type noticeLog struct {
	mu      sync.Mutex
	notices []approval.Notice
}

func (l *noticeLog) record(_ context.Context, n approval.Notice) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.notices = append(l.notices, n)
}

// of lists the events reported about request id, with their actors and
// the state the request was in when each was recorded.
func (l *noticeLog) of(id string) (events, actors []string, states []storage.ApprovalStatus) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, n := range l.notices {
		if n.Request.ID == id {
			events = append(events, n.Event)
			actors = append(actors, n.Actor)
			states = append(states, n.Request.Status)
		}
	}
	return events, actors, states
}

func (l *noticeLog) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.notices)
}

// Every transition the mechanism records is reported after it commits,
// in order, with its actor and the request as it stood then: filing,
// each decision, the approval, a rejection, a withdrawal, staleness.
func TestNotify_ReportsEveryCommittedTransition(t *testing.T) {
	f := newFixture(t)
	f.kind.SetRequired(2)
	ctx := context.Background()

	approved := f.file("approved")
	_, err := f.approve(approved, f.alice)
	require.NoError(t, err)
	_, err = f.approve(approved, f.bob)
	require.NoError(t, err)
	events, actors, states := f.notices.of(approved.ID)
	assert.Equal(t, []string{approval.EventFiled, approval.EventDecided, approval.EventDecided, approval.EventApproved}, events)
	assert.Equal(t, []string{f.requester.ID, f.alice.ID, f.bob.ID, f.bob.ID}, actors)
	assert.Equal(t, storage.ApprovalApproved, states[3])

	rejected := f.file("rejected")
	_, err = f.reject(rejected, f.alice)
	require.NoError(t, err)
	events, actors, _ = f.notices.of(rejected.ID)
	assert.Equal(t, []string{approval.EventFiled, approval.EventDecided, approval.EventRejected}, events)
	assert.Equal(t, []string{f.requester.ID, f.alice.ID, f.alice.ID}, actors)

	withdrawn := f.file("withdrawn")
	_, err = f.svc.Withdraw(ctx, withdrawn.ID, f.requester)
	require.NoError(t, err)
	events, _, _ = f.notices.of(withdrawn.ID)
	assert.Equal(t, []string{approval.EventFiled, approval.EventWithdrawn}, events)

	// A stale ending is committed, then refused to the approver: it is
	// reported all the same.
	stale := f.file("stale")
	_, err = f.approve(stale, f.alice)
	require.NoError(t, err)
	f.kind.Bump(f.target.ID)
	_, err = f.approve(stale, f.bob)
	requireCode(t, err, approval.CodeStale)
	events, _, states = f.notices.of(stale.ID)
	assert.Equal(t, []string{approval.EventFiled, approval.EventDecided, approval.EventDecided, approval.EventStale}, events)
	assert.Equal(t, storage.ApprovalStale, states[3])
}

// What rolls back is never reported: a refused filing, a refused
// decision, and a decision whose apply failed.
func TestNotify_NothingForWhatRollsBack(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.kind.FailApply(errors.New("disk full"))
	r := f.file("rollback")
	before := f.notices.count()
	_, err := f.approve(r, f.alice)
	require.Error(t, err)
	assert.Equal(t, before, f.notices.count(), "a decision rolled back with its failed apply is not reported")

	_, err = f.approve(r, f.requester)
	requireCode(t, err, approval.CodeRequesterCannotDecide)
	assert.Equal(t, before, f.notices.count(), "a refused decision is not reported")

	_, err = f.svc.File(ctx, approval.Filing{Kind: "no.such.kind", Action: "x", Requester: f.requester, Reason: "r"})
	requireCode(t, err, approval.CodeKindUnknown)
	assert.Equal(t, before, f.notices.count(), "a refused filing is not reported")
}

// A hook that panics after the commit is logged and skipped: the
// committed result still returns, and the notices after it still run.
func TestNotify_APanickingHookNeverFailsACommittedCall(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	calls := 0
	svc, err := approval.NewService(f.drv.Approvals(), f.drv.AuditLog(), f.svc.Kinds(), approval.Options{
		PendingTTL: ttl, Now: f.clock.now,
		Notify: func(context.Context, approval.Notice) {
			calls++
			panic("hook bug")
		},
	})
	require.NoError(t, err)

	r, err := svc.File(ctx, f.filing("panic"))
	require.NoError(t, err, "a filing that committed returns whatever the hook does")
	require.NotNil(t, r)
	assert.Equal(t, 1, calls)

	d, err := svc.Approve(ctx, r.ID, f.alice, r.PayloadDigest, "ok")
	require.NoError(t, err)
	assert.Equal(t, approval.StateApproved, d.State)
	assert.Equal(t, 3, calls, "the decision and the approval were both reported, the first panic did not stop the second")
	assert.Equal(t, storage.ApprovalApproved, f.stored(r.ID).Status)
}
