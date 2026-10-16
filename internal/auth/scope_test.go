package auth

import (
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
// bundle its verb implies, and nowhere else. read:ui (mint a login code)
// is a read, so every role holds it; signout:ui is session-only, so no
// role does, admin included.
func TestRoleBundles(t *testing.T) {
	cases := map[string][]Scope{
		RoleReader: {
			ScopeReadObjects, ScopeReadMCP, ScopeReadInbox, ScopeReadFeeds, ScopeReadJobs,
			ScopeReadRegistries, ScopeReadSystem, ScopeWriteSelf, ScopeReadPipelines, ScopeReadWatches,
			ScopeReadUI,
		},
		RoleWriter: {
			ScopeReadObjects, ScopeWriteObjects, ScopeReadMCP,
			ScopeReadInbox, ScopeWriteInbox,
			ScopeReadFeeds, ScopeWriteFeeds,
			ScopeReadJobs, ScopeWriteJobs,
			ScopeReadRegistries,
			ScopeReadSystem, ScopeWriteSystem,
			ScopeWriteSelf,
			ScopeReadPipelines, ScopeReadWatches,
			ScopeReadUI,
		},
		RoleAdmin: slices.DeleteFunc(slices.Clone(AllScopes), func(s Scope) bool { return s == ScopeSignoutUI }),
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
	for _, s := range []Scope{ScopeAdminPrincipals, ScopeAdminShares} {
		if slices.Contains(got, s) {
			t.Errorf("writer bundle must not hold %q", s)
		}
	}
	for _, s := range []Scope{
		ScopeDeleteObjects, ScopeDeleteEdges, ScopeDeleteAliases, ScopeDeleteSearches, ScopeProcessInbox,
		ScopeCancelJobs, ScopeSyncRegistries, ScopeAdminAudit, ScopeSignoutUI,
	} {
		if slices.Contains(got, s) {
			t.Errorf("writer bundle must not hold %q", s)
		}
	}
}

// signout:ui belongs to browser sessions only; read:ui (minting) to
// every role and never to a session.
func TestUIScopesPlacement(t *testing.T) {
	for _, role := range Roles {
		b, _ := Bundle(role)
		if slices.Contains(b, ScopeSignoutUI) {
			t.Errorf("role %s holds %s: only browser sessions may", role, ScopeSignoutUI)
		}
		if !slices.Contains(b, ScopeReadUI) {
			t.Errorf("role %s lacks %s: every role may sign a browser in", role, ScopeReadUI)
		}
	}
	if slices.Contains(UISessionScopes, ScopeReadUI) {
		t.Errorf("the session ui set holds %s: a session could mint sessions", ScopeReadUI)
	}
	if !slices.Contains(UISessionScopes, ScopeSignoutUI) {
		t.Errorf("the session ui set lacks %s: a session could not sign out", ScopeSignoutUI)
	}
}

// The web UI session set is pinned: the reads the web UI makes, its own
// writes (delete an object, retry a job, sign out) and the Sharing page
// (admin:shares, ADR-080 §11), nothing else. admin:shares is the one
// admin scope a session may hold: the role bundles keep it admin-only,
// so only an admin's session reaches the sharing routes.
func TestUISessionScopesPinned(t *testing.T) {
	want := []Scope{
		ScopeReadObjects, ScopeDeleteObjects,
		ScopeReadInbox, ScopeReadFeeds,
		ScopeReadJobs, ScopeWriteJobs,
		ScopeReadRegistries, ScopeReadSystem,
		ScopeWriteSelf,
		ScopeAdminShares,
		ScopeSignoutUI,
	}
	if !slices.Equal(UISessionScopes, want) {
		t.Errorf("UISessionScopes =\n  %v\nwant\n  %v", UISessionScopes, want)
	}
	for _, s := range UISessionScopes {
		if !slices.Contains(AllScopes, s) {
			t.Errorf("ui set holds unknown scope %q", s)
		}
		switch s.Verb() {
		case "sync", "process":
			t.Errorf("ui set holds %q", s)
		case "admin":
			if s != ScopeAdminShares {
				t.Errorf("ui set holds %q: admin:shares is the only admin scope a session may hold", s)
			}
		}
	}
	for _, s := range []Scope{
		ScopeReadUI, ScopeReadMCP, ScopeWriteObjects, ScopeDeleteEdges, ScopeDeleteAliases, ScopeDeleteSearches,
		ScopeReadPipelines, ScopeReadWatches, ScopeWriteSystem, ScopeAdminAudit,
		ScopeAdminPrincipals,
	} {
		if slices.Contains(UISessionScopes, s) {
			t.Errorf("ui set must not hold %q", s)
		}
	}
	if SessionScopes(SessionKindUI) == nil || SessionScopes("full") != nil {
		t.Error("SessionScopes: ui kind maps to the ui set, unknown kinds to nothing")
	}
}

// A session holds its principal's scopes intersected with the kind's
// set, plus signout:ui whatever the principal holds; nothing for an
// unknown kind.
func TestSessionScopesFor(t *testing.T) {
	principal := []Scope{ScopeWriteJobs, ScopeReadObjects, ScopeAdminAudit, ScopeReadUI}
	if got, want := SessionScopesFor(principal, SessionKindUI), []Scope{ScopeReadObjects, ScopeWriteJobs, ScopeSignoutUI}; !slices.Equal(got, want) {
		t.Errorf("SessionScopesFor = %v, want %v", got, want)
	}
	if got, want := SessionScopesFor(nil, SessionKindUI), []Scope{ScopeSignoutUI}; !slices.Equal(got, want) {
		t.Errorf("SessionScopesFor(nil) = %v, want %v", got, want)
	}
	if got := SessionScopesFor(AllScopes, "full"); got == nil || len(got) != 0 {
		t.Errorf("SessionScopesFor(all, unknown kind) = %#v, want empty non-nil", got)
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

// ADR-079 §5: write:self is in every role bundle and in the browser
// session set, so any principal manages its own credentials from a key
// or a browser; admin:principals and admin:shares are admin-only. The
// target check every write:self route makes is IsSelf: the caller
// itself, never another principal, never an empty id.
func TestSelfAndAdminScopes(t *testing.T) {
	for _, role := range Roles {
		b, _ := Bundle(role)
		if !slices.Contains(b, ScopeWriteSelf) {
			t.Errorf("role %s lacks %s", role, ScopeWriteSelf)
		}
		for _, s := range []Scope{ScopeAdminPrincipals, ScopeAdminShares} {
			if got, want := slices.Contains(b, s), role == RoleAdmin; got != want {
				t.Errorf("role %s holds %s = %v, want %v", role, s, got, want)
			}
		}
	}
	if !slices.Contains(UISessionScopes, ScopeWriteSelf) {
		t.Errorf("the session ui set lacks %s: a browser could not register a passkey", ScopeWriteSelf)
	}
	if got := SessionScopesFor(ScopesForRoles([]string{RoleReader}), SessionKindUI); !slices.Contains(got, ScopeWriteSelf) {
		t.Errorf("a reader's session lacks %s: %v", ScopeWriteSelf, got)
	}

	p := &Principal{ID: "prn_alice"}
	for id, want := range map[string]bool{"prn_alice": true, "prn_bob": false, "": false, "owner": false} {
		if got := p.IsSelf(id); got != want {
			t.Errorf("IsSelf(%q) = %v, want %v", id, got, want)
		}
	}
	var none *Principal
	if none.IsSelf("prn_alice") {
		t.Error("a nil principal is nobody")
	}
}

// config validates role names without importing this package; the two
// lists must agree.
func TestConfigAuthRolesMatchRoles(t *testing.T) {
	if !slices.Equal(config.AuthRoles, Roles) {
		t.Errorf("config.AuthRoles = %v, auth.Roles = %v", config.AuthRoles, Roles)
	}
}

// Step install and uninstall are guarded by admin:steps, named after the
// resource the routes act on (/steps), like admin:pipelines and
// admin:watches. There is no plugin system, and no admin:plugins alias:
// a scope string that names no scope grants nothing.
func TestStepAdminScopeName(t *testing.T) {
	if ScopeAdminSteps != "admin:steps" {
		t.Errorf("ScopeAdminSteps = %q, want admin:steps", ScopeAdminSteps)
	}
	b, _ := Bundle(RoleAdmin)
	if !slices.Contains(b, ScopeAdminSteps) {
		t.Errorf("admin bundle lacks %s", ScopeAdminSteps)
	}
	for _, s := range AllScopes {
		if strings.Contains(string(s), "plugin") {
			t.Errorf("scope %q names plugins: there is no plugin system", s)
		}
	}
}
