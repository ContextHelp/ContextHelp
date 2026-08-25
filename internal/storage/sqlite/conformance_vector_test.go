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
