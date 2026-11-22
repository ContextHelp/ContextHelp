package sharing

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// resolverCacheMax bounds the compiled viewers a Resolver keeps; past
// it the cache is dropped and refilled.
const resolverCacheMax = 1024

// Resolver gives each request its viewer (ADR-080 §4): the owner is
// Unrestricted, and every other principal gets the viewer compiled from
// its grants, ceiling and key policy.
//
// Compiled viewers are cached by (identity, Input.Key): the policy key
// names the principal, its ceiling, its key policy and every applying
// grant at its policy version, so any grant, policy edit, membership,
// ceiling, key binding or principal change yields a new key and a cache
// miss, and an expired grant or membership drops out of the key as the
// clock passes it. The grants are read from the store on every request
// (three indexed point queries; the compile they key is the expensive
// part), so the cache can never serve a viewer the registry no longer
// grants and no write path has to remember to invalidate it. Invalidate
// drops the compiled viewers anyway, for callers that want a cold
// cache.
type Resolver struct {
	store storage.SharingStore
	// registry is the principal registry ViewAs looks principals up
	// in; nil resolves previews by policy name only.
	registry storage.AuthStore
	// now is the clock; nil means time.Now.
	now func() time.Time
	// records adds the record-level reads an external authority provisions
	// to each viewer; nil adds none.
	records RecordSource
	// steward wires steward grants (WithSteward); nil refuses every
	// elevation.
	steward *StewardOptions

	mu    sync.Mutex
	cache map[string]Viewer
}

// NewResolver returns a Resolver over store.
func NewResolver(store storage.SharingStore) *Resolver {
	return &Resolver{store: store, cache: map[string]Viewer{}}
}

// WithRecords gives the resolver the source of record-level reads: each
// viewer it compiles carries the records the source lists for its principal
// at the moment of the request. The policy key names them, so a record that
// starts or stops being readable (an entry's start or end, a stale set)
// yields a new key and a cold cache entry, never a stale viewer.
func (r *Resolver) WithRecords(src RecordSource) *Resolver {
	r.records = src
	return r
}

// WithClock sets the clock the resolver reads grant expiry against
// (tests).
func (r *Resolver) WithClock(now func() time.Time) *Resolver {
	r.now = now
	return r
}

// Resolve returns the viewer of principal principalID of kind kind,
// authenticated through identity identityID ("" when the method names
// none). The owner (storage.PrincipalKindOwner) is Unrestricted. A
// principal or identity the registry does not know resolves to the
// zero viewer, which sees nothing: an unresolved caller fails closed
// without an error. Any other store or compile failure is returned, and
// the transport refuses the request rather than guess. A nil Resolver
// resolves the owner and otherwise the zero viewer.
//
// With AsSteward the request is an elevated one: the viewer is exactly
// the named steward grant's compiled scope, and a grant that fails a
// check is a *StewardRefusal, never the ordinary view (resolveSteward).
func (r *Resolver) Resolve(ctx context.Context, principalID, kind, identityID string, opts ...ResolveOption) (Viewer, error) {
	var o resolveOptions
	for _, opt := range opts {
		opt(&o)
	}
	if o.stewardGrant != "" {
		return r.resolveSteward(ctx, principalID, kind, identityID, o.stewardGrant)
	}
	if kind == storage.PrincipalKindOwner {
		return Unrestricted, nil
	}
	if r == nil || r.store == nil {
		return Viewer{}, nil
	}
	in, err := loadInput(ctx, r.store, principalID, identityID, r.clock())
	if errors.Is(err, storage.ErrNotFound) {
		return Viewer{}, nil
	}
	if err != nil {
		return Viewer{}, err
	}
	if r.records != nil {
		if in.Records, err = r.records.ReadableRecords(ctx, principalID); err != nil {
			return Viewer{}, err
		}
	}
	key := identityID + "\x00" + in.Key()

	r.mu.Lock()
	v, ok := r.cache[key]
	r.mu.Unlock()
	if ok {
		return v, nil
	}
	v, err = Compile(in)
	if err != nil {
		return Viewer{}, err
	}
	r.mu.Lock()
	if len(r.cache) >= resolverCacheMax {
		r.cache = map[string]Viewer{}
	}
	r.cache[key] = v
	r.mu.Unlock()
	return v, nil
}

// clock is the resolver's time: its injected clock, else time.Now.
func (r *Resolver) clock() time.Time {
	if r.now != nil {
		return r.now()
	}
	return time.Now()
}

// Invalidate drops every compiled viewer. The next Resolve of each
// principal compiles again.
func (r *Resolver) Invalidate() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.cache = map[string]Viewer{}
	r.mu.Unlock()
}

// CacheSize reports how many compiled viewers the resolver holds.
func (r *Resolver) CacheSize() int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.cache)
}
