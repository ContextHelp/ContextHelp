package identity

import (
	"context"
	"errors"

	"hop.top/kit/go/runtime/domain"
)

// ErrNotFound aliases the kit-wide sentinel so lateral identity callers
// using errors.Is(err, identity.ErrNotFound) interoperate with any kit-backed
// Graph adapter that returns domain.ErrNotFound directly.
var ErrNotFound = domain.ErrNotFound

// Graph is the subset of the canonical store the resolver queries: lookup by
// source URL, lookup by identity key (e.g. "@github.user.<login>" via aliases).
type Graph interface {
	FindByURL(ctx context.Context, url string) (string, error)
	FindByIdentityKey(ctx context.Context, key string) (string, error)
}

// Candidate is the input to Resolve. URL is required; IdentityKey is optional
// and consulted only when URL match misses.
type Candidate struct {
	URL         string
	IdentityKey string
}

// Result is what Resolve produces. EdgeOnly=true means CanonicalID points at
// an existing canonical entity and the materializer should write only an
// edge (no probationary shadow). EdgeOnly=false means the candidate is new
// and the materializer should create a probationary record.
type Result struct {
	EdgeOnly    bool
	CanonicalID string
	Ambiguous   bool
}

// Resolver runs the three-step identity lookup against a Graph.
type Resolver struct{ g Graph }

// NewResolver wires a Resolver to its graph backend.
func NewResolver(g Graph) *Resolver { return &Resolver{g: g} }

// Resolve returns the resolution decision for c. URL match is tried first,
// then identity-key match if URL missed and IdentityKey is non-empty. Real
// graph errors (anything other than ErrNotFound) propagate.
func (r *Resolver) Resolve(ctx context.Context, c Candidate) (Result, error) {
	id, err := r.g.FindByURL(ctx, c.URL)
	if err == nil {
		return Result{EdgeOnly: true, CanonicalID: id}, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Result{}, err
	}
	if c.IdentityKey != "" {
		id, err = r.g.FindByIdentityKey(ctx, c.IdentityKey)
		if err == nil {
			return Result{EdgeOnly: true, CanonicalID: id}, nil
		}
		if !errors.Is(err, ErrNotFound) {
			return Result{}, err
		}
	}
	return Result{EdgeOnly: false}, nil
}
