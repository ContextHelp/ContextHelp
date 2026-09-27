package auth

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/testguard"
)

func TestMain(m *testing.M) {
	os.Exit(testguard.Main(m, nil))
}

// The bundles are pinned scope by scope: a new scope must land in the
// bundle its verb implies, and nowhere else.
func TestRoleBundles(t *testing.T) {
	cases := map[string][]Scope{
		RoleReader: {
			ScopeReadObjects, ScopeReadInbox, ScopeReadFeeds, ScopeReadJobs,
			ScopeReadRegistries, ScopeReadSystem,
		},
		RoleWriter: {
			ScopeReadObjects, ScopeWriteObjects,
			ScopeReadInbox, ScopeWriteInbox,
			ScopeReadFeeds, ScopeWriteFeeds,
			ScopeReadJobs, ScopeWriteJobs,
			ScopeReadRegistries,
			ScopeReadSystem, ScopeWriteSystem,
		},
		RoleAdmin: AllScopes,
	}
	for role, want := range cases {
		got, ok := Bundle(role)
		if !ok {
			t.Fatalf("Bundle(%q): unknown role", role)
		}
		if !slices.Equal(got, want) {
			t.Errorf("Bundle(%q) =\n  %v\nwant\n  %v", role, got, want)
		}
	}
}

func TestWriterBundleExcludesDeleteProcessSyncAdmin(t *testing.T) {
	got, _ := Bundle(RoleWriter)
	for _, s := range got {
		switch s.Verb() {
		case "read", "write":
		default:
			t.Errorf("writer bundle holds %q", s)
		}
	}
	for _, s := range []Scope{ScopeDeleteObjects, ScopeProcessInbox, ScopeSyncRegistries, ScopeAdminAudit} {
		if slices.Contains(got, s) {
			t.Errorf("writer bundle must not hold %q", s)
		}
	}
}

func TestAllScopesUniqueAndWellFormed(t *testing.T) {
	seen := map[Scope]bool{}
	for _, s := range AllScopes {
		if seen[s] {
			t.Errorf("duplicate scope %q", s)
		}
		seen[s] = true
		verb, resource, ok := strings.Cut(string(s), ":")
		if !ok || verb == "" || resource == "" {
			t.Errorf("scope %q is not verb:resource", s)
		}
	}
}

func TestValidateRoles(t *testing.T) {
	if err := ValidateRoles(nil); err == nil {
		t.Error("empty role list must be rejected")
	}
	if err := ValidateRoles([]string{"root"}); err == nil || !strings.Contains(err.Error(), `"root"`) {
		t.Errorf("unknown role error = %v, want it to name the role", err)
	}
	if err := ValidateRoles([]string{RoleReader, RoleWriter}); err != nil {
		t.Errorf("known roles rejected: %v", err)
	}
}

func TestScopesForRolesUnion(t *testing.T) {
	got := ScopesForRoles([]string{RoleReader, RoleWriter})
	want, _ := Bundle(RoleWriter)
	if !slices.Equal(got, want) {
		t.Errorf("reader+writer = %v, want the writer bundle %v", got, want)
	}
}

func TestNewStaticRejectsUnknownOrMissingRole(t *testing.T) {
	if _, err := NewStatic([]StaticToken{{Token: "t", Principal: "p"}}); err == nil {
		t.Error("token without a role must be rejected")
	}
	if _, err := NewStatic([]StaticToken{{Token: "t", Principal: "p", Roles: []string{"owner"}}}); err == nil {
		t.Error("token with an unknown role must be rejected")
	}
}

func TestStaticPrincipalCarriesRoleScopes(t *testing.T) {
	p, err := NewStatic([]StaticToken{
		{Token: "tok-r", Principal: "phone", Roles: []string{RoleReader}},
		{Token: "tok-w", Principal: "laptop", Roles: []string{RoleWriter}},
	})
	if err != nil {
		t.Fatalf("NewStatic: %v", err)
	}
	reader, err := p.Authenticate(context.Background(), Credential{Token: "tok-r"})
	if err != nil {
		t.Fatalf("Authenticate reader: %v", err)
	}
	if !reader.HasScope(ScopeReadObjects) || reader.HasScope(ScopeWriteObjects) {
		t.Errorf("reader scopes = %v", reader.Scopes)
	}
	writer, err := p.Authenticate(context.Background(), Credential{Token: "tok-w"})
	if err != nil {
		t.Fatalf("Authenticate writer: %v", err)
	}
	if !writer.HasScope(ScopeWriteObjects) || writer.HasScope(ScopeDeleteObjects) {
		t.Errorf("writer scopes = %v", writer.Scopes)
	}
}

func TestBypassesEntityGateOnlyForAdmin(t *testing.T) {
	for role, want := range map[string]bool{RoleAdmin: true, RoleWriter: false, RoleReader: false} {
		p := &Principal{ID: "x", Roles: []string{role}}
		if got := p.BypassesEntityGate(); got != want {
			t.Errorf("%s bypass = %v, want %v", role, got, want)
		}
	}
	var none *Principal
	if none.BypassesEntityGate() {
		t.Error("nil principal must not bypass the gate")
	}
}

func TestLocalPrincipalHoldsEveryScope(t *testing.T) {
	p := LocalPrincipal()
	for _, s := range AllScopes {
		if !p.HasScope(s) {
			t.Errorf("local principal lacks %q", s)
		}
	}
	p.Scopes[0] = "mutated"
	if LocalPrincipal().Scopes[0] != AllScopes[0] {
		t.Error("LocalPrincipal shares its scope slice with AllScopes")
	}
}

// config validates role names without importing this package; the two
// lists must agree.
func TestConfigAuthRolesMatchRoles(t *testing.T) {
	if !slices.Equal(config.AuthRoles, Roles) {
		t.Errorf("config.AuthRoles = %v, auth.Roles = %v", config.AuthRoles, Roles)
	}
}
