package postgres

import (
	"errors"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/search"
)

// TestFTSRegconfigMatchesCompiler pins the driver's tsvector regconfig to the
// one the search compiler binds into similar== tsqueries. The two are
// declared in separate packages (the driver deliberately does not import the
// compiler); drifting configurations would make the compiled predicate and
// the generated column disagree on what matches.
func TestFTSRegconfigMatchesCompiler(t *testing.T) {
	if ftsRegconfig != search.PostgresFTSRegconfig {
		t.Errorf("driver regconfig %q != compiler regconfig %q",
			ftsRegconfig, search.PostgresFTSRegconfig)
	}
}

// TestPgvectorUnavailableError pins the operator-facing degradation of a
// CREATE EXTENSION failure: one clear, actionable error instead of a raw
// "migration 1" failure. This is the first error a self-hosting operator
// sees on managed Postgres where extension enablement needs console
// privileges.
func TestPgvectorUnavailableError(t *testing.T) {
	cause := errors.New(`pq: could not open extension control file "/usr/share/postgresql/16/extension/vector.control": No such file or directory`)
	err := pgvectorUnavailable(cause)
	if err == nil {
		t.Fatal("pgvectorUnavailable returned nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "pgvector extension unavailable") {
		t.Errorf("error should name the missing extension clearly, got: %q", msg)
	}
	if !strings.Contains(msg, "enable it") {
		t.Errorf("error should tell the operator what to do, got: %q", msg)
	}
	if !errors.Is(err, cause) {
		t.Error("original driver error must remain unwrappable via errors.Is")
	}
}
