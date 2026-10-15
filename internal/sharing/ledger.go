package sharing

import (
	"context"
	"slices"
	"strings"
	"sync"
)

// Ledger records, for one request, what the scoped reads released to a
// restricted viewer (ADR-080 §8): every object ID, entity slug and edge
// ID, and the raw values the render layer withheld from each released
// object. The backstop checks a response against it before the
// response leaves the server, and the read audit counts from it. It is
// safe for concurrent use: a request's search legs may read in
// parallel.
type Ledger struct {
	mu       sync.Mutex
	objects  map[string]struct{}
	entities map[string]struct{}
	edges    map[string]struct{}
	// canaries maps a withheld value to its category.
	canaries map[string]string
}

// NewLedger returns an empty ledger.
func NewLedger() *Ledger {
	return &Ledger{
		objects: map[string]struct{}{}, entities: map[string]struct{}{},
		edges: map[string]struct{}{}, canaries: map[string]string{},
	}
}

// AddObject records a released object.
func (l *Ledger) AddObject(id string) {
	if l == nil || id == "" {
		return
	}
	l.mu.Lock()
	l.objects[id] = struct{}{}
	l.mu.Unlock()
}

// AddEntity records a released entity by slug.
func (l *Ledger) AddEntity(slug string) {
	if l == nil || slug == "" {
		return
	}
	l.mu.Lock()
	l.entities[slug] = struct{}{}
	l.mu.Unlock()
}

// AddEdge records a released edge.
func (l *Ledger) AddEdge(id string) {
	if l == nil || id == "" {
		return
	}
	l.mu.Lock()
	l.edges[id] = struct{}{}
	l.mu.Unlock()
}

// AddEndpoint records an edge endpoint the scoped edge store released:
// an object by ID, or an entity by its ctxt://entity/ URI.
func (l *Ledger) AddEndpoint(typ, id string) {
	if typ == "entity" {
		l.AddEntity(strings.TrimPrefix(id, EntityURIPrefix))
		return
	}
	l.AddObject(id)
}

// AddWithheld records the values the render layer withheld from a
// released object.
func (l *Ledger) AddWithheld(w []Withheld) {
	if l == nil || len(w) == 0 {
		return
	}
	l.mu.Lock()
	for _, x := range w {
		l.canaries[x.Value] = x.Category
	}
	l.mu.Unlock()
}

// HasObject reports whether id was released.
func (l *Ledger) HasObject(id string) bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_, ok := l.objects[id]
	return ok
}

// HasEntity reports whether slug (or its ctxt://entity/ URI) was
// released.
func (l *Ledger) HasEntity(slug string) bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_, ok := l.entities[strings.TrimPrefix(slug, EntityURIPrefix)]
	return ok
}

// HasEdge reports whether edge id was released.
func (l *Ledger) HasEdge(id string) bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_, ok := l.edges[id]
	return ok
}

// Objects lists the released object IDs, sorted.
func (l *Ledger) Objects() []string { return l.sorted(func() map[string]struct{} { return l.objects }) }

// Entities lists the released entity slugs, sorted.
func (l *Ledger) Entities() []string {
	return l.sorted(func() map[string]struct{} { return l.entities })
}

// Edges lists the released edge IDs, sorted.
func (l *Ledger) Edges() []string { return l.sorted(func() map[string]struct{} { return l.edges }) }

func (l *Ledger) sorted(get func() map[string]struct{}) []string {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	m := get()
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// Canaries lists the withheld values, longest first so a value that
// contains another is replaced whole, with their categories.
func (l *Ledger) Canaries() []Withheld {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]Withheld, 0, len(l.canaries))
	for v, c := range l.canaries {
		out = append(out, Withheld{Category: c, Value: v})
	}
	slices.SortFunc(out, func(a, b Withheld) int {
		if d := len(b.Value) - len(a.Value); d != 0 {
			return d
		}
		return strings.Compare(a.Value, b.Value)
	})
	return out
}

// Empty reports whether nothing was released or withheld.
func (l *Ledger) Empty() bool {
	if l == nil {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.objects) == 0 && len(l.entities) == 0 && len(l.edges) == 0 && len(l.canaries) == 0
}

// EntityURIPrefix precedes an entity slug in an edge endpoint.
const EntityURIPrefix = "ctxt://entity/"

type ledgerKey struct{}

// WithLedger returns a context carrying l. The transports attach one
// per restricted request ahead of the handler; the rendering driver
// records into it and the backstop reads it after the handler.
func WithLedger(ctx context.Context, l *Ledger) context.Context {
	return context.WithValue(ctx, ledgerKey{}, l)
}

// LedgerFrom returns the ledger attached to ctx, or nil.
func LedgerFrom(ctx context.Context) *Ledger {
	l, _ := ctx.Value(ledgerKey{}).(*Ledger)
	return l
}
