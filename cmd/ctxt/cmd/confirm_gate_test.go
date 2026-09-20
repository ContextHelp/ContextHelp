// Package cmd: regression guard for kit's confirmation gate.
//
// Declaring kit/side-effect and kit/destructive-token on a leaf does
// nothing on its own. The gate that reads those annotations is
// installed by Root.WrapRunE, which kit calls from its own
// Root.Execute — a method ctxt does not use, because fang.WithVersion
// would intercept ctxt's custom --version. Execute therefore
// replicates kit's pre-flight by hand, and a dropped WrapRunE there
// leaves every destructive command running unchallenged while the
// annotations still read as if it were guarded.
//
// That failure is silent: the tree validates, --help looks right, and
// the only symptom is `ctxt inbox clear` wiping the inbox with no
// prompt. These tests pin both halves.
//
// Deliberately hermetic: no DB, no network, and — importantly — no
// mutation of the package-global rootCmd. WrapRunE is annotation
// guarded and irreversible once applied, so calling it on the shared
// tree installs the gate for every other test in this package and
// breaks the 14 sibling tests that drive destructive leaves directly
// (measured, not assumed). So the wiring is checked statically and
// the behavior is checked on a throwaway tree of this file's own.

package cmd

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	kitcli "hop.top/kit/go/console/cli"
)

// TestPrepareTree_CallsWrapRunE asserts by source inspection that the
// pre-flight Execute runs calls root.WrapRunE.
//
// Source inspection rather than execution is deliberate. Calling
// prepareTree() here would be the stronger assertion, but WrapRunE is
// annotation guarded and irreversible: it would install the gate on
// the package-global rootCmd for the rest of the run and break every
// sibling test that drives a destructive leaf directly (delete, feed
// remove, inbox clear, unlink, schema remove, profile delete — 14 of
// them). A test that breaks the suite it lives in is not a guard.
//
// The behavioral proof lives in TestConfirmGate_RefusesAndAccepts,
// which drives a real gate on a tree of its own. This test pins the
// one thing that test cannot see: that ctxt's own Execute path is
// wired to install it.
func TestPrepareTree_CallsWrapRunE(t *testing.T) {
	src, err := os.ReadFile("root.go")
	if err != nil {
		t.Fatalf("read root.go: %v", err)
	}

	fn := funcBody(string(src), "func prepareTree()")
	if fn == "" {
		t.Fatal("prepareTree not found in root.go; Execute's pre-flight was restructured — " +
			"re-point this guard at whatever now runs before fang.Execute")
	}
	if !strings.Contains(fn, "root.WrapRunE()") {
		t.Error("prepareTree must call root.WrapRunE(): without it every " +
			"kit/side-effect and kit/destructive-token annotation in the tree is " +
			"inert and destructive commands run with no confirmation challenge")
	}
}

// TestDestructiveLeavesDeclareTier is the companion static check:
// every leaf opting into the typed-token flow must also carry a
// destructive side-effect tier. A kit/destructive-token on a read
// command would, now that the gate fires, demand confirmation for a
// command that changes nothing.
func TestDestructiveLeavesDeclareTier(t *testing.T) {
	var checked int
	walkTree(rootCmd, func(c *cobra.Command) {
		if c.RunE == nil || c.Annotations["kit/destructive-token"] == "" {
			return
		}
		checked++
		se := c.Annotations["kit/side-effect"]
		if !strings.HasPrefix(se, "destructive") {
			t.Errorf("%q requires a typed confirmation token but declares "+
				"kit/side-effect=%q; the gate would challenge a non-destructive command",
				c.CommandPath(), se)
		}
	})

	// Guard the guard: if the annotation key were ever renamed, the
	// walk above would silently check nothing and pass.
	if checked == 0 {
		t.Fatal("no kit/destructive-token leaves found; this assertion checked nothing")
	}
	t.Logf("tier verified on %d typed-token destructive leaves", checked)
}

// TestConfirmGate_RefusesAndAccepts drives the gate end to end on an
// isolated tree: a destructive leaf refuses without a token, refuses
// with the wrong token, and runs with the right one.
//
// This is the half that proves the gate blocks rather than merely
// being installed. It uses its own Root and its own command so the
// package-global tree is untouched.
func TestConfirmGate_RefusesAndAccepts(t *testing.T) {
	newTree := func() (*kitcli.Root, *cobra.Command, *bool) {
		ran := false
		leaf := &cobra.Command{
			Use:   "wipe",
			Short: "Delete everything",
			RunE: func(*cobra.Command, []string) error {
				ran = true
				return nil
			},
		}
		kitcli.SetSideEffect(leaf, kitcli.SideEffectDestructive)
		kitcli.SetDestructiveToken(leaf)

		r := kitcli.New(kitcli.Config{Name: "gatetest", Version: "test"})
		r.Cmd.AddCommand(leaf)
		r.WrapRunE()
		return r, leaf, &ran
	}

	run := func(t *testing.T, args ...string) (string, error) {
		t.Helper()
		r, _, ran := newTree()
		var out bytes.Buffer
		r.Cmd.SetOut(&out)
		r.Cmd.SetErr(&out)
		r.Cmd.SetArgs(args)
		err := r.Cmd.Execute()
		if *ran {
			return out.String(), nil
		}
		if err == nil {
			// Body did not run and nothing failed: the gate refused and
			// swallowed its own rendering. Surface that as a refusal.
			return out.String(), errRefused
		}
		return out.String(), err
	}

	t.Run("refuses without a token", func(t *testing.T) {
		out, err := run(t, "wipe")
		if err == nil {
			t.Fatal("destructive leaf ran with no confirmation token")
		}
		if !strings.Contains(out, "confirm-token") {
			t.Errorf("refusal must tell the caller the token to echo back; got:\n%s", out)
		}
	})

	t.Run("refuses a wrong token", func(t *testing.T) {
		out, err := run(t, "wipe", "--confirm-token=not-the-token")
		if err == nil {
			t.Fatal("destructive leaf ran with a mismatched confirmation token")
		}
		if !strings.Contains(out, "mismatch") {
			t.Errorf("refusal must name the mismatch; got:\n%s", out)
		}
	})

	t.Run("refuses --confirm=yes alone", func(t *testing.T) {
		// A typed-token destructive must not be satisfiable by the
		// blanket --confirm=yes that non-token destructives accept.
		if _, err := run(t, "wipe", "--confirm=yes"); err == nil {
			t.Fatal("--confirm=yes alone satisfied a typed-token destructive")
		}
	})

	t.Run("accepts the right token", func(t *testing.T) {
		// Recover the expected token from the refusal text rather than
		// recomputing kit's sha here: asserting against our own copy of
		// the formula would pass even if kit changed it.
		out, refusal := run(t, "wipe")
		if refusal == nil {
			t.Fatal("expected the unconfirmed run to be refused")
		}
		token := tokenFrom(out)
		if token == "" {
			t.Fatalf("could not recover the expected token from:\n%s", out)
		}
		if _, err := run(t, "wipe", "--confirm-token="+token); err != nil {
			t.Fatalf("correct token must let the command run; got: %v", err)
		}
	})
}

// errRefused marks a gate refusal that rendered itself and returned
// nil to cobra.
var errRefused = refusalError{}

type refusalError struct{}

func (refusalError) Error() string { return "refused by the confirmation gate" }

// tokenFrom pulls the sha out of kit's "Re-run with
// --confirm-token=<sha>" line.
func tokenFrom(s string) string {
	const marker = "--confirm-token="
	i := strings.Index(s, marker)
	if i < 0 {
		return ""
	}
	rest := s[i+len(marker):]
	return strings.TrimSpace(strings.FieldsFunc(rest, func(r rune) bool {
		return r == ' ' || r == '\n' || r == '\r' || r == '.' || r == '"'
	})[0])
}

// walkTree visits cmd and every descendant.
func walkTree(cmd *cobra.Command, fn func(*cobra.Command)) {
	fn(cmd)
	for _, c := range cmd.Commands() {
		walkTree(c, fn)
	}
}
