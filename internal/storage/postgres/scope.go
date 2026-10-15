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
