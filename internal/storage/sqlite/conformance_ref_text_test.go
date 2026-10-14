package sqlite

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/indexsig"
	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_RefText runs the cross-driver ref-text repair contract
// against a fresh SQLite database.
func TestConformance_RefText(t *testing.T) {
	d := newTestDriver(t)
	storagetest.RefTextConformance(t, d, storagetest.ProjectionDB{DB: d.DB(), Dialect: indexsig.DialectSQLite})
}
