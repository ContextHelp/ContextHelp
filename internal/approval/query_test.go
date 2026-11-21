package approval_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/approval"
	"github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func itemIDs(items []*approval.Item) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Request.ID
	}
	return out
}

// A viewer lists the requests it filed, is the subject of, or may
// decide; the owner lists every one; an outsider none. CanDecide is
// the separation verdict for that viewer.
func TestList_Visibility(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	mine := f.file("mine")
	f.clock.advance(time.Millisecond)
	fl := f.filing("alice's")
	fl.Requester, fl.SubjectID = f.alice, ""
	alices, err := f.svc.File(ctx, fl)
	require.NoError(t, err)

	list := func(p *auth.Principal) []*approval.Item {
		t.Helper()
		items, err := f.svc.List(ctx, p, approval.ListFilter{})
		require.NoError(t, err)
		return items
	}
	assert.Equal(t, []string{alices.ID, mine.ID}, itemIDs(list(f.owner)), "newest first")
	assert.Equal(t, []string{mine.ID}, itemIDs(list(f.requester)), "the requester: its own")
	assert.Equal(t, []string{alices.ID, mine.ID}, itemIDs(list(f.target)), "the subject, and an approver of the kind")
	assert.Empty(t, list(f.outsider))

	can := map[string]map[string]bool{}
	for _, p := range []*auth.Principal{f.owner, f.requester, f.alice, f.bob, f.target} {
		can[p.ID] = map[string]bool{}
		for _, it := range list(p) {
			can[p.ID][it.Request.ID] = it.CanDecide
		}
	}
	assert.True(t, can[f.owner.ID][mine.ID])
	assert.False(t, can[f.requester.ID][mine.ID], "requester")
	assert.True(t, can[f.alice.ID][mine.ID])
	assert.False(t, can[f.alice.ID][alices.ID], "alice filed it")
	assert.True(t, can[f.bob.ID][alices.ID])
	assert.False(t, can[f.target.ID][mine.ID], "the target benefits")
	assert.False(t, can[f.target.ID][alices.ID], "the target benefits from alice's too")

	_, err = f.approve(mine, f.alice)
	require.NoError(t, err)
	it, err := f.svc.Get(ctx, mine.ID, f.bob)
	require.NoError(t, err)
	assert.Equal(t, approval.StateApproved, it.State)
	assert.False(t, it.CanDecide, "nothing to decide once approved")
}

// Filters: kind and state, expired computed; the limit counts visible
// requests only.
func TestList_Filters(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	old := f.file("old")
	f.clock.advance(ttl / 2)
	approved := f.file("approved")
	_, err := f.approve(approved, f.alice)
	require.NoError(t, err)
	f.clock.advance(time.Millisecond)
	pending := f.file("pending")
	f.clock.advance(ttl/2 + time.Millisecond)

	ids := func(filter approval.ListFilter) []string {
		t.Helper()
		items, err := f.svc.List(ctx, f.owner, filter)
		require.NoError(t, err)
		return itemIDs(items)
	}
	assert.Equal(t, []string{old.ID}, ids(approval.ListFilter{State: approval.StateExpired}))
	assert.Equal(t, []string{pending.ID}, ids(approval.ListFilter{State: approval.StateRequested}))
	assert.Equal(t, []string{approved.ID}, ids(approval.ListFilter{State: approval.StateApproved}))
	assert.Empty(t, ids(approval.ListFilter{State: approval.StateStale}))
	assert.Equal(t, []string{pending.ID, approved.ID, old.ID}, ids(approval.ListFilter{Kind: f.kind.Name()}))
	assert.Empty(t, ids(approval.ListFilter{Kind: "steward.grant"}))
	assert.Equal(t, []string{pending.ID, approved.ID}, ids(approval.ListFilter{Limit: 2}))

	_, err = f.svc.List(ctx, f.owner, approval.ListFilter{State: "pending"})
	requireCode(t, err, approval.CodeInvalid)

	items, err := f.svc.List(ctx, f.requester, approval.ListFilter{State: approval.StateExpired})
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, approval.StateExpired, items[0].State)
	assert.False(t, items[0].CanDecide)
}

// An owner key or session narrowed below the approval scopes keeps the
// owner kind and nothing else: it lists nothing, and sees, decides and
// withdraws nothing, each answered as not found.
func TestNarrowedOwnerHasNoReachOverTheQueue(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	r := f.file("pending")
	narrowed := &auth.Principal{ID: f.owner.ID, Kind: f.owner.Kind, Scopes: []auth.Scope{auth.ScopeReadObjects, auth.ScopeWriteSelf}}

	items, err := f.svc.List(ctx, narrowed, approval.ListFilter{})
	require.NoError(t, err)
	assert.Empty(t, items)
	_, err = f.svc.Get(ctx, r.ID, narrowed)
	assert.ErrorIs(t, err, storage.ErrNotFound)
	_, err = f.approve(r, narrowed)
	assert.ErrorIs(t, err, storage.ErrNotFound)
	_, err = f.reject(r, narrowed)
	assert.ErrorIs(t, err, storage.ErrNotFound)
	_, err = f.svc.Withdraw(ctx, r.ID, narrowed)
	assert.ErrorIs(t, err, storage.ErrNotFound)
	assert.Equal(t, storage.ApprovalRequested, f.stored(r.ID).Status, "nothing moved")

	// The owner holding the scopes still reaches all of it.
	_, err = f.svc.Get(ctx, r.ID, f.owner)
	assert.NoError(t, err)
	_, err = f.svc.Withdraw(ctx, r.ID, f.owner)
	assert.NoError(t, err)
}
