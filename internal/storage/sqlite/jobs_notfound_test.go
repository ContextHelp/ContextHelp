package sqlite

import (
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// A missing job must be recognizable from outside the driver without
// matching on message text, and must not carry the sql package's own
// words into anything an operator reads.
//
// Both sentinels are load bearing and for different callers:
// storage.ErrNotFound is what the CLI keys on to answer NOT_FOUND, and
// sql.ErrNoRows is what AcquireNext reads as "queue empty" rather than
// as a failure. Dropping either one breaks a real caller — dropping
// sql.ErrNoRows would turn an idle queue into an error.
func TestErrJobNotFound_CarriesBothSentinels(t *testing.T) {
	if !errors.Is(errJobNotFound, storage.ErrNotFound) {
		t.Error("errJobNotFound does not wrap storage.ErrNotFound; " +
			"the CLI cannot classify a missing job as NOT_FOUND")
	}
	if !errors.Is(errJobNotFound, sql.ErrNoRows) {
		t.Error("errJobNotFound does not wrap sql.ErrNoRows; " +
			"AcquireNext would read an empty queue as a failure")
	}
}

// The message an operator sees must describe the caller's request, not
// the database library. "sql: no rows in result set" is an
// implementation detail that tells a caller nothing it can act on.
func TestErrJobNotFound_HidesDriverInternals(t *testing.T) {
	if got := errJobNotFound.Error(); strings.Contains(got, "sql: no rows in result set") {
		t.Errorf("errJobNotFound leaks driver internals: %q", got)
	}
}
