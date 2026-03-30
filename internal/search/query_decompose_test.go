package search

import "testing"

func TestDecomposeQuery(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		minTerms int
		want     string
	}{
		{
			name:     "long question strips stopwords",
			query:    "what are the best ways to use Redis for caching",
			minTerms: 2,
			want:     "use Redis caching",
		},
		{
			name:     "short single-term query unchanged",
			query:    "Redis",
			minTerms: 2,
			want:     "Redis",
		},
		{
			name:     "single stopword unchanged",
			query:    "what",
			minTerms: 2,
			want:     "what",
		},
		{
			name:     "tech terms with numbers pass through",
			query:    "go1.21 upgrade v2 http2",
			minTerms: 2,
			want:     "go1.21 upgrade http2",
		},
		{
			name:     "all stopwords falls back to original",
			query:    "what is the best way",
			minTerms: 2,
			want:     "what is the best way",
		},
		{
			name:     "mixed meaningful and stopword terms",
			query:    "how to implement authentication and authorization",
			minTerms: 2,
			want:     "implement authentication authorization",
		},
		{
			name:     "minTerms=1 allows single result",
			query:    "what is Redis",
			minTerms: 1,
			want:     "Redis",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DecomposeQuery(tt.query, tt.minTerms)
			if got != tt.want {
				t.Errorf("DecomposeQuery(%q, %d) = %q; want %q", tt.query, tt.minTerms, got, tt.want)
			}
		})
	}
}
