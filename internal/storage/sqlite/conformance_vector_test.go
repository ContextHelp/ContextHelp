package sqlite

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/storage/storagetest"
)

// TestConformance_VectorRankFixture runs the cross-driver golden rank
// fixture through the ANN (vec0) path: the driver is configured with the
// fixture dimension so Create mirrors embeddings into vec_objects and
// VectorSearch takes the KNN route.
func TestConformance_VectorRankFixture(t *testing.T) {
	d := newTestDriverDim(t, storagetest.VectorRankDimension)
	storagetest.RunVectorRankFixture(t, d)
}

// TestConformance_Search runs the full cross-driver search conformance
// suite. SQLite claims both legs; dimension enforcement is lenient — mixed
// dimensions are load-bearing for its brute-force path.
func TestConformance_Search(t *testing.T) {
	d := newTestDriverDim(t, storagetest.VectorRankDimension)
	storagetest.RunSearchConformance(t, d, storagetest.SearchCapabilities{
		FTS:                    true,
		Vectors:                true,
		StrictDimensionOnWrite: false,
	})
}
