package sqlite

import (
	"database/sql"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// capability describes an engine feature the schema depends on, the probe that
// proves it is present, and the remedy to print when it is not.
type capability struct {
	name   string
	probe  string
	remedy string
}

// requiredCapabilities are verified on every store open, before migrations.
//
// The fts5 build tag is enforced at compile time (see sqlite3_fts5.go and its
// !fts5 counterpart). These probes cover what a compile guard cannot see: a
// dynamically linked system SQLite without FTS5, a CGO_ENABLED=0 binary, or a
// future driver swap.
// The fts5 probe reads pragma_compile_options rather than calling a version
// function: mattn/go-sqlite3 compiles FTS5 in as a module, so ENABLE_FTS5 in
// the compile options is the authoritative signal. sqlite-vec registers
// vec_version() as a scalar function once sqlite_vec.Auto() has run, so a
// direct call is the right probe there.
var requiredCapabilities = []capability{
	{
		name:   "fts5",
		probe:  "SELECT count(*) FROM pragma_compile_options WHERE compile_options LIKE 'ENABLE_FTS5%'",
		remedy: "rebuild with -tags fts5 (CGO_ENABLED=1)",
	},
	{
		name:   "sqlite-vec",
		probe:  "SELECT vec_version()",
		remedy: "rebuild with CGO_ENABLED=1 so the sqlite-vec extension is linked",
	},
}

// probeCapabilities verifies every required engine capability is available on
// the freshly opened database. Failures wrap storage.ErrCapabilityMissing and
// name both the capability and its remedy.
func probeCapabilities(db *sql.DB) error {
	for _, c := range requiredCapabilities {
		var version string
		if err := db.QueryRow(c.probe).Scan(&version); err != nil {
			return fmt.Errorf(
				"%w: sqlite %s unavailable (%s): %w",
				storage.ErrCapabilityMissing, c.name, c.remedy, err,
			)
		}
	}
	return nil
}
