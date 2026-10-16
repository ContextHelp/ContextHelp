package sharechange_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/approval"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// A request is filed against the rows its class was read from. When one
// of them moves before the last approval, the request ends stale and
// nothing is applied; the requester files again (ADR-083 §9, §10 B).
// The move is an owner's direct change, except where it is the
// administrator's own neutral edit.
func TestStale_PreconditionMovedBeforeTheLastApproval(t *testing.T) { stale(t, sqliteDriver) }

func stale(t *testing.T, newDriver driverFunc) {
	for _, c := range wideCases() {
		t.Run(c.name, func(t *testing.T) {
			f := newFx(t, newDriver)
			if c.setup != nil {
				c.setup(f)
			}
			r := f.pending(f.admin, c.change(f))
			c.move(f)
			moved := f.snapshot()
			sharesBefore := len(f.shareEvents())

			_, err := f.approve(r, c.other(f))
			requireCode(t, err, approval.CodeStale)
			assert.Equal(t, storage.ApprovalStale, f.stored(r.ID).Status)
			assert.Equal(t, moved, f.snapshot(), "a stale request applies nothing")
			assert.Len(t, f.shareEvents(), sharesBefore, "no change was recorded as made")
			assert.Len(t, f.eventsOf("approval.request.stale"), 1)

			// Stale is terminal: a late approver finds it closed.
			_, err = f.approve(r, f.owner)
			requireCode(t, err, approval.CodeClosed)
		})
	}
}

// A policy edit while a grant of that policy is pending makes the
// request stale: the edit applies at once (no grant is in force yet),
// and the approver would otherwise grant a policy they never reviewed.
func TestStale_PolicyEditedWhileAGrantRequestIsPending(t *testing.T) {
	f := newFx(t, sqliteDriver)
	f.join(f.erin.ID, nil)
	r := f.pending(f.admin, sharing.GrantCreate{AudienceID: f.team.ID, PolicyID: f.base.ID})
	out := f.submit(f.admin, sharing.PolicyUpdate{PolicyID: f.base.ID, Policy: widerBase()})
	require.NotNil(t, out.Applied)
	assert.Equal(t, sharing.Neutral, out.Class.Class)

	_, err := f.approve(r, f.owner)
	requireCode(t, err, approval.CodeStale)
	gs, err := f.drv.Sharing().Grants().List(context.Background(), storage.GrantFilter{})
	require.NoError(t, err)
	assert.Empty(t, gs, "the stale grant was not made")
}

// A member suspended since the request was filed ends it stale instead
// of failing its application.
func TestStale_MemberSuspendedWhilePending(t *testing.T) {
	f := newFx(t, sqliteDriver)
	r := f.pending(f.admin, sharing.MemberPut{AudienceID: f.team.ID, PrincipalID: f.dana.ID})
	require.NoError(t, f.drv.Auth().Principals().Suspend(context.Background(), f.dana.ID))
	_, err := f.approve(r, f.owner)
	requireCode(t, err, approval.CodeStale)
	ms, err := f.drv.Sharing().Audiences().ListMembers(context.Background(), f.team.ID)
	require.NoError(t, err)
	assert.Empty(t, ms)
}

// An expiry the change itself carries can pass while it waits: applying
// it would write a membership or a grant already over, so it is stale.
func TestStale_ExpiryOfTheChangeLapsedWhilePending(t *testing.T) {
	for _, c := range []struct {
		name   string
		change func(f *fx) sharing.Change
	}{
		{"membership", func(f *fx) sharing.Change {
			return sharing.MemberPut{AudienceID: f.team.ID, PrincipalID: f.dana.ID, ExpiresAt: f.clock.at(time.Hour)}
		}},
		{"grant", func(f *fx) sharing.Change {
			return sharing.GrantCreate{AudienceID: f.team.ID, PolicyID: f.base.ID, ExpiresAt: f.clock.at(time.Hour)}
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFx(t, sqliteDriver)
			r := f.pending(f.admin, c.change(f))
			before := f.snapshot()
			f.clock.advance(2 * time.Hour) // within the pending TTL
			_, err := f.approve(r, f.owner)
			requireCode(t, err, approval.CodeStale)
			assert.Equal(t, before, f.snapshot())
		})
	}
}

// A request nobody decides expires at its pending TTL, computed from
// the clock; an expired request cannot be approved.
func TestExpired_RequestPastItsTTLCannotBeApproved(t *testing.T) {
	f := newFx(t, sqliteDriver)
	r := f.pending(f.admin, sharing.MemberPut{AudienceID: f.team.ID, PrincipalID: f.dana.ID})
	before := f.snapshot()
	f.clock.advance(pendingTTL)
	_, err := f.approve(r, f.owner)
	requireCode(t, err, approval.CodeExpired)
	assert.Equal(t, before, f.snapshot())
}
