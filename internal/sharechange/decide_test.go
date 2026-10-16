package sharechange_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/approval"
	"github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/sharechange"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// The last approval applies the frozen change as its requester, in the
// approval's transaction: the registry changes, the change is recorded
// under its own event with the request and its approvers, and the
// request keeps the row it wrote. The owner approves; so does a
// share approver.
func TestApprove_AppliesTheFrozenChange(t *testing.T) { applies(t, sqliteDriver) }

func applies(t *testing.T, newDriver driverFunc) {
	for _, c := range wideCases() {
		for _, who := range []string{"owner", "approver"} {
			t.Run(c.name+"/"+who, func(t *testing.T) {
				f := newFx(t, newDriver)
				if c.setup != nil {
					c.setup(f)
				}
				r := f.pending(f.admin, c.change(f))
				before := f.snapshot()

				approver := f.owner
				if who == "approver" {
					approver = c.other(f)
				}
				d, err := f.approve(r, approver)
				require.NoError(t, err)
				assert.Equal(t, approval.StateApproved, d.State)
				assert.NotEqual(t, before, f.snapshot(), "the approval applied the change")
				c.applied(t, f, f.admin.ID)

				stored := f.stored(r.ID)
				assert.Equal(t, storage.ApprovalApproved, stored.Status)
				assert.NotEmpty(t, stored.ResultRef)

				events := f.shareEvents()
				require.Len(t, events, 1)
				e := events[0]
				assert.Equal(t, c.event, e.EventType)
				assert.Equal(t, f.admin.ID, e.Actor, "the requester is the actor of the change")
				assert.Equal(t, r.ID, e.Payload["request_id"])
				assert.Equal(t, []any{approver.ID}, e.Payload["approvers"])
			})
		}
	}
}

// One rejection rejects the request and applies nothing; a rejected or
// withdrawn request cannot be approved afterwards.
func TestReject_AppliesNothing(t *testing.T) {
	f := newFx(t, sqliteDriver)
	r := f.pending(f.admin, sharing.MemberPut{AudienceID: f.team.ID, PrincipalID: f.dana.ID})
	before := f.snapshot()
	d, err := f.reject(r, f.alice)
	require.NoError(t, err)
	assert.Equal(t, approval.StateRejected, d.State)
	_, err = f.approve(r, f.bob)
	requireCode(t, err, approval.CodeClosed)
	assert.Equal(t, before, f.snapshot())
	assert.Empty(t, f.shareEvents())
}

// The requester, whatever it holds, never decides its own request; a
// beneficiary of the change never decides it, though it holds the
// approver scope; an administrator without approve:shares cannot
// decide it at all; the owner and a share approver can.
func TestDecide_SeparationOfRequesterAndBeneficiary(t *testing.T) { separation(t, sqliteDriver) }

func separation(t *testing.T, newDriver driverFunc) {
	for _, c := range wideCases() {
		t.Run(c.name, func(t *testing.T) {
			f := newFx(t, newDriver)
			if c.setup != nil {
				c.setup(f)
			}
			r := f.pending(f.admin, c.change(f))
			before := f.snapshot()

			// A requester that also holds approve:shares.
			zr := f.pending(f.zed, c.change(f))
			_, err := f.approve(zr, f.zed)
			requireCode(t, err, approval.CodeRequesterCannotDecide)
			_, err = f.reject(zr, f.zed)
			requireCode(t, err, approval.CodeRequesterCannotDecide)

			// The beneficiary holds the scope and is refused.
			ben := c.beneficiary(f)
			_, err = f.approve(r, ben)
			requireCode(t, err, approval.CodeBeneficiaryCannotDecide)
			_, err = f.reject(r, ben)
			requireCode(t, err, approval.CodeBeneficiaryCannotDecide)

			// An administrator holding admin:shares and no approve:shares.
			_, err = f.approve(r, f.admin2)
			require.Error(t, err)
			_, err = f.approve(r, f.erin)
			require.Error(t, err)

			assert.Equal(t, before, f.snapshot(), "nobody who may not decide changed anything")
			assert.Empty(t, f.stored(r.ID).Decisions)
			assert.Empty(t, f.stored(zr.ID).Decisions)

			// A share approver the change does not benefit decides.
			_, err = f.approve(r, c.other(f))
			require.NoError(t, err)
		})
	}
}

// Beneficiaries are computed when the decision is made: a principal
// that joined the audience a grant would reach after the request was
// filed benefits from it and may not approve it.
func TestDecide_BeneficiariesAreComputedAtDecisionTime(t *testing.T) {
	f := newFx(t, sqliteDriver)
	f.join(f.erin.ID, nil)
	r := f.pending(f.admin, sharing.GrantCreate{AudienceID: f.team.ID, PolicyID: f.base.ID})
	f.join(f.bob.ID, nil) // joins after the request was filed
	_, err := f.approve(r, f.bob)
	requireCode(t, err, approval.CodeBeneficiaryCannotDecide)
	_, err = f.approve(r, f.alice)
	require.NoError(t, err)
}

// A policy update is approved by none of the members of the audiences
// that hold a grant of it, however late they joined.
func TestDecide_LateMemberOfAGrantedAudienceCannotApprove(t *testing.T) {
	f := newFx(t, sqliteDriver)
	f.grant(f.base)
	f.join(f.erin.ID, nil)
	r := f.pending(f.admin, sharing.PolicyUpdate{PolicyID: f.base.ID, Policy: widerBase()})
	f.join(f.bob.ID, nil) // joins after the request was filed
	_, err := f.approve(r, f.bob)
	requireCode(t, err, approval.CodeBeneficiaryCannotDecide)
	_, err = f.approve(r, f.alice)
	require.NoError(t, err)
}

// A grant's beneficiaries are the members of its audience, expired
// memberships included, local or not: a member whose membership has
// lapsed is still a principal the grant is for.
func TestDecide_GrantBeneficiariesAreEveryMemberOfTheAudience(t *testing.T) {
	f := newFx(t, sqliteDriver)
	f.join(f.bob.ID, f.clock.at(-1)) // lapsed
	r := f.pending(f.admin, sharing.GrantCreate{AudienceID: f.team.ID, PolicyID: f.base.ID})
	_, err := f.approve(r, f.bob)
	requireCode(t, err, approval.CodeBeneficiaryCannotDecide)
	_, err = f.approve(r, f.alice)
	require.NoError(t, err)
}

// approvals.share.required is N, frozen on the request when filed: the
// first approval of two leaves the change pending and unapplied, a
// second distinct approver applies it, and one approver decides once.
func TestApprove_NApprovals(t *testing.T) {
	f := newFx(t, sqliteDriver, withRequired(2))
	r := f.pending(f.admin, sharing.MemberPut{AudienceID: f.team.ID, PrincipalID: f.dana.ID})
	assert.Equal(t, 2, r.ApprovalsRequired)
	before := f.snapshot()

	d, err := f.approve(r, f.alice)
	require.NoError(t, err)
	assert.Equal(t, approval.StateRequested, d.State)
	assert.Equal(t, before, f.snapshot(), "one approval of two applies nothing")
	_, err = f.approve(r, f.alice)
	requireCode(t, err, approval.CodeAlreadyDecided)

	d, err = f.approve(r, f.bob)
	require.NoError(t, err)
	assert.Equal(t, approval.StateApproved, d.State)
	assert.NotEqual(t, before, f.snapshot())
	events := f.shareEvents()
	require.Len(t, events, 1)
	assert.Equal(t, []any{f.alice.ID, f.bob.ID}, events[0].Payload["approvers"])
}

// The share.change kind files all ten actions, and its approvers are
// the holders of approve:shares and the owner: not an administrator by
// being one, not a reader.
func TestKind_EligibleApproversHoldApproveShares(t *testing.T) {
	f := newFx(t, sqliteDriver)
	k, ok := f.appr.Kinds().Lookup(sharechange.KindName)
	require.True(t, ok)
	assert.ElementsMatch(t, sharechange.Actions, k.Actions)
	for _, c := range []struct {
		who  *auth.Principal
		want bool
	}{{f.owner, true}, {f.alice, true}, {f.zed, true}, {f.admin, false}, {f.erin, false}} {
		got, err := k.Eligible(context.Background(), nil, c.who)
		require.NoError(t, err)
		assert.Equal(t, c.want, got, c.who.ID)
	}
}
