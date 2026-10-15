package registry

import (
	"context"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Grants manages the inbound entitlement grants InboundGate enforces:
// one row per principal listing the namespace globs it may read. A
// principal with no row is unrestricted; a row with no namespaces, or
// one past its expiry, denies every gated read.
type Grants struct {
	store storage.EntitlementStore
}

// NewGrants returns a Grants over the inbound rows of store.
func NewGrants(store storage.EntitlementStore) *Grants {
	return &Grants{store: store}
}

// GrantOptions carries the optional parts of a grant.
type GrantOptions struct {
	// SetExpiry replaces the grant's expiry with ExpiresAt (the zero
	// time clears it). Without it a new grant never expires and an
	// existing grant keeps its expiry.
	SetExpiry bool
	ExpiresAt time.Time
}

// GrantResult reports what a grant, revoke or ungate did (or, on a dry
// run, would do).
type GrantResult struct {
	Principal string `json:"principal"`
	// Changed is false when the call left the stored grant as it was.
	Changed bool `json:"changed"`
	// Created is true when a grant created the principal's row.
	Created bool `json:"created,omitempty"`
	// Added lists namespaces a grant added.
	Added []string `json:"added,omitempty"`
	// Removed lists namespaces a revoke removed, or every namespace an
	// ungate dropped with the row.
	Removed []string `json:"removed,omitempty"`
	// Unchanged lists requested namespaces that needed no change: already
	// granted (grant) or not granted (revoke).
	Unchanged []string `json:"unchanged,omitempty"`
	// Entitlement is the principal's row after the call; nil when there
	// is none (never granted, or ungated).
	Entitlement *storage.RegistryEntitlement `json:"entitlement"`
}

// List returns every inbound grant, ordered by principal.
func (g *Grants) List(ctx context.Context) ([]*storage.RegistryEntitlement, error) {
	return g.store.List(ctx, storage.EntitlementInbound)
}

// Grant adds namespaces to principal's grant, creating it when absent.
// Namespaces already granted are left as they are.
func (g *Grants) Grant(ctx context.Context, principal string, namespaces []string, opts GrantOptions, dryRun bool) (*GrantResult, error) {
	principal, namespaces, err := validateGrantInput(principal, namespaces)
	if err != nil {
		return nil, err
	}
	cur, err := g.find(ctx, principal)
	if err != nil {
		return nil, err
	}
	res := &GrantResult{Principal: principal}
	next := &storage.RegistryEntitlement{Kind: storage.EntitlementInbound, RegistryName: principal}
	if cur == nil {
		res.Created = true
	} else {
		next.Plan = cur.Plan
		next.Namespaces = slices.Clone(cur.Namespaces)
		next.ExpiresAt = cur.ExpiresAt
	}
	for _, ns := range namespaces {
		if slices.Contains(next.Namespaces, ns) {
			res.Unchanged = append(res.Unchanged, ns)
			continue
		}
		next.Namespaces = append(next.Namespaces, ns)
		res.Added = append(res.Added, ns)
	}
	if opts.SetExpiry {
		next.ExpiresAt = opts.ExpiresAt.UTC().Truncate(time.Second)
	}
	res.Changed = res.Created || len(res.Added) > 0 || (cur != nil && !next.ExpiresAt.Equal(cur.ExpiresAt))
	if !res.Changed {
		res.Entitlement = cur
		return res, nil
	}
	return g.write(ctx, res, next, dryRun)
}

// Revoke removes namespaces from principal's grant. Removing the last
// namespace keeps the row with none, so the principal is denied every
// gated read; Ungate removes the row. Revoking namespaces that are not
// granted, or revoking from a principal with no grant, changes nothing.
func (g *Grants) Revoke(ctx context.Context, principal string, namespaces []string, dryRun bool) (*GrantResult, error) {
	principal, namespaces, err := validateGrantInput(principal, namespaces)
	if err != nil {
		return nil, err
	}
	cur, err := g.find(ctx, principal)
	if err != nil {
		return nil, err
	}
	res := &GrantResult{Principal: principal, Entitlement: cur}
	if cur == nil {
		res.Unchanged = namespaces
		return res, nil
	}
	next := *cur
	next.Namespaces = make([]string, 0, len(cur.Namespaces))
	for _, ns := range cur.Namespaces {
		if !slices.Contains(namespaces, ns) {
			next.Namespaces = append(next.Namespaces, ns)
		}
	}
	for _, ns := range namespaces {
		if slices.Contains(cur.Namespaces, ns) {
			res.Removed = append(res.Removed, ns)
		} else {
			res.Unchanged = append(res.Unchanged, ns)
		}
	}
	if len(res.Removed) == 0 {
		return res, nil
	}
	res.Changed = true
	return g.write(ctx, res, &next, dryRun)
}

// Ungate deletes principal's grant, returning the principal to
// unrestricted access. Ungating a principal with no grant changes
// nothing.
func (g *Grants) Ungate(ctx context.Context, principal string, dryRun bool) (*GrantResult, error) {
	principal = strings.TrimSpace(principal)
	if principal == "" {
		return nil, errors.New("principal must not be empty")
	}
	cur, err := g.find(ctx, principal)
	if err != nil {
		return nil, err
	}
	res := &GrantResult{Principal: principal}
	if cur == nil {
		return res, nil
	}
	res.Changed = true
	res.Removed = slices.Clone(cur.Namespaces)
	if dryRun {
		return res, nil
	}
	if err := g.store.Delete(ctx, storage.EntitlementInbound, principal); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			// Removed between the read and the delete: the outcome the
			// caller asked for.
			return res, nil
		}
		return nil, fmt.Errorf("delete grant: %w", err)
	}
	return res, nil
}

// find returns principal's inbound row, or nil when there is none. It
// lists rather than calling Get so a failed read is an error, never
// mistaken for a missing grant.
func (g *Grants) find(ctx context.Context, principal string) (*storage.RegistryEntitlement, error) {
	rows, err := g.store.List(ctx, storage.EntitlementInbound)
	if err != nil {
		return nil, fmt.Errorf("list grants: %w", err)
	}
	for _, r := range rows {
		if r.RegistryName == principal {
			return r, nil
		}
	}
	return nil, nil
}

func (g *Grants) write(ctx context.Context, res *GrantResult, next *storage.RegistryEntitlement, dryRun bool) (*GrantResult, error) {
	if next.Namespaces == nil {
		next.Namespaces = []string{}
	}
	if !dryRun {
		if err := g.store.Upsert(ctx, next); err != nil {
			return nil, fmt.Errorf("store grant: %w", err)
		}
	}
	res.Entitlement = next
	return res, nil
}

// validateGrantInput trims principal, requires at least one namespace,
// and checks each is a non-empty, well-formed glob; duplicates collapse.
func validateGrantInput(principal string, namespaces []string) (string, []string, error) {
	principal = strings.TrimSpace(principal)
	if principal == "" {
		return "", nil, errors.New("principal must not be empty")
	}
	if len(namespaces) == 0 {
		return "", nil, errors.New("give at least one namespace")
	}
	out := make([]string, 0, len(namespaces))
	for _, ns := range namespaces {
		ns = strings.TrimSpace(ns)
		if ns == "" {
			return "", nil, errors.New("namespace must not be empty")
		}
		if _, err := path.Match(ns, ""); err != nil {
			return "", nil, fmt.Errorf("namespace %q: %w", ns, err)
		}
		if !slices.Contains(out, ns) {
			out = append(out, ns)
		}
	}
	return principal, out, nil
}
