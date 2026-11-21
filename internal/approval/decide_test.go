package approval_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/approval"
	"github.com/ideacrafterslabs/ctxt/internal/approval/approvaltest"
	"github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// The requester never decides its own request, whatever it holds: not
// even when it also holds the approver scope.
func TestDecide_RequesterCannotDecide(t *testing.T) {
	f := newFixture(t)
	r := f.file("self")
	_, err := f.approve(r, f.requester)
	requireCode(t, err, approval.CodeRequesterCannotDecide)
	_, err = f.reject(r, f.requester)
	requireCode(t, err, approval.CodeRequesterCannotDecide)

	// An approver who files is still the requester.
	fl := f.filing("approver files")
	fl.Requester = f.alice
	own, err := f.svc.File(context.Background(), fl)
	require.NoError(t, err)
	_, err = f.approve(own, f.alice)
	requireCode(t, err, approval.CodeRequesterCannotDecide)
	assert.Empty(t, f.stored(own.ID).Decisions)
	assert.Empty(t, f.stored(r.ID).Decisions)
}

// The subject and every beneficiary the kind names are refused, even
// holding the approver scope; a kind that admits its subject lets the
// subject decide.
func TestDecide_BeneficiariesCannotDecide(t *testing.T) {
	f := newFixture(t)
	r := f.file("benefit")
	_, err := f.approve(r, f.target)
	requireCode(t, err, approval.CodeBeneficiaryCannotDecide)
	_, err = f.reject(r, f.target)
	requireCode(t, err, approval.CodeBeneficiaryCannotDecide)

	// A beneficiary that is not the subject: the payload benefits bob.
	fl := f.filing("benefit bob")
	fl.Payload = approvaltest.Raw(f.bob.ID, "x")
	other, err := f.svc.File(context.Background(), fl)
	require.NoError(t, err)
	_, err = f.approve(other, f.bob)
	requireCode(t, err, approval.CodeBeneficiaryCannotDecide)

	// A subject that is no beneficiary of the payload is still refused.
	fl = f.filing("about carol")
	fl.SubjectID, fl.Payload = f.carol.ID, approvaltest.Raw(f.bob.ID, "y")
	about, err := f.svc.File(context.Background(), fl)
	require.NoError(t, err)
	_, err = f.approve(about, f.carol)
	requireCode(t, err, approval.CodeBeneficiaryCannotDecide)

	g := newFixture(t, func(k *approvaltest.Kind) { k.SubjectMayApprove = true })
	bind := g.file("bind")
	d, err := g.approve(bind, g.target)
	require.NoError(t, err, "a kind that admits its subject")
	assert.Equal(t, approval.StateApproved, d.State)
}

// Eligibility is the kind's rule: a principal without the approver
// scope is refused, and does not see the request at all.
func TestDecide_IneligibleIsRefused(t *testing.T) {
	f := newFixture(t)
	r := f.file("eligible")
	_, err := f.approve(r, f.outsider)
	assert.True(t, errors.Is(err, storage.ErrNotFound), "an outsider learns nothing: %v", err)
	_, err = f.svc.Get(context.Background(), r.ID, f.outsider)
	assert.True(t, errors.Is(err, storage.ErrNotFound))

	// Another kind's approver scope does not make one an approver of
	// this kind: it neither sees nor decides the request.
	steward := f.principal("prn_steward", auth.RoleReader, auth.RoleStewardApprover)
	_, err = f.approve(r, steward)
	assert.True(t, errors.Is(err, storage.ErrNotFound))

	// The owner is eligible for every kind (it holds every approval
	// scope).
	d, err := f.approve(r, f.owner)
	require.NoError(t, err)
	assert.Equal(t, approval.StateApproved, d.State)
}

// N distinct approvals: the first N-1 keep the request pending and apply
// nothing; the N-th applies it once and only its approver gets the
// reveal. One principal records one decision.
func TestDecide_NApprovers(t *testing.T) {
	f := newFixture(t)
	f.kind.SetRequired(3)
	r := f.file("three")

	d, err := f.approve(r, f.alice)
	require.NoError(t, err)
	assert.Equal(t, approval.StateRequested, d.State)
	assert.Nil(t, d.Reveal)
	_, err = f.approve(r, f.alice)
	requireCode(t, err, approval.CodeAlreadyDecided)
	_, err = f.reject(r, f.alice)
	requireCode(t, err, approval.CodeAlreadyDecided)
	mine, err := f.svc.Get(context.Background(), r.ID, f.alice)
	require.NoError(t, err)
	assert.False(t, mine.CanDecide, "alice has decided")
	theirs, err := f.svc.Get(context.Background(), r.ID, f.bob)
	require.NoError(t, err)
	assert.True(t, theirs.CanDecide)

	d, err = f.approve(r, f.bob)
	require.NoError(t, err)
	assert.Equal(t, approval.StateRequested, d.State)
	assert.Zero(t, f.kind.Attempts(), "nothing applied before the N-th approval")

	d, err = f.approve(r, f.carol)
	require.NoError(t, err)
	assert.Equal(t, approval.StateApproved, d.State)
	assert.Equal(t, approvaltest.Reveal(r.ID), d.Reveal)
	assert.Equal(t, 1, f.kind.Attempts())

	got := f.stored(r.ID)
	assert.Equal(t, storage.ApprovalApproved, got.Status)
	assert.Equal(t, approvaltest.Ref(r.ID), got.ResultRef)
	require.NotNil(t, got.DecidedAt)
	require.Len(t, got.Decisions, 3)
	for i, p := range []*auth.Principal{f.alice, f.bob, f.carol} {
		assert.Equal(t, p.ID, got.Decisions[i].ApproverID)
		assert.Equal(t, storage.ApprovalVerdictApprove, got.Decisions[i].Decision)
		assert.Equal(t, r.PayloadDigest, got.Decisions[i].PayloadDigest)
	}
	_, err = f.approve(r, f.owner)
	requireCode(t, err, approval.CodeClosed)
}

// One rejection by an eligible approver rejects the request, whatever
// approvals it already has; nothing is applied.
func TestDecide_OneRejectionRejects(t *testing.T) {
	f := newFixture(t)
	f.kind.SetRequired(2)
	r := f.file("rejected")
	_, err := f.approve(r, f.alice)
	require.NoError(t, err)
	d, err := f.reject(r, f.bob)
	require.NoError(t, err)
	assert.Equal(t, approval.StateRejected, d.State)
	assert.Equal(t, storage.ApprovalRejected, f.stored(r.ID).Status)
	assert.Zero(t, f.kind.Attempts())
	_, err = f.approve(r, f.carol)
	requireCode(t, err, approval.CodeClosed)
	_, err = f.svc.Withdraw(context.Background(), r.ID, f.requester)
	requireCode(t, err, approval.CodeClosed)
}

// Every decision binds to the request's payload digest.
func TestDecide_DigestMustMatch(t *testing.T) {
	f := newFixture(t)
	r := f.file("digest")
	_, err := f.svc.Approve(context.Background(), r.ID, f.alice, approval.Digest([]byte(`{}`)), "")
	requireCode(t, err, approval.CodeDigestMismatch)
	_, err = f.svc.Reject(context.Background(), r.ID, f.alice, "", "")
	requireCode(t, err, approval.CodeDigestMismatch)
	assert.Empty(t, f.stored(r.ID).Decisions)
}

// Expiry is computed from the clock: up to the instant before expires_at
// a request is pending; from expires_at on it is expired, without any
// sweeper, and nothing decides or withdraws it.
func TestDecide_ExpiryByClock(t *testing.T) {
	f := newFixture(t)
	f.kind.SetRequired(2)
	r := f.file("ttl")
	f.clock.advance(ttl - time.Millisecond)
	_, err := f.approve(r, f.alice)
	require.NoError(t, err, "one millisecond before expiry")

	f.clock.advance(time.Millisecond)
	it, err := f.svc.Get(context.Background(), r.ID, f.requester)
	require.NoError(t, err)
	assert.Equal(t, approval.StateExpired, it.State)
	_, err = f.approve(r, f.bob)
	requireCode(t, err, approval.CodeExpired)
	_, err = f.reject(r, f.bob)
	requireCode(t, err, approval.CodeExpired)
	_, err = f.svc.Withdraw(context.Background(), r.ID, f.requester)
	requireCode(t, err, approval.CodeExpired)

	got := f.stored(r.ID)
	assert.Equal(t, storage.ApprovalRequested, got.Status, "expired is never written")
	assert.Len(t, got.Decisions, 1)
	assert.Zero(t, f.kind.Attempts())
}

// Withdrawal: the requester or the owner, while pending; no approver.
func TestWithdraw(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	r := f.file("withdraw")
	_, err := f.svc.Withdraw(ctx, r.ID, f.alice)
	requireCode(t, err, approval.CodeNotRequester)
	_, err = f.svc.Withdraw(ctx, r.ID, f.target)
	requireCode(t, err, approval.CodeNotRequester)
	_, err = f.svc.Withdraw(ctx, r.ID, f.outsider)
	assert.True(t, errors.Is(err, storage.ErrNotFound))
	got, err := f.svc.Withdraw(ctx, r.ID, f.requester)
	require.NoError(t, err)
	assert.Equal(t, storage.ApprovalWithdrawn, got.Status)
	_, err = f.approve(r, f.alice)
	requireCode(t, err, approval.CodeClosed)

	byOwner := f.file("owner withdraws")
	got, err = f.svc.Withdraw(ctx, byOwner.ID, f.owner)
	require.NoError(t, err)
	assert.Equal(t, storage.ApprovalWithdrawn, got.Status)
	_, err = f.svc.Withdraw(ctx, byOwner.ID, f.requester)
	requireCode(t, err, approval.CodeClosed)
	_, err = f.svc.Withdraw(ctx, "apr_missing", f.owner)
	assert.True(t, errors.Is(err, storage.ErrNotFound))

	// The actor is checked before the state: someone who may not
	// withdraw a request learns nothing of whether it is still pending.
	_, err = f.svc.Withdraw(ctx, byOwner.ID, f.alice)
	requireCode(t, err, approval.CodeNotRequester)
}

// Every transition writes its audit entry, naming the actor, the
// request and the digest, in the transaction of the transition.
func TestAudit_EntryPerTransition(t *testing.T) {
	f := newFixture(t)
	f.kind.SetRequired(2)
	approved := f.file("audited approve")
	_, err := f.approve(approved, f.alice)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{approval.EventFiled, approval.EventDecided}, eventTypes(f.events(approved.ID)))
	_, err = f.approve(approved, f.bob)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{approval.EventFiled, approval.EventDecided, approval.EventDecided,
		approvaltest.EventApplied, approval.EventApproved}, eventTypes(f.events(approved.ID)))

	rejected := f.file("audited reject")
	_, err = f.reject(rejected, f.alice)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{approval.EventFiled, approval.EventDecided, approval.EventRejected}, eventTypes(f.events(rejected.ID)))

	withdrawn := f.file("audited withdraw")
	_, err = f.svc.Withdraw(context.Background(), withdrawn.ID, f.requester)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{approval.EventFiled, approval.EventWithdrawn}, eventTypes(f.events(withdrawn.ID)))

	actors := map[string][]string{}
	for _, e := range f.events(approved.ID) {
		actors[e.EventType] = append(actors[e.EventType], e.Actor)
		if e.EventType != approvaltest.EventApplied {
			assert.Equal(t, approved.PayloadDigest, e.Payload["payload_digest"], e.EventType)
			assert.Equal(t, f.kind.Name(), e.Payload["kind"], e.EventType)
			assert.Equal(t, approvaltest.Action, e.Payload["action"], e.EventType)
		}
	}
	assert.Equal(t, []string{f.requester.ID}, actors[approval.EventFiled])
	assert.ElementsMatch(t, []string{f.alice.ID, f.bob.ID}, actors[approval.EventDecided])
	assert.Equal(t, []string{f.bob.ID}, actors[approval.EventApproved])
	for _, e := range f.events(approved.ID) {
		if e.EventType == approval.EventApproved {
			assert.Equal(t, approvaltest.Ref(approved.ID), e.Payload["result_ref"])
			assert.ElementsMatch(t, []any{f.alice.ID, f.bob.ID}, e.Payload["approvers"])
		}
	}
	for _, e := range f.events(withdrawn.ID) {
		if e.EventType == approval.EventWithdrawn {
			assert.Equal(t, f.requester.ID, e.Actor)
		}
	}
	// A refused decision writes nothing.
	before := len(f.events(rejected.ID))
	_, err = f.approve(rejected, f.bob)
	requireCode(t, err, approval.CodeClosed)
	assert.Len(t, f.events(rejected.ID), before)
}

// A refused decision on a request the decider can see leaves a security
// row: the requester, the subject, an ineligible viewer and a wrong
// digest. The request does not move, and an invisible request, which is
// not found, leaves none.
func TestDecide_RefusalsLeaveATrace(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	r := f.file("traced")
	ineligible := f.principal("prn_ineligible", auth.RoleReader)
	_, err := f.approve(r, f.requester)
	requireCode(t, err, approval.CodeRequesterCannotDecide)
	_, err = f.reject(r, f.target)
	requireCode(t, err, approval.CodeBeneficiaryCannotDecide)
	_, err = f.svc.Approve(ctx, r.ID, f.alice, "sha256:wrong", "")
	requireCode(t, err, approval.CodeDigestMismatch)
	_, err = f.approve(r, ineligible)
	assert.True(t, errors.Is(err, storage.ErrNotFound), "an outsider does not see the request")

	var codes, actors []string
	for _, e := range f.events(r.ID) {
		if e.EventType == approval.EventRefused {
			codes = append(codes, e.Payload["code"].(string))
			actors = append(actors, e.Actor)
			assert.Equal(t, "security", e.Payload["event_class"])
			assert.Equal(t, r.PayloadDigest, e.Payload["payload_digest"])
		}
	}
	assert.Equal(t, []string{approval.CodeRequesterCannotDecide, approval.CodeBeneficiaryCannotDecide, approval.CodeDigestMismatch}, codes)
	assert.Equal(t, []string{f.requester.ID, f.target.ID, f.alice.ID}, actors)
	assert.Equal(t, storage.ApprovalRequested, f.stored(r.ID).Status)
}
