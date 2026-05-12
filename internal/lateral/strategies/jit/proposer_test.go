package jit

import (
	"strings"
	"testing"
)

func TestBuildPromptIncludesInputs(t *testing.T) {
	req := ProposalRequest{
		SourceDomain: "blog.acme.io",
		PageType:     "blog_index",
	}
	got := BuildPrompt(req)
	if !strings.Contains(got, "blog.acme.io") {
		t.Errorf("prompt missing SourceDomain; got:\n%s", got)
	}
	if !strings.Contains(got, "blog_index") {
		t.Errorf("prompt missing PageType; got:\n%s", got)
	}
}

func TestBuildPromptDeterministic(t *testing.T) {
	req := ProposalRequest{SourceDomain: "x.example", PageType: "person"}
	a := BuildPrompt(req)
	b := BuildPrompt(req)
	if a != b {
		t.Fatalf("BuildPrompt non-deterministic:\nA=%q\nB=%q", a, b)
	}
}

func TestParseProposalResponse(t *testing.T) {
	// Build a 30-line input to exercise the 25-cap.
	manyLines := make([]string, 0, 30)
	for i := 0; i < 30; i++ {
		// Use distinct entries so dedup doesn't shrink the list before the cap.
		manyLines = append(manyLines, "/p"+string(rune('a'+i%26))+itoa(i))
	}

	cases := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "empty",
			in:   "",
			want: nil,
		},
		{
			name: "bullets dash and star",
			in:   "- /about\n* /team\n/blog",
			want: []string{"/about", "/team", "/blog"},
		},
		{
			name: "crlf and mixed whitespace",
			in:   "  /a  \r\n\r\n/b\r/c\n",
			want: []string{"/a", "/b", "/c"},
		},
		{
			name: "comments stripped",
			in:   "# header\n// note\n/keep\n  # indented-still-comment-after-trim\n/another",
			want: []string{"/keep", "/another"},
		},
		{
			name: "dedup preserves order",
			in:   "/x\n/y\n/x\n/z\n/y",
			want: []string{"/x", "/y", "/z"},
		},
		{
			name: "cap at 25",
			in:   strings.Join(manyLines, "\n"),
			want: manyLines[:25],
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseProposalResponse(tc.in)
			if !equal(got, tc.want) {
				t.Fatalf("ParseProposalResponse(%q)\n got: %v\nwant: %v", tc.in, got, tc.want)
			}
		})
	}
}

// itoa is a tiny local helper to avoid importing strconv in a test that only
// needs ascending integer suffixes.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
