package sharing_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/sharing/sharingtest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// stewardFix is a store with the seeded corpus, a steward (a person
// holding steward-requester and an ordinary colleagues grant), a
// second steward, a resolver wired for steward grants and a clock the
// test moves.
type stewardFix struct {
	*storeFixture
	other    *storage.Principal
	settings *sharing.StewardSettings
	res      *sharing.Resolver
	now      time.Time
}

// stewardNow is the fixture's clock origin: inside the default window.
var stewardNow = t0.Add(time.Hour)

func newStewardFix(t *testing.T) *stewardFix {
	t.Helper()
	ctx := context.Background()
	sf := newStoreFixture(t)
	f := &stewardFix{storeFixture: sf, now: stewardNow, settings: &sharing.StewardSettings{
		Enabled: true, MaxDuration: 14 * 24 * time.Hour, WithheldClasses: []string{"personal"},
	}}
	// The fixture's person is the steward: give it the requester role.
	f.setRoles(f.person.ID, authn.RoleReader, authn.RoleStewardRequester)
	f.other = &storage.Principal{ID: "prn_other_steward", Kind: storage.PrincipalKindPerson, DisplayName: "Other", Roles: []string{authn.RoleReader, authn.RoleStewardRequester}, CreatedAt: t0}
	require.NoError(t, f.drv.Auth().Principals().Create(ctx, f.other))
	f.res = sharing.NewResolver(f.st).
		WithRegistry(f.drv.Auth()).
		WithClock(func() time.Time { return f.now }).
		WithSteward(f.options())
	return f
}

// options wires a resolver for the fixture's settings.
func (f *stewardFix) options() sharing.StewardOptions {
	return sharingtest.StewardOptions(func() sharing.StewardSettings { return *f.settings })
}

// setRoles rewrites a principal's stored roles, as removing an IdP
// group does.
func (f *stewardFix) setRoles(id string, roles ...string) {
	f.t.Helper()
	raw, err := storage.RolesJSON(roles)
	require.NoError(f.t, err)
	db := f.drv.(interface{ DB() *sql.DB }).DB()
	res, err := db.ExecContext(context.Background(), `UPDATE principals SET roles = ? WHERE id = ?`, raw, id)
	require.NoError(f.t, err)
	n, _ := res.RowsAffected()
	require.EqualValues(f.t, 1, n)
}

// grant stores a steward grant for steward with scope over [nb, exp),
// with the digest of scope.
func (f *stewardFix) grant(steward, scope string, nb, exp time.Time) *storage.StewardGrant {
	f.t.Helper()
	return sharingtest.GrantSteward(f.t, f.drv, steward, scope, nb, exp)
}

// grantDigest is grant with the stored digest given.
func (f *stewardFix) grantDigest(steward, scope, digest string, nb, exp time.Time) *storage.StewardGrant {
	f.t.Helper()
	return sharingtest.GrantStewardWithDigest(f.t, f.drv, steward, scope, digest, nb, exp)
}

// open is a grant of the whole window around stewardNow.
func (f *stewardFix) open(steward, scope string) *storage.StewardGrant {
	return f.grant(steward, scope, t0, t0.Add(48*time.Hour))
}

// resolve resolves the steward's viewer under grant g at the fixture's
// clock.
func (f *stewardFix) resolve(principal string, g *storage.StewardGrant, identity string) (sharing.Viewer, error) {
	return f.res.Resolve(context.Background(), principal, storage.PrincipalKindPerson, identity, sharing.AsSteward(g.ID))
}

func (f *stewardFix) refused(principal string, g *storage.StewardGrant, wantReason string, deferred bool) {
	f.t.Helper()
	v, err := f.resolve(principal, g, "")
	var refusal *sharing.StewardRefusal
	require.True(f.t, errors.As(err, &refusal), "want a steward refusal, got viewer %q and error %v", v.PolicyKey(), err)
	assert.Equal(f.t, wantReason, refusal.Reason)
	assert.Equal(f.t, deferred, refusal.Deferred, "deferred only when the cause is unavailability")
	assert.False(f.t, v.IsUnrestricted(), "a refused elevation is never unrestricted")
	assert.True(f.t, v.MatchesNothing(), "a refused elevation never falls back to a view")
	assert.Empty(f.t, v.PolicyKey())
}

// ordinaryGrant gives the steward its ordinary view (c1, c4).
func (f *stewardFix) ordinaryGrant() {
	f.join("colleagues", nil)
	f.storeFixture.grant("colleagues", sharing.TemplateColleagues, nil, t0)
}

const familyScope = `{"policies":[{"data":"tag==family","fields":{"contact":["title"]},"capabilities":{"search":["fts"]}}]}`

// With a grant named, the viewer is exactly the grant's compiled
// scope: not a merge with the steward's ordinary grants, whose view is
// unchanged. The policy key names the grant and its digest.
func TestStewardViewerIsExactlyTheGrantScope(t *testing.T) {
	f := newStewardFix(t)
	f.ordinaryGrant()
	g := f.open(f.person.ID, familyScope)

	ordinary, err := f.res.Resolve(context.Background(), f.person.ID, storage.PrincipalKindPerson, "")
	require.NoError(t, err)
	assert.Equal(t, want("c1", "c4"), visible(t, f.drv, ordinary), "the ordinary view is the colleagues grant, with or without a steward grant")
	assert.Empty(t, ordinary.StewardGrantID())
	assert.False(t, ordinary.StrictBackstop())

	v, err := f.resolve(f.person.ID, g, "")
	require.NoError(t, err)
	assert.Equal(t, want("c2"), visible(t, f.drv, v), "the elevated view is the scope: no ordinary grant merged in")
	assert.Equal(t, g.ID, v.StewardGrantID())
	assert.True(t, v.StrictBackstop(), "an elevated request runs the backstop strictly")
	assert.Equal(t, f.person.ID, v.PrincipalID())
	assert.Contains(t, v.PolicyKey(), g.ID)
	assert.Contains(t, v.PolicyKey(), g.ScopeDigest)
	plan := v.Plan()
	require.Len(t, plan.Grants, 1)
	assert.Equal(t, g.ID, plan.Grants[0].ID)
	assert.Equal(t, "steward:"+g.ID, plan.Grants[0].Audience)
	assert.Equal(t, 1, plan.Grants[0].Version, "the policy is frozen at version 1")
	assert.Equal(t, g.ID+"/0", plan.Grants[0].Policy, "the policy is the grant's first (only) spec")
	assert.Equal(t, []string{"title"}, plan.FieldsFor("contact"), "fields are the grant's, not the colleagues'")
	assert.Equal(t, []string{"fts"}, plan.Capabilities.Search)
}

// Differential: nothing outside the scope is reachable. Adding rows
// the scope does not select (whatever the steward's ordinary grant
// shows of them) changes nothing about the elevated view; adding one
// the scope selects adds exactly that row.
func TestStewardViewerDifferentialHiddenRowsChangeNothing(t *testing.T) {
	f := newStewardFix(t)
	f.ordinaryGrant()
	g := f.open(f.person.ID, familyScope)
	before, err := f.resolve(f.person.ID, g, "")
	require.NoError(t, err)
	visibleBefore := visible(t, f.drv, before)
	require.Equal(t, want("c2"), visibleBefore)

	add := func(id, typ, tag string, classes ...string) {
		t.Helper()
		obj := &storage.KnowledgeObject{
			ID: "obj-" + id, Type: typ, RawContent: id, Status: storage.ObjectStatusActive,
			Tags: []storage.Tag{{Label: tag}}, Classes: classes, ContentHash: "sha256:" + id,
			Metadata: map[string]any{}, CreatedAt: t0, UpdatedAt: t0,
		}
		require.NoError(t, f.drv.Objects().Create(context.Background(), obj))
	}
	// Rows the colleagues grant shows and the scope does not, and rows
	// neither shows.
	add("h-work", "contact", "work")
	add("h-client", "contact", "client")
	add("h-note", "note", "work")
	add("h-near", "contact", "family-adjacent")
	add("h-friend", "contact", "friends", "personal")
	after, err := f.resolve(f.person.ID, g, "")
	require.NoError(t, err)
	assert.Equal(t, visibleBefore, visible(t, f.drv, after), "rows outside the scope do not enter the elevated view")
	assert.Equal(t, before.PolicyKey(), after.PolicyKey())

	add("h-in", "note", "family")
	in, err := f.resolve(f.person.ID, g, "")
	require.NoError(t, err)
	assert.Equal(t, want("c2", "h-in"), visible(t, f.drv, in), "a row the scope selects is the one addition")
}

// Expiry is computed from the clock on every use, never cached: the
// same request one tick before the effective expiry passes and at it
// is refused, with a viewer already compiled and cached.
func TestStewardExpiryIsComputedOnEveryUse(t *testing.T) {
	f := newStewardFix(t)
	g := f.grant(f.person.ID, familyScope, t0, t0.Add(2*time.Hour))
	_, err := f.resolve(f.person.ID, g, "")
	require.NoError(t, err)
	require.Equal(t, 1, f.res.CacheSize(), "the elevated viewer is cached")

	f.now = g.ExpiresAt.Add(-time.Millisecond)
	_, err = f.resolve(f.person.ID, g, "")
	require.NoError(t, err, "one tick before expiry the grant still works")
	f.now = g.ExpiresAt
	f.refused(f.person.ID, g, sharing.StewardReasonGrantExpired, false)
	f.now = g.ExpiresAt.Add(24 * time.Hour)
	f.refused(f.person.ID, g, sharing.StewardReasonGrantExpired, false)
}

// A window that has not opened yet is not active.
func TestStewardNotYetActive(t *testing.T) {
	f := newStewardFix(t)
	g := f.grant(f.person.ID, familyScope, f.now.Add(time.Hour), f.now.Add(2*time.Hour))
	f.refused(f.person.ID, g, sharing.StewardReasonGrantNotActive, false)
	f.now = g.NotBefore
	_, err := f.resolve(f.person.ID, g, "")
	require.NoError(t, err, "not_before is inside the window")
}

// A revocation between two requests is seen by the second: the check
// reads the store on every request, so the compile cache cannot serve
// a revoked grant.
func TestStewardRevocationIsSeenOnTheNextRequest(t *testing.T) {
	f := newStewardFix(t)
	g := f.open(f.person.ID, familyScope)
	_, err := f.resolve(f.person.ID, g, "")
	require.NoError(t, err)
	require.Equal(t, 1, f.res.CacheSize())

	require.NoError(t, f.st.StewardGrants().Revoke(context.Background(), g.ID, storage.OwnerPrincipal, "matter closed", f.now))
	f.refused(f.person.ID, g, sharing.StewardReasonGrantRevoked, false)
	assert.Equal(t, 1, f.res.CacheSize(), "the cached viewer is still there; it is just never asked for")
}

// Lowering steward.max_duration shortens a live grant on its next use.
func TestStewardMaxDurationShortensALiveGrant(t *testing.T) {
	f := newStewardFix(t)
	g := f.grant(f.person.ID, familyScope, t0, t0.Add(10*24*time.Hour))
	f.now = t0.Add(5 * time.Hour)
	_, err := f.resolve(f.person.ID, g, "")
	require.NoError(t, err)

	f.settings.MaxDuration = 4 * time.Hour
	f.refused(f.person.ID, g, sharing.StewardReasonGrantExpired, false)
	f.now = t0.Add(3*time.Hour + 59*time.Minute)
	_, err = f.resolve(f.person.ID, g, "")
	require.NoError(t, err, "inside the lowered cap it works")

	f.settings.MaxDuration = 0
	f.now = t0.Add(9 * 24 * time.Hour)
	_, err = f.resolve(f.person.ID, g, "")
	require.NoError(t, err, "no cap: the approved window")
}

// The principal must still hold request:steward and be active: removing
// the role, suspending or revoking the principal ends use at once.
func TestStewardRoleMissingEndsUseAtOnce(t *testing.T) {
	f := newStewardFix(t)
	g := f.open(f.person.ID, familyScope)
	_, err := f.resolve(f.person.ID, g, "")
	require.NoError(t, err)

	f.setRoles(f.person.ID, authn.RoleReader)
	f.refused(f.person.ID, g, sharing.StewardReasonRoleMissing, false)
	f.setRoles(f.person.ID, authn.RoleReader, authn.RoleStewardRequester)
	_, err = f.resolve(f.person.ID, g, "")
	require.NoError(t, err, "the role back, the grant works again")

	// Roles that carry the scope under another name also pass: the check
	// is the scope, not a role name.
	f.setRoles(f.person.ID, authn.RoleStewardApprover)
	f.refused(f.person.ID, g, sharing.StewardReasonRoleMissing, false)
	f.setRoles(f.person.ID, authn.RoleReader, authn.RoleStewardRequester)

	require.NoError(t, f.drv.Auth().Principals().Suspend(context.Background(), f.person.ID))
	f.refused(f.person.ID, g, sharing.StewardReasonRoleMissing, false)
	require.NoError(t, f.drv.Auth().Principals().Revoke(context.Background(), f.person.ID, f.now))
	f.refused(f.person.ID, g, sharing.StewardReasonRoleMissing, false)
}

// Only the grant's steward may use it; an unknown grant, another
// principal and the owner are refused.
func TestStewardGrantBelongsToItsSteward(t *testing.T) {
	f := newStewardFix(t)
	g := f.open(f.person.ID, familyScope)

	f.refused(f.other.ID, g, sharing.StewardReasonNotSteward, false)
	v, err := f.res.Resolve(context.Background(), storage.OwnerPrincipal, storage.PrincipalKindOwner, "", sharing.AsSteward(g.ID))
	var refusal *sharing.StewardRefusal
	require.True(t, errors.As(err, &refusal))
	assert.Equal(t, sharing.StewardReasonNotSteward, refusal.Reason)
	assert.False(t, v.IsUnrestricted(), "naming a grant never leaves the owner unrestricted")

	_, err = f.res.Resolve(context.Background(), f.person.ID, storage.PrincipalKindPerson, "", sharing.AsSteward("stg_nothing"))
	require.True(t, errors.As(err, &refusal))
	assert.Equal(t, sharing.StewardReasonGrantUnknown, refusal.Reason)
}

// A stored scope that no longer matches its digest is refused, however
// the row changed; a re-spaced copy of the same scope is not a change.
func TestStewardTamperedScopeIsRefused(t *testing.T) {
	f := newStewardFix(t)
	widened := `{"policies":[{"data":"type!=__none__","fields":{"*":["*"]}}]}`
	digestOfNarrow, err := sharing.StewardScopeDigest(json.RawMessage(familyScope))
	require.NoError(t, err)
	g := f.grantDigest(f.person.ID, widened, digestOfNarrow, t0, t0.Add(48*time.Hour))
	f.refused(f.person.ID, g, sharing.StewardReasonScopeChanged, false)

	respaced := "{ \"policies\": [ { \"capabilities\": {\"search\":[\"fts\"]},\n \"fields\": {\"contact\":[\"title\"]}, \"data\":\"tag==family\" } ] }"
	ok := f.grantDigest(f.person.ID, respaced, digestOfNarrow, t0, t0.Add(48*time.Hour))
	v, err := f.resolve(f.person.ID, ok, "")
	require.NoError(t, err)
	assert.Equal(t, want("c2"), visible(t, f.drv, v))

	// A digest that is not a digest of anything.
	bogus := f.grantDigest(f.person.ID, familyScope, "v1:sha256:00", t0, t0.Add(48*time.Hour))
	f.refused(f.person.ID, bogus, sharing.StewardReasonScopeChanged, false)
}

// A scope the policy parser refuses (the digest matches, so it is not
// tampering) is refused as invalid, not served.
func TestStewardInvalidScopeIsRefused(t *testing.T) {
	f := newStewardFix(t)
	g := f.open(f.person.ID, `{"policies":[{"data":"mention==x"}]}`)
	f.refused(f.person.ID, g, sharing.StewardReasonScopeInvalid, false)
}

// failingGrants fails every read of a grant.
type failingGrants struct {
	storage.StewardGrantStore
	err error
}

func (f failingGrants) Get(context.Context, string) (*storage.StewardGrant, error) { return nil, f.err }

// failingSharing is a sharing store whose steward grant reads fail.
type failingSharing struct {
	storage.SharingStore
	err error
}

func (f failingSharing) StewardGrants() storage.StewardGrantStore {
	return failingGrants{StewardGrantStore: f.SharingStore.StewardGrants(), err: f.err}
}

// failingRegistry fails every principal read.
type failingRegistry struct {
	storage.AuthStore
	err error
}

type failingPrincipals struct {
	storage.PrincipalStore
	err error
}

func (f failingRegistry) Principals() storage.PrincipalStore {
	return failingPrincipals{PrincipalStore: f.AuthStore.Principals(), err: f.err}
}

func (f failingPrincipals) Get(context.Context, string) (*storage.Principal, error) {
	return nil, f.err
}

// A store that cannot be read is a refusal, never an active grant:
// deferred, since the cause is unavailability.
func TestStewardStoreErrorRefusesDeferred(t *testing.T) {
	f := newStewardFix(t)
	g := f.open(f.person.ID, familyScope)
	boom := errors.New("connection reset")

	f.res = sharing.NewResolver(failingSharing{SharingStore: f.st, err: boom}).WithRegistry(f.drv.Auth()).
		WithClock(func() time.Time { return f.now }).
		WithSteward(f.options())
	f.refused(f.person.ID, g, sharing.StewardReasonGrantUnavailable, true)
	_, err := f.resolve(f.person.ID, g, "")
	assert.ErrorIs(t, err, boom, "the cause is kept for the log")

	// An unreadable principal registry too.
	f.res = sharing.NewResolver(f.st).WithRegistry(failingRegistry{AuthStore: f.drv.Auth(), err: boom}).
		WithClock(func() time.Time { return f.now }).
		WithSteward(f.options())
	f.refused(f.person.ID, g, sharing.StewardReasonGrantUnavailable, true)

	// No registry to read roles from is the same: roles cannot be shown.
	f.res = sharing.NewResolver(f.st).
		WithClock(func() time.Time { return f.now }).
		WithSteward(f.options())
	f.refused(f.person.ID, g, sharing.StewardReasonGrantUnavailable, true)
}

// A resolver with no steward wiring, or with the feature off, refuses
// every elevation.
func TestStewardDisabledRefusesEverything(t *testing.T) {
	f := newStewardFix(t)
	g := f.open(f.person.ID, familyScope)

	f.settings.Enabled = false
	f.refused(f.person.ID, g, sharing.StewardReasonStewardDisabled, false)
	f.settings.Enabled = true
	_, err := f.resolve(f.person.ID, g, "")
	require.NoError(t, err)

	unwired := sharing.NewResolver(f.st).WithRegistry(f.drv.Auth()).WithClock(func() time.Time { return f.now })
	v, err := unwired.Resolve(context.Background(), f.person.ID, storage.PrincipalKindPerson, "", sharing.AsSteward(g.ID))
	var refusal *sharing.StewardRefusal
	require.True(t, errors.As(err, &refusal))
	assert.Equal(t, sharing.StewardReasonStewardDisabled, refusal.Reason)
	assert.True(t, v.MatchesNothing())

	var none *sharing.Resolver
	v, err = none.Resolve(context.Background(), f.person.ID, storage.PrincipalKindPerson, "", sharing.AsSteward(g.ID))
	require.True(t, errors.As(err, &refusal), "a nil resolver refuses an elevation, it does not answer the zero viewer")
	assert.True(t, v.MatchesNothing())
}

// A withheld class stays out of an elevated view unless the instance
// allows it AND the grant names the class in include_classes.
func TestStewardWithheldClassNeedsBothSwitches(t *testing.T) {
	f := newStewardFix(t)
	// Everything contact is in scope; c3 is a contact classed personal.
	for name, c := range map[string]struct {
		allow   bool
		include string
		want    []string
	}{
		"neither": {false, "", want("c1", "c2", "c4")},
		"the grant names it, the instance forbids":    {false, `,"include_classes":["personal"]`, want("c1", "c2", "c4")},
		"the instance allows it, the grant is silent": {true, "", want("c1", "c2", "c4")},
		"both": {true, `,"include_classes":["personal"]`, want("c1", "c2", "c3", "c4")},
	} {
		f.settings.AllowWithheld = c.allow
		g := f.open(f.person.ID, `{"policies":[{"data":"type==contact"}]`+c.include+`}`)
		v, err := f.resolve(f.person.ID, g, "")
		require.NoError(t, err, name)
		assert.Equal(t, c.want, visible(t, f.drv, v), name)
		if len(c.want) == 3 {
			assert.Contains(t, v.Plan().PartExclusions, "personal", name+": parts of the withheld class are withheld too")
		} else {
			assert.NotContains(t, v.Plan().PartExclusions, "personal", name)
		}
	}

	// The withheld classes are the instance's setting, applied per
	// grant at compile time: a grant approved under an older setting
	// follows the current one.
	f.settings.AllowWithheld = true
	g := f.open(f.person.ID, `{"policies":[{"data":"type==contact"}],"include_classes":["personal"]}`)
	v, err := f.resolve(f.person.ID, g, "")
	require.NoError(t, err)
	assert.Contains(t, visible(t, f.drv, v), "obj-c3")
	f.settings.AllowWithheld = false
	v, err = f.resolve(f.person.ID, g, "")
	require.NoError(t, err)
	assert.NotContains(t, visible(t, f.drv, v), "obj-c3", "turning allow_withheld off takes the class back on the next request")
	f.settings.WithheldClasses = []string{"financial"}
	v, err = f.resolve(f.person.ID, g, "")
	require.NoError(t, err)
	assert.NotContains(t, visible(t, f.drv, v), "obj-c4", "the withheld list is the setting's")
	assert.Contains(t, visible(t, f.drv, v), "obj-c3")
}

// The capabilities of a steward scope are forced: mcp and events never,
// export only where steward.allow_export is on, search and entities as
// the scope asks.
func TestStewardCapabilitiesAreForced(t *testing.T) {
	f := newStewardFix(t)
	g := f.open(f.person.ID, `{"policies":[{"data":"type==contact","capabilities":{"search":["fts","vector"],"entities":true,"export":true,"mcp":true,"events":true}}]}`)
	v, err := f.resolve(f.person.ID, g, "")
	require.NoError(t, err)
	assert.False(t, v.Allows(storage.CapabilityMCP), "mcp is forced off")
	assert.False(t, v.Allows(storage.CapabilityEvents), "events is forced off")
	assert.False(t, v.Allows(storage.CapabilityExport), "export is off until the instance allows it")
	assert.True(t, v.Allows(storage.CapabilityEntities))
	assert.True(t, v.Allows("search:fts"))

	f.settings.AllowExport = true
	v, err = f.resolve(f.person.ID, g, "")
	require.NoError(t, err)
	assert.True(t, v.Allows(storage.CapabilityExport), "allow_export opens it, for a scope that asks")
	assert.False(t, v.Allows(storage.CapabilityMCP))
	assert.False(t, v.Allows(storage.CapabilityEvents))

	quiet := f.open(f.person.ID, `{"policies":[{"data":"type==contact"}]}`)
	v, err = f.resolve(f.person.ID, quiet, "")
	require.NoError(t, err)
	assert.False(t, v.Allows(storage.CapabilityExport), "allow_export opens nothing for a scope that does not ask")
}

// The steward's own limits still intersect: the ceiling takes classes
// the scope would show, and a key policy bound to the credential narrows
// the scope.
func TestStewardCeilingAndKeyPolicyStillIntersect(t *testing.T) {
	f := newStewardFix(t)
	g := f.open(f.person.ID, `{"policies":[{"data":"type==contact"}]}`)
	v, err := f.resolve(f.person.ID, g, "")
	require.NoError(t, err)
	assert.Equal(t, want("c1", "c2", "c4"), visible(t, f.drv, v))

	require.NoError(t, f.st.SetDenyClasses(context.Background(), f.person.ID, []string{"financial"}))
	v, err = f.resolve(f.person.ID, g, "")
	require.NoError(t, err)
	assert.Equal(t, want("c1", "c2"), visible(t, f.drv, v), "the ceiling removes financial whatever the grant says")

	require.NoError(t, f.st.BindIdentityPolicy(context.Background(), f.key.ID, f.policies[sharing.TemplateColleagues].ID))
	v, err = f.resolve(f.person.ID, g, f.key.ID)
	require.NoError(t, err)
	assert.Equal(t, want("c1"), visible(t, f.drv, v), "the key policy (work or client contacts) narrows the scope")
	assert.Equal(t, f.key.ID, v.IdentityID())
	assert.NotEqual(t, f.mustResolve(g, "").PolicyKey(), v.PolicyKey())
}

func (f *stewardFix) mustResolve(g *storage.StewardGrant, identity string) sharing.Viewer {
	f.t.Helper()
	v, err := f.resolve(f.person.ID, g, identity)
	require.NoError(f.t, err)
	return v
}
