package sharing_test

import (
	"fmt"
	"math/rand"
	"slices"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/sharing"
)

// clsBase is a valid policy every spec case starts from.
func clsBase() sharing.Policy {
	return sharing.Policy{
		Name:           "base",
		Data:           "type==note;tag=in=(work,client)",
		ExcludeClasses: []string{"personal"},
		Fields: map[string][]string{
			"contact": {"title", "org"},
			"*":       {"title", "summary"},
		},
		Redact: sharing.Redact{Mask: []string{"email"}, Reveal: []string{"phone"}, Style: sharing.RedactCategory},
		Capabilities: sharing.Capabilities{
			Search:   []string{sharing.SearchFTS, sharing.SearchVector},
			Entities: true,
		},
	}
}

func clsWith(edit func(*sharing.Policy)) sharing.Policy {
	p := clsBase()
	edit(&p)
	return p
}

func TestCompareSpecs_SameSpecIsEqual(t *testing.T) {
	policies := append([]sharing.Policy{clsBase()}, sharing.Templates()...)
	for _, p := range policies {
		got := sharing.CompareSpecs(p, p)
		if got.Order != sharing.SpecEqual {
			t.Errorf("%s against itself: got %v (%s), want equal", p.Name, got.Order, got.Component)
		}
	}
}

func TestCompareSpecs_Components(t *testing.T) {
	cases := []struct {
		name      string
		edit      func(*sharing.Policy)
		want      sharing.SpecOrder
		component string
	}{
		// data
		{"data: conjuncts reordered", func(p *sharing.Policy) { p.Data = "tag=in=(client,work);type==note" }, sharing.SpecEqual, ""},
		{"data: value list reordered", func(p *sharing.Policy) { p.Data = "type==note;tag=in=(client,work)" }, sharing.SpecEqual, ""},
		{"data: redundant parentheses", func(p *sharing.Policy) { p.Data = "(type==note);(tag=in=(work,client))" }, sharing.SpecEqual, ""},
		{"data: AND conjunct added", func(p *sharing.Policy) { p.Data = "type==note;tag=in=(work,client);source==mail" }, sharing.SpecNarrower, ""},
		{"data: AND around the old filter", func(p *sharing.Policy) { p.Data = "(type==note;tag=in=(work,client));source==mail" }, sharing.SpecNarrower, ""},
		{"data: OR branch added", func(p *sharing.Policy) { p.Data = "type==note;tag=in=(work,client),source==mail" }, sharing.SpecWider, "data"},
		{"data: OR around the old filter", func(p *sharing.Policy) { p.Data = "(type==note;tag=in=(work,client)),type==task" }, sharing.SpecWider, "data"},
		{"data: conjunct dropped", func(p *sharing.Policy) { p.Data = "type==note" }, sharing.SpecWider, "data"},
		{"data: value added to a list", func(p *sharing.Policy) { p.Data = "type==note;tag=in=(work,client,home)" }, sharing.SpecWider, "data"},
		{"data: value removed from a list is not proved", func(p *sharing.Policy) { p.Data = "type==note;tag=in=(work)" }, sharing.SpecWider, "data"},
		{"data: operator changed", func(p *sharing.Policy) { p.Data = "type!=note;tag=in=(work,client)" }, sharing.SpecWider, "data"},
		{"data: unrelated filter", func(p *sharing.Policy) { p.Data = "type==task" }, sharing.SpecWider, "data"},
		// exclude_classes
		{"exclude_classes: class added", func(p *sharing.Policy) { p.ExcludeClasses = []string{"personal", "health"} }, sharing.SpecNarrower, ""},
		{"exclude_classes: duplicates and order", func(p *sharing.Policy) { p.ExcludeClasses = []string{"personal", "personal"} }, sharing.SpecEqual, ""},
		{"exclude_classes: class removed", func(p *sharing.Policy) { p.ExcludeClasses = nil }, sharing.SpecWider, "exclude_classes"},
		{"exclude_classes: class swapped", func(p *sharing.Policy) { p.ExcludeClasses = []string{"health"} }, sharing.SpecWider, "exclude_classes"},
		// fields
		{"fields: field removed from a type", func(p *sharing.Policy) { p.Fields["contact"] = []string{"title"} }, sharing.SpecNarrower, ""},
		{"fields: field removed from *", func(p *sharing.Policy) { p.Fields["*"] = []string{"title"} }, sharing.SpecNarrower, ""},
		{"fields: alias spelling is the same field", func(p *sharing.Policy) { p.Fields["*"] = []string{"title", "summaries"} }, sharing.SpecEqual, ""},
		{"fields: field added to a type", func(p *sharing.Policy) { p.Fields["contact"] = []string{"title", "org", "email"} }, sharing.SpecWider, "fields"},
		{"fields: field added to *", func(p *sharing.Policy) { p.Fields["*"] = []string{"title", "summary", "content"} }, sharing.SpecWider, "fields"},
		{"fields: a type opens every field", func(p *sharing.Policy) { p.Fields["contact"] = []string{sharing.FieldAll} }, sharing.SpecWider, "fields"},
		{"fields: * opens every field", func(p *sharing.Policy) { p.Fields["*"] = []string{sharing.FieldAll} }, sharing.SpecWider, "fields"},
		{"fields: type entry dropped falls back to a wider *", func(p *sharing.Policy) {
			p.Fields = map[string][]string{"*": {"title", "summary", "org"}}
		}, sharing.SpecWider, "fields"},
		// A narrower per-type entry that is new, or an entry dropped,
		// changes which types fall back to "*" in a merged plan, so it
		// is not proved narrowing (see CompareSpecs).
		{"fields: new type entry narrower than *", func(p *sharing.Policy) { p.Fields["note"] = []string{"title"} }, sharing.SpecWider, "fields"},
		{"fields: type entry dropped onto a narrower *", func(p *sharing.Policy) {
			p.Fields = map[string][]string{"*": {"title"}}
		}, sharing.SpecWider, "fields"},
		{"fields: every entry dropped", func(p *sharing.Policy) { p.Fields = nil }, sharing.SpecWider, "fields"},
		// capabilities
		{"capabilities: search mode removed", func(p *sharing.Policy) { p.Capabilities.Search = []string{sharing.SearchFTS} }, sharing.SpecNarrower, ""},
		{"capabilities: search order", func(p *sharing.Policy) {
			p.Capabilities.Search = []string{sharing.SearchVector, sharing.SearchFTS}
		}, sharing.SpecEqual, ""},
		{"capabilities: search mode added", func(p *sharing.Policy) {
			p.Capabilities.Search = []string{sharing.SearchFTS, sharing.SearchVector, sharing.SearchGraph}
		}, sharing.SpecWider, "capabilities"},
		{"capabilities: entities off", func(p *sharing.Policy) { p.Capabilities.Entities = false }, sharing.SpecNarrower, ""},
		{"capabilities: export on", func(p *sharing.Policy) { p.Capabilities.Export = true }, sharing.SpecWider, "capabilities"},
		{"capabilities: mcp on", func(p *sharing.Policy) { p.Capabilities.MCP = true }, sharing.SpecWider, "capabilities"},
		{"capabilities: events on", func(p *sharing.Policy) { p.Capabilities.Events = true }, sharing.SpecWider, "capabilities"},
		// redact
		{"redact: mask added", func(p *sharing.Policy) { p.Redact.Mask = []string{"email", "phone"} }, sharing.SpecNarrower, ""},
		{"redact: mask removed", func(p *sharing.Policy) { p.Redact.Mask = nil }, sharing.SpecWider, "redact"},
		{"redact: reveal removed", func(p *sharing.Policy) { p.Redact.Reveal = nil }, sharing.SpecNarrower, ""},
		{"redact: reveal added", func(p *sharing.Policy) { p.Redact.Reveal = []string{"phone", "address"} }, sharing.SpecWider, "redact"},
		{"redact: empty style is category", func(p *sharing.Policy) { p.Redact.Style = "" }, sharing.SpecEqual, ""},
		{"redact: style changed", func(p *sharing.Policy) { p.Redact.Style = sharing.RedactPartial }, sharing.SpecWider, "redact"},
		// mixed
		{"narrower data, wider capabilities", func(p *sharing.Policy) {
			p.Data += ";source==mail"
			p.Capabilities.Export = true
		}, sharing.SpecWider, "capabilities"},
		{"narrower on every component", func(p *sharing.Policy) {
			p.Data += ";source==mail"
			p.ExcludeClasses = append(p.ExcludeClasses, "health")
			p.Fields["*"] = []string{"title"}
			p.Capabilities = sharing.Capabilities{}
			p.Redact.Mask = append(p.Redact.Mask, "phone")
		}, sharing.SpecNarrower, ""},
		// unparseable
		{"new spec invalid", func(p *sharing.Policy) { p.Data = "mention==x" }, sharing.SpecWider, "spec"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sharing.CompareSpecs(clsBase(), clsWith(tc.edit))
			if got.Order != tc.want || got.Component != tc.component {
				t.Errorf("got %v/%q, want %v/%q", got.Order, got.Component, tc.want, tc.component)
			}
		})
	}
}

func TestCompareSpecs_FieldAll(t *testing.T) {
	all := func(p *sharing.Policy) {
		p.Fields = map[string][]string{"contact": {sharing.FieldAll}, "*": {sharing.FieldAll}}
	}
	cases := []struct {
		name     string
		old, new func(*sharing.Policy)
		want     sharing.SpecOrder
	}{
		{"all to all", all, all, sharing.SpecEqual},
		{"all to a list", all, func(p *sharing.Policy) {
			p.Fields = map[string][]string{"contact": {"title"}, "*": {"title", "content", "graph"}}
		}, sharing.SpecNarrower},
		{"a list to all", func(*sharing.Policy) {}, all, sharing.SpecWider},
		{"one type to all", func(*sharing.Policy) {}, func(p *sharing.Policy) { p.Fields["contact"] = []string{sharing.FieldAll} }, sharing.SpecWider},
		{"one type from all", func(p *sharing.Policy) { p.Fields["contact"] = []string{sharing.FieldAll} }, func(*sharing.Policy) {}, sharing.SpecNarrower},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sharing.CompareSpecs(clsWith(tc.old), clsWith(tc.new))
			if got.Order != tc.want {
				t.Errorf("got %v (%s), want %v", got.Order, got.Component, tc.want)
			}
		})
	}
}

func TestCompareSpecs_InvalidOldIsWider(t *testing.T) {
	old := clsWith(func(p *sharing.Policy) { p.Data = "" })
	if got := sharing.CompareSpecs(old, clsBase()); got.Order != sharing.SpecWider || got.Component != "spec" {
		t.Errorf("got %v/%q, want wider/spec", got.Order, got.Component)
	}
}

// clsFilters builds random RSQL filters over the policy fields, for
// the property tests.
type clsFilters struct{ r *rand.Rand }

func (g clsFilters) comparison() string {
	fields := []string{"type", "tag", "source", "profile", "subtype"}
	values := []string{"note", "task", "work", "client", "mail", "home"}
	f := fields[g.r.Intn(len(fields))]
	switch g.r.Intn(3) {
	case 0:
		return f + "==" + values[g.r.Intn(len(values))]
	case 1:
		return f + "!=" + values[g.r.Intn(len(values))]
	default:
		a, b := values[g.r.Intn(len(values))], values[g.r.Intn(len(values))]
		return f + "=in=(" + a + "," + b + ")"
	}
}

// filter returns a random filter of the given depth.
func (g clsFilters) filter(depth int) string {
	if depth == 0 || g.r.Intn(3) == 0 {
		return g.comparison()
	}
	n := 2 + g.r.Intn(2)
	parts := make([]string, n)
	for i := range parts {
		parts[i] = "(" + g.filter(depth-1) + ")"
	}
	sep := ";"
	if g.r.Intn(2) == 0 {
		sep = ","
	}
	return strings.Join(parts, sep)
}

func clsData(data string) sharing.Policy {
	return clsWith(func(p *sharing.Policy) { p.Data = data })
}

func TestCompareSpecs_DataProperties(t *testing.T) {
	g := clsFilters{r: rand.New(rand.NewSource(82))} //nolint:gosec // deterministic test data
	wider := 0
	for i := 0; i < 500; i++ {
		old := g.filter(3)
		extra := g.filter(2)
		name := fmt.Sprintf("old=%s extra=%s", old, extra)

		// The same filter, spelled again, is equal.
		if got := sharing.CompareSpecs(clsData(old), clsData(old)); got.Order != sharing.SpecEqual {
			t.Fatalf("%s: same filter: got %v", name, got.Order)
		}
		// Wrapping in parentheses changes nothing.
		if got := sharing.CompareSpecs(clsData(old), clsData("("+old+")")); got.Order != sharing.SpecEqual {
			t.Fatalf("%s: parenthesized: got %v", name, got.Order)
		}
		// AND with anything is narrower or equal, on either side.
		for _, data := range []string{"(" + old + ");(" + extra + ")", "(" + extra + ");(" + old + ")"} {
			if got := sharing.CompareSpecs(clsData(old), clsData(data)); got.Order == sharing.SpecWider {
				t.Fatalf("%s: AND %s: got wider", name, data)
			}
		}
		// OR with anything is never proved narrower. It is equal only
		// when the extra branch is already one of the old disjuncts.
		orData := "(" + old + "),(" + extra + ")"
		got := sharing.CompareSpecs(clsData(old), clsData(orData))
		if got.Order == sharing.SpecNarrower {
			t.Fatalf("%s: OR: got narrower", name)
		}
		if got.Order == sharing.SpecWider {
			wider++
		}
	}
	if wider < 400 {
		t.Fatalf("only %d of 500 OR branches were wider; the generator repeats itself", wider)
	}
}

func TestCompareSpecs_ConjunctPermutationIsEqual(t *testing.T) {
	g := clsFilters{r: rand.New(rand.NewSource(10))} //nolint:gosec // deterministic test data
	for i := 0; i < 200; i++ {
		parts := []string{g.comparison(), g.comparison(), g.comparison(), g.comparison()}
		shuffled := slices.Clone(parts)
		g.r.Shuffle(len(shuffled), func(a, b int) { shuffled[a], shuffled[b] = shuffled[b], shuffled[a] })
		for _, sep := range []string{";", ","} {
			old, new := strings.Join(parts, sep), strings.Join(shuffled, sep)
			if got := sharing.CompareSpecs(clsData(old), clsData(new)); got.Order != sharing.SpecEqual {
				t.Fatalf("%q vs %q: got %v, want equal", old, new, got.Order)
			}
		}
	}
}

// TestCompareSpecs_FieldsSoundAgainstCompile checks the field rule
// against what Compile renders: whenever CompareSpecs does not call a
// field change wider, every type renders no field after it that it did
// not render before, with another grant merged into the plan.
func TestCompareSpecs_FieldsSoundAgainstCompile(t *testing.T) {
	r := rand.New(rand.NewSource(7)) //nolint:gosec // deterministic test data
	types := []string{"*", "contact", "note", "task"}
	fieldPool := []string{"title", "summary", "content", "tags", "org"}
	randomFields := func() map[string][]string {
		out := map[string][]string{}
		for _, typ := range types {
			if r.Intn(2) == 0 {
				continue
			}
			if r.Intn(5) == 0 {
				out[typ] = []string{sharing.FieldAll}
				continue
			}
			var fs []string
			for _, f := range fieldPool {
				if r.Intn(2) == 0 {
					fs = append(fs, f)
				}
			}
			if len(fs) == 0 {
				fs = []string{"title"}
			}
			out[typ] = fs
		}
		return out
	}
	checked := 0
	for i := 0; i < 3000; i++ {
		oldP := clsWith(func(p *sharing.Policy) { p.Fields = randomFields() })
		newP := clsWith(func(p *sharing.Policy) { p.Fields = randomFields() })
		if sharing.CompareSpecs(oldP, newP).Order == sharing.SpecWider {
			continue
		}
		checked++
		other := clsWith(func(p *sharing.Policy) { p.Name = "other"; p.Fields = randomFields() })
		before := clsPlanFields(t, other, oldP)
		after := clsPlanFields(t, other, newP)
		for _, typ := range append(types, "event") {
			if !clsSubset(after(typ), before(typ)) {
				t.Fatalf("fields %v -> %v with other grant %v: type %s renders %v after, %v before",
					oldP.Fields, newP.Fields, other.Fields, typ, after(typ), before(typ))
			}
		}
	}
	if checked < 20 {
		t.Fatalf("only %d non-wider samples; the property is not exercised", checked)
	}
}

// clsPlanFields compiles a viewer from grants of other and p and
// returns the fields its plan renders per type.
func clsPlanFields(t *testing.T, other, p sharing.Policy) func(string) []string {
	t.Helper()
	v, err := sharing.Compile(sharing.Input{PrincipalID: "prn_x", Grants: []sharing.Grant{
		{ID: "grt_a", Audience: "a", Policy: other},
		{ID: "grt_b", Audience: "b", Policy: p},
	}})
	if err != nil {
		t.Fatal(err)
	}
	plan := v.Plan()
	return plan.FieldsFor
}

// clsSubset reports whether field list a renders nothing b does not.
func clsSubset(a, b []string) bool {
	if slices.Contains(b, sharing.FieldAll) {
		return true
	}
	if slices.Contains(a, sharing.FieldAll) {
		return false
	}
	for _, f := range a {
		if !slices.Contains(b, f) {
			return false
		}
	}
	return true
}
