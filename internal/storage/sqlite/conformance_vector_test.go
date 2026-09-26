package sqlite

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_VectorRankFixture runs the cross-driver golden rank
// fixture through the driver's per-model index (EmbeddingStore.Put, then
// VectorSearch on the fixture model). It skips until the driver's
// EmbeddingStore is implemented.
func TestConformance_VectorRankFixture(t *testing.T) {
	storagetest.RunVectorRankFixture(t, newTestDriver(t))
}

// TestConformance_Search runs the full cross-driver search conformance
// suite. SQLite claims both legs.
func TestConformance_Search(t *testing.T) {
	storagetest.RunSearchConformance(t, newTestDriver(t), storagetest.SearchCapabilities{
		FTS:     true,
		Vectors: true,
	})
}

// memEmbeddingsDriver is the SQLite driver with its EmbeddingStore swapped
// for the in-memory double, so the vector conformance runs this driver's
// VectorSearch (over-fetch, hydrate, filter, score) before the per-model
// index lands.
type memEmbeddingsDriver struct {
	*Driver
	objects *ObjectStore
	emb     storage.EmbeddingStore
}

func (d *memEmbeddingsDriver) Objects() storage.ObjectStore       { return d.objects }
func (d *memEmbeddingsDriver) Embeddings() storage.EmbeddingStore { return d.emb }

func newMemEmbeddingsDriver(t *testing.T) *memEmbeddingsDriver {
	d := newTestDriver(t)
	emb := storagetest.NewMemEmbeddingStore()
	return &memEmbeddingsDriver{Driver: d, objects: &ObjectStore{db: d.db, emb: emb}, emb: emb}
}

func TestConformance_VectorRankFixture_MemEmbeddings(t *testing.T) {
	storagetest.RunVectorRankFixture(t, newMemEmbeddingsDriver(t))
}

func TestConformance_Search_MemEmbeddings(t *testing.T) {
	storagetest.RunSearchConformance(t, newMemEmbeddingsDriver(t), storagetest.SearchCapabilities{
		FTS:     true,
		Vectors: true,
	})
}
