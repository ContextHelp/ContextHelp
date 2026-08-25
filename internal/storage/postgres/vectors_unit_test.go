package postgres

import "testing"

// TestPgvectorSupportsIterativeScan pins the version gate for
// hnsw.iterative_scan: introduced in pgvector 0.8.0. Older extensions do not
// recognize the GUC, so SET LOCAL against them would error every search.
func TestPgvectorSupportsIterativeScan(t *testing.T) {
	cases := []struct {
		version string
		want    bool
	}{
		{"0.8.0", true},
		{"0.8.6", true},
		{"0.9.1", true},
		{"1.0.0", true},
		{"0.7.4", false},
		{"0.7.0", false},
		{"", false},
		{"garbage", false},
		{"0", false},
	}
	for _, c := range cases {
		if got := pgvectorSupportsIterativeScan(c.version); got != c.want {
			t.Errorf("pgvectorSupportsIterativeScan(%q) = %v, want %v", c.version, got, c.want)
		}
	}
}
