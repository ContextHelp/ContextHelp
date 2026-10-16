package sharing_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// The resolver gives the owner Unrestricted, an unknown principal the
// zero viewer, a grant-less principal a viewer that matches nothing,
// and a nil resolver answers the owner and fails everyone else closed.
func TestResolverResolvesOwnerUnknownAndUngranted(t *testing.T) {
	ctx := context.Background()
	f := newStoreFixture(t)
	r := sharing.NewResolver(f.st).WithClock(func() time.Time { return t0 })

	owner, err := r.Resolve(ctx, storage.OwnerPrincipal, storage.PrincipalKindOwner, "")
	if err != nil || !owner.IsUnrestricted() {
		t.Fatalf("owner = (%+v, %v), want Unrestricted", owner, err)
	}
	unknown, err := r.Resolve(ctx, "prn_missing", storage.PrincipalKindPerson, "")
	if err != nil || !unknown.MatchesNothing() || unknown.PolicyKey() != "" {
		t.Fatalf("unknown principal = (%q, %v), want the zero viewer", unknown.PolicyKey(), err)
	}
	if r.CacheSize() != 0 {
		t.Fatalf("cache holds %d viewers before any compile", r.CacheSize())
	}
	nothing, err := r.Resolve(ctx, f.person.ID, storage.PrincipalKindPerson, "")
	if err != nil || !nothing.MatchesNothing() {
		t.Fatalf("no grants = (%q, %v), want a viewer that matches nothing", nothing.PolicyKey(), err)
	}

	var none *sharing.Resolver
	if v, err := none.Resolve(ctx, f.person.ID, storage.PrincipalKindPerson, ""); err != nil || !v.MatchesNothing() {
		t.Fatalf("nil resolver = (%+v, %v), want the zero viewer", v, err)
	}
	if v, err := none.Resolve(ctx, storage.OwnerPrincipal, storage.PrincipalKindOwner, ""); err != nil || !v.IsUnrestricted() {
		t.Fatalf("nil resolver owner = (%+v, %v), want Unrestricted", v, err)
	}
}

// A granted principal gets its compiled viewer, and the same grants
// reuse the compile: one cache entry per policy key.
func TestResolverCachesByPolicyKey(t *testing.T) {
	ctx := context.Background()
	f := newStoreFixture(t)
	r := sharing.NewResolver(f.st).WithClock(func() time.Time { return t0 })
	f.join("colleagues", nil)
	f.grant("colleagues", sharing.TemplateColleagues, nil, t0)

	v1, err := r.Resolve(ctx, f.person.ID, storage.PrincipalKindPerson, "")
	if err != nil || v1.MatchesNothing() {
		t.Fatalf("granted = (%q, %v), want a compiled viewer", v1.PolicyKey(), err)
	}
	if got := visible(t, f.drv, v1); len(got) == 0 {
		t.Fatal("granted viewer sees nothing")
	}
	v2, err := r.Resolve(ctx, f.person.ID, storage.PrincipalKindPerson, "")
	if err != nil || v2.PolicyKey() != v1.PolicyKey() {
		t.Fatalf("second resolve = (%q, %v), want the same key %q", v2.PolicyKey(), err, v1.PolicyKey())
	}
	if n := r.CacheSize(); n != 1 {
		t.Fatalf("cache holds %d viewers, want 1", n)
	}
}

// Any grant, policy or ceiling change compiles again, so a revoked
// grant is gone on the next request without an invalidation hook;
// Invalidate drops the compiled viewers anyway.
func TestResolverFollowsGrantAndPolicyChanges(t *testing.T) {
	ctx := context.Background()
	f := newStoreFixture(t)
	r := sharing.NewResolver(f.st).WithClock(func() time.Time { return t0 })
	f.join("colleagues", nil)
	g := f.grant("colleagues", sharing.TemplateColleagues, nil, t0)
	v1, err := r.Resolve(ctx, f.person.ID, storage.PrincipalKindPerson, "")
	if err != nil {
		t.Fatal(err)
	}

	// A policy edit bumps the version: a new key, a new compile.
	spec, _ := json.Marshal(map[string]any{"name": sharing.TemplateColleagues, "data": "type==contact", "capabilities": map[string]any{"entities": true}})
	if _, err := f.st.Policies().Update(ctx, f.policies[sharing.TemplateColleagues].ID, spec, t0); err != nil {
		t.Fatal(err)
	}
	v3, err := r.Resolve(ctx, f.person.ID, storage.PrincipalKindPerson, "")
	if err != nil || v3.PolicyKey() == v1.PolicyKey() {
		t.Fatalf("after policy edit key = %q, %v; want a new key", v3.PolicyKey(), err)
	}
	if n := r.CacheSize(); n != 2 {
		t.Fatalf("cache holds %d viewers after the edit, want 2", n)
	}

	// Revoking the grant: the next resolve sees nothing.
	if err := f.st.Grants().Revoke(ctx, g.ID, t0); err != nil {
		t.Fatal(err)
	}
	v4, err := r.Resolve(ctx, f.person.ID, storage.PrincipalKindPerson, "")
	if err != nil || !v4.MatchesNothing() {
		t.Fatalf("after revoke = (%q, %v), want a viewer that matches nothing", v4.PolicyKey(), err)
	}

	r.Invalidate()
	if r.CacheSize() != 0 {
		t.Fatal("Invalidate left viewers in the cache")
	}
}

// An identity bound to a narrower policy gets its own cache entry: the
// key names the identity.
func TestResolverKeysOnIdentity(t *testing.T) {
	ctx := context.Background()
	f := newStoreFixture(t)
	r := sharing.NewResolver(f.st).WithClock(func() time.Time { return t0 })
	f.join("partners", nil)
	f.grant("partners", sharing.TemplatePartners, nil, t0)
	if err := f.st.BindIdentityPolicy(ctx, f.key.ID, f.policies[sharing.TemplateColleagues].ID); err != nil {
		t.Fatal(err)
	}
	bare, err := r.Resolve(ctx, f.person.ID, storage.PrincipalKindPerson, "")
	if err != nil {
		t.Fatal(err)
	}
	keyed, err := r.Resolve(ctx, f.person.ID, storage.PrincipalKindPerson, f.key.ID)
	if err != nil {
		t.Fatal(err)
	}
	if bare.PolicyKey() == keyed.PolicyKey() || keyed.IdentityID() != f.key.ID {
		t.Fatalf("keyed viewer %q (identity %q) must differ from the bare one %q", keyed.PolicyKey(), keyed.IdentityID(), bare.PolicyKey())
	}
	if r.CacheSize() != 2 {
		t.Fatalf("cache holds %d viewers, want 2", r.CacheSize())
	}
}
