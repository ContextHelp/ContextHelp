package approval_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/approval"
	"github.com/ideacrafterslabs/ctxt/internal/approval/approvaltest"
	"github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// When the state the payload was frozen against changed, the last
// approval ends the request stale: its decision is recorded, nothing is
// applied, and the approver is told. Approvals before the last do not
// re-check.
func TestApply_StaleOnFingerprintMismatch(t *testing.T) {
	f := newFixture(t)
	f.kind.SetRequired(2)
	r := f.file("stale")
	_, err := f.approve(r, f.alice)
	require.NoError(t, err)
	f.kind.Bump(f.target.ID)

	_, err = f.approve(r, f.bob)
	requireCode(t, err, approval.CodeStale)
	got := f.stored(r.ID)
	assert.Equal(t, storage.ApprovalStale, got.Status)
	assert.Empty(t, got.ResultRef)
	assert.Len(t, got.Decisions, 2, "the last approval is on record")
	assert.Zero(t, f.kind.Attempts(), "nothing applied")
	assert.ElementsMatch(t, []string{approval.EventFiled, approval.EventDecided, approval.EventDecided, approval.EventStale},
		eventTypes(f.events(r.ID)))
	_, err = f.approve(r, f.carol)
	requireCode(t, err, approval.CodeClosed)

	// A change to another target's state leaves this one fresh.
	fresh := f.file("fresh")
	f.kind.Bump("prn_someone_else")
	d, err := f.approve(fresh, f.alice)
	require.NoError(t, err)
	_, err = f.approve(fresh, f.bob)
	require.NoError(t, err)
	assert.Equal(t, approval.StateRequested, d.State)
	assert.Equal(t, storage.ApprovalApproved, f.stored(fresh.ID).Status)
}

// The N-th approval applies only when every recorded approval binds to
// the request's current payload digest: a payload rewritten between two
// approvals (digest recomputed, so each single check holds) has an
// approval given to another payload on record, and the request ends
// stale with nothing applied.
func TestApply_StaleWhenAnApprovalBindsToAnotherDigest(t *testing.T) {
	f := newFixture(t)
	f.kind.SetRequired(2)
	r := f.file("bound")
	_, err := f.approve(r, f.alice)
	require.NoError(t, err)

	moved, err := approval.Canonicalize(approvaltest.Raw(f.target.ID, "rewritten"))
	require.NoError(t, err)
	digest := approval.Digest(moved)
	require.NotEqual(t, r.PayloadDigest, digest)
	db := f.drv.(interface{ DB() *sql.DB }).DB()
	_, err = db.ExecContext(context.Background(), `UPDATE approval_requests SET payload = ?, payload_digest = ? WHERE id = ?`, string(moved), digest, r.ID)
	require.NoError(t, err)

	_, err = f.svc.Approve(context.Background(), r.ID, f.bob, digest, "ok")
	requireCode(t, err, approval.CodeStale)
	got := f.stored(r.ID)
	assert.Equal(t, storage.ApprovalStale, got.Status)
	assert.Empty(t, got.ResultRef)
	assert.Len(t, got.Decisions, 2, "the last approval is on record")
	assert.Zero(t, f.kind.Attempts(), "nothing applied")
	assert.Contains(t, eventTypes(f.events(r.ID)), approval.EventStale)
}

// Apply runs in the last approval's transaction: it sees the decision
// being recorded, and a failure rolls back the decision, the audit
// entries and everything Apply wrote; the request stays pending, so a
// retry applies it once.
func TestApply_FailureRollsBackTheApproval(t *testing.T) {
	f := newFixture(t)
	r := f.file("rollback")
	boom := errors.New("apply failed")
	f.kind.FailApply(boom)

	_, err := f.approve(r, f.alice)
	require.ErrorIs(t, err, boom)
	assert.Equal(t, 1, f.kind.Attempts())
	assert.Equal(t, []int{1}, f.kind.Seen(), "Apply read its request in the approval's transaction")
	got := f.stored(r.ID)
	assert.Equal(t, storage.ApprovalRequested, got.Status)
	assert.Empty(t, got.Decisions, "the decision rolled back with the apply")
	assert.Equal(t, []string{approval.EventFiled}, eventTypes(f.events(r.ID)), "no entry of the failed approval, the kind's own included")

	f.kind.FailApply(nil)
	d, err := f.approve(r, f.alice)
	require.NoError(t, err)
	assert.Equal(t, approval.StateApproved, d.State)
	assert.Equal(t, 2, f.kind.Attempts())
	assert.Equal(t, storage.ApprovalApproved, f.stored(r.ID).Status)
	applied := 0
	for _, e := range f.events(r.ID) {
		if e.EventType == approvaltest.EventApplied {
			applied++
		}
	}
	assert.Equal(t, 1, applied, "applied exactly once")
}

// Concurrent final approvals apply once: the request's row is locked
// for the decision, the first approval closes it, the others find it
// closed.
func TestApply_RunsOnceUnderConcurrency(t *testing.T) {
	f := newFixture(t)
	r := f.file("race")
	type outcome struct {
		d   *approval.Decided
		err error
	}
	approvers := []*auth.Principal{f.alice, f.bob, f.carol}
	results := make([]outcome, len(approvers))
	var wg sync.WaitGroup
	for i, p := range approvers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, err := f.svc.Approve(context.Background(), r.ID, p, r.PayloadDigest, "")
			results[i] = outcome{d, err}
		}()
	}
	wg.Wait()
	won, reveals := 0, 0
	for _, o := range results {
		if o.err != nil {
			assert.Equal(t, approval.CodeClosed, approval.CodeOf(o.err), "%v", o.err)
			continue
		}
		won++
		if o.d.Reveal != nil {
			reveals++
		}
	}
	assert.Equal(t, 1, won)
	assert.Equal(t, 1, reveals, "one reveal, to the final approver")
	assert.Equal(t, 1, f.kind.Attempts())
	assert.Len(t, f.stored(r.ID).Decisions, 1)
}

// A stored request of a kind no longer registered fails closed: no one
// decides it; its requester, subject and the owner still see it.
func TestApply_UnregisteredKindFailsClosed(t *testing.T) {
	f := newFixture(t)
	now := f.clock.now()
	r := &storage.ApprovalRequest{
		Kind: "test.gone", Action: "test.gone.do", RequestedBy: f.requester.ID, SubjectID: f.target.ID,
		Payload: json.RawMessage(`{}`), PayloadDigest: approval.Digest([]byte(`{}`)), ApprovalsRequired: 1,
		RequestedAt: now, ExpiresAt: now.Add(time.Hour),
	}
	require.NoError(t, f.drv.Approvals().Create(context.Background(), r))
	_, err := f.svc.Approve(context.Background(), r.ID, f.owner, r.PayloadDigest, "")
	requireCode(t, err, approval.CodeKindUnknown)
	_, err = f.svc.Approve(context.Background(), r.ID, f.alice, r.PayloadDigest, "")
	assert.True(t, errors.Is(err, storage.ErrNotFound), "an approver of no kind sees nothing")
	for _, p := range []*auth.Principal{f.owner, f.requester, f.target} {
		it, err := f.svc.Get(context.Background(), r.ID, p)
		require.NoError(t, err, p.ID)
		assert.False(t, it.CanDecide, p.ID)
	}
	assert.Equal(t, storage.ApprovalRequested, f.stored(r.ID).Status)
}
