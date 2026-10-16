package sharing

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// SpecOrder is how a new policy spec compares with an old one.
type SpecOrder int

// Spec orders. SpecWider means "not proved equal or narrower": the
// comparison is syntactic and never claims narrowing it cannot show.
const (
	SpecEqual SpecOrder = iota
	SpecNarrower
	SpecWider
)

func (o SpecOrder) String() string {
	switch o {
	case SpecEqual:
		return "equal"
	case SpecNarrower:
		return "narrower"
	default:
		return "wider"
	}
}

// Spec components, as SpecComparison.Component names the one that
// widened.
const (
	SpecComponentSpec           = "spec"
	SpecComponentData           = "data"
	SpecComponentExcludeClasses = "exclude_classes"
	SpecComponentFields         = "fields"
	SpecComponentCapabilities   = "capabilities"
	SpecComponentRedact         = "redact"
)

// SpecComparison is the result of CompareSpecs.
type SpecComparison struct {
	Order SpecOrder
	// Component is the first component, in the order data,
	// exclude_classes, fields, capabilities, redact, that is not equal
	// or narrower; "spec" when a spec does not validate. Empty unless
	// Order is SpecWider.
	Component string
}

// CompareSpecs compares the new spec of a policy with its old one
// (ADR-083 §10 B, "Policy spec comparison"), component by component.
// The result is SpecEqual when every component is equal, SpecNarrower
// when every component is equal or narrower and one is narrower, and
// SpecWider otherwise, including when either spec fails Validate.
//
// The rules, all syntactic:
//
//   - data: both filters are put in canonical form: AND and OR
//     operands flattened (a;(b;c) is a;b;c), sorted and deduplicated,
//     a single operand standing for itself, and =in= / =out= value
//     lists sorted and deduplicated. Equal canonical forms are equal.
//     The new filter is narrower when its conjuncts (the operands of a
//     top-level AND, or the filter itself) include every conjunct of
//     the old one: new = old AND extra in any spelling. Nothing else is
//     proved: removing an OR branch or a list value, or a tighter
//     comparison, is wider.
//   - exclude_classes (storage.NormalizeClasses): narrower when the
//     new list strictly includes the old one.
//   - fields: for every type either spec names ("*" included, which is
//     every other type), the fields the new spec renders (Policy.FieldsFor,
//     so after the "*" type fallback; aliases resolved; FieldAll as
//     every field) are a subset of those the old spec renders. A list
//     that is not FieldAll is never proved to cover FieldAll. Nothing
//     more is asked: an object renders the union, over the grants that
//     cover it, of what each grant renders for the object's type
//     (mergeFields resolves each grant's own entry, else its "*"
//     entry, before the union), so a spec that renders no more of any
//     type renders no more in any merge, and a type entry added or
//     dropped is judged by what the type renders after it.
//   - capabilities: search modes a subset; each of entities, export,
//     mcp and events on only if it was on.
//   - redact (inert today): mask a superset, reveal a subset, style
//     unchanged ("" is RedactCategory).
//
// Name and version are not compared.
func CompareSpecs(old, next Policy) SpecComparison {
	if old.Validate() != nil || next.Validate() != nil {
		return SpecComparison{Order: SpecWider, Component: SpecComponentSpec}
	}
	parts := []struct {
		name  string
		order func(old, next Policy) SpecOrder
	}{
		{SpecComponentData, compareData},
		{SpecComponentExcludeClasses, func(o, n Policy) SpecOrder {
			return compareSets(storage.NormalizeClasses(o.ExcludeClasses), storage.NormalizeClasses(n.ExcludeClasses), true)
		}},
		{SpecComponentFields, compareFields},
		{SpecComponentCapabilities, compareCapabilities},
		{SpecComponentRedact, compareRedact},
	}
	out := SpecComparison{Order: SpecEqual}
	for _, part := range parts {
		switch part.order(old, next) {
		case SpecWider:
			return SpecComparison{Order: SpecWider, Component: part.name}
		case SpecNarrower:
			out.Order = SpecNarrower
		}
	}
	return out
}

// compareSets orders two normalized sets: with superset true the new
// set narrows by growing (exclusions, masks), otherwise by shrinking
// (search modes, reveals).
func compareSets(old, next []string, superset bool) SpecOrder {
	small, large := next, old
	if superset {
		small, large = old, next
	}
	switch {
	case !containsAll(large, small):
		return SpecWider
	case len(small) == len(large):
		return SpecEqual
	default:
		return SpecNarrower
	}
}

// combine is the order of two components together.
func combine(a, b SpecOrder) SpecOrder {
	return max(a, b)
}

func compareData(old, next Policy) SpecOrder {
	oldNode, err := old.DataNode()
	if err != nil {
		return SpecWider
	}
	nextNode, err := next.DataNode()
	if err != nil {
		return SpecWider
	}
	o, n := canonicalNode(oldNode), canonicalNode(nextNode)
	if o.key == n.key {
		return SpecEqual
	}
	if containsAll(n.conjuncts(), o.conjuncts()) {
		return SpecNarrower
	}
	return SpecWider
}

// canonical is a filter in canonical form. Its key is equal for two
// filters exactly when their canonical forms are equal.
type canonical struct {
	key string
	// op is "and" or "or" for a junction, "" for a comparison;
	// operands are then its sorted, distinct operands.
	op       string
	operands []canonical
}

// conjuncts returns the keys of the filter's top-level conjuncts: an
// AND's operands, or the filter itself.
func (c canonical) conjuncts() []string {
	if c.op != "and" {
		return []string{c.key}
	}
	keys := make([]string, len(c.operands))
	for i, o := range c.operands {
		keys[i] = o.key
	}
	return keys
}

// canonicalNode returns node's canonical form.
func canonicalNode(node search.Node) canonical {
	switch n := node.(type) {
	case search.ComparisonNode:
		return canonical{key: canonicalComparison(n)}
	case search.AndNode:
		return canonicalJunction("and", n.Children)
	case search.OrNode:
		return canonicalJunction("or", n.Children)
	default:
		// Unreachable for a validated filter; a key no other node
		// spells keeps it unequal to anything.
		return canonical{key: fmt.Sprintf("unknown(%T)", node)}
	}
}

// canonicalJunction canonicalizes the operands of an AND or OR,
// splices in the operands of an operand that is a junction of the same
// operator, sorts and deduplicates them by key. A single operand left
// stands for itself.
func canonicalJunction(op string, children []search.Node) canonical {
	operands := make([]canonical, 0, len(children))
	for _, child := range children {
		c := canonicalNode(child)
		if c.op == op {
			operands = append(operands, c.operands...)
			continue
		}
		operands = append(operands, c)
	}
	slices.SortFunc(operands, func(a, b canonical) int { return strings.Compare(a.key, b.key) })
	operands = slices.CompactFunc(operands, func(a, b canonical) bool { return a.key == b.key })
	if len(operands) == 1 {
		return operands[0]
	}
	keys := make([]string, len(operands))
	for i, o := range operands {
		keys[i] = o.key
	}
	return canonical{key: op + "(" + strings.Join(keys, ",") + ")", op: op, operands: operands}
}

// canonicalComparison spells a comparison with every part quoted, so
// no two comparisons share a key; list values are sorted and
// deduplicated.
func canonicalComparison(n search.ComparisonNode) string {
	var value string
	switch v := n.Value.(type) {
	case string:
		value = strconv.Quote(v)
	case []string:
		vals := slices.Clone(v)
		slices.Sort(vals)
		vals = slices.Compact(vals)
		quoted := make([]string, len(vals))
		for i, x := range vals {
			quoted[i] = strconv.Quote(x)
		}
		value = "[" + strings.Join(quoted, " ") + "]"
	default:
		value = fmt.Sprintf("%T:%v", v, v)
	}
	return "cmp(" + strconv.Quote(n.Field) + " " + n.Operator.String() + " " + value + ")"
}

func compareFields(old, next Policy) SpecOrder {
	// The "*" entry is a key when either spec names it, and a type
	// neither names renders what "*" renders: comparing the named
	// entries, "*" among them, compares every type.
	types := slices.Concat(slices.Collect(maps.Keys(old.Fields)), slices.Collect(maps.Keys(next.Fields)))
	slices.Sort(types)
	order := SpecEqual
	for _, typ := range slices.Compact(types) {
		order = combine(order, compareFieldSets(newFieldSet(old.FieldsFor(typ)), newFieldSet(next.FieldsFor(typ))))
	}
	return order
}

// compareFieldSets orders the fields one type renders.
func compareFieldSets(old, next fieldSet) SpecOrder {
	switch {
	case old.all && next.all:
		return SpecEqual
	case old.all:
		return SpecNarrower
	case next.all:
		return SpecWider
	}
	return compareSets(slices.Sorted(maps.Keys(old.fields)), slices.Sorted(maps.Keys(next.fields)), false)
}

func compareCapabilities(old, next Policy) SpecOrder {
	modes := func(m []string) []string {
		out := slices.Clone(m)
		slices.Sort(out)
		return slices.Compact(out)
	}
	order := compareSets(modes(old.Capabilities.Search), modes(next.Capabilities.Search), false)
	for _, pair := range [][2]bool{
		{old.Capabilities.Entities, next.Capabilities.Entities},
		{old.Capabilities.Export, next.Capabilities.Export},
		{old.Capabilities.MCP, next.Capabilities.MCP},
		{old.Capabilities.Events, next.Capabilities.Events},
	} {
		order = combine(order, compareFlag(pair[0], pair[1]))
	}
	return order
}

// compareFlag orders a capability switch: on only if it was on.
func compareFlag(old, next bool) SpecOrder {
	switch {
	case old == next:
		return SpecEqual
	case next:
		return SpecWider
	default:
		return SpecNarrower
	}
}

func compareRedact(old, next Policy) SpecOrder {
	style := func(s string) string {
		if s == "" {
			return RedactCategory
		}
		return s
	}
	if style(old.Redact.Style) != style(next.Redact.Style) {
		return SpecWider
	}
	norm := func(c []string) []string {
		out := slices.Clone(c)
		slices.Sort(out)
		return slices.Compact(out)
	}
	return combine(
		compareSets(norm(old.Redact.Mask), norm(next.Redact.Mask), true),
		compareSets(norm(old.Redact.Reveal), norm(next.Redact.Reveal), false),
	)
}
