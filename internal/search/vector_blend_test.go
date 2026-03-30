package search

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBlendVectors_Alpha1(t *testing.T) {
	a := []float32{3, 4}
	b := []float32{1, 0}
	got := BlendVectors(a, b, 1.0)
	want := normalizeL2([]float32{3, 4})
	require.Len(t, got, 2)
	assert.InDelta(t, want[0], got[0], 1e-5)
	assert.InDelta(t, want[1], got[1], 1e-5)
	assertUnitLength(t, got)
}

func TestBlendVectors_Alpha0(t *testing.T) {
	a := []float32{3, 4}
	b := []float32{1, 0}
	got := BlendVectors(a, b, 0.0)
	want := normalizeL2([]float32{1, 0})
	require.Len(t, got, 2)
	assert.InDelta(t, want[0], got[0], 1e-5)
	assert.InDelta(t, want[1], got[1], 1e-5)
	assertUnitLength(t, got)
}

func TestBlendVectors_NilB_ReturnsA(t *testing.T) {
	a := []float32{1, 2, 3}
	got := BlendVectors(a, nil, 0.85)
	require.Equal(t, a, got)
}

func TestBlendVectors_DifferentLength_ReturnsA(t *testing.T) {
	a := []float32{1, 2, 3}
	b := []float32{1, 2}
	got := BlendVectors(a, b, 0.85)
	require.Equal(t, a, got)
}

func TestBlendVectors_UnitLength(t *testing.T) {
	a := []float32{1, 2, 3, 4}
	b := []float32{4, 3, 2, 1}
	got := BlendVectors(a, b, 0.7)
	assertUnitLength(t, got)
}

func assertUnitLength(t *testing.T, v []float32) {
	t.Helper()
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	assert.InDelta(t, 1.0, math.Sqrt(sum), 1e-5, "vector should be unit length")
}
