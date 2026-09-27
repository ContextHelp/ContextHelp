//go:build integration

package jobs

// The re-projection scenarios on Postgres, on a throwaway database. Reads
// the POSTGRES_* env block the other integration suites use:
//
//	POSTGRES_HOST=localhost POSTGRES_USER=contexthelp POSTGRES_PASSWORD=test_password \
//	  go test -tags integration,fts5 -count=1 -run 'TestReproject_.*_Postgres' ./internal/jobs/

import (
	"database/sql"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/indexsig"
	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

func postgresReprojectDriver(t *testing.T) (storage.StorageDriver, storagetest.ProjectionDB) {
	t.Helper()
	drv, _ := sharedPostgres(t)
	h, ok := drv.(interface{ DB() *sql.DB })
	if !ok {
		t.Fatalf("%T exposes no DB()", drv)
	}
	return drv, storagetest.ProjectionDB{DB: h.DB(), Dialect: indexsig.DialectPostgres}
}

func TestReproject_StaleDatabaseOnStart_Postgres(t *testing.T) {
	drv, pdb := postgresReprojectDriver(t)
	runReprojectStaleDatabaseOnStart(t, drv, pdb)
}

func TestReproject_ResumesAfterCrash_Postgres(t *testing.T) {
	drv, pdb := postgresReprojectDriver(t)
	runReprojectResumesAfterCrash(t, drv, pdb)
}

func TestReproject_IngestDuringRun_Postgres(t *testing.T) {
	drv, pdb := postgresReprojectDriver(t)
	runReprojectIngestDuringRun(t, drv, pdb)
}
