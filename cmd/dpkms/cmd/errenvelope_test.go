package cmd

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/spf13/cobra"
	"hop.top/kit/go/console/output"
)

// The contract under test: a dpkms failure names its class, so a caller
// reading the exit status can tell "wait and retry" from "this will
// never exist" from "fix the request". Before this wiring every failure
// was GENERIC / exit 1 and the three were indistinguishable.

func TestClassifyErr(t *testing.T) {
	cases := []struct {
		err  error
		name string
		want string
		ok   bool
	}{
		// A refused connection is the daemon being down, not the thing
		// being absent. TRANSIENT so a retry loop retries.
		{
			name: "connection refused is transient",
			err:  errors.New(`Get "http://localhost:8080/healthz": dial tcp [::1]:8080: connect: connection refused`),
			want: output.CodeTransient, ok: true,
		},
		{
			name: "no running instance is transient",
			err:  errors.New("no running dpkms instance named foo"),
			want: output.CodeTransient, ok: true,
		},
		// The storage sentinel, not the message text, is what proves a
		// row is missing. This is the case that must not depend on the
		// driver's wording.
		{
			name: "storage sentinel is not found",
			err:  fmt.Errorf("get job: %w", storage.ErrNotFound),
			want: output.CodeNotFound, ok: true,
		},
		{
			name: "policy denied is conflict",
			err:  fmt.Errorf("refused: %w", ErrPolicyDenied),
			want: output.CodeConflict, ok: true,
		},
		{
			name: "unknown flag is usage",
			err:  errors.New("unknown flag: --bogus-flag"),
			want: output.CodeUsage, ok: true,
		},
		// An unrecognized failure stays unclassified rather than being
		// guessed at: a wrong class is worse than an honest catch-all,
		// because a caller acts on it.
		{
			name: "unrecognized stays unclassified",
			err:  errors.New("disk on fire"),
			want: "", ok: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := classifyErr(tc.err)
			if ok != tc.ok || got != tc.want {
				t.Fatalf("classifyErr(%v) = (%q, %v), want (%q, %v)",
					tc.err, got, ok, tc.want, tc.ok)
			}
		})
	}
}

// A missing job must reach the operator as a NOT_FOUND envelope that
// echoes the id and names a recovery — and must never carry the
// database driver's own words. "sql: no rows in result set" tells a
// caller nothing it can act on and couples the user-facing contract to
// the sql package.
func TestClassifyReturn_NotFoundHidesDriverInternals(t *testing.T) {
	parent := &cobra.Command{Use: "job"}
	leaf := &cobra.Command{Use: "status"}
	parent.AddCommand(leaf)

	// The shape the sqlite driver actually returns: storage.ErrNotFound
	// joined with sql.ErrNoRows, wrapped by the command body.
	driverErr := fmt.Errorf("get job: %w",
		fmt.Errorf("job %w", errors.Join(storage.ErrNotFound, sql.ErrNoRows)))

	err := classifyReturn(leaf, []string{"no-such-job-0000"}, driverErr)

	var env *output.Error
	if !errors.As(err, &env) {
		t.Fatalf("classifyReturn returned %T, want a kit envelope", err)
	}
	if env.Code != output.CodeNotFound {
		t.Errorf("Code = %q, want %q", env.Code, output.CodeNotFound)
	}
	if env.ExitCode != output.ExitNotFound {
		t.Errorf("ExitCode = %d, want %d", env.ExitCode, output.ExitNotFound)
	}
	if strings.Contains(env.Message, "sql:") {
		t.Errorf("Message leaks driver internals: %q", env.Message)
	}
	if !strings.Contains(env.Message, "no-such-job-0000") {
		t.Errorf("Message does not echo the failing id: %q", env.Message)
	}
	if !strings.Contains(env.SuggestedFix, "dpkms job list") {
		t.Errorf("SuggestedFix does not name a recovery: %q", env.SuggestedFix)
	}
	// The original chain survives, so callers matching the sentinel
	// keep matching it.
	if !errors.Is(err, storage.ErrNotFound) {
		t.Error("envelope dropped storage.ErrNotFound from the chain")
	}
}

// An unreachable daemon must name the command that starts one. That is
// what turns "refused" into a next step rather than a dead end.
func TestClassifyReturn_TransientNamesRecovery(t *testing.T) {
	cmd := &cobra.Command{Use: "healthcheck"}
	err := classifyReturn(cmd, nil,
		errors.New(`healthcheck http://localhost:8080: dial tcp: connect: connection refused`))

	var env *output.Error
	if !errors.As(err, &env) {
		t.Fatalf("classifyReturn returned %T, want a kit envelope", err)
	}
	if env.ExitCode != output.ExitTransient {
		t.Errorf("ExitCode = %d, want %d", env.ExitCode, output.ExitTransient)
	}
	if !strings.Contains(env.SuggestedFix, "dpkms serve") {
		t.Errorf("SuggestedFix does not name `dpkms serve`: %q", env.SuggestedFix)
	}
}

// An error that already names its own class is more specific than
// anything a message match can infer, so it passes through untouched.
func TestClassifyReturn_PreservesExistingEnvelope(t *testing.T) {
	cmd := &cobra.Command{Use: "status"}
	orig := output.ConflictError("already running")

	// Identity, not errors.Is: the point is that the SAME envelope comes
	// back untouched. errors.Is would also hold for a rewrapped copy,
	// which is exactly the behavior this test exists to rule out.
	got := classifyReturn(cmd, nil, orig)
	var env *output.Error
	if !errors.As(got, &env) || env != orig {
		t.Errorf("classifyReturn rewrapped an existing envelope: %v", got)
	}
}

// ExitCodeFor is the single table mapping a failure to a process exit
// code. Previously it had one entry (policy denied) and everything else
// was 1.
func TestExitCodeFor(t *testing.T) {
	cases := []struct {
		err  error
		name string
		want int
	}{
		{name: "nil is ok", err: nil, want: output.ExitOK},
		{name: "envelope wins", err: output.NotFoundError("gone"), want: output.ExitNotFound},
		{name: "policy denied is conflict", err: fmt.Errorf("x: %w", ErrPolicyDenied), want: output.ExitConflict},
		{name: "refused is transient", err: errors.New("connect: connection refused"), want: output.ExitTransient},
		{name: "storage sentinel is not found", err: fmt.Errorf("q: %w", storage.ErrNotFound), want: output.ExitNotFound},
		{name: "unknown flag is usage", err: errors.New("unknown flag: --nope"), want: output.ExitUsage},
		// Unclassifiable still reads as a failure, just not a lying one.
		{name: "unclassified falls back to generic", err: errors.New("disk on fire"), want: output.ExitGeneric},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ExitCodeFor(tc.err); got != tc.want {
				t.Errorf("ExitCodeFor(%v) = %d, want %d", tc.err, got, tc.want)
			}
		})
	}
}

// flattenErr must see through errors.Join, because the storage drivers
// join their sentinels. A walker that followed only Unwrap() error
// would stop at the join and miss everything beneath it.
func TestFlattenErr_WalksJoinedErrors(t *testing.T) {
	leafA := errors.New("a")
	leafB := errors.New("b")
	err := fmt.Errorf("outer: %w", errors.Join(leafA, leafB))

	var sawA, sawB bool
	for _, e := range flattenErr(err) {
		if errors.Is(e, leafA) {
			sawA = true
		}
		if errors.Is(e, leafB) {
			sawB = true
		}
	}
	if !sawA || !sawB {
		t.Errorf("flattenErr missed a joined branch: sawA=%v sawB=%v", sawA, sawB)
	}
}
