package postgres

import (
	"encoding/json"
	"regexp"
	"slices"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/auth"
)

// The owner roles migration 41 writes are auth.OwnerRoles, in order:
// the literal in its statement cannot drift from the roles the auth
// chain gives the owner key.
func TestOwnerRolesMigrationMatchesAuthOwnerRoles(t *testing.T) {
	literal := regexp.MustCompile(`SET roles = '(\[[^']*\])' WHERE kind = 'owner'`)
	for _, m := range pgMigrations {
		if m.Version != 41 {
			continue
		}
		if len(m.Statements) != 1 {
			t.Fatalf("migration 41 has %d statements, want 1", len(m.Statements))
		}
		sub := literal.FindStringSubmatch(m.Statements[0])
		if sub == nil {
			t.Fatalf("migration 41 statement %q sets no owner roles", m.Statements[0])
		}
		var got []string
		if err := json.Unmarshal([]byte(sub[1]), &got); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(got, auth.OwnerRoles) {
			t.Errorf("migration 41 owner roles = %v, want auth.OwnerRoles %v", got, auth.OwnerRoles)
		}
		return
	}
	t.Fatal("migration 41 not found")
}
