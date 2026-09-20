package cmd

import (
	"errors"
	"fmt"
	"testing"

	"hop.top/kit/go/console/output"
)

// TestExitCodeFor_Classes pins the numeric code each failure class
// produces. The numbers are kit's, read from the class table rather
// than retyped, so a table that moves upstream fails here rather than
// silently changing what callers observe.
func TestExitCodeFor_Classes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		err  error
		name string
		want int
	}{
		{name: "nil is success", err: nil, want: 0},
		{name: "unclassified falls back to generic", err: errors.New("something broke"), want: 1},

		// Envelopes kit's own seams build.
		{name: "usage envelope", err: output.UsageError("bad flag"), want: 2},
		{name: "not found envelope", err: output.NotFoundError("missing"), want: 3},
		{name: "conflict envelope", err: output.ConflictError("taken"), want: 4},

		// A wrapped envelope is still found: main.go sees whatever
		// the middleware chain returned, not the envelope directly.
		{name: "wrapped envelope", err: fmt.Errorf("context: %w", output.NotFoundError("missing")), want: 3},

		// Bare errors the command tree returns, classified by shape.
		{name: "bare not found", err: errors.New(`resolve object "obj_x": object not found`), want: 3},
		{name: "bare entity not found", err: errors.New(`resolve entity "@x": entity not found`), want: 3},
		{name: "bare profile not found", err: errors.New("profile not found: nope"), want: 3},
		{name: "bare conflict", err: errors.New("profile already exists: demo"), want: 4},
		{name: "bare unsupported format", err: errors.New(`unsupported format "yaml" for resolve`), want: 2},
		{name: "bare prerequisite", err: errors.New(`no running dpkms instance named "db"`), want: 70},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ExitCodeFor(tc.err); got != tc.want {
				t.Fatalf("ExitCodeFor(%v) = %d, want %d", tc.err, got, tc.want)
			}
		})
	}
}

// TestExitCodeFor_PrerequisiteBeatsNotFound guards the ORDER of the
// arms in classifyErr, not merely that each arm works.
//
// The two patterns can both match one message: a routing failure that
// mentions the missing daemon is easily phrased so that "not found"
// appears in it too. Whichever arm is tested first wins, and getting
// it wrong is not cosmetic — reading an unreachable daemon as
// NOT_FOUND tells a caller the ref does not exist when nothing was
// ever looked up, so it stops retrying a failure that starting the
// daemon would fix.
//
// The message below deliberately contains BOTH substrings, so the
// test fails if the arms are ever reordered. A message matching only
// one arm would pass under either order and assert nothing.
func TestExitCodeFor_PrerequisiteBeatsNotFound(t *testing.T) {
	t.Parallel()

	err := errors.New(`no running dpkms instance named "db": pidfile not found`)
	if got, want := ExitCodeFor(err), output.ExitPrerequisite; got != want {
		t.Fatalf("ExitCodeFor(%v) = %d, want %d (PREREQUISITE must be tested before NOT_FOUND)", err, got, want)
	}
}

// TestExitCodeFor_EnvelopeBeatsMessage guards the precedence between
// a named class and a message match. A command that deliberately
// returns GENERIC for a message reading "not found" must keep
// GENERIC: the envelope is the command's own statement about the
// failure, and it is more specific than anything inferred.
func TestExitCodeFor_EnvelopeBeatsMessage(t *testing.T) {
	t.Parallel()

	err := output.GenericError("widget not found, but generically so")
	if got := ExitCodeFor(err); got != output.ExitGeneric {
		t.Fatalf("ExitCodeFor(envelope) = %d, want %d", got, output.ExitGeneric)
	}
}

// TestClassifyReturn_PreservesChain pins that promoting a bare error
// to an envelope keeps the original in the chain, so errors.Is still
// matches whatever sentinel a caller wrapped.
func TestClassifyReturn_PreservesChain(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("object not found")
	wrapped := classifyReturn(fmt.Errorf("resolve object: %w", sentinel))

	if !errors.Is(wrapped, sentinel) {
		t.Fatal("classifyReturn dropped the original error from the chain")
	}
	if got := ExitCodeFor(wrapped); got != output.ExitNotFound {
		t.Fatalf("ExitCodeFor = %d, want %d", got, output.ExitNotFound)
	}
}

// TestClassifyReturn_LeavesUnrecognizedAlone pins that an error
// classifyErr does not recognize is returned unchanged rather than
// wrapped in a GENERIC envelope. Wrapping here would rob kit's own
// middleware of the bare error it expects, and would make "not yet
// classified" indistinguishable from "deliberately generic".
func TestClassifyReturn_LeavesUnrecognizedAlone(t *testing.T) {
	t.Parallel()

	orig := errors.New("disk on fire")
	if got := classifyReturn(orig); got != orig { //nolint:errorlint // identity is the assertion
		t.Fatalf("classifyReturn rewrote an unrecognized error: %v", got)
	}
}
