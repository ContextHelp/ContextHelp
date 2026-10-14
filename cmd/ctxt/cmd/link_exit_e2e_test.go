package cmd

import (
	"slices"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/dpkmstest"
	"github.com/ideacrafterslabs/ctxt/internal/testguard"
)

// The exit codes of `ctxt link`, checked on the built binary against an
// in-process protected dpkms: a writer links but its unlink is refused
// (5, naming delete:edges) and leaves the edges; an admin unlinks; no
// token is 5; a missing object 3; an unknown type 2; an unreachable
// instance 70. link delete is destructive, so each delete first takes
// kit's confirmation token and then runs with it.
func TestE2ELinkExitCodes(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	db := setupTestDB(t, dpkmstest.WithStaticTokens())
	seedTwoObjects(t, db)

	steps := []struct {
		name string
		role string
		args []string
		want int
		// says is text the output must hold.
		says string
	}{
		{"writer creates", dpkmstest.RoleWriter, []string{"link", "create", "obj_a", "obj_b", "--type", "extends"}, 0, "Linked"},
		{"writer delete refused", dpkmstest.RoleWriter, []string{"link", "delete", "obj_a", "obj_b"}, 5, "delete:edges"},
		{"reader lists", dpkmstest.RoleReader, []string{"link", "list", "obj_a", "--follow", "3"}, 0, "[extends] obj_b"},
		{"no token", "none", []string{"link", "list", "obj_a"}, 5, "UNAUTHORIZED"},
		{"missing object", dpkmstest.RoleWriter, []string{"link", "create", "obj_a", "obj_missing", "--type", "extends"}, 3, "obj_missing"},
		{"unknown type", dpkmstest.RoleWriter, []string{"link", "create", "obj_a", "obj_b", "--type", "imaginary"}, 2, "imaginary"},
		{"unreachable", dpkmstest.RoleAdmin, []string{"link", "delete", "obj_a", "obj_b", "--server", testguard.ClosedServerURL}, 70, ""},
		{"admin deletes", dpkmstest.RoleAdmin, []string{"link", "delete", "obj_a", "obj_b"}, 0, "Removed 2 link(s)"},
	}
	for _, s := range steps {
		db.useRole(t, s.role)
		args := s.args
		if args[1] == "delete" {
			// The gate refuses before any request; its token lets the
			// same command through.
			code, out := runBuiltCtxt(t, args...)
			tok := tokenFrom(out)
			if code != 5 || tok == "" {
				t.Fatalf("%s: unconfirmed delete: exit %d, no token\n%s", s.name, code, out)
			}
			args = append(slices.Clone(args), "--confirm-token="+tok)
		}
		got, out := runBuiltCtxt(t, args...)
		if got != s.want || !strings.Contains(out, s.says) {
			t.Fatalf("%s: ctxt %v: exit %d, want %d with %q\n%s", s.name, args, got, s.want, s.says, out)
		}
		if s.name == "writer delete refused" {
			if edges := storedLinkEdges(t, db); len(edges) != 2 {
				t.Fatalf("refused delete changed the edges: %v", edges)
			}
		}
	}
	if edges := storedLinkEdges(t, db); len(edges) != 0 {
		t.Errorf("admin delete left %v", edges)
	}
}
