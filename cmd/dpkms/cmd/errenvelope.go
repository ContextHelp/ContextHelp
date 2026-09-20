package cmd

import (
	"errors"
	"io"
	"reflect"
	"strings"

	"charm.land/fang/v2"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/spf13/cobra"
	"hop.top/kit/go/console/output"
)

// Structured-error rendering for dpkms (12fcc Factor 4 + Factor 11).
//
// This mirrors cmd/ctxt/cmd/errenvelope.go and cmd/ctxt/cmd/exitcode.go
// deliberately rather than inventing a second mechanism: the classes,
// the envelope shape and the exit-code table are kit's, and dpkms and
// ctxt ship in the same repo and are read by the same agents. Two
// spellings of the same contract is the drift this file exists to
// prevent.
//
// What dpkms lacked: every failure collapsed to GENERIC / exit 1. An
// unreachable daemon, a job id that never existed, and a mistyped flag
// were indistinguishable to a caller reading the exit status — which is
// exactly the distinction a retry loop needs. "Wait and retry" (the
// daemon is down), "never retry" (the row does not exist) and "repair
// the request" (the flag is wrong) all read as the same number.

// kitCLIPkgPath is the import path of kit's console/cli package, and
// kitRenderedMarkerType the unexported wrapper it tags an error with
// once the RunE middleware has already written that error to stderr.
//
// Matching on the type is a second choice forced by kit's surface:
// both the sentinel (errAlreadyRendered) and the marker's Is target are
// unexported, and the marker compares targets by pointer identity, so
// an equivalent sentinel constructed here can never match. The type
// identity is the only signal kit leaves reachable from outside.
const (
	kitCLIPkgPath         = "hop.top/kit/go/console/cli"
	kitRenderedMarkerType = "renderedMarker"
)

// alreadyRendered reports whether kit's RunE middleware (or its
// usage-classification seam) has already written err to stderr as an
// envelope.
//
// Walks the unwrap chain rather than testing only the outermost error:
// the middleware marks the error it returns, and the layers above it —
// cobra, dpkms's own Execute — are free to wrap that further before it
// reaches main.
func alreadyRendered(err error) bool {
	for _, e := range flattenErr(err) {
		t := reflect.TypeOf(e)
		if t == nil {
			continue
		}
		if t.Kind() == reflect.Ptr {
			t = t.Elem()
		}
		if t.PkgPath() == kitCLIPkgPath && t.Name() == kitRenderedMarkerType {
			return true
		}
	}
	return false
}

// flattenErr walks err's full tree — both the single-Unwrap chain and
// the multi-Unwrap fan-out that errors.Join produces.
//
// The storage drivers join storage.ErrNotFound with sql.ErrNoRows so a
// missing job matches both the cross-backend sentinel the CLI keys on
// and the queue-empty check AcquireNext makes. A walker that followed
// only Unwrap() error would stop at that join and miss everything
// under it.
func flattenErr(err error) []error {
	var out []error
	var walk func(error)
	walk = func(e error) {
		for e != nil {
			out = append(out, e)
			switch u := e.(type) { //nolint:errorlint // intentional type switch on the unwrap shape
			case interface{ Unwrap() []error }:
				for _, sub := range u.Unwrap() {
					walk(sub)
				}
				return
			case interface{ Unwrap() error }:
				e = u.Unwrap()
			default:
				return
			}
		}
	}
	walk(err)
	return out
}

// ErrorAlreadyRendered reports whether err has already been written to
// stderr as a structured envelope, so main() can skip its own
// "Error: %v" line rather than printing a second, prose rendering of
// the same failure underneath the envelope.
//
// Without this, `--format json` emits a valid JSON document followed by
// a plaintext line, and the pair parses as neither — the exact defect
// the envelope exists to fix, moved one layer out.
func ErrorAlreadyRendered(err error) bool { return alreadyRendered(err) }

// classifyErr places a bare error — one carrying no kit envelope —
// into a kit class, reporting false when it recognizes nothing.
//
// Sentinels are consulted before message text, and the text matches
// that remain are narrow on purpose: a phrase not listed is left
// unclassified and reported as GENERIC rather than guessed at, because
// a wrong class is worse than an honest catch-all — a caller acts on it.
func classifyErr(err error) (string, bool) {
	// Sentinel matches first. These are structural facts about the
	// failure, not inferences from prose, so they outrank any text
	// match that might also fire.
	switch {
	case errors.Is(err, ErrPolicyDenied):
		return output.CodeConflict, true
	case errors.Is(err, storage.ErrNotFound):
		return output.CodeNotFound, true
	}

	msg := strings.ToLower(err.Error())

	switch {
	// The daemon is unreachable. TRANSIENT, not NOT_FOUND: the thing
	// asked for may well exist, the process answering for it is down,
	// and a retry after `dpkms serve` is the correct next move.
	// Checked before not-found so "no running dpkms instance" is not
	// read as a missing object — the object was never looked for.
	case strings.Contains(msg, "connection refused"),
		strings.Contains(msg, "no running dpkms instance"),
		strings.Contains(msg, "no such host"),
		strings.Contains(msg, "connection reset"),
		strings.Contains(msg, "i/o timeout"),
		strings.Contains(msg, "context deadline exceeded"),
		strings.Contains(msg, "server misbehaving"):
		return output.CodeTransient, true

	// The named resource does not exist.
	case strings.Contains(msg, "not found"),
		strings.Contains(msg, "does not exist"),
		strings.Contains(msg, "unknown pipeline"),
		strings.Contains(msg, "unknown detector"):
		return output.CodeNotFound, true

	// The request cannot be satisfied against current state.
	case strings.Contains(msg, "already exists"),
		strings.Contains(msg, "already running"):
		return output.CodeConflict, true

	// The invocation itself is wrong. These are the shapes a command
	// body rejects after cobra has accepted the arity — an unsupported
	// --format value, an operand that will not parse.
	case strings.Contains(msg, "unsupported format"),
		strings.Contains(msg, "invalid format"),
		strings.Contains(msg, "unknown format"),
		strings.Contains(msg, "unsupported output format"),
		strings.Contains(msg, "unknown flag"),
		strings.Contains(msg, "unknown command"),
		strings.Contains(msg, "invalid argument"),
		strings.Contains(msg, "accepts ") && strings.Contains(msg, "arg"):
		return output.CodeUsage, true
	}

	return "", false
}

// classifiedAnnotation marks a command whose RunE has already been
// wrapped by installErrorClassification, so a second call is a no-op.
const classifiedAnnotation = "dpkms.cli.runE.classified"

// installErrorClassification wraps every runnable command's RunE so a
// bare error it returns is promoted to a kit envelope naming its class.
//
// Order matters: this must run BEFORE kit's Root.WrapRunE. Kit's
// middleware wraps anything that does not already implement AsCLIError
// into Code=GENERIC / ExitCode=1, and it does so at the outermost
// layer, so a classifier installed afterwards never sees the bare error
// — it sees kit's GENERIC envelope and has no way to tell "genuinely
// unclassifiable" from "not yet classified". Running first means kit's
// wrapper finds an envelope already present and passes it through
// untouched, which is its documented behavior.
//
// Central rather than per-command: the classes are a property of the
// failure, not of the verb. A command with a genuinely special case can
// still return an [output.Error] itself — this wrapper leaves any error
// already carrying an envelope alone.
func installErrorClassification(cmd *cobra.Command) {
	if cmd == nil {
		return
	}
	if cmd.RunE != nil && (cmd.Annotations == nil || cmd.Annotations[classifiedAnnotation] != "true") {
		inner := cmd.RunE
		cmd.RunE = func(c *cobra.Command, args []string) error {
			return classifyReturn(c, args, inner(c, args))
		}
		if cmd.Annotations == nil {
			cmd.Annotations = make(map[string]string)
		}
		cmd.Annotations[classifiedAnnotation] = "true"
	}
	for _, child := range cmd.Commands() {
		installErrorClassification(child)
	}
}

// classifyReturn promotes a bare error to a kit envelope of the
// matching class, preserving err in the chain so errors.Is and
// errors.As keep matching whatever sentinel it wrapped, and attaches
// the recovery the caller can actually act on.
//
// An error already carrying an envelope is returned unchanged: it has
// named its own class, and that naming is more specific than anything a
// message match can infer.
func classifyReturn(cmd *cobra.Command, args []string, err error) error {
	if err == nil {
		return nil
	}
	var env *output.Error
	if errors.As(err, &env) {
		return err
	}
	class, ok := classifyErr(err)
	if !ok {
		return err
	}
	code, known := output.ExitCodeForClass(class)
	if !known {
		return err
	}

	wrapped := output.WrapError(err, class, code)
	// A NOT_FOUND message must not carry the driver's own words. The
	// storage sentinel is what told us the row is missing; the text
	// underneath it ("sql: no rows in result set") is an implementation
	// detail of the database library and tells an operator nothing it
	// can act on.
	if class == output.CodeNotFound {
		wrapped.Message = notFoundMessage(cmd, args)
	}
	if fix := suggestedFix(cmd, args, class); fix != "" {
		wrapped.SuggestedFix = fix
	}
	return wrapped
}

// notFoundMessage states the missing thing in the caller's own terms:
// the noun the command operates on and the id the caller supplied.
//
// Echoing the id matters for a batch caller, which would otherwise have
// to correlate a failure back to its input by position.
func notFoundMessage(cmd *cobra.Command, args []string) string {
	noun := resourceNoun(cmd)
	if len(args) > 0 && args[0] != "" {
		return "no " + noun + " with id " + strconv0(args[0])
	}
	return "no such " + noun
}

// strconv0 quotes an id so an empty-looking or space-bearing value is
// still visible in the rendered message.
func strconv0(s string) string { return `"` + s + `"` }

// resourceNoun names what a command addresses, for use in a NOT_FOUND
// message and its recovery hint. Falls back to the parent command's
// name, which is the noun for every `dpkms <noun> <verb>` pair in the
// tree (job, pipeline, detector, secret, key).
func resourceNoun(cmd *cobra.Command) string {
	if cmd == nil {
		return "resource"
	}
	if p := cmd.Parent(); p != nil && p.Name() != "" && p.Name() != "dpkms" {
		return p.Name()
	}
	return cmd.Name()
}

// suggestedFix names the command that turns a failure into a next step.
//
// A recovery is offered only where one is genuinely obvious. An
// invented hint is worse than none: it sends a caller — especially an
// agent, which will run it — down a path that does not lead anywhere.
func suggestedFix(cmd *cobra.Command, _ []string, class string) string {
	switch class {
	case output.CodeTransient:
		// The daemon is not answering. Starting it is the recovery,
		// and `dpkms ps` is how a caller confirms whether one is
		// already running before it starts a second.
		return "start the daemon with `dpkms serve`, " +
			"or check for a running instance with `dpkms ps`, then retry"
	case output.CodeNotFound:
		noun := resourceNoun(cmd)
		return "run `dpkms " + noun + " list` to see what exists"
	case output.CodeUsage:
		return "run `" + cmd.CommandPath() + " --help` for accepted flags and arguments"
	}
	return ""
}

// envelopeErrorHandler is the single stderr writer for a failed run.
//
// fang calls its error handler whenever root.ExecuteContext returns
// non-nil, and cobra's SilenceErrors does not reach that call. So
// without this handler kit's middleware and fang would each render the
// same failure: the envelope, then fang's styled block underneath it.
// Under --format json that is a JSON document followed by prose, which
// parses as neither.
//
// Errors kit already rendered are therefore dropped here. Everything
// else — a failure raised outside the RunE chain, such as boot-time
// validation or a -c parse error — keeps fang's styling, which is the
// best rendering available for it.
//
// styles is passed by value because that is fang.ErrorHandler's
// signature, not a choice: a pointer would not satisfy the type.
//
//nolint:gocritic // hugeParam is unavoidable here; see above.
func envelopeErrorHandler(w io.Writer, styles fang.Styles, err error) {
	if err == nil || alreadyRendered(err) {
		return
	}
	fang.DefaultErrorHandler(w, styles, err)
}
