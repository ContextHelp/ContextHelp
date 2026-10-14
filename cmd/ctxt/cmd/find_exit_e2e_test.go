package cmd

import (
	"os"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/dpkmstest"
)

// The exit codes of find and ctxt://search on the built binary, per
// ADR-077: an answer is 0, nothing answering PREREQUISITE (70), no token
// UNAUTHORIZED (5), and a request dpkms refuses (a bad date is 400, a
// semantic leg that cannot run with fallback_to_fts off is 422) USAGE
// (2), as is an --embedding-* flag without --graph.
func TestE2EFindExitCodes(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	db := setupTestDB(t, dpkmstest.WithStaticTokens())
	seedFindCorpus(t, db)
	detachLocalStore(t, db)

	// Each setupTestDB points XDG at its own tree, so the other
	// instances are built inside their subtest.
	strict := func(t *testing.T) *testDB {
		s := setupTestDB(t)
		seedFindCorpus(t, s)
		raw, err := os.ReadFile(s.ConfigPath)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(s.ConfigPath, append(raw, "search:\n  fallback_to_fts: false\n"...), 0o600); err != nil {
			t.Fatal(err)
		}
		return s
	}
	down := func(t *testing.T) *testDB { return setupTestDB(t, dpkmstest.Unreachable()) }

	cases := []struct {
		name   string
		setup  func(*testing.T) *testDB
		role   string
		args   []string
		want   int
		stderr string
	}{
		{"hybrid", nil, dpkmstest.RoleReader, []string{"find", "authentication"}, 0, ""},
		{"fts json", nil, dpkmstest.RoleReader, []string{"find", "authentication", "--fts", "--format", "json"}, 0, ""},
		{"search uri", nil, dpkmstest.RoleReader, []string{"ctxt://search/authentication"}, 0, ""},
		{"no token", nil, "none", []string{"find", "authentication"}, 5, ""},
		{"nothing answering", down, dpkmstest.RoleReader, []string{"find", "authentication"}, 70, ""},
		{"search uri, nothing answering", down, dpkmstest.RoleReader, []string{"ctxt://search/authentication"}, 70, ""},
		{"bad date", nil, dpkmstest.RoleReader, []string{"find", "authentication", "--since", "2026-13-45"}, 2, "filter.since"},
		{"fallback off, no default model", strict, dpkmstest.RoleAdmin, []string{"find", "authentication"}, 2, "SEMANTIC_UNAVAILABLE"},
		{"embedding flag without graph", nil, dpkmstest.RoleReader, []string{"find", "authentication", "--embedding-model", "m"}, 2, "--embedding-model only applies to --graph"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			target := db
			if tc.setup != nil {
				target = tc.setup(t)
			}
			target.useRole(t, tc.role)
			args := append([]string{"--config", target.ConfigPath}, tc.args...)
			got, out := runBuiltCtxt(t, args...)
			if got != tc.want {
				t.Fatalf("ctxt %v: exit %d, want %d\n%s", tc.args, got, tc.want, out)
			}
			if !strings.Contains(out, tc.stderr) {
				t.Errorf("ctxt %v: output lacks %q\n%s", tc.args, tc.stderr, out)
			}
		})
	}
}
