package tagfilter

import (
	"slices"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	cases := []struct {
		name     string
		in       []string
		labels   []string
		required []string
	}{
		{"single", []string{"a"}, []string{"a"}, nil},
		{"comma list", []string{"a,b,c"}, []string{"a", "b", "c"}, nil},
		{"repeated flag", []string{"a", "b"}, []string{"a", "b"}, nil},
		{"mixed concat order", []string{"a,b", "c"}, []string{"a", "b", "c"}, nil},
		{"spaced label", []string{"machine learning"}, []string{"machine learning"}, nil},
		{"spaced labels trimmed", []string{" machine learning , ux "}, []string{"machine learning", "ux"}, nil},
		{"spaced required", []string{"+a,+machine learning"}, []string{"a", "machine learning"}, []string{"a", "machine learning"}},
		{"case kept", []string{"UX,ux"}, []string{"UX", "ux"}, nil},
		{"all required", []string{"+a,+b,+c"}, []string{"a", "b", "c"}, []string{"a", "b", "c"}},
		{"required across values", []string{"+a", "+b"}, []string{"a", "b"}, []string{"a", "b"}},
		{"mixed required and plain", []string{"+a,b,+c"}, []string{"a", "b", "c"}, []string{"a", "c"}},
		{"plus inside a label is literal", []string{"c++,a+b"}, []string{"c++", "a+b"}, nil},
		{"required label with inner plus", []string{"+c++"}, []string{"c++"}, []string{"c++"}},
		{"question and bang are literal now", []string{"what?,!x"}, []string{"what?", "!x"}, nil},
		{"dedupe", []string{"a,b,a", "b"}, []string{"a", "b"}, nil},
		{"dedupe required wins", []string{"b,+b,a"}, []string{"b", "a"}, []string{"b"}},
		{"dedupe required first", []string{"+b,b"}, []string{"b"}, []string{"b"}},
		{"plus space label", []string{"+ a"}, []string{"a"}, []string{"a"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, err := Parse(tc.in)
			if err != nil {
				t.Fatalf("Parse(%q): %v", tc.in, err)
			}
			if got := f.Labels(); !slices.Equal(got, tc.labels) {
				t.Errorf("Labels = %q, want %q", got, tc.labels)
			}
			if got := f.Required(); !slices.Equal(got, tc.required) {
				t.Errorf("Required = %q, want %q", got, tc.required)
			}
		})
	}
}

func TestParseNone(t *testing.T) {
	f, err := Parse(nil)
	if err != nil || !f.IsZero() {
		t.Fatalf("Parse(nil) = %+v, %v; want zero", f, err)
	}
}

func TestParseErrors(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want string
	}{
		{"empty value", []string{""}, "empty"},
		{"blank value", []string{"  "}, "empty"},
		{"empty segment", []string{"a,,b"}, "empty"},
		{"trailing comma", []string{"a,"}, "empty"},
		{"leading comma", []string{",a"}, "empty"},
		{"empty second value", []string{"a", ""}, "empty"},
		{"bare plus", []string{"+"}, "label"},
		{"bare plus in list", []string{"a,+,b"}, "label"},
		{"plus then space", []string{"+ "}, "label"},
		{"exclusion", []string{"-a"}, "not supported yet"},
		{"exclusion in list", []string{"a,-b"}, "not supported yet"},
		{"required exclusion", []string{"+-a"}, "not supported yet"},
		{"exclusion across values", []string{"a", "-b"}, "not supported yet"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.in)
			if err == nil {
				t.Fatalf("Parse(%q): want error", tc.in)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

func TestMatch(t *testing.T) {
	cases := []struct {
		name   string
		filter string
		labels []string
		want   bool
	}{
		{"any hits one", "a,b", []string{"b", "z"}, true},
		{"any misses", "a,b", []string{"z"}, false},
		{"all needs every", "+a,+b", []string{"a", "b", "z"}, true},
		{"all missing one", "+a,+b", []string{"a"}, false},
		{"plain tag only ranks beside a required one", "+a,b,+c", []string{"a", "c"}, true},
		{"plain tag does not satisfy a missing required", "+a,b,+c", []string{"a", "b"}, false},
		{"plain tag alone is not enough beside required", "+a,b", []string{"b"}, false},
		{"unknown tag matches nothing", "nope", []string{"a"}, false},
		{"no tags", "a", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, err := Parse([]string{tc.filter})
			if err != nil {
				t.Fatal(err)
			}
			if got := f.Match(tc.labels); got != tc.want {
				t.Errorf("Match(%q) = %v, want %v", tc.labels, got, tc.want)
			}
		})
	}
	if !(Filter{}).Match(nil) {
		t.Error("zero filter must match everything")
	}
}

func TestMatched(t *testing.T) {
	f, _ := Parse([]string{"+a,b,c"})
	// Required and plain tags count alike; unselected labels never.
	for labels, want := range map[string]int{"": 0, "a": 1, "a b": 2, "a b c": 3, "b c": 2, "z a a": 1} {
		if got := f.Matched(strings.Fields(labels)); got != want {
			t.Errorf("Matched(%q) = %d, want %d", labels, got, want)
		}
	}
	g, _ := Parse([]string{"x,y,z"})
	if g.Matched([]string{"x", "z"}) != 2 || g.Matched([]string{"y"}) != 1 {
		t.Error("any mode counts every listed tag")
	}
}

func TestRanks(t *testing.T) {
	for in, want := range map[string]bool{
		"a":       false, // one tag: the count never differs
		"+a":      false,
		"+a,+b":   false, // all required: they always match
		"a,b":     true,  // no required tag, 2+ tags
		"+a,b":    true,  // a plain tag beside a required one
		"a,+b,c":  true,
		"+a,+b,c": true,
	} {
		f, err := Parse([]string{in})
		if err != nil {
			t.Fatal(err)
		}
		if got := f.Ranks(); got != want {
			t.Errorf("Ranks(%q) = %v, want %v", in, got, want)
		}
	}
	if (Filter{}).Ranks() {
		t.Error("zero filter never ranks")
	}
}

func TestStringRoundTrip(t *testing.T) {
	for _, in := range []string{"a", "a,b,c", "+a,+b,+c", "+a,b,+c", "machine learning,+c++"} {
		f, err := Parse([]string{in})
		if err != nil {
			t.Fatal(err)
		}
		if got := f.String(); got != in {
			t.Errorf("String() = %q, want %q", got, in)
		}
		g, err := Parse([]string{f.String()})
		if err != nil || g.String() != in {
			t.Errorf("round trip %q: %v %v", in, g, err)
		}
	}
	if (Filter{}).String() != "" {
		t.Error("zero filter String must be empty")
	}
}
