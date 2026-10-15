package sqlite

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_ThinEntity runs the cross-driver thin-entity round trip
// against a fresh SQLite database.
func TestConformance_ThinEntity(t *testing.T) {
	storagetest.ThinEntityConformance(t, newTestDriver(t))
}

// TestConformance_EntityQuery runs the cross-driver entity search and
// resolve contract against a fresh SQLite database.
func TestConformance_EntityQuery(t *testing.T) {
	storagetest.EntityQueryConformance(t, newTestDriver(t))
}

// TestConformance_PendingEntities runs the cross-driver approval queue
// contract against a fresh SQLite database.
func TestConformance_PendingEntities(t *testing.T) {
	storagetest.PendingEntityConformance(t, newTestDriver(t))
}

// TestConformance_EntityNamespaces runs the cross-driver namespace
// listing and restriction contract against a fresh SQLite database.
func TestConformance_EntityNamespaces(t *testing.T) {
	storagetest.EntityNamespaceConformance(t, newTestDriver(t))
}
