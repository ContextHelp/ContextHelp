package sqlite

import (
	"context"
	"fmt"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Scoped reads (ADR-080 §4, §6). A store bound to a storage.Scope ANDs
// the scope into every read: object reads take the predicate directly
// (the objects table is the only one in their FROM), every other
// table joins visibility through visibleObjects, the subquery of the
// ids the scope selects. Writes are unchanged. The stores are copied
// on binding, so the driver's own store stays unscoped.

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
func (s *aliasStore) Scoped(sc storage.Scope) storage.AliasStore {
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
	_ storage.AliasScoper     = (*aliasStore)(nil)
)

// visibleObjects is the subquery of the object ids sc selects, with
// its arguments: "col IN " + visibleObjects keeps the rows whose col
// names a visible object.
func visibleObjects(sc storage.Scope) (string, []any) {
	return "(SELECT id FROM objects WHERE " + sc.SQL + ")", append([]any(nil), sc.Args...)
}

// scopeTerm is the scope's predicate as one parenthesised term for a
// query over the objects table alone, with its arguments; empty with
// no scope.
func scopeTerm(sc *storage.Scope) (string, []any) {
	if sc == nil {
		return "", nil
	}
	return "(" + sc.SQL + ")", append([]any(nil), sc.Args...)
}

// andObjectIn is " AND col IN visibleObjects" with its arguments, or
// empty with no scope.
func andObjectIn(col string, sc *storage.Scope) (string, []any) {
	if sc == nil {
		return "", nil
	}
	vis, args := visibleObjects(*sc)
	return " AND " + col + " IN " + vis, args
}

// edgeEndVisible is the predicate keeping an edge whose end (typ, id)
// is visible: an object end names a visible object; an entity end is
// visible when the scope admits entities; any other end is hidden.
func edgeEndVisible(typ, id string, sc storage.Scope) (string, []any) {
	vis, args := visibleObjects(sc)
	object := "(" + typ + " = 'object' AND " + id + " IN " + vis + ")"
	if !sc.Entities {
		return object, args
	}
	return "(" + typ + " = 'entity' OR " + object + ")", args
}

// edgeScope is the clause keeping the edges whose both ends are
// visible, " AND (...) AND (...)" with its arguments; empty with no
// scope.
func edgeScope(sc *storage.Scope) (string, []any) {
	if sc == nil {
		return "", nil
	}
	from, fromArgs := edgeEndVisible("from_type", "from_id", *sc)
	to, toArgs := edgeEndVisible("to_type", "to_id", *sc)
	return " AND " + from + " AND " + to, append(fromArgs, toArgs...)
}

// mentionTargetExpr is the SQL expression of the node ids a mention
// edge may name an entities row by: its ctxt://entity/<slug> URI
// (storage.MentionTarget, what every writer stores) or its bare slug,
// which older rows and fixtures carry.
const mentionTargetExpr = "('ctxt://entity/' || entities.slug, entities.slug)"

// entityScope is the clause keeping the entities a scoped viewer may
// see (ADR-080 §6): a registry-sourced entity, or a placeholder at
// least one visible object mentions; empty with no scope. A scope that
// admits no entities is answered by the callers before any query.
func entityScope(sc *storage.Scope) (string, []any) {
	if sc == nil {
		return "", nil
	}
	vis, args := visibleObjects(*sc)
	return ` AND (entities.registry_url <> '' OR EXISTS (SELECT 1 FROM edges me
		WHERE me.from_type = 'object' AND me.to_type = 'entity' AND me.edge_type = 'mentions'
		  AND me.to_id IN ` + mentionTargetExpr + ` AND me.from_id IN ` + vis + `))`, args
}

// filterVisible keeps, in order, the hits whose object the store's
// scope selects: one IN-list query per embeddingsByIDChunk ids.
func (s *EmbeddingStore) filterVisible(ctx context.Context, hits []storage.EmbeddingHit) ([]storage.EmbeddingHit, error) {
	if s.scope == nil || len(hits) == 0 {
		return hits, nil
	}
	ids := make([]string, 0, len(hits))
	seen := map[string]bool{}
	for _, h := range hits {
		if !seen[h.ObjectID] {
			seen[h.ObjectID] = true
			ids = append(ids, h.ObjectID)
		}
	}
	visible := map[string]bool{}
	for start := 0; start < len(ids); start += embeddingsByIDChunk {
		if err := s.visibleChunk(ctx, ids[start:min(start+embeddingsByIDChunk, len(ids))], visible); err != nil {
			return nil, fmt.Errorf("visible objects: %w", err)
		}
	}
	out := hits[:0:0]
	for _, h := range hits {
		if visible[h.ObjectID] {
			out = append(out, h)
		}
	}
	return out, nil
}

// visibleChunk marks in out the ids of chunk the store's scope selects.
func (s *EmbeddingStore) visibleChunk(ctx context.Context, chunk []string, out map[string]bool) error {
	args := make([]any, 0, len(chunk)+len(s.scope.Args))
	for _, id := range chunk {
		args = append(args, id)
	}
	args = append(args, s.scope.Args...)
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(chunk)), ",")
	// #nosec G202 -- placeholders are literal "?" markers and the scope is compiled SQL; every value is bound.
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM objects WHERE id IN (`+placeholders+`) AND (`+s.scope.SQL+`)`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		out[id] = true
	}
	return rows.Err()
}
