package sharing

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// MatchNothing is the predicate of a viewer no grant applies to: it is
// valid SQL on both dialects and selects no row (ADR-080 §2, deny by
// default).
const MatchNothing = "(1 = 0)"

// Grant is one grant that applies to a principal: the policy it carries
// at its current version and the audience it comes through.
type Grant struct {
	ID       string
	Audience string
	Policy   Policy
}

// Input is what Compile builds a viewer from: the principal's active
// grants, its ceiling and the policy its credential narrows to.
type Input struct {
	PrincipalID string
	IdentityID  string
	Grants      []Grant
	// DenyClasses is the principal's ceiling (principals.deny_classes):
	// ANDed after the grant union, whatever the grants say.
	DenyClasses []string
	// KeyPolicy is the share policy the credential is bound to
	// (principal_identities.share_policy_id), nil when unbound. It
	// intersects with the principal's view: keys narrow, never widen.
	KeyPolicy *Policy
}

// GrantPlan is one grant's share of the render plan: what the grant
// shows of an object its data filter matched.
type GrantPlan struct {
	ID       string
	Audience string
	Policy   string
	Version  int
	// Data is the grant's data filter, so the render layer can tell
	// which grants match a given object.
	Data           search.Node
	ExcludeClasses []string
	Fields         map[string][]string
	Mask           []string
	Capabilities   Capabilities
}

// RenderPlan is what the render layer (ADR-080 §5) applies to the rows
// the predicate let through.
type RenderPlan struct {
	// Grants are the applying grants, oldest first.
	Grants []GrantPlan
	// Fields is the union, per type, of the fields the grants render,
	// narrowed to what the key policy renders when one is bound. A type
	// with no entry falls back to "*"; a type with neither shows only
	// what is always kept (IDs, type, created_at).
	Fields map[string][]string
	// DenyClasses is the ceiling, applied to parts in the render layer.
	DenyClasses []string
	// Capabilities is the union across grants, ANDed with the key
	// policy's.
	Capabilities Capabilities
	// Masked lists the categories every applying grant masks (a category
	// stays masked only if every matching grant masks it). Inert until
	// span masking lands.
	Masked []string
}

// FieldsFor returns the fields the plan renders for type typ.
func (p RenderPlan) FieldsFor(typ string) []string {
	if f, ok := p.Fields[typ]; ok {
		return f
	}
	return p.Fields["*"]
}

// view is a compiled restricted viewer.
type view struct {
	principal string
	identity  string
	key       string
	// node is the predicate's AST; nil when the viewer matches nothing.
	node search.Node
	// sql is the predicate per dialect as ListBySQL takes it; unnumbered
	// is the same with ? placeholders in every dialect, for the scoped
	// reads that splice it into their own statements.
	sql        map[search.Dialect]predicate
	unnumbered map[search.Dialect]predicate
	plan       RenderPlan
}

type predicate struct {
	sql  string
	args []any
}

// Compile builds a principal's viewer from in (ADR-080 §2): the union
// over grants of (data AND NOT excluded), ANDed with NOT denied for the
// ceiling, ANDed with the key policy's own (data AND NOT excluded); the
// render plan as the union of the grants' fields and capabilities
// narrowed by the key policy; and the cache key over every policy
// version. No grants compiles to a viewer that matches nothing.
func Compile(in Input) (Viewer, error) {
	if in.PrincipalID == "" {
		return Viewer{}, errors.New("compile viewer: principal id is required")
	}
	v := &view{
		principal: in.PrincipalID, identity: in.IdentityID,
		sql: map[search.Dialect]predicate{}, unnumbered: map[search.Dialect]predicate{},
	}
	grants := slices.Clone(in.Grants)
	sort.SliceStable(grants, func(i, j int) bool { return grants[i].ID < grants[j].ID })

	union := make([]search.Node, 0, len(grants))
	for _, g := range grants {
		node, err := g.Policy.DataNode()
		if err != nil {
			return Viewer{}, fmt.Errorf("compile viewer: grant %s: policy %s: %w", g.ID, g.Policy.Name, err)
		}
		union = append(union, andNot(node, g.Policy.ExcludeClasses))
		v.plan.Grants = append(v.plan.Grants, GrantPlan{
			ID: g.ID, Audience: g.Audience, Policy: g.Policy.Name, Version: g.Policy.Version,
			Data: node, ExcludeClasses: storage.NormalizeClasses(g.Policy.ExcludeClasses),
			Fields: g.Policy.Fields, Mask: g.Policy.Redact.Mask, Capabilities: g.Policy.Capabilities,
		})
	}
	v.plan.DenyClasses = storage.NormalizeClasses(in.DenyClasses)
	v.plan.Fields, v.plan.Capabilities, v.plan.Masked = unionPlan(grants)

	var keyNode search.Node
	if in.KeyPolicy != nil {
		node, err := in.KeyPolicy.DataNode()
		if err != nil {
			return Viewer{}, fmt.Errorf("compile viewer: key policy %s: %w", in.KeyPolicy.Name, err)
		}
		keyNode = andNot(node, in.KeyPolicy.ExcludeClasses)
		v.plan.Fields = intersectFields(v.plan.Fields, *in.KeyPolicy)
		v.plan.Capabilities = intersectCapabilities(v.plan.Capabilities, in.KeyPolicy.Capabilities)
	}

	if len(union) > 0 {
		v.node = buildPredicate(union, v.plan.DenyClasses, keyNode)
		for _, d := range []search.Dialect{search.DialectSQLite, search.DialectPostgres} {
			sql, args, err := search.CompileFor(d, v.node)
			if err != nil {
				return Viewer{}, fmt.Errorf("compile viewer: %w", err)
			}
			v.sql[d] = predicate{sql: sql, args: args}
			unnumbered, uargs, err := search.CompileUnnumbered(d, v.node, nil)
			if err != nil {
				return Viewer{}, fmt.Errorf("compile viewer: %w", err)
			}
			v.unnumbered[d] = predicate{sql: unnumbered, args: uargs}
		}
	}
	v.key = policyKey(in, grants)
	return Viewer{view: v}, nil
}

// buildPredicate ANDs the grant union with the ceiling and the key
// policy's predicate.
func buildPredicate(union []search.Node, deny []string, key search.Node) search.Node {
	var node search.Node = search.OrNode{Children: union}
	if len(union) == 1 {
		node = union[0]
	}
	parts := []search.Node{node}
	if len(deny) > 0 {
		parts = append(parts, classOut(deny))
	}
	if key != nil {
		parts = append(parts, key)
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return search.AndNode{Children: parts}
}

// andNot ANDs node with the exclusion of classes, when any.
func andNot(node search.Node, classes []string) search.Node {
	classes = storage.NormalizeClasses(classes)
	if len(classes) == 0 {
		return node
	}
	return search.AndNode{Children: []search.Node{node, classOut(classes)}}
}

// classOut is the comparison that holds when an object carries none of
// classes.
func classOut(classes []string) search.Node {
	return search.ComparisonNode{Field: "class", Operator: search.OpOut, Value: classes}
}

// unionPlan unions the grants' fields per type, their capabilities and
// intersects their mask lists.
func unionPlan(grants []Grant) (map[string][]string, Capabilities, []string) {
	fields := map[string][]string{}
	var caps Capabilities
	var masked []string
	for i, g := range grants {
		for typ, fs := range g.Policy.Fields {
			for _, f := range fs {
				if !slices.Contains(fields[typ], f) {
					fields[typ] = append(fields[typ], f)
				}
			}
		}
		caps = unionCapabilities(caps, g.Policy.Capabilities)
		if i == 0 {
			masked = slices.Clone(g.Policy.Redact.Mask)
		} else {
			masked = intersect(masked, g.Policy.Redact.Mask)
		}
	}
	if len(fields) == 0 {
		fields = nil
	}
	return fields, caps, storage.NormalizeClasses(masked)
}

// intersectFields keeps, per type of fields, the fields key renders for
// that type; a type key renders nothing for is dropped.
func intersectFields(fields map[string][]string, key Policy) map[string][]string {
	out := map[string][]string{}
	for typ, fs := range fields {
		allowed := key.FieldsFor(typ)
		if kept := intersect(fs, allowed); len(kept) > 0 {
			out[typ] = kept
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// intersect keeps the entries of a that b also lists, in a's order.
func intersect(a, b []string) []string {
	var out []string
	for _, x := range a {
		if slices.Contains(b, x) {
			out = append(out, x)
		}
	}
	return out
}

func unionCapabilities(a, b Capabilities) Capabilities {
	out := Capabilities{
		Search:   slices.Clone(a.Search),
		Entities: a.Entities || b.Entities,
		Export:   a.Export || b.Export,
		MCP:      a.MCP || b.MCP,
		Events:   a.Events || b.Events,
	}
	for _, m := range b.Search {
		if !slices.Contains(out.Search, m) {
			out.Search = append(out.Search, m)
		}
	}
	return out
}

func intersectCapabilities(a, b Capabilities) Capabilities {
	return Capabilities{
		Search:   intersect(a.Search, b.Search),
		Entities: a.Entities && b.Entities,
		Export:   a.Export && b.Export,
		MCP:      a.MCP && b.MCP,
		Events:   a.Events && b.Events,
	}
}

// policyKey is the viewer's cache key: the principal, its ceiling, its
// key policy at its version and every applying grant with its policy
// version, so any grant, policy edit, membership, ceiling or key change
// yields a new key.
func policyKey(in Input, grants []Grant) string {
	var b strings.Builder
	fmt.Fprintf(&b, "principal=%s;deny=%s", in.PrincipalID, strings.Join(storage.NormalizeClasses(in.DenyClasses), ","))
	if in.KeyPolicy != nil {
		fmt.Fprintf(&b, ";key=%s@%d", in.KeyPolicy.Name, in.KeyPolicy.Version)
	}
	for _, g := range grants {
		fmt.Fprintf(&b, ";grant=%s:%s@%d", g.ID, g.Policy.Name, g.Policy.Version)
	}
	return b.String()
}

// CompileFor reads principalID's active grants at now, its ceiling and
// the policy identityID is bound to (when identityID is set) from st,
// and compiles its viewer. An unknown principal is storage.ErrNotFound.
func CompileFor(ctx context.Context, st storage.SharingStore, principalID, identityID string, now time.Time) (Viewer, error) {
	deny, err := st.DenyClasses(ctx, principalID)
	if err != nil {
		return Viewer{}, fmt.Errorf("compile viewer %s: ceiling: %w", principalID, err)
	}
	active, err := st.Grants().ListActiveForPrincipal(ctx, principalID, now)
	if err != nil {
		return Viewer{}, fmt.Errorf("compile viewer %s: grants: %w", principalID, err)
	}
	in := Input{PrincipalID: principalID, IdentityID: identityID, DenyClasses: deny}
	for _, ag := range active {
		p, err := PolicyFromSpec(&ag.Policy)
		if err != nil {
			return Viewer{}, fmt.Errorf("compile viewer %s: grant %s: %w", principalID, ag.Grant.ID, err)
		}
		in.Grants = append(in.Grants, Grant{ID: ag.Grant.ID, Audience: ag.Audience.Name, Policy: p})
	}
	if identityID != "" {
		policyID, err := st.IdentityPolicy(ctx, identityID)
		if err != nil {
			return Viewer{}, fmt.Errorf("compile viewer %s: identity %s: %w", principalID, identityID, err)
		}
		if policyID != "" {
			row, err := st.Policies().Get(ctx, policyID)
			if err != nil {
				return Viewer{}, fmt.Errorf("compile viewer %s: key policy %s: %w", principalID, policyID, err)
			}
			p, err := PolicyFromSpec(row)
			if err != nil {
				return Viewer{}, fmt.Errorf("compile viewer %s: %w", principalID, err)
			}
			in.KeyPolicy = &p
		}
	}
	return Compile(in)
}
