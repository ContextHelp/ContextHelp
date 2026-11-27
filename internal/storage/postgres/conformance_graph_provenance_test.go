//go:build integration

package postgres_test

import (
	"context"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/keys"
	pgdrv "github.com/ideacrafterslabs/ctxt/internal/storage/postgres"
	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// TestConformance_GraphProvenance runs the cross-driver graph stamp
// contract against a fresh Postgres database.
func TestConformance_GraphProvenance(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	storagetest.GraphProvenanceConformance(t, drv)
}

// TestConformance_GraphMarker runs the cross-driver graph mark contract
// against a fresh Postgres database.
func TestConformance_GraphMarker(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	storagetest.GraphMarkerConformance(t, drv)
}

// TestConformance_GraphMarkOwnership runs the cross-driver mark ownership
// contract against a fresh Postgres database.
func TestConformance_GraphMarkOwnership(t *testing.T) {
	drv, _ := freshIntegrationDriver(t)
	storagetest.GraphMarkOwnershipConformance(t, drv)
}

// freshEncryptedDriver is freshIntegrationDriver with every object column
// encrypted (column_encryption: all), so graph_json is stored as a box.
func freshEncryptedDriver(t *testing.T) *pgdrv.Driver {
	t.Helper()
	dsn, _ := freshDatabaseDSN(t)
	p, err := keys.NewMemory("k1")
	if err != nil {
		t.Fatal(err)
	}
	drv, err := pgdrv.New(dsn, storageutil.NewTestBlobStore(t), pgdrv.WithEncryption(pgdrv.EncryptionConfig{
		ColumnEncryption: config.ColumnEncryptionAll, FTSOnEncrypted: config.FTSOnEncryptedOmit, KeyProvider: p,
	}))
	if err != nil {
		t.Fatalf("postgres.New(encrypted): %v", err)
	}
	t.Cleanup(func() { drv.Close(context.Background()) })
	if err := drv.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return drv
}

// With column encryption all, the stored graph is a box: the mark is read
// from the opened column.
func TestConformance_GraphMarkerEncrypted(t *testing.T) {
	storagetest.GraphMarkerConformance(t, freshEncryptedDriver(t))
}

func TestConformance_GraphMarkOwnershipEncrypted(t *testing.T) {
	storagetest.GraphMarkOwnershipConformance(t, freshEncryptedDriver(t))
}
