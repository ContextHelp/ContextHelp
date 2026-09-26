package upgrade

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/sqlite"
)

// Every lowercase allowlisted identifier is an objects column; each must
// exist on a fully migrated objects table, so the allowlist cannot admit a
// predicate on a column a migration dropped.
func TestAllowedIdentifiers_ColumnsExistOnMigratedSchema(t *testing.T) {
	ctx := context.Background()
	d, err := sqlite.New(filepath.Join(t.TempDir(), "selector.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close(ctx) })
	if err := d.Init(ctx); err != nil {
		t.Fatal(err)
	}
	for ident := range allowedIdentifiers {
		if ident != strings.ToLower(ident) {
			continue // operator keyword
		}
		if _, err := d.DB().ExecContext(ctx, `SELECT `+ident+` FROM objects LIMIT 0`); err != nil {
			t.Errorf("allowlisted column %q is not on objects: %v", ident, err)
		}
	}
}
