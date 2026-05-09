package identity

import (
	"context"
	"errors"

	"github.com/ideacrafterslabs/ctxt/internal/lateral/strategies/identitykey"
	"hop.top/kit/go/runtime/domain"
)

// ErrNotFound aliases the kit-wide sentinel so lateral identity callers
// using errors.Is(err, identity.ErrNotFound) interoperate with any kit-backed
// Graph adapter that returns domain.ErrNotFound directly.
var ErrNotFound = domain.ErrNotFound

// Graph is the subset of the canonical store the resolver queries: lookup by
// source URL, lookup by identity key (per the identitykey package contract).
type Graph interface {
	FindByURL(ctx context.Context, url string) (string, error)
	FindByIdentityKey(ctx context.Context, key string) (string, error)
}

// Candidate is the input to Resolve. URL is required; Preview carries
// strategy-supplied fields and is consulted via identitykey.Get to obtain
// the canonical identity_key. Per the identitykey package contract,
// strategies write the key into Preview[identitykey.KeyField]; the resolver
// reads it the same way so the two sides stay in lockstep.
type Candidate struct {
	URL     string
	Preview map[string]any
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

// Resolver runs the identity lookup against a Graph.
type Resolver struct{ g Graph }

// NewResolver wires a Resolver to its graph backend.
func NewResolver(g Graph) *Resolver { return &Resolver{g: g} }

// Resolve returns the resolution decision for c. identity_key (read via
// identitykey.Get(c.Preview)) takes precedence over URL: when non-empty and
// found in the graph, that result wins regardless of URL match. When the
// identity_key is empty or missing in the graph, Resolve falls back to URL
// equality. Real graph errors (anything other than ErrNotFound) propagate.
func (r *Resolver) Resolve(ctx context.Context, c Candidate) (Result, error) {
	if key := identitykey.Get(c.Preview); key != "" {
		id, err := r.g.FindByIdentityKey(ctx, key)
		if err == nil {
			return Result{EdgeOnly: true, CanonicalID: id}, nil
		}
		if !errors.Is(err, ErrNotFound) {
			return Result{}, err
		}
	}
	id, err := r.g.FindByURL(ctx, c.URL)
	if err == nil {
		return Result{EdgeOnly: true, CanonicalID: id}, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Result{}, err
	}
	return Result{EdgeOnly: false}, nil
}
