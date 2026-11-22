package steward_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/steward"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Any holder of approve:steward, the owner, the requester and the
// steward may revoke; nobody else can even see the grant. A revocation
// is effective on the steward's next request, with a viewer already
// compiled and cached.
func TestRevokeByEveryoneWhoMay(t *testing.T) {
	for name, by := range map[string]func(f *fixture) *auth.Principal{
		"an approver":   func(f *fixture) *auth.Principal { return f.bob },
		"the owner":     func(f *fixture) *auth.Principal { return f.owner },
		"the requester": func(f *fixture) *auth.Principal { return f.requester },
		"the steward":   func(f *fixture) *auth.Principal { return f.stewardP },
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.seedCorpus()
			g := f.approved(f.request())
			_, err := f.elevated(g)
			require.NoError(t, err)
			require.Equal(t, 1, f.viewers.CacheSize())

			f.clock.advance(time.Hour)
			got, err := f.grants.Revoke(t.Context(), g.ID, by(f), "matter closed")
			require.NoError(t, err)
			assert.Equal(t, storage.StewardGrantRevoked, got.Status)
			assert.Equal(t, by(f).ID, got.RevokedBy)
			assert.Equal(t, "matter closed", got.RevokeReason)
			require.NotNil(t, got.RevokedAt)
			assert.True(t, got.RevokedAt.Equal(f.clock.now()))

			v, err := f.elevated(g)
			var refusal *sharing.StewardRefusal
			require.ErrorAs(t, err, &refusal, "the second request is refused")
			assert.Equal(t, sharing.StewardReasonGrantRevoked, refusal.Reason)
			assert.True(t, v.MatchesNothing())

			entries := f.audit(steward.EventGrantRevoked)
			require.Len(t, entries, 1)
			assert.Equal(t, by(f).ID, entries[0].Actor)
			assert.Equal(t, g.ID, entries[0].Payload["grant_id"])
			assert.Equal(t, "matter closed", entries[0].Payload["note"])
		})
	}
}

// Revoking is not for just anyone, and a grant can be revoked once:
// revoked is terminal.
func TestRevokeRules(t *testing.T) {
	f := newFixture(t)
	g := f.approved(f.request())

	for _, p := range []*auth.Principal{f.outsider, f.plain} {
		_, err := f.grants.Revoke(t.Context(), g.ID, p, "")
		assert.ErrorIs(t, err, storage.ErrNotFound, "%s cannot see the grant", p.ID)
	}
	// An owner key narrowed below approve:steward has no reach over the
	// grant, though its principal is the owner.
	narrowed := *f.owner
	narrowed.Scopes = []auth.Scope{auth.ScopeReadObjects}
	_, err := f.grants.Revoke(t.Context(), g.ID, &narrowed, "")
	assert.ErrorIs(t, err, storage.ErrNotFound, "a narrowed owner key cannot see the grant")

	still, err := f.sharing.StewardGrants().Get(t.Context(), g.ID)
	require.NoError(t, err)
	assert.Equal(t, storage.StewardGrantApproved, still.Status)

	_, err = f.grants.Revoke(t.Context(), "stg_nothing", f.owner, "")
	assert.ErrorIs(t, err, storage.ErrNotFound)

	_, err = f.grants.Revoke(t.Context(), g.ID, f.bob, "first")
	require.NoError(t, err)
	_, err = f.grants.Revoke(t.Context(), g.ID, f.owner, "second")
	assert.ErrorIs(t, err, storage.ErrStewardGrantNotApproved)
	again, err := f.sharing.StewardGrants().Get(t.Context(), g.ID)
	require.NoError(t, err)
	assert.Equal(t, f.bob.ID, again.RevokedBy, "the first revocation stands")
	assert.Len(t, f.audit(steward.EventGrantRevoked), 1, "a refused revocation writes no entry")

	// A grant past its window is still approved as stored: it may be
	// revoked, once.
	g2 := f.approved(f.request())
	f.clock.advance(49 * time.Hour)
	require.Equal(t, storage.StewardStateExpired, f.grants.State(g2))
	got, err := f.grants.Revoke(t.Context(), g2.ID, f.owner, "late")
	require.NoError(t, err)
	assert.Equal(t, storage.StewardGrantRevoked, got.Status)
	_, err = f.grants.Revoke(t.Context(), g2.ID, f.bob, "again")
	assert.ErrorIs(t, err, storage.ErrStewardGrantNotApproved)
}

// A lowered cap makes a live grant read expired, and raising the cap
// brings it back: expired is computed, not terminal. An operator can
// still end the grant for cause while the cap keeps it dormant, and a
// revoked grant stays ended when the cap is raised again.
func TestRevokeACapExpiredGrant(t *testing.T) {
	f := newFixture(t)
	f.seedCorpus()
	g := f.approved(f.request()) // 48h window, 14d cap
	f.settings.MaxDuration = time.Hour
	f.clock.advance(2 * time.Hour)
	require.Equal(t, storage.StewardStateExpired, f.grants.State(g))

	got, err := f.grants.Revoke(t.Context(), g.ID, f.owner, "for cause")
	require.NoError(t, err, "an expired grant whose stored status is approved may be revoked")
	assert.Equal(t, storage.StewardGrantRevoked, got.Status)
	entries := f.audit(steward.EventGrantRevoked)
	require.Len(t, entries, 1)
	assert.Equal(t, string(storage.StewardStateExpired), entries[0].Payload["state"], "the entry records the state it was revoked in")

	f.settings.MaxDuration = 14 * 24 * time.Hour
	assert.Equal(t, storage.StewardStateRevoked, f.grants.State(got), "raising the cap does not bring a revoked grant back")
	_, err = f.elevated(g)
	var refusal *sharing.StewardRefusal
	require.ErrorAs(t, err, &refusal)
	assert.Equal(t, sharing.StewardReasonGrantRevoked, refusal.Reason)
}

// A grant not yet active can be revoked too (approved to revoked).
func TestRevokeBeforeTheWindowOpens(t *testing.T) {
	f := newFixture(t)
	req := f.request()
	req.NotBefore = f.clock.now().Add(24 * time.Hour)
	req.ExpiresAt = req.NotBefore.Add(24 * time.Hour)
	g := f.approved(req)
	got, err := f.grants.Revoke(t.Context(), g.ID, f.owner, "")
	require.NoError(t, err)
	assert.Equal(t, storage.StewardGrantRevoked, got.Status)
}

// State reports a grant's state under today's settings.
func TestServiceStateIsComputed(t *testing.T) {
	f := newFixture(t)
	g := f.approved(f.request())
	assert.Equal(t, storage.StewardStateActive, f.grants.State(g))
	f.settings.MaxDuration = time.Hour
	f.clock.advance(2 * time.Hour)
	assert.Equal(t, storage.StewardStateExpired, f.grants.State(g), "the cap shortens a live grant")
	f.settings.MaxDuration = 14 * 24 * time.Hour
	assert.Equal(t, storage.StewardStateActive, f.grants.State(g))
	f.clock.advance(48 * time.Hour)
	assert.Equal(t, storage.StewardStateExpired, f.grants.State(g))
}
