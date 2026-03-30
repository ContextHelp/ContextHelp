package search

import "math"

// BlendVectors returns alpha*a + (1-alpha)*b, normalized to unit length.
// If b is nil or differs in length from a, returns a unchanged.
func BlendVectors(a, b []float32, alpha float64) []float32 {
	if b == nil || len(a) != len(b) {
		return a
	}

	out := make([]float32, len(a))
	for i := range a {
		out[i] = float32(alpha*float64(a[i]) + (1-alpha)*float64(b[i]))
	}
	return normalizeL2(out)
}

// normalizeL2 returns a copy of v scaled to unit length.
// If the norm is zero, returns v unchanged.
func normalizeL2(v []float32) []float32 {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	if sum == 0 {
		return v
	}
	norm := math.Sqrt(sum)
	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = float32(float64(x) / norm)
	}
	return out
}
