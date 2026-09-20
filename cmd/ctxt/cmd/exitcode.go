package cmd

import (
	"errors"
	"strings"

	"github.com/spf13/cobra"
	"hop.top/kit/go/console/output"
)

// fallbackExitCode is the code ctxt reports for a failure it cannot
// place in kit's taxonomy, and for a class kit itself does not know.
//
// GENERIC (1) rather than 0 or a fresh number: 1 is the spec's
// catch-all failure slot, so an unclassified error still reads as a
// failure to every caller, and a class that kit later adds surfaces
// as "not yet classified" rather than as a collision with USAGE or
// NOT_FOUND. [output.ExitCodeForClass] deliberately declines to pick
// a fallback (its second result is false for anything it does not
// define) so the choice is made here, once, in the open.
const fallbackExitCode = output.ExitGeneric

// ExitCodeFor maps an error returned from [Execute] to a process exit
// code, using kit's standard class table as the single source of
// truth. nil maps to 0.
//
// Resolution order, most specific first:
//
//  1. An error carrying a kit envelope (*output.Error, or anything
//     wrapping one) already names its class — kit's own seams build
//     these for flag-parse failures, Args-arity failures and the
//     unknown-subcommand refusal. Its ExitCode is authoritative; when
//     the envelope carries only a Code, the class table resolves it.
//  2. A ctxt-owned classification (see classifyErr), which recognizes
//     the not-found and conflict shapes the command tree returns as
//     bare errors.
//  3. fallbackExitCode.
//
// One central mapping rather than per-command exits: the classes are
// a property of the failure, not of which verb produced it, and a
// table each command keeps its own copy of is a table that drifts.
func ExitCodeFor(err error) int {
	if err == nil {
		return output.ExitOK
	}

	if code, ok := exitCodeFromEnvelope(err); ok {
		return code
	}

	if class, ok := classifyErr(err); ok {
		if code, known := output.ExitCodeForClass(class); known {
			return code
		}
	}

	return fallbackExitCode
}

// exitCodeFromEnvelope reads the exit code off a kit error envelope
// anywhere in err's chain. An envelope whose ExitCode is unset (a
// hand-built *output.Error that named only a Code) falls back to the
// class table, so the two spellings agree.
func exitCodeFromEnvelope(err error) (int, bool) {
	var env *output.Error
	if !errors.As(err, &env) || env == nil {
		return 0, false
	}
	if env.ExitCode != 0 {
		return env.ExitCode, true
	}
	if code, ok := output.ExitCodeForClass(env.Code); ok {
		return code, true
	}
	return 0, false
}

// classifyErr places a bare error — one carrying no kit envelope —
// into a kit class, reporting false when it recognizes nothing.
//
// ctxt has no error type of its own to key on: the command tree and
// the sqlite stores return errors built with fmt.Errorf, and the
// not-found sentinels that do exist (errObjectNotFound in
// internal/storage/sqlite, "entity not found" from scanEntity) are
// unexported and unwrapped by the time they reach Execute. Matching
// on the message is therefore the only signal available, and it is
// confined to this one function so that introducing real sentinels
// later is a change here and nowhere else.
//
// Deliberately narrow. A phrase not listed is left unclassified and
// reported as GENERIC rather than guessed at: a wrong class is worse
// than an honest catch-all, because a caller acts on it.
func classifyErr(err error) (string, bool) {
	msg := strings.ToLower(err.Error())

	switch {
	// A declared dependency is unreachable. Checked before not-found
	// so "no running dpkms instance named ..." is not read as a
	// missing object: the object was never looked for.
	case strings.Contains(msg, "no running dpkms instance"),
		strings.Contains(msg, "connection refused"),
		strings.Contains(msg, "no such host"):
		return output.CodePrerequisite, true

	// The named resource does not exist.
	case strings.Contains(msg, "not found"),
		strings.Contains(msg, "no such profile"),
		strings.Contains(msg, "does not exist"):
		return output.CodeNotFound, true

	// The request cannot be satisfied against current state.
	case strings.Contains(msg, "already exists"):
		return output.CodeConflict, true

	// The invocation itself is wrong. These are the shapes a command
	// body rejects after cobra has accepted the arity — an
	// unsupported --format value, an operand that will not parse.
	case strings.Contains(msg, "unsupported format"),
		strings.Contains(msg, "invalid format"),
		strings.Contains(msg, "unknown format"),
		strings.Contains(msg, "unsupported output format"):
		return output.CodeUsage, true
	}

	return "", false
}

// classifiedAnnotation marks a command whose RunE has already been
// wrapped by installErrorClassification, so a second call is a no-op.
const classifiedAnnotation = "ctxt.cli.runE.classified"

// installErrorClassification wraps every runnable command's RunE so a
// bare error it returns is promoted to a kit envelope naming its
// class.
//
// Order matters: this must run BEFORE kit's Root.WrapRunE. Kit's
// middleware wraps anything that does not already implement
// AsCLIError into Code=GENERIC / ExitCode=1, and it does so at the
// outermost layer, so a classifier installed afterwards never sees
// the bare error — it sees kit's GENERIC envelope and has no way to
// tell "genuinely unclassifiable" from "not yet classified". Running
// first means kit's wrapper finds an envelope already present and
// passes it through untouched, which is its documented behavior.
//
// Central rather than per-command: the classes are a property of the
// failure, not of the verb. A command with a genuinely special case
// can still return an [output.Error] itself — this wrapper leaves
// any error already carrying an envelope alone.
func installErrorClassification(cmd *cobra.Command) {
	if cmd == nil {
		return
	}
	if cmd.RunE != nil && (cmd.Annotations == nil || cmd.Annotations[classifiedAnnotation] != "true") {
		inner := cmd.RunE
		cmd.RunE = func(c *cobra.Command, args []string) error {
			return classifyReturn(inner(c, args))
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
// errors.As keep matching whatever sentinel it wrapped.
//
// An error already carrying an envelope is returned unchanged: it has
// named its own class and that naming is more specific than anything
// a message match can infer.
func classifyReturn(err error) error {
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
	return output.WrapError(err, class, code)
}
