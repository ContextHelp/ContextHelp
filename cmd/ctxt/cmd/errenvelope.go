package cmd

import (
	"errors"
	"fmt"
	"io"
	"reflect"

	"charm.land/fang/v2"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"hop.top/kit/go/console/output"
)

// Structured-error rendering (12fcc Factor 4, Corrective Error Model).
//
// A caller that asked for machine-readable output must get a
// machine-readable failure. Before this wiring, `ctxt <anything>
// --format json` answered a failure with zero bytes on stdout and
// fang's styled human ERROR block on stderr — prose for an agent to
// regex, carrying no code, no exit class and no recovery hint.
//
// kit already ships the envelope and the middleware that emits it:
// Root.WrapRunE wraps every leaf RunE so a returned error is
// materialized as an output.Error and written to stderr through
// output.RenderError, which honors --format. ctxt did not call it.
// ctxt replicates kit's Root.Execute rather than delegating (kit calls
// fang.WithVersion, which would intercept ctxt's own --version), and
// the replication copied the validation and shape steps but dropped
// the two error-rendering ones. This file restores them.
//
// Nothing is invented here. The envelope shape, the code vocabulary,
// the exit-code table and the transience classes are all kit's; see
// hop.top/kit/go/console/output/envelope.

// kitCLIPkgPath is the import path of kit's console/cli package, and
// kitRenderedMarkerType the unexported wrapper it tags an error with
// once the RunE middleware has already written that error to stderr.
//
// Matching on the type is a deliberate second choice. kit's own fang
// handler tests the same condition with errors.Is against a sentinel,
// but both the sentinel (errAlreadyRendered) and the marker's Is
// method's target are unexported, and the marker compares targets by
// pointer identity, so an equivalent sentinel constructed here can
// never match. The type identity is the only signal kit leaves
// reachable from outside the package.
//
// The cost of a wrong answer is bounded and visible either way: a
// false negative doubles the stderr of a failed run, a false positive
// silences a failure ctxt never rendered. Both would show up the first
// time a command fails, and TestAlreadyRendered pins both directions
// against an error kit itself rendered.
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
// cobra, ctxt's own Execute — are free to wrap that further before it
// reaches the fang handler.
func alreadyRendered(err error) bool {
	for e := err; e != nil; e = errors.Unwrap(e) {
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

// refLookupError classifies a failed lookup of ref (a "object" or
// "entity" kind) into kit's envelope.
//
// A ref that does not exist is NOT_FOUND (exit 3, permanent) and gets
// the recovery action the caller actually needs: search for it. Every
// other failure — a broken query, an unreadable database — is left
// uncharacterized so the middleware wraps it as GENERIC, because
// claiming NOT_FOUND for a store that failed to answer would tell an
// agent to go re-search when the right move is to stop.
//
// The distinction rests on storage.ErrNotFound, which both the sqlite
// and postgres drivers now wrap, rather than on the message text.
func refLookupError(kind, ref string, err error) error {
	if !errors.Is(err, storage.ErrNotFound) {
		return fmt.Errorf("resolve %s %q: %w", kind, ref, err)
	}
	e := output.NotFoundError(fmt.Sprintf("no %s matching %q", kind, ref))
	e.SuggestedFix = "run `ctxt find " + ref + "` to search for it, " +
		"or `ctxt list` to browse what is stored"
	return e
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
