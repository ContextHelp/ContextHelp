package storage

import "errors"

// The SQL dialect names a driver reports through SQLDialect(); a driver
// that reports none is SQLite.
const (
	SQLDialectSQLite   = "sqlite"
	SQLDialectPostgres = "postgres"
)

// Capabilities a viewer may hold (ADR-080 §2); the search modes are
// "search:<mode>".
const (
	CapabilityEntities = "entities"
	CapabilityExport   = "export"
	CapabilityMCP      = "mcp"
	CapabilityEvents   = "events"
	// CapabilitySearchPrefix precedes a search mode: "search:fts",
	// "search:vector", "search:graph", "search:rsql".
	CapabilitySearchPrefix = "search:"
)

// Viewer is what ScopedDriver needs to know about the caller: whether it
// bypasses visibility, and otherwise the predicate selecting the objects
// it may read, the capabilities it holds and the principal it is.
// sharing.Viewer satisfies it; the interface lives here so storage never
// imports sharing, which imports the search compiler (and so storage).
// A nil Viewer is the most restrictive caller: every read refuses.
type Viewer interface {
	IsUnrestricted() bool
	// PrincipalID is the principal the viewer was compiled for; the
	// per-principal rows (saved searches, search history, the
	// resurfacing queue) are read and written as it.
	PrincipalID() string
	// ObjectPredicate returns the SQL selecting the objects the viewer
	// may read, over one objects row's columns, for dialect
	// (SQLDialectSQLite or SQLDialectPostgres), with ? placeholders in
	// either dialect and its bound arguments. The drivers renumber the
	// placeholders when they must. An empty predicate selects nothing.
	ObjectPredicate(dialect string) (string, []any)
	// Allows reports whether the viewer holds capability c
	// (CapabilityEntities, "search:fts", ...).
	Allows(c string) bool
}

// Scope is a restricted viewer's visibility as the drivers apply it
// (ADR-080 §4, §6): the predicate selecting the objects it may read,
// whether it may see entities at all, and the principal it is.
type Scope struct {
	// SQL is a predicate over one objects row's columns, unqualified,
	// with ? placeholders in every dialect. It always selects active
	// objects only. A driver that numbers placeholders renumbers it;
	// a driver that joins objects to another table wraps it as
	// "id IN (SELECT id FROM objects WHERE <SQL>)".
	SQL  string
	Args []any
	// Entities reports whether the viewer may see entities at all
	// (capabilities.entities). Without it no entity and no edge
	// touching an entity is visible.
	Entities bool
	// Principal is the viewer's principal: per-principal rows are kept
	// to it.
	Principal string
}

// matchNothing is the predicate of a viewer no grant applies to: valid
// SQL on both dialects, selecting no row.
const matchNothing = "(1 = 0)"

// ViewerScope returns the scope ScopedDriver binds the driver's stores
// to for v under dialect, and false when v reads unscoped (an
// unrestricted viewer) or may not read at all (a nil viewer). The scope
// ANDs status = active ahead of the viewer's predicate: a restricted
// viewer never reaches an inbox, raw or discarded object, whatever its
// filter asks for (ADR-080 §4). A viewer with no predicate matches
// nothing: a forgotten compile fails closed.
func ViewerScope(v Viewer, dialect string) (Scope, bool) {
	if v == nil || v.IsUnrestricted() {
		return Scope{}, false
	}
	pred, args := v.ObjectPredicate(dialect)
	if pred == "" {
		pred, args = matchNothing, nil
	}
	sc := Scope{
		SQL:       "status = ? AND (" + pred + ")",
		Args:      append([]any{ObjectStatusActive}, args...),
		Entities:  v.Allows(CapabilityEntities),
		Principal: v.PrincipalID(),
	}
	return sc, true
}

// ErrNoScopedReads is returned by a ScopedDriver read when the driver's
// store cannot bind itself to a scope (it implements none of the
// scoper interfaces below): the read fails closed instead of answering
// unscoped rows. It wraps ErrRestrictedViewer.
var ErrNoScopedReads = errors.New("driver store has no scoped reads")

// The scoper interfaces are implemented by the stores of a driver that
// can answer a restricted viewer's reads: Scoped returns the store
// bound to sc, whose reads AND sc into their SQL and whose writes are
// the driver's own. ScopedDriver binds each store it wraps through
// them; a store that implements none refuses its reads.

// ObjectScoper binds an object store to a scope: Get, List, ListBySQL,
// CountByType, the searches and the content-hash and source-key
// lookups answer only objects the scope selects, a hidden Get is
// ErrNotFound, full-text search runs on the masked index and vector
// search on the masked variant (ADR-080 §7).
type ObjectScoper interface {
	Scoped(sc Scope) ObjectStore
}

// EntityScoper binds an entity store to a scope: an entity is visible
// when sc.Entities holds and it is registry-sourced, or when a visible
// object mentions it (ADR-080 §6).
type EntityScoper interface {
	Scoped(sc Scope) EntityStore
}

// EdgeScoper binds an edge store to a scope: an edge is visible when
// every object it touches is visible and, unless sc.Entities holds, it
// touches no entity; counts and traversals run over visible edges.
type EdgeScoper interface {
	Scoped(sc Scope) EdgeStore
}

// EmbeddingScoper binds an embedding store to a scope: Search ranks
// the masked variant over visible objects only, widening until it has
// its k or the index is exhausted.
type EmbeddingScoper interface {
	Scoped(sc Scope) EmbeddingStore
}

// ProximityScoper binds a proximity store to a scope: neighbours are
// visible when both objects of the pair are.
type ProximityScoper interface {
	Scoped(sc Scope) ProximityStore
}

// AliasScoper binds an alias store to a scope: an alias resolves or
// lists only when its object is visible.
type AliasScoper interface {
	Scoped(sc Scope) AliasStore
}
