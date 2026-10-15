package postgres

import (
	"fmt"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Scoped reads (ADR-080 §4, §6). A store bound to a storage.Scope ANDs
// the scope into every read: object reads take the predicate directly
// (the objects table is the only one in their FROM), every other
// table joins visibility through visibleObjects, the subquery of the
// ids the scope selects. The scope's ? placeholders are renumbered
// into the query's $N sequence. Writes are unchanged. The stores are
// copied on binding, so the driver's own store stays unscoped.

// Scoped implements storage.ObjectScoper.
func (s *ObjectStore) Scoped(sc storage.Scope) storage.ObjectStore {
	c := *s
	c.scope = &sc
	return &c
}

// Scoped implements storage.EntityScoper.
func (s *EntityStore) Scoped(sc storage.Scope) storage.EntityStore {
	c := *s
	c.scope = &sc
	return &c
}

// Scoped implements storage.EdgeScoper.
func (s *EdgeStore) Scoped(sc storage.Scope) storage.EdgeStore {
	c := *s
	c.scope = &sc
	return &c
}

// Scoped implements storage.EmbeddingScoper.
func (s *EmbeddingStore) Scoped(sc storage.Scope) storage.EmbeddingStore {
	c := *s
	c.scope = &sc
	return &c
}

// Scoped implements storage.ProximityScoper.
func (s *ProximityStore) Scoped(sc storage.Scope) storage.ProximityStore {
	c := *s
	c.scope = &sc
	return &c
}

// Scoped implements storage.AliasScoper.
func (s *AliasStore) Scoped(sc storage.Scope) storage.AliasStore {
	c := *s
	c.scope = &sc
	return &c
}

var (
	_ storage.ObjectScoper    = (*ObjectStore)(nil)
	_ storage.EntityScoper    = (*EntityStore)(nil)
	_ storage.EdgeScoper      = (*EdgeStore)(nil)
	_ storage.EmbeddingScoper = (*EmbeddingStore)(nil)
	_ storage.ProximityScoper = (*ProximityStore)(nil)
	_ storage.AliasScoper     = (*AliasStore)(nil)
)

// rebindFrom renumbers the ? placeholders of a compiled predicate into
// $start, $start+1, ... and returns the next free index. Every ? in a
// compiled predicate is a placeholder: values travel in its args, so
// none sits in a literal.
func rebindFrom(sql string, start int) (string, int) {
	var b strings.Builder
	n := start
	for i := 0; i < len(sql); i++ {
		if sql[i] == '?' {
			fmt.Fprintf(&b, "$%d", n)
			n++
			continue
		}
		b.WriteByte(sql[i])
	}
	return b.String(), n
}

// scopeTerm is the scope's predicate as one parenthesised term for a
// query over the objects table alone, numbered from *idx, which it
// advances; empty with no scope.
func scopeTerm(sc *storage.Scope, idx *int) (string, []any) {
	if sc == nil {
		return "", nil
	}
	sql, next := rebindFrom(sc.SQL, *idx)
	*idx = next
	return "(" + sql + ")", append([]any(nil), sc.Args...)
}

// visibleObjects is the subquery of the object ids sc selects, numbered
// from *idx, which it advances: "col IN " + visibleObjects keeps the
// rows whose col names a visible object. The subquery's own objects
// table shadows any outer one, so the scope's unqualified columns bind
// to it.
func visibleObjects(sc storage.Scope, idx *int) (string, []any) {
	sql, next := rebindFrom(sc.SQL, *idx)
	*idx = next
	return "(SELECT id FROM objects WHERE " + sql + ")", append([]any(nil), sc.Args...)
}

// andObjectIn is " AND col IN visibleObjects" with its arguments, or
// empty with no scope.
func andObjectIn(col string, sc *storage.Scope, idx *int) (string, []any) {
	if sc == nil {
		return "", nil
	}
	vis, args := visibleObjects(*sc, idx)
	return " AND " + col + " IN " + vis, args
}

// edgeScope is the clause keeping the edges whose both ends are
// visible, " AND (...) AND (...)" with its arguments, numbered from
// *idx; empty with no scope. An object end names a visible object; an
// entity end is visible when the scope admits entities; any other end
// is hidden. Both ends reference one numbering of the subquery.
func edgeScope(sc *storage.Scope, idx *int) (string, []any) {
	if sc == nil {
		return "", nil
	}
	vis, args := visibleObjects(*sc, idx)
	end := func(typ, id string) string {
		object := "(" + typ + " = 'object' AND " + id + " IN " + vis + ")"
		if !sc.Entities {
			return object
		}
		return "(" + typ + " = 'entity' OR " + object + ")"
	}
	return " AND " + end("from_type", "from_id") + " AND " + end("to_type", "to_id"), args
}

// mentionTargetExpr is the SQL expression of the node id a mention
// edge names an entities row by (storage.MentionTarget): its
// ctxt://entity/<slug> URI.
const mentionTargetExpr = "'ctxt://entity/' || entities.slug"

// entityScope is the clause keeping the entities a scoped viewer may
// see (ADR-080 §6): a registry-sourced entity, or a placeholder at
// least one visible object mentions; numbered from *idx; empty with no
// scope. A scope that admits no entities is answered by the callers
// before any query.
func entityScope(sc *storage.Scope, idx *int) (string, []any) {
	if sc == nil {
		return "", nil
	}
	vis, args := visibleObjects(*sc, idx)
	return ` AND (entities.registry_url <> '' OR EXISTS (SELECT 1 FROM edges me
		WHERE me.from_type = 'object' AND me.to_type = 'entity' AND me.edge_type = 'mentions'
		  AND me.to_id = ` + mentionTargetExpr + ` AND me.from_id IN ` + vis + `))`, args
}
