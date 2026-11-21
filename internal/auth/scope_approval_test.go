package auth

import (
	"slices"
	"testing"
)

// approvalScopeSet is the set ADR-082 §1 keeps out of admin: the steward
// scopes and the sharing-change approver scope.
var approvalScopeSet = []Scope{ScopeRequestSteward, ScopeApproveSteward, ScopeApproveShares}

// ADR-082 §1 invariant: administering is not data access, so the admin
// bundle, and every verb bundle, has an empty intersection with the
// approval scopes, alone and in any combination.
func TestApprovalScopes_NoVerbBundleHoldsThem(t *testing.T) {
	for _, role := range Roles {
		b, ok := Bundle(role)
		if !ok {
			t.Fatalf("Bundle(%q): unknown role", role)
		}
		for _, s := range approvalScopeSet {
			if slices.Contains(b, s) {
				t.Errorf("role %s holds %s: only its approval role may", role, s)
			}
		}
	}
	all := ScopesForRoles(Roles)
	for _, s := range approvalScopeSet {
		if slices.Contains(all, s) {
			t.Errorf("admin+writer+reader hold %s", s)
		}
		if !slices.Contains(AllScopes, s) {
			t.Errorf("%s is not a known scope", s)
		}
		if !IsApprovalScope(s) {
			t.Errorf("IsApprovalScope(%s) = false", s)
		}
		if v := s.Verb(); v == verbRead || v == verbWrite {
			t.Errorf("%s has verb %q: it would join a verb bundle", s, v)
		}
	}
	if IsApprovalScope(ScopeAdminShares) {
		t.Error("admin:shares is an admin scope, not an approval scope")
	}
}

// Each approval role grants its one scope and write:self, nothing else.
func TestApprovalScopes_RoleBundles(t *testing.T) {
	cases := map[string]Scope{
		RoleStewardRequester: ScopeRequestSteward,
		RoleStewardApprover:  ScopeApproveSteward,
		RoleShareApprover:    ScopeApproveShares,
	}
	if len(cases) != len(ApprovalRoles) {
		t.Fatalf("ApprovalRoles = %v, want the three of ADR-082", ApprovalRoles)
	}
	for _, role := range ApprovalRoles {
		got, ok := Bundle(role)
		if !ok {
			t.Fatalf("Bundle(%q): unknown role", role)
		}
		if want := []Scope{ScopeWriteSelf, cases[role]}; !slices.Equal(got, want) {
			t.Errorf("Bundle(%q) = %v, want %v", role, got, want)
		}
		if err := ValidateRoles([]string{RoleReader, role}); err != nil {
			t.Errorf("ValidateRoles(reader, %s): %v", role, err)
		}
	}
	if !slices.Equal(KnownRoles(), append(slices.Clone(Roles), ApprovalRoles...)) {
		t.Errorf("KnownRoles() = %v", KnownRoles())
	}
}

// The owner holds every approval role explicitly, so it is the one
// principal whose ceiling covers them; an admin's ceiling never does.
func TestApprovalScopes_OwnerHoldsThemAdminDoesNot(t *testing.T) {
	for _, role := range append([]string{RoleAdmin}, ApprovalRoles...) {
		if !slices.Contains(OwnerRoles, role) {
			t.Errorf("OwnerRoles lacks %s", role)
		}
	}
	owner := ScopesForRoles(OwnerRoles)
	admin := ScopesForRoles([]string{RoleAdmin})
	for _, s := range approvalScopeSet {
		if !slices.Contains(owner, s) {
			t.Errorf("the owner's roles lack %s", s)
		}
		if slices.Contains(admin, s) {
			t.Errorf("admin holds %s", s)
		}
	}
	if lp := LocalPrincipal(); !slices.Equal(lp.Roles, OwnerRoles) {
		t.Errorf("LocalPrincipal().Roles = %v, want %v", lp.Roles, OwnerRoles)
	}
}

// A browser session holds an approval scope only when its principal
// does: the owner's session holds all three, an admin's none.
func TestApprovalScopes_SessionsFollowThePrincipal(t *testing.T) {
	ownerSession := SessionScopesFor(ScopesForRoles(OwnerRoles), SessionKindUI)
	adminSession := SessionScopesFor(ScopesForRoles([]string{RoleAdmin}), SessionKindUI)
	approver := SessionScopesFor(ScopesForRoles([]string{RoleReader, RoleShareApprover}), SessionKindUI)
	for _, s := range approvalScopeSet {
		if !slices.Contains(ownerSession, s) {
			t.Errorf("the owner's session lacks %s", s)
		}
		if slices.Contains(adminSession, s) {
			t.Errorf("an admin's session holds %s", s)
		}
	}
	if !slices.Contains(approver, ScopeApproveShares) || slices.Contains(approver, ScopeApproveSteward) {
		t.Errorf("a share approver's session = %v", approver)
	}
}
