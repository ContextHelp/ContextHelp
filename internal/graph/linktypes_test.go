package graph

import (
	"testing"
)

func TestValidLinkType(t *testing.T) {
	for _, lt := range []LinkType{
		LinkExtends, LinkExtendedBy, LinkContradicts, LinkContradictedBy,
		LinkSupersedes, LinkSupersededBy, LinkSupports, LinkSupportedBy,
		LinkRelatedTo, LinkDerivedFrom, LinkDerivedTo,
	} {
		if !ValidLinkType(lt) {
			t.Errorf("expected %q to be valid", lt)
		}
	}

	if ValidLinkType("bogus") {
		t.Error("expected 'bogus' to be invalid")
	}
}

func TestInverseLinkType(t *testing.T) {
	cases := []struct {
		input LinkType
		want  LinkType
	}{
		{LinkExtends, LinkExtendedBy},
		{LinkExtendedBy, LinkExtends},
		{LinkContradicts, LinkContradictedBy},
		{LinkSupersedes, LinkSupersededBy},
		{LinkSupports, LinkSupportedBy},
		{LinkRelatedTo, LinkRelatedTo},
		{LinkDerivedFrom, LinkDerivedTo},
		{LinkDerivedTo, LinkDerivedFrom},
	}
	for _, tc := range cases {
		got, err := InverseLinkType(tc.input)
		if err != nil {
			t.Errorf("InverseLinkType(%q): %v", tc.input, err)
			continue
		}
		if got != tc.want {
			t.Errorf("InverseLinkType(%q) = %q; want %q", tc.input, got, tc.want)
		}
	}

	_, err := InverseLinkType("nonsense")
	if err == nil {
		t.Error("expected error for unknown link type")
	}
}

func TestIsSymmetric(t *testing.T) {
	if !IsSymmetric(LinkRelatedTo) {
		t.Error("related-to should be symmetric")
	}
	if IsSymmetric(LinkExtends) {
		t.Error("extends should not be symmetric")
	}
}

func TestUserLinkTypeStrings(t *testing.T) {
	strs := UserLinkTypeStrings()
	if len(strs) != len(UserLinkTypes) {
		t.Fatalf("len mismatch: %d vs %d", len(strs), len(UserLinkTypes))
	}
	for i, s := range strs {
		if s != string(UserLinkTypes[i]) {
			t.Errorf("index %d: %q != %q", i, s, UserLinkTypes[i])
		}
	}
}

func TestInverseSymmetry(t *testing.T) {
	// every type's inverse's inverse should be the original
	for lt := range inverseMap {
		inv, _ := InverseLinkType(lt)
		back, _ := InverseLinkType(inv)
		if back != lt {
			t.Errorf("inverse(inverse(%q)) = %q; want %q", lt, back, lt)
		}
	}
}
