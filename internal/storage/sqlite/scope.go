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

// Scoped implements storage.EmbeddingScoper.
func (s *EmbeddingStore) Scoped(sc storage.Scope) storage.EmbeddingStore {
	c := *s
	c.scope = &sc
	return &c
}

var (
	_ storage.ObjectScoper    = (*ObjectStore)(nil)
	_ storage.EmbeddingScoper = (*EmbeddingStore)(nil)
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
