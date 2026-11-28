package rederive

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/indexsig"
	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

func sigOfRow(t *testing.T, drv storage.StorageDriver, id string) string {
	t.Helper()
	return storagetest.ProjectionDB{DB: sqliteDB(t, drv), Dialect: indexsig.DialectSQLite}.DerivationSig(t, id)
}
