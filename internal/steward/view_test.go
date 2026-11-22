package steward_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/steward"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ids lists the grants of views.
func ids(views []*steward.GrantView) []string {
	out := []string{}
	for _, v := range views {
		out = append(out, v.Grant.ID)
	}
	return out
}

// A grant is seen by its steward, the requester of the request it came
// from, any holder of approve:steward and the owner (ADR-083 §2, §5);
// to anyone else it does not exist, by id or in a listing.
func TestView_WhoSeesAGrant(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	g := f.approved(f.request())

	for name, p := range map[string]*auth.Principal{
		"steward": f.stewardP, "requester": f.requester, "approver": f.bob, "owner": f.owner,
	} {
		v, err := f.grants.Get(ctx, g.ID, p)
		require.NoError(t, err, name)
		assert.Equal(t, g.ID, v.Grant.ID, name)
		assert.Equal(t, f.requester.ID, v.RequestedBy, name)
		list, err := f.grants.List(ctx, p, steward.ListFilter{})
		require.NoError(t, err, name)
		assert.Equal(t, []string{g.ID}, ids(list), name)
	}
	for name, p := range map[string]*auth.Principal{
		"no steward role": f.plain, "outsider": f.outsider,
		"a requester of nothing here": f.person("prn_other_requester", auth.RoleReader, auth.RoleStewardRequester),
	} {
		_, err := f.grants.Get(ctx, g.ID, p)
		require.ErrorIs(t, err, storage.ErrNotFound, name)
		list, err := f.grants.List(ctx, p, steward.ListFilter{})
		require.NoError(t, err, name)
		assert.Empty(t, list, name)
	}
	_, err := f.grants.Get(ctx, "stg_nothing", f.owner)
	require.ErrorIs(t, err, storage.ErrNotFound)
}

// A view carries the grant's state and effective expiry now, under
// today's cap: a lowered cap shortens it, a revocation ends it.
func TestView_StateAndEffectiveExpiry(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	g := f.approved(f.request())
	v, err := f.grants.Get(ctx, g.ID, f.stewardP)
	require.NoError(t, err)
	assert.Equal(t, storage.StewardStateActive, v.State)
	assert.Equal(t, g.ExpiresAt, v.EffectiveExpiry)

	f.settings.MaxDuration = time.Hour
	v, err = f.grants.Get(ctx, g.ID, f.stewardP)
	require.NoError(t, err)
	assert.Equal(t, g.NotBefore.Add(time.Hour), v.EffectiveExpiry, "a lowered cap shortens the grant")
	f.clock.advance(2 * time.Hour)
	v, err = f.grants.Get(ctx, g.ID, f.stewardP)
	require.NoError(t, err)
	assert.Equal(t, storage.StewardStateExpired, v.State)

	_, err = f.grants.Revoke(ctx, g.ID, f.alice, "closed")
	require.NoError(t, err)
	v, err = f.grants.Get(ctx, g.ID, f.stewardP)
	require.NoError(t, err)
	assert.Equal(t, storage.StewardStateRevoked, v.State)
}

// A listing narrows by steward and by state, and stops at the limit.
func TestView_ListFilters(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	first := f.approved(f.request())
	f.clock.advance(time.Minute)
	second := f.approved(f.request())
	_, err := f.grants.Revoke(ctx, first.ID, f.alice, "closed")
	require.NoError(t, err)

	all, err := f.grants.List(ctx, f.owner, steward.ListFilter{})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{first.ID, second.ID}, ids(all))
	active, err := f.grants.List(ctx, f.owner, steward.ListFilter{State: storage.StewardStateActive})
	require.NoError(t, err)
	assert.Equal(t, []string{second.ID}, ids(active))
	revoked, err := f.grants.List(ctx, f.owner, steward.ListFilter{State: storage.StewardStateRevoked})
	require.NoError(t, err)
	assert.Equal(t, []string{first.ID}, ids(revoked))
	none, err := f.grants.List(ctx, f.owner, steward.ListFilter{StewardID: f.requester.ID})
	require.NoError(t, err)
	assert.Empty(t, none)
	one, err := f.grants.List(ctx, f.owner, steward.ListFilter{Limit: 1})
	require.NoError(t, err)
	assert.Len(t, one, 1)
}
