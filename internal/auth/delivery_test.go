package auth

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Administering an instance is not access to its data: a working
// credential for a principal reaches that principal's data, so only
// the owner issues one for another principal. Anyone may take one for
// itself; nobody but the owner for anyone else, whatever its roles and
// scopes.
func TestMayIssueCredentialFor(t *testing.T) {
	owner := LocalPrincipal()
	admin := &Principal{ID: "prn_admin", Kind: storage.PrincipalKindDevice, Roles: []string{RoleAdmin}, Scopes: ScopesForRoles([]string{RoleAdmin})}
	reader := &Principal{ID: "prn_reader", Kind: storage.PrincipalKindDevice, Roles: []string{RoleReader}}
	cases := []struct {
		name   string
		by     *Principal
		target string
		want   bool
	}{
		{"owner for another", owner, "prn_reader", true},
		{"owner for itself", owner, storage.OwnerPrincipal, true},
		{"admin for itself", admin, "prn_admin", true},
		{"admin for another", admin, "prn_reader", false},
		{"admin for the owner", admin, storage.OwnerPrincipal, false},
		{"reader for itself", reader, "prn_reader", true},
		{"reader for another", reader, "prn_admin", false},
		{"no caller", nil, "prn_reader", false},
		{"empty target", admin, "", false},
	}
	for _, c := range cases {
		if got := MayIssueCredentialFor(c.by, c.target); got != c.want {
			t.Errorf("%s: MayIssueCredentialFor = %v, want %v", c.name, got, c.want)
		}
	}
}
