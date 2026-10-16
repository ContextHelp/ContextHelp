// Package sharing carries per-viewer visibility through a request
// (ADR-080). A Viewer says what the caller may see; storage reads are
// scoped by it and the render layer masks by it.
//
// The package must stay free of internal/auth imports: transports
// resolve a principal (auth) and then attach a Viewer (sharing), so
// auth never depends on sharing and sharing never depends on auth.
package sharing

import (
	"context"
	"slices"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Viewer is what ScopedDriver sees of a viewer.
var _ storage.Viewer = Viewer{}

// Viewer is what a request's caller is allowed to see.
//
// The zero Viewer is the most restrictive value: nothing visible. It
// is what a context holds when no transport attached a viewer, and
// what an unresolved or revoked principal resolves to, so a forgotten
// attach fails closed. Unrestricted is the only value that bypasses
// visibility and it must always be chosen explicitly. Every other
// viewer comes from Compile: it carries the predicate the scoped
// driver ANDs into object reads, the render plan and the policy key.
//
// Fields are unexported on purpose; the compiled view sits behind a
// pointer so the value stays comparable.
type Viewer struct {
	unrestricted bool
	view         *view
}

// Unrestricted sees everything. It is the owner's viewer, and the
// viewer of every caller on a private instance, where every request is
// the owner (ADR-079 §1). Attach it only where the caller's identity
// has been established; never as a default.
var Unrestricted = Viewer{unrestricted: true}

// IsUnrestricted reports whether v bypasses visibility entirely. The
// zero Viewer and every restricted viewer answer false.
func (v Viewer) IsUnrestricted() bool { return v.unrestricted }

// PrincipalID is the principal v was compiled for; empty for the zero
// viewer and for Unrestricted.
func (v Viewer) PrincipalID() string {
	if v.view == nil {
		return ""
	}
	return v.view.principal
}

// IdentityID is the credential v was compiled for, when one was named.
func (v Viewer) IdentityID() string {
	if v.view == nil {
		return ""
	}
	return v.view.identity
}

// PolicyKey is the cache key of the compiled view: it changes with any
// grant, policy version, membership, ceiling or key binding. It is
// "unrestricted" for Unrestricted and empty for the zero viewer.
func (v Viewer) PolicyKey() string {
	switch {
	case v.unrestricted:
		return "unrestricted"
	case v.view == nil:
		return ""
	}
	return v.view.key
}

// MatchesNothing reports whether no object is visible to v: the zero
// viewer, and a compiled viewer with no applying grant.
func (v Viewer) MatchesNothing() bool {
	return !v.unrestricted && (v.view == nil || v.view.node == nil)
}

// Predicate returns the SQL the scoped driver ANDs into every object
// read for v, in dialect d, with its bound arguments (Postgres
// placeholders numbered from $1, as ListBySQL takes them). A viewer that
// matches nothing yields MatchNothing. Unrestricted has no predicate
// and yields ""; callers check IsUnrestricted first.
func (v Viewer) Predicate(d search.Dialect) (string, []any) {
	if v.unrestricted {
		return "", nil
	}
	if v.MatchesNothing() {
		return MatchNothing, nil
	}
	p := v.view.sql[d]
	return p.sql, append([]any(nil), p.args...)
}

// ObjectPredicate implements storage.Viewer: the predicate of v for the
// storage dialect name (storage.SQLDialectPostgres or SQLite), with ?
// placeholders in either dialect, for the drivers' scoped reads to
// number themselves. A viewer that matches nothing yields MatchNothing;
// Unrestricted yields "".
func (v Viewer) ObjectPredicate(dialect string) (string, []any) {
	if v.unrestricted {
		return "", nil
	}
	if v.MatchesNothing() {
		return MatchNothing, nil
	}
	d := search.DialectSQLite
	if dialect == storage.SQLDialectPostgres {
		d = search.DialectPostgres
	}
	p := v.view.unnumbered[d]
	return p.sql, append([]any(nil), p.args...)
}

// Allows implements storage.Viewer: whether v holds capability c
// (storage.CapabilityEntities, "search:fts", ...). Unrestricted holds
// every capability; the zero viewer none.
func (v Viewer) Allows(c string) bool {
	if v.unrestricted {
		return true
	}
	if v.view == nil {
		return false
	}
	caps := v.view.plan.Capabilities
	if mode, ok := strings.CutPrefix(c, storage.CapabilitySearchPrefix); ok {
		return slices.Contains(caps.Search, mode)
	}
	switch c {
	case storage.CapabilityEntities:
		return caps.Entities
	case storage.CapabilityExport:
		return caps.Export
	case storage.CapabilityMCP:
		return caps.MCP
	case storage.CapabilityEvents:
		return caps.Events
	}
	return false
}

// PredicateNode returns the predicate's AST, for callers that compile
// it into a larger statement themselves; nil when v has no predicate
// (Unrestricted) or matches nothing.
func (v Viewer) PredicateNode() search.Node {
	if v.view == nil {
		return nil
	}
	return v.view.node
}

// Plan returns v's render plan; the zero plan for the zero viewer and
// for Unrestricted.
func (v Viewer) Plan() RenderPlan {
	if v.view == nil {
		return RenderPlan{}
	}
	return v.view.plan
}

type ctxKey struct{}

// WithViewer returns a context carrying v. Transports call it right
// after authentication, in the same step that attaches the principal,
// so a request can never reach a handler authenticated but unscoped.
func WithViewer(ctx context.Context, v Viewer) context.Context {
	return context.WithValue(ctx, ctxKey{}, v)
}

// FromContext returns the viewer attached to ctx and whether one was
// attached at all. A context without a viewer yields the zero Viewer,
// never Unrestricted: callers that read a missing viewer see nothing.
func FromContext(ctx context.Context) (Viewer, bool) {
	v, ok := ctx.Value(ctxKey{}).(Viewer)
	return v, ok
}
