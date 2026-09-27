package cmd

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/dpkmstest"
)

// The exit codes of the entity leaves and resolve, on the built binary:
// found is 0, a ref that names nothing NOT_FOUND (3), no token
// UNAUTHORIZED (5), nothing answering PREREQUISITE (70).
func TestE2EEntityAndResolveExitCodes(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	db := setupTestDB(t, dpkmstest.WithStaticTokens())
	seedEntityCommands(t, db)
	detachLocalStore(t, db)

	cases := []struct {
		name string
		role string
		down bool
		args []string
		want int
	}{
		{"entity show", dpkmstest.RoleReader, false, []string{"entity", "show", "ui.cart"}, 0},
		{"entity search", dpkmstest.RoleReader, false, []string{"entity", "search", "basket"}, 0},
		{"resolve alias", dpkmstest.RoleReader, false, []string{"resolve", "@basket"}, 0},
		{"entity show, unknown slug", dpkmstest.RoleReader, false, []string{"entity", "show", "no.such"}, 3},
		{"resolve, unknown mention", dpkmstest.RoleReader, false, []string{"resolve", "@no.such"}, 3},
		{"resolve, unknown object", dpkmstest.RoleReader, false, []string{"resolve", "obj_nosuch"}, 3},
		{"entity list, no token", "none", false, []string{"entity", "list"}, 5},
		{"resolve, no token", "none", false, []string{"resolve", "@basket"}, 5},
		{"resolve, nothing answering", dpkmstest.RoleReader, true, []string{"resolve", "@basket"}, 70},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			target := db
			if tc.down {
				// Its own XDG tree, scoped to this subtest.
				target = setupTestDB(t, dpkmstest.Unreachable())
			}
			target.useRole(t, tc.role)
			args := append([]string{"--config", target.ConfigPath}, tc.args...)
			if got, out := runBuiltCtxt(t, args...); got != tc.want {
				t.Fatalf("ctxt %v: exit %d, want %d\n%s", tc.args, got, tc.want, out)
			}
		})
	}
}
