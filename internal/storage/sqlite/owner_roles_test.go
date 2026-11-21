package sqlite

import (
	"slices"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// The owner roles migration 060 writes are auth.OwnerRoles, in order:
// the literal cannot drift from the roles the auth chain gives the
// owner key.
func TestOwnerRolesJSONMatchesAuthOwnerRoles(t *testing.T) {
	got, err := storage.ParseRolesJSON(ownerRolesJSON)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, auth.OwnerRoles) {
		t.Errorf("ownerRolesJSON = %v, want auth.OwnerRoles %v", got, auth.OwnerRoles)
	}
}
