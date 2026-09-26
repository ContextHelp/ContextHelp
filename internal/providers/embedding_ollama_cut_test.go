package providers

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// Every cut is valid UTF-8, fits the budget, and keeps the longest prefix
// that does: 1-, 2-, 3- and 4-byte runes, at every byte offset.
func TestTruncateUTF8_NeverSplitsARune(t *testing.T) {
	s := "aé€𝄞 Genève, déjà vu — 日本語 🙂 fin"
	for n := -1; n <= len(s)+1; n++ {
		got := truncateUTF8(s, n)
		if !utf8.ValidString(got) {
			t.Fatalf("truncateUTF8(s, %d) = %q: invalid UTF-8", n, got)
		}
		if !strings.HasPrefix(s, got) || len(got) > max(n, 0) {
			t.Fatalf("truncateUTF8(s, %d) = %q: not a prefix within the budget", n, got)
		}
		if rest := s[len(got):]; rest != "" {
			_, size := utf8.DecodeRuneInString(rest)
			if len(got)+size <= n {
				t.Fatalf("truncateUTF8(s, %d) = %q: dropped a rune that fits", n, got)
			}
		}
	}
}

func TestInputByteBudget(t *testing.T) {
	for ctx, want := range map[int]int{8192: 8184, 2048: 2040, 9: 1, 8: 1, 0: 1} {
		if got := inputByteBudget(ctx); got != want {
			t.Errorf("inputByteBudget(%d) = %d, want %d", ctx, got, want)
		}
	}
}

func TestModelContextLength(t *testing.T) {
	for name, tc := range map[string]struct {
		info map[string]any
		want int
		ok   bool
	}{
		"architecture key": {map[string]any{"general.architecture": "bert", "bert.context_length": 8192.0}, 8192, true},
		"architecture wins": {map[string]any{
			"general.architecture": "nomic-bert", "nomic-bert.context_length": 2048.0, "other.context_length": 512.0,
		}, 2048, true},
		"single fallback key": {map[string]any{"x.context_length": 512.0}, 512, true},
		"ambiguous keys":      {map[string]any{"a.context_length": 512.0, "b.context_length": 1024.0}, 0, false},
		"missing":             {map[string]any{"general.architecture": "bert"}, 0, false},
		"not a count":         {map[string]any{"general.architecture": "bert", "bert.context_length": "8192"}, 0, false},
	} {
		t.Run(name, func(t *testing.T) {
			got, ok := modelContextLength(tc.info)
			if got != tc.want || ok != tc.ok {
				t.Errorf("modelContextLength = %d, %v; want %d, %v", got, ok, tc.want, tc.ok)
			}
		})
	}
}
