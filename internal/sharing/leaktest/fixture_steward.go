package leaktest

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/sharing/sharingtest"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Steward access (ADR-083): a person holding the steward-requester
// bundle and three steward grants over the contact corpus, one in
// force, one expired, one revoked. The browser elevation that names a
// grant (the session's steward_grant_id) is built elsewhere; the
// fixture authenticates a token per grant to the steward principal
// with the grant in its Meta, as the session authenticator will.
const (
	// StewardID is the steward principal.
	StewardID = "prn_leak_steward"
	// NameSteward is the steward elevated into its grant in force.
	NameSteward = "steward"
	// NameStewardExpired names the grant whose window has ended.
	NameStewardExpired = "steward-expired"
	// NameStewardRevoked names the grant that was revoked.
	NameStewardRevoked = "steward-revoked"
	// stewardAuthMethod is the test authenticator's name, so the
	// trail's authn check reads authn.leaktest.
	stewardAuthMethod = "leaktest"
)

// StewardScope is the scope of the fixture's steward grants: every
// contact (the personal class stays withheld, as steward.withheld_classes
// says by default), its card fields, every search mode and entities.
// The rows it opens are the partner template's.
const StewardScope = `{"policies":[{"data":"type==contact",` +
	`"fields":{"contact":["title","org","role","email","tags","city"]},` +
	`"capabilities":{"search":["fts","vector","graph","rsql"],"entities":true}}]}`

// WithStewards adds the steward and its grants, turns steward.enabled
// on, and authenticates through the keys, the browser sessions and a
// token per grant (Steward, StewardOf).
func WithStewards() Option {
	return func(o *options) { o.stewards = true }
}

// stewardAuth is the fixture's chain when it holds stewards: the
// steward tokens, then the instance's own key and session methods
// (bound once the stack exists).
type stewardAuth struct {
	tokens   map[string]*authn.Principal
	keys     authn.Method
	sessions atomic.Pointer[authn.SessionMethod]
}

func newStewardAuth(driver storage.StorageDriver) *stewardAuth {
	return &stewardAuth{
		tokens: map[string]*authn.Principal{},
		keys:   authn.NewAPIKeyMethod(authn.NewKeysOver(driver.Auth(), nil), nil),
	}
}

// methods is the chain the instance runs.
func (a *stewardAuth) methods() []authn.Method {
	return []authn.Method{stewardTokens{a}, a.keys, lateSessions{a}}
}

// stewardTokens authenticates a steward token to its elevated
// principal; any other credential is the key method's.
type stewardTokens struct{ a *stewardAuth }

func (stewardTokens) Name() string  { return stewardAuthMethod }
func (stewardTokens) Ambient() bool { return false }

func (m stewardTokens) Authenticate(_ context.Context, cred authn.Credential) (*authn.Principal, bool, error) {
	p, ok := m.a.tokens[cred.Token]
	if cred.Token == "" || !ok {
		return nil, false, nil
	}
	out := *p
	out.Roles = append([]string(nil), p.Roles...)
	out.Scopes = append([]authn.Scope(nil), p.Scopes...)
	out.Meta = map[string]string{}
	for k, v := range p.Meta {
		out.Meta[k] = v
	}
	return &out, true, nil
}

// lateSessions is the instance's session method, bound after the stack
// is built.
type lateSessions struct{ a *stewardAuth }

func (lateSessions) Name() string  { return authn.MethodSession }
func (lateSessions) Ambient() bool { return true }

func (m lateSessions) Authenticate(ctx context.Context, cred authn.Credential) (*authn.Principal, bool, error) {
	s := m.a.sessions.Load()
	if s == nil {
		return nil, false, nil
	}
	return s.Authenticate(ctx, cred)
}

// seedStewards creates the steward, its three grants and a token for
// each, and turns steward access on.
func (f *Fixture) seedStewards(t testing.TB, a *stewardAuth) {
	t.Helper()
	ctx := context.Background()
	driver := f.Server.Driver
	f.Server.Config.Steward.Enabled = true
	if s := f.Server.Stack.UISessions; s != nil {
		a.sessions.Store(s.Method())
	}
	sharingtest.EnsurePrincipal(t, driver, StewardID, storage.PrincipalKindPerson, authn.RoleReader, authn.RoleStewardRequester)
	if err := driver.Entitlements().Upsert(ctx, &storage.RegistryEntitlement{
		Kind: storage.EntitlementInbound, RegistryName: StewardID, Plan: "inbound", Namespaces: []string{"*"}, FetchedAt: time.Now(),
	}); err != nil {
		t.Fatalf("leaktest: entitle the steward: %v", err)
	}
	now := time.Now()
	grants := map[string]*storage.StewardGrant{
		NameSteward:        sharingtest.GrantSteward(t, driver, StewardID, StewardScope, now.Add(-time.Hour), now.Add(24*time.Hour)),
		NameStewardExpired: sharingtest.GrantSteward(t, driver, StewardID, StewardScope, now.Add(-48*time.Hour), now.Add(-time.Hour)),
		NameStewardRevoked: sharingtest.GrantSteward(t, driver, StewardID, StewardScope, now.Add(-time.Hour), now.Add(24*time.Hour)),
	}
	if err := driver.Sharing().StewardGrants().Revoke(ctx, grants[NameStewardRevoked].ID, storage.OwnerPrincipal, "matter closed", now); err != nil {
		t.Fatalf("leaktest: revoke the steward grant: %v", err)
	}
	roles := []string{authn.RoleReader, authn.RoleStewardRequester}
	for name, g := range grants {
		token := "leaktest-" + name + "-" + g.ID
		a.tokens[token] = &authn.Principal{
			ID: StewardID, Kind: storage.PrincipalKindPerson, Provider: stewardAuthMethod,
			Roles: roles, Scopes: authn.ScopesForRoles(roles),
			Meta: map[string]string{authn.MetaStewardGrantID: g.ID},
		}
		p := Principal{Name: name, ID: StewardID, Token: token, grant: g.ID}
		if name == NameSteward {
			v, err := f.Server.Stack.Service.Viewers().Resolve(ctx, StewardID, storage.PrincipalKindPerson, "", sharing.AsSteward(g.ID))
			if err != nil {
				t.Fatalf("leaktest: resolve the elevated viewer: %v", err)
			}
			p.viewer = v
		}
		f.stewards = append(f.stewards, p)
	}
}

// Steward is the steward elevated into its grant in force; the zero
// Principal when the fixture holds no stewards.
func (f *Fixture) Steward() Principal { return f.StewardOf(NameSteward) }

// StewardOf is the steward elevated into the grant named name
// (NameSteward, NameStewardExpired, NameStewardRevoked).
func (f *Fixture) StewardOf(name string) Principal {
	for _, p := range f.stewards {
		if p.Name == name {
			return p
		}
	}
	return Principal{}
}

// Grant is the steward grant p is elevated into; "" for every other
// principal.
func (p Principal) Grant() string { return p.grant }
