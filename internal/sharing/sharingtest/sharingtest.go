// Package sharingtest seeds the sharing registry for tests: principals
// granted a share policy, so a non-owner caller sees data. Import it
// from test files only.
package sharingtest

import (
	"context"
	"errors"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// EverythingPolicy is the policy GrantEverything grants: every object,
// every capability. A non-owner principal sees nothing without a grant
// (ADR-080 §2); tests of the API's shape as a reader or writer want
// the data, not the sharing rules, and tests of the sharing rules
// build their own grants.
const EverythingPolicy = "test-everything"

// Everything is the EverythingPolicy spec.
func Everything() sharing.Policy {
	return sharing.Policy{
		Name: EverythingPolicy,
		// Every row has a type; the bound is one no object carries, so
		// fixtures with a zero created_at are visible too.
		Data: "type!=__none__",
		Capabilities: sharing.Capabilities{
			Search: sharing.SearchModes, Entities: true, Export: true, MCP: true, Events: true,
		},
	}
}

// EnsurePrincipal creates principal id of kind kind with roles in
// driver's registry when it does not exist.
func EnsurePrincipal(t testing.TB, driver storage.StorageDriver, id, kind string, roles ...string) {
	t.Helper()
	err := driver.Auth().Principals().Create(context.Background(), &storage.Principal{ID: id, Kind: kind, DisplayName: id, Roles: roles})
	if err != nil && !errors.Is(err, storage.ErrAlreadyExists) {
		t.Fatalf("sharingtest: create principal %s: %v", id, err)
	}
}

// GrantEverything puts principals, which must exist in the registry,
// in one audience granted EverythingPolicy. It installs the policy and
// the audience on first use.
func GrantEverything(t testing.TB, driver storage.StorageDriver, principals ...string) {
	t.Helper()
	Grant(t, driver, Everything(), principals...)
}

// Grant puts principals, which must exist in the registry, in an
// audience named after policy and grants it policy, installing the
// policy and the audience when they are not there yet.
func Grant(t testing.TB, driver storage.StorageDriver, policy sharing.Policy, principals ...string) {
	t.Helper()
	ctx := context.Background()
	st := driver.Sharing()
	row, err := st.Policies().GetByName(ctx, policy.Name)
	if errors.Is(err, storage.ErrNotFound) {
		spec, serr := policy.Spec()
		if serr != nil {
			t.Fatalf("sharingtest: policy %s: %v", policy.Name, serr)
		}
		row = &storage.SharePolicy{Name: policy.Name, Spec: spec}
		err = st.Policies().Create(ctx, row)
	}
	if err != nil {
		t.Fatalf("sharingtest: policy %s: %v", policy.Name, err)
	}
	audience, err := st.Audiences().GetByName(ctx, policy.Name)
	if errors.Is(err, storage.ErrNotFound) {
		audience = &storage.Audience{Name: policy.Name}
		if err = st.Audiences().Create(ctx, audience); err == nil {
			err = st.Grants().Create(ctx, &storage.ShareGrant{AudienceID: audience.ID, PolicyID: row.ID, CreatedBy: storage.OwnerPrincipal})
		}
	}
	if err != nil {
		t.Fatalf("sharingtest: audience %s: %v", policy.Name, err)
	}
	for _, p := range principals {
		m := &storage.AudienceMember{AudienceID: audience.ID, PrincipalID: p, AddedBy: storage.OwnerPrincipal}
		if err := st.Audiences().AddMember(ctx, m); err != nil {
			t.Fatalf("sharingtest: add member %s: %v", p, err)
		}
	}
}
