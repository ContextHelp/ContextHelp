package mentions_test

import (
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/mentions"
	uri "hop.top/cite/scheme"
)

func TestParse(t *testing.T) {
	tests := []struct {
		in      string
		wantURI string
		wantOK  bool
	}{
		// slug form: single dot
		{"@project.signup-redesign", "ctxt://entity/project/signup-redesign", true},
		// slug form: multiple dots → fully path-separated
		{"@stripe.api.checkout", "ctxt://entity/stripe/api/checkout", true},
		{"@person.jane.doe", "ctxt://entity/person/jane/doe", true},
		// slug form: no @ prefix
		{"org.acme", "ctxt://entity/org/acme", true},
		// URI form: passed through unchanged
		{"ctxt://entity/project/signup-redesign", "ctxt://entity/project/signup-redesign", true},
		{"ctxt://entity/stripe/api/checkout", "ctxt://entity/stripe/api/checkout", true},
		// edge cases
		{"", "", false},
		{"@", "", false},
	}

	for _, tc := range tests {
		u, ok := mentions.Parse(tc.in)
		if ok != tc.wantOK {
			t.Errorf("Parse(%q): got ok=%v, want %v", tc.in, ok, tc.wantOK)
			continue
		}
		if ok && u.String() != tc.wantURI {
			t.Errorf("Parse(%q): got %q, want %q", tc.in, u.String(), tc.wantURI)
		}
	}
}

func TestParseSlice(t *testing.T) {
	in := []string{
		"@stripe.api.checkout",            // slug form, multi-dot
		"ctxt://entity/project/dashboard", // URI form
		"@person.alice",                   // slug form, single dot
		"",                                // dropped
	}
	got := mentions.ParseSlice(in)
	want := []uri.URI{
		{Scheme: "ctxt", Namespace: "entity", ID: "stripe/api/checkout"},
		{Scheme: "ctxt", Namespace: "entity", ID: "project/dashboard"},
		{Scheme: "ctxt", Namespace: "entity", ID: "person/alice"},
	}
	if len(got) != len(want) {
		t.Fatalf("ParseSlice: got %d results, want %d: %v", len(got), len(want), got)
	}
	for i, u := range got {
		if u != want[i] {
			t.Errorf("ParseSlice[%d]: got %v, want %v", i, u, want[i])
		}
	}
}

func TestParseSlice_Deduplication(t *testing.T) {
	// Same entity expressed in both forms — ParseSlice doesn't deduplicate,
	// that's the caller's job; just verify both parse to the same URI.
	in := []string{"@stripe.api.checkout", "ctxt://entity/stripe/api/checkout"}
	got := mentions.ParseSlice(in)
	if len(got) != 2 {
		t.Fatalf("expected 2 results, got %d", len(got))
	}
	if got[0] != got[1] {
		t.Errorf("same entity via slug and URI should produce equal URIs: %v != %v", got[0], got[1])
	}
}
