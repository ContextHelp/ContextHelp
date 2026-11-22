package sharing

import (
	"context"
	"crypto/sha256"
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
	// Node, when set, is the grant's data predicate and replaces the one
	// Policy.Data parses to. Only code that builds a predicate itself sets it
	// (the record-level reads of an external authority): a stored policy's
	// data filter always goes through Policy.DataNode and its field check.
	Node search.Node
	// Attest is extra key material for a grant whose policy is derived
	// rather than stored: a steward grant's scope digest and the
	// settings applied to it. It joins the policy key, so a change in
	// what the policy was derived from is a new key.
	Attest string
}

// dataNode is the grant's data predicate.
func (g Grant) dataNode() (search.Node, error) {
	if g.Node != nil {
		return g.Node, nil
	}
	return g.Policy.DataNode()
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
	// Records are the source keys (bos addresses) of the records an external
	// authority lets the principal read now through record-level entries,
	// sorted. They are one more grant of the principal, bound to the source
	// the authority ingests under (RecordSource), so everything that narrows
	// a grant (ceiling, key policy, exclusions) narrows them too. The caller
	// evaluates the entries' dates and the set's freshness at the moment of
	// the read; Compile only turns the result into a predicate.
	Records []string
	// StewardGrant names the steward grant this input is the elevation
	// of (ADR-083 §3); "" for an ordinary viewer. Every one of an
	// elevation's Grants is that steward grant's (one per spec of its
	// scope, in order), and there is at least one.
	StewardGrant string
}

// GrantPlan is one grant's share of the render plan: what the grant
// shows of an object it covers. A grant covers an object when the
// object matches its data filter and carries none of its excluded
// classes; the scoped driver asks the database which grants cover each
// row a read released (ObjectPlan), with the same compiled predicates
// the viewer's own predicate is built from.
type GrantPlan struct {
	ID       string
	Audience string
	Policy   string
	Version  int
	// Data is the grant's data filter.
	Data           search.Node
	ExcludeClasses []string
	Fields         map[string][]string
	Mask           []string
	Capabilities   Capabilities
}

// RenderPlan is what the render layer (ADR-080 §5) applies to the rows
// the predicate let through. Viewer.Plan is the plan over every
// applying grant; an object renders under ObjectPlan, the plan over
// the grants that cover it alone, so a grant shows its fields only of
// the objects it covers.
type RenderPlan struct {
	// Grants are the applying grants, ordered by id.
	Grants []GrantPlan
	// Fields is, per type, the union of the fields the grants the plan
	// is over render for that type (each grant's own entry, else its "*"
	// entry), narrowed to what the key policy renders when one is
	// bound. A type with no entry falls back to "*"; a type with neither
	// shows only what is always kept (IDs, type, created_at). Viewer.Plan
	// holds the union over every grant: the most any one object renders.
	Fields map[string][]string
	// DenyClasses is the ceiling, applied to parts in the render layer.
	DenyClasses []string
	// Capabilities is the union across every applying grant, ANDed with
	// the key policy's: the request-level gate that opens a surface. A
	// surface a capability opens answers only the objects a grant
	// holding it covers (Viewer.ForCapability).
	Capabilities Capabilities
	// Masked lists the categories every grant the plan is over masks (a
	// category stays masked only if every covering grant masks it).
	// Inert until span masking lands.
	Masked []string
	// PartExclusions are the classes the render layer drops parts
	// (graph nodes, classed fields) for: the ceiling, every applying
	// grant's exclusions and the key policy's, whichever grants cover
	// the object. Exclusions decide rows per grant (a grant covers no
	// object of a class it excludes); a part of an object is withheld
	// when any applying policy excludes its class (fail closed).
	PartExclusions []string
}

// FieldsFor returns the fields the plan renders for type typ.
func (p RenderPlan) FieldsFor(typ string) []string {
	if f, ok := p.Fields[typ]; ok {
		return f
	}
	return p.Fields["*"]
}

// ExcludesAny reports whether any of classes is in PartExclusions.
func (p RenderPlan) ExcludesAny(classes []string) bool {
	for _, c := range classes {
		if slices.Contains(p.PartExclusions, c) {
			return true
		}
	}
	return false
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
	// grants are the applying grants in plan order; grantNodes each
	// one's (data AND NOT excluded); grantSQL the same per dialect with
	// ? placeholders, the predicates ObjectPlan's attribution runs.
	grants     []Grant
	grantNodes []search.Node
	grantSQL   map[search.Dialect][]storage.SQLPredicate
	// keyPolicy and keyNode are the bound key policy and its predicate,
	// nil when none is bound.
	keyPolicy *Policy
	keyNode   search.Node
	// steward is the steward grant this viewer is the elevation of, ""
	// for an ordinary viewer.
	steward string
}

type predicate struct {
	sql  string
	args []any
}

// Compile builds a principal's viewer from in (ADR-080 §2): the union
// over grants of (data AND NOT excluded), ANDed with NOT denied for the
// ceiling, ANDed with the key policy's own (data AND NOT excluded); the
// render plan over every grant, narrowed by the key policy, and each
// grant's own predicate, which attributes a released object to the
// grants covering it (ObjectPlan); and the cache key over every policy
// version. No grants compiles to a viewer that matches nothing.
func Compile(in Input) (Viewer, error) {
	if in.PrincipalID == "" {
		return Viewer{}, errors.New("compile viewer: principal id is required")
	}
	v := &view{
		principal: in.PrincipalID, identity: in.IdentityID,
		sql: map[search.Dialect]predicate{}, unnumbered: map[search.Dialect]predicate{},
		grantSQL: map[search.Dialect][]storage.SQLPredicate{},
	}
	grants := withRecordGrant(in)
	sort.SliceStable(grants, func(i, j int) bool { return grants[i].ID < grants[j].ID })
	v.grants = grants
	if in.StewardGrant != "" {
		// An elevated viewer is its steward grant and nothing else: never
		// a merge with the principal's ordinary grants. The grant holds a
		// Grant per spec of its scope, each with the steward grant's ID.
		if len(grants) == 0 {
			return Viewer{}, fmt.Errorf("compile viewer: an elevation to %s holds no grant", in.StewardGrant)
		}
		for _, g := range grants {
			if g.ID != in.StewardGrant {
				return Viewer{}, fmt.Errorf("compile viewer: an elevation to %s holds only that grant's specs, got grant %s", in.StewardGrant, g.ID)
			}
		}
		v.steward = in.StewardGrant
	}

	union := make([]search.Node, 0, len(grants))
	for _, g := range grants {
		node, err := g.dataNode()
		if err != nil {
			return Viewer{}, fmt.Errorf("compile viewer: grant %s: policy %s: %w", g.ID, g.Policy.Name, err)
		}
		covers := andNot(node, g.Policy.ExcludeClasses)
		union = append(union, covers)
		for _, d := range dialects {
			sql, args, err := search.CompileUnnumbered(d, covers, nil)
			if err != nil {
				return Viewer{}, fmt.Errorf("compile viewer: grant %s: %w", g.ID, err)
			}
			v.grantSQL[d] = append(v.grantSQL[d], storage.SQLPredicate{SQL: sql, Args: args})
		}
		v.plan.Grants = append(v.plan.Grants, GrantPlan{
			ID: g.ID, Audience: g.Audience, Policy: g.Policy.Name, Version: g.Policy.Version,
			Data: node, ExcludeClasses: storage.NormalizeClasses(g.Policy.ExcludeClasses),
			Fields: g.Policy.Fields, Mask: g.Policy.Redact.Mask, Capabilities: g.Policy.Capabilities,
		})
	}
	v.grantNodes = union
	v.plan.DenyClasses = storage.NormalizeClasses(in.DenyClasses)
	v.plan.Fields, v.plan.Capabilities, v.plan.Masked = unionPlan(grants)
	exclusions := slices.Clone(in.DenyClasses)
	for _, g := range grants {
		exclusions = append(exclusions, g.Policy.ExcludeClasses...)
	}

	if in.KeyPolicy != nil {
		node, err := in.KeyPolicy.DataNode()
		if err != nil {
			return Viewer{}, fmt.Errorf("compile viewer: key policy %s: %w", in.KeyPolicy.Name, err)
		}
		key := *in.KeyPolicy
		v.keyPolicy, v.keyNode = &key, andNot(node, in.KeyPolicy.ExcludeClasses)
		v.plan.Fields = intersectFields(v.plan.Fields, *in.KeyPolicy)
		v.plan.Capabilities = intersectCapabilities(v.plan.Capabilities, in.KeyPolicy.Capabilities)
		exclusions = append(exclusions, in.KeyPolicy.ExcludeClasses...)
	}
	v.plan.PartExclusions = storage.NormalizeClasses(exclusions)

	if err := v.compilePredicate(union); err != nil {
		return Viewer{}, err
	}
	v.key = policyKey(in, grants)
	return Viewer{view: v}, nil
}

// dialects are the SQL dialects a viewer's predicates are compiled for.
var dialects = []search.Dialect{search.DialectSQLite, search.DialectPostgres}

// compilePredicate sets v's predicate to the union of grant nodes,
// ANDed with the ceiling and the key policy, in every dialect; no node
// leaves v matching nothing.
func (v *view) compilePredicate(union []search.Node) error {
	v.node = nil
	v.sql, v.unnumbered = map[search.Dialect]predicate{}, map[search.Dialect]predicate{}
	if len(union) == 0 {
		return nil
	}
	v.node = buildPredicate(union, v.plan.DenyClasses, v.keyNode)
	for _, d := range dialects {
		sql, args, err := search.CompileFor(d, v.node)
		if err != nil {
			return fmt.Errorf("compile viewer: %w", err)
		}
		v.sql[d] = predicate{sql: sql, args: args}
		unnumbered, uargs, err := search.CompileUnnumbered(d, v.node, nil)
		if err != nil {
			return fmt.Errorf("compile viewer: %w", err)
		}
		v.unnumbered[d] = predicate{sql: unnumbered, args: uargs}
	}
	return nil
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

// unionPlan unions grants' fields per type (mergeFields) and their
// capabilities, and intersects their mask lists.
func unionPlan(grants []Grant) (map[string][]string, Capabilities, []string) {
	var caps Capabilities
	var masked []string
	for i, g := range grants {
		caps = unionCapabilities(caps, g.Policy.Capabilities)
		if i == 0 {
			masked = slices.Clone(g.Policy.Redact.Mask)
		} else {
			masked = intersect(masked, g.Policy.Redact.Mask)
		}
	}
	return mergeFields(grants), caps, storage.NormalizeClasses(masked)
}

// mergeFields unions, for every type any of grants names (the "*"
// entry included), the fields each grant renders for that type: its own
// entry, else its "*" entry, as Policy.FieldsFor resolves it. A grant
// that names only "*" thus adds its fields to a type another grant
// names. A type one grant projects every field of (FieldAll) is every
// field in the union; nil when no grant renders anything.
func mergeFields(grants []Grant) map[string][]string {
	fields := map[string][]string{}
	for _, g := range grants {
		for typ := range g.Policy.Fields {
			if _, done := fields[typ]; done {
				continue
			}
			var fs []string
			for _, h := range grants {
				for _, f := range h.Policy.FieldsFor(typ) {
					if !slices.Contains(fs, f) {
						fs = append(fs, f)
					}
				}
			}
			if slices.Contains(fs, FieldAll) {
				fs = []string{FieldAll}
			}
			fields[typ] = fs
		}
	}
	for typ, fs := range fields {
		if len(fs) == 0 {
			delete(fields, typ)
		}
	}
	if len(fields) == 0 {
		return nil
	}
	return fields
}

// intersectFields keeps, per type of fields, the fields key renders for
// that type; a type key renders nothing for is dropped. FieldAll on
// either side yields the other side's list.
func intersectFields(fields map[string][]string, key Policy) map[string][]string {
	out := map[string][]string{}
	for typ, fs := range fields {
		allowed := key.FieldsFor(typ)
		var kept []string
		switch {
		case slices.Contains(allowed, FieldAll):
			kept = slices.Clone(fs)
		case slices.Contains(fs, FieldAll):
			kept = slices.Clone(allowed)
		default:
			kept = intersect(fs, allowed)
		}
		if len(kept) > 0 {
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

// policyKey is the viewer's cache key: the principal, the steward
// grant an elevation is of, its ceiling, its key policy at its version
// and every applying grant with its policy version, so any grant,
// policy edit, membership, ceiling or key change yields a new key, and
// an ordinary viewer and an elevated one never share a key.
func policyKey(in Input, grants []Grant) string {
	var b strings.Builder
	fmt.Fprintf(&b, "principal=%s", in.PrincipalID)
	if in.StewardGrant != "" {
		fmt.Fprintf(&b, ";steward=%s", in.StewardGrant)
	}
	fmt.Fprintf(&b, ";deny=%s", strings.Join(storage.NormalizeClasses(in.DenyClasses), ","))
	if in.KeyPolicy != nil {
		fmt.Fprintf(&b, ";key=%s@%d", in.KeyPolicy.Name, in.KeyPolicy.Version)
	}
	for _, g := range grants {
		fmt.Fprintf(&b, ";grant=%s:%s@%d", g.ID, g.Policy.Name, g.Policy.Version)
		if g.Attest != "" {
			fmt.Fprintf(&b, "#%s", g.Attest)
		}
	}
	if len(in.Records) > 0 {
		sum := sha256.Sum256([]byte(strings.Join(in.Records, "\n")))
		fmt.Fprintf(&b, ";records=%d:%x", len(in.Records), sum[:8])
	}
	return b.String()
}

// CompileFor reads principalID's active grants at now, its ceiling and
// the policy identityID is bound to (when identityID is set) from st,
// and compiles its viewer. An unknown principal is storage.ErrNotFound.
func CompileFor(ctx context.Context, st storage.SharingStore, principalID, identityID string, now time.Time) (Viewer, error) {
	in, err := loadInput(ctx, st, principalID, identityID, now)
	if err != nil {
		return Viewer{}, err
	}
	return Compile(in)
}

// loadInput reads what Compile needs for principalID at now from st:
// its ceiling, its active grants with their policies, and the policy
// identityID is bound to when identityID is set. An unknown principal
// or identity is storage.ErrNotFound.
func loadInput(ctx context.Context, st storage.SharingStore, principalID, identityID string, now time.Time) (Input, error) {
	deny, err := st.DenyClasses(ctx, principalID)
	if err != nil {
		return Input{}, fmt.Errorf("compile viewer %s: ceiling: %w", principalID, err)
	}
	active, err := st.Grants().ListActiveForPrincipal(ctx, principalID, now)
	if err != nil {
		return Input{}, fmt.Errorf("compile viewer %s: grants: %w", principalID, err)
	}
	in := Input{PrincipalID: principalID, IdentityID: identityID, DenyClasses: deny}
	for _, ag := range active {
		p, err := PolicyFromSpec(&ag.Policy)
		if err != nil {
			return Input{}, fmt.Errorf("compile viewer %s: grant %s: %w", principalID, ag.Grant.ID, err)
		}
		in.Grants = append(in.Grants, Grant{ID: ag.Grant.ID, Audience: ag.Audience.Name, Policy: p})
	}
	keyPolicy, err := loadKeyPolicy(ctx, st, principalID, identityID)
	if err != nil {
		return Input{}, err
	}
	in.KeyPolicy = keyPolicy
	return in, nil
}

// loadKeyPolicy reads the policy identityID is bound to, nil when
// identityID is empty or unbound. An unknown identity or policy is
// storage.ErrNotFound.
func loadKeyPolicy(ctx context.Context, st storage.SharingStore, principalID, identityID string) (*Policy, error) {
	if identityID == "" {
		return nil, nil
	}
	policyID, err := st.IdentityPolicy(ctx, identityID)
	if err != nil {
		return nil, fmt.Errorf("compile viewer %s: identity %s: %w", principalID, identityID, err)
	}
	if policyID == "" {
		return nil, nil
	}
	row, err := st.Policies().Get(ctx, policyID)
	if err != nil {
		return nil, fmt.Errorf("compile viewer %s: key policy %s: %w", principalID, policyID, err)
	}
	p, err := PolicyFromSpec(row)
	if err != nil {
		return nil, fmt.Errorf("compile viewer %s: %w", principalID, err)
	}
	return &p, nil
}

// Key returns the policy key Compile would give in: its principal,
// ceiling, key policy and every grant with its policy version, in
// grant order. It is what a cache of compiled viewers keys on.
func (in Input) Key() string {
	grants := withRecordGrant(in)
	sort.SliceStable(grants, func(i, j int) bool { return grants[i].ID < grants[j].ID })
	return policyKey(in, grants)
}
