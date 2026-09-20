package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/spf13/cobra"
	kitcli "hop.top/kit/go/console/cli"
	"hop.top/kit/go/console/output"
)

// TestRefLookupError_NotFoundIsStructured pins the corrective half of
// Factor 4: a ref that does not exist must come back as a NOT_FOUND
// envelope carrying the exit code and the recovery action, not as a
// bare wrapped error that renders as GENERIC.
func TestRefLookupError_NotFoundIsStructured(t *testing.T) {
	err := refLookupError("object", "obj_deadbeef", fmt.Errorf("object %w", storage.ErrNotFound))

	var env *output.Error
	if !errors.As(err, &env) {
		t.Fatalf("refLookupError returned %T, want an *output.Error envelope", err)
	}
	if env.Code != output.CodeNotFound {
		t.Errorf("Code = %q, want %q", env.Code, output.CodeNotFound)
	}
	if env.ExitCode != 3 {
		t.Errorf("ExitCode = %d, want 3", env.ExitCode)
	}
	if env.Transience != output.TransiencePermanent {
		t.Errorf("Transience = %q, want %q", env.Transience, output.TransiencePermanent)
	}
	if !strings.Contains(env.Message, "obj_deadbeef") {
		t.Errorf("Message = %q, want it to name the ref", env.Message)
	}
	// The corrective field is the point of the factor: an agent must be
	// told what to do next without parsing prose.
	if !strings.Contains(env.SuggestedFix, "ctxt find") {
		t.Errorf("SuggestedFix = %q, want it to name a recovery command", env.SuggestedFix)
	}
}

// TestRefLookupError_RealFailureStaysUncharacterized guards the other
// direction. Claiming NOT_FOUND for a store that failed to answer would
// tell an agent to go re-search when the right move is to stop, so a
// non-sentinel error must NOT be converted into a NOT_FOUND envelope.
func TestRefLookupError_RealFailureStaysUncharacterized(t *testing.T) {
	boom := errors.New("database is locked")
	err := refLookupError("object", "obj_deadbeef", boom)

	var env *output.Error
	if errors.As(err, &env) && env.Code == output.CodeNotFound {
		t.Fatalf("a storage failure was misclassified as %s", env.Code)
	}
	if !errors.Is(err, boom) {
		t.Errorf("underlying error was not preserved for errors.Is")
	}
}

// TestRefLookupError_RendersAsJSON is the end-to-end shape assertion:
// what reaches stderr under --format json must be one parseable
// document with the fields a caller branches on.
func TestRefLookupError_RendersAsJSON(t *testing.T) {
	err := refLookupError("entity", "@nosuch", fmt.Errorf("entity %w", storage.ErrNotFound))

	var env *output.Error
	if !errors.As(err, &env) {
		t.Fatalf("refLookupError returned %T, want an *output.Error envelope", err)
	}

	var buf strings.Builder
	if rerr := output.RenderError(&buf, "json", env); rerr != nil {
		t.Fatalf("RenderError: %v", rerr)
	}

	var got map[string]any
	if uerr := json.Unmarshal([]byte(buf.String()), &got); uerr != nil {
		t.Fatalf("stderr is not parseable JSON: %v\ngot: %s", uerr, buf.String())
	}
	for _, key := range []string{"code", "message", "exit_code", "transience", "suggested_fix"} {
		if _, ok := got[key]; !ok {
			t.Errorf("envelope is missing %q; got %v", key, got)
		}
	}
	if got["code"] != output.CodeNotFound {
		t.Errorf("code = %v, want %q", got["code"], output.CodeNotFound)
	}
}

// TestExecuteWrapsLeavesForEnvelope pins the central wiring itself.
//
// Everything else in this file tests pieces that only matter once
// kit's RunE middleware is actually installed on ctxt's tree. Execute
// is where that happens, and it is one call: drop root.WrapRunE() and
// every leaf silently reverts to prose on stderr under --format json,
// with no unit test in the repo noticing. This asserts the annotation
// kit stamps on each wrapped leaf.
func TestExecuteWrapsLeavesForEnvelope(t *testing.T) {
	const wrappedAnnotation = "kit.cli.runE.wrapped"

	// Read prepareTree's source rather than running it.
	//
	// Running it is what the assertion wants and what it must not do:
	// WrapRunE mutates the package-level rootCmd every other test in
	// this package shares, and it is annotation-guarded, so there is no
	// way to undo it afterwards. Installing kit's middleware also
	// installs kit's confirmation gate, which turns every destructive
	// command in the suite into an UNAUTHORIZED failure — ten of them,
	// caused purely by this test having run first. A test that breaks
	// its neighbors to check a wiring step is a worse trade than
	// checking the wiring statically.
	//
	// So this asserts the call is present in the function Execute
	// delegates its pre-flight to, which is the property that was
	// actually missing. The end-to-end behavior it stands for is
	// covered by the rest of this file against a locally built tree.
	src, err := os.ReadFile("root.go")
	if err != nil {
		t.Fatalf("read root.go: %v", err)
	}
	fn := funcBody(string(src), "func prepareTree() error {")
	if fn == "" {
		t.Fatal("prepareTree not found in root.go; Execute's pre-flight may have been restructured")
	}
	if !strings.Contains(fn, "root.WrapRunE()") {
		t.Error("prepareTree does not call root.WrapRunE(); every leaf reverts to " +
			"prose on stderr and --format json callers stop getting an envelope")
	}

	// And the middleware it installs really does stamp the annotation
	// this test names, so the check above cannot pass against a kit
	// that stopped wrapping.
	probe := kitcli.New(kitcli.Config{Name: "probe", Version: "0.0.1", Short: "probe"})
	leaf := &cobra.Command{
		Use:   "boom",
		Short: "boom",
		RunE:  func(*cobra.Command, []string) error { return nil },
	}
	kitcli.SetSideEffect(leaf, kitcli.SideEffectRead)
	probe.Cmd.AddCommand(leaf)
	probe.WrapRunE()
	if leaf.Annotations[wrappedAnnotation] != "true" {
		t.Errorf("kit's WrapRunE no longer stamps %q; this test's premise is stale",
			wrappedAnnotation)
	}
}

// funcBody returns the source of the function introduced by header, up
// to its closing brace at column 0. Good enough for a gofmt'ed file,
// which is enforced in CI.
func funcBody(src, header string) string {
	i := strings.Index(src, header)
	if i < 0 {
		return ""
	}
	rest := src[i:]
	if end := strings.Index(rest, "\n}\n"); end >= 0 {
		return rest[:end]
	}
	return rest
}

// TestAlreadyRendered pins the duplicate-suppression contract. The
// predicate decides whether main() prints a second, prose rendering
// underneath the envelope; under --format json the two together parse
// as neither, so a false negative reintroduces the original defect.
func TestAlreadyRendered(t *testing.T) {
	if alreadyRendered(nil) {
		t.Error("alreadyRendered(nil) = true, want false")
	}
	if alreadyRendered(errors.New("plain")) {
		t.Error("a plain error was reported as already rendered")
	}
	if alreadyRendered(output.NotFoundError("nope")) {
		t.Error("an unrendered envelope was reported as already rendered")
	}

	// The positive case, driven through kit rather than asserted
	// against a hand-built value: the marker type is unexported, so
	// the only honest way to produce one is to let kit's middleware
	// render a failure. A predicate that never answers true would
	// leave every envelope with a prose line stapled underneath it,
	// and the three checks above cannot catch that.
	envRoot := kitcli.New(kitcli.Config{Name: "envtest", Version: "0.0.1", Short: "envtest"})
	leaf := &cobra.Command{
		Use:   "boom",
		Short: "boom",
		RunE:  func(*cobra.Command, []string) error { return output.NotFoundError("nope") },
	}
	kitcli.SetSideEffect(leaf, kitcli.SideEffectRead)
	envRoot.Cmd.AddCommand(leaf)
	envRoot.WrapRunE()

	var stdout, stderr bytes.Buffer
	envRoot.Cmd.SetOut(&stdout)
	envRoot.Cmd.SetErr(&stderr)
	envRoot.Cmd.SetArgs([]string{"boom", "--format", "json"})
	execErr := envRoot.Cmd.ExecuteContext(context.Background())

	if execErr == nil {
		t.Fatal("expected the leaf failure to propagate")
	}
	if !alreadyRendered(execErr) {
		t.Errorf("a kit-rendered error was not recognized; got %T", execErr)
	}
	// And it really was rendered, as one parseable document.
	var got map[string]any
	if uerr := json.Unmarshal(stderr.Bytes(), &got); uerr != nil {
		t.Fatalf("kit stderr is not parseable JSON: %v\ngot: %s", uerr, stderr.String())
	}
	if got["code"] != output.CodeNotFound {
		t.Errorf("code = %v, want %q", got["code"], output.CodeNotFound)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty (errors belong on stderr)", stdout.String())
	}
}
