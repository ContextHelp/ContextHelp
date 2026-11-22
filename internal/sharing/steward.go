package sharing

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Reason codes of the steward_grant check (ADR-083 §3, §4). Like the
// receipt's, they are stable snake_case and never reworded. The first
// is the pass; the next seven are the ADR's ordered failures; the last
// five are the same refusal for causes outside that list. Every one is
// denied except grant_unavailable, which is deferred.
const (
	// StewardReasonGrantActive: the grant passed every check.
	StewardReasonGrantActive = "grant_active"

	// Check 1: the grant exists and belongs to the caller.
	StewardReasonGrantUnknown = "grant_unknown"
	StewardReasonNotSteward   = "not_steward"
	// Check 2: approved, inside its window under the current cap, not
	// revoked.
	StewardReasonGrantNotActive = "grant_not_active"
	StewardReasonGrantExpired   = "grant_expired"
	StewardReasonGrantRevoked   = "grant_revoked"
	// Check 3: the principal still holds request:steward and is active.
	StewardReasonRoleMissing = "role_missing"
	// Check 4: the stored scope still matches its digest.
	StewardReasonScopeChanged = "scope_changed"

	// StewardReasonScopeInvalid: the stored scope, whose digest holds,
	// is no valid scope (the policy rules moved under it). Denied.
	StewardReasonScopeInvalid = "scope_invalid"
	// StewardReasonStewardDisabled: steward.enabled is off, or the
	// instance has no steward wiring. Denied.
	StewardReasonStewardDisabled = "steward_disabled"
	// StewardReasonIdentityUnknown: the credential the request was
	// authenticated through is not in the registry. Denied, as the
	// ordinary path's unknown identity sees nothing.
	StewardReasonIdentityUnknown = "identity_unknown"
	// StewardReasonKeyPolicyInvalid: the share policy the credential is
	// bound to cannot be read as a policy (its row is gone or its spec
	// no longer validates). Denied.
	StewardReasonKeyPolicyInvalid = "key_policy_invalid"
	// StewardReasonGrantUnavailable: a store could not be read. The
	// grant is not treated as active: the request is refused, deferred.
	StewardReasonGrantUnavailable = "grant_unavailable"
)

// StewardRefusal is a refused elevation: the request named a steward
// grant and it failed a check, so the request fails; it is never served
// from the ordinary view (ADR-083 §3). Resolver.Resolve returns it with
// the zero viewer, which sees nothing.
type StewardRefusal struct {
	// Reason is one of the StewardReason codes.
	Reason string
	// Deferred is true when the cause is unavailability (a store that
	// could not be read), false when the answer is no.
	Deferred bool
	// Err is the cause, kept for the log: it may name internals and is
	// never shown to the caller.
	Err error
}

func (e *StewardRefusal) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("steward grant refused (%s): %v", e.Reason, e.Err)
	}
	return "steward grant refused (" + e.Reason + ")"
}

func (e *StewardRefusal) Unwrap() error { return e.Err }

// Code is the refusal's stable API error code: STEWARD_ and the reason
// in capitals.
func (e *StewardRefusal) Code() string { return "STEWARD_" + strings.ToUpper(e.Reason) }

// refuseSteward returns a denied refusal.
func refuseSteward(reason string) *StewardRefusal { return &StewardRefusal{Reason: reason} }

// stewardUnavailable returns the deferred refusal for a cause that is
// not an answer.
func stewardUnavailable(err error) *StewardRefusal {
	return &StewardRefusal{Reason: StewardReasonGrantUnavailable, Deferred: true, Err: err}
}

// StewardOptions wires a Resolver for steward grants.
type StewardOptions struct {
	// Settings returns the instance's steward settings; read on every
	// request. nil refuses every elevation.
	Settings func() StewardSettings
	// HoldsRequestSteward reports whether roles carry request:steward.
	// The resolver cannot expand roles into scopes (this package does
	// not import internal/auth), so the transport's wiring supplies it.
	// nil refuses every elevation.
	HoldsRequestSteward func(roles []string) bool
}

// WithSteward wires r for steward grants; without it every elevation
// is refused (steward_disabled). It returns r.
func (r *Resolver) WithSteward(o StewardOptions) *Resolver {
	if r != nil {
		r.steward = &o
	}
	return r
}

// ResolveOption refines one Resolve.
type ResolveOption func(*resolveOptions)

type resolveOptions struct {
	stewardGrant string
}

// AsSteward resolves the viewer of an elevated request: the principal
// acting under steward grant grantID, whose viewer is exactly that
// grant's compiled scope (ADR-083 §3). An empty id is no option. The
// transport passes it whenever the authenticated principal names a
// grant; a grant that fails any check refuses the request
// (*StewardRefusal) and never falls back to the principal's ordinary
// view.
func AsSteward(grantID string) ResolveOption {
	return func(o *resolveOptions) { o.stewardGrant = grantID }
}

// resolveSteward runs the four ordered checks of ADR-083 §3 for
// principalID acting under grant grantID and compiles the elevated
// viewer. Every failure, a store error included, is a *StewardRefusal
// and the zero viewer.
func (r *Resolver) resolveSteward(ctx context.Context, principalID, kind, identityID, grantID string) (Viewer, error) {
	if r == nil || r.store == nil || r.steward == nil || r.steward.Settings == nil || r.steward.HoldsRequestSteward == nil {
		return Viewer{}, refuseSteward(StewardReasonStewardDisabled)
	}
	set := r.steward.Settings()
	if !set.Enabled {
		return Viewer{}, refuseSteward(StewardReasonStewardDisabled)
	}
	in, err := r.loadStewardInput(ctx, set, principalID, kind, identityID, grantID)
	if err != nil {
		return Viewer{}, err
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
		// The key policy was validated as it was read (stewardKeyPolicy):
		// what fails to compile here is the scope.
		return Viewer{}, &StewardRefusal{Reason: StewardReasonScopeInvalid, Err: err}
	}
	r.mu.Lock()
	if len(r.cache) >= resolverCacheMax {
		r.cache = map[string]Viewer{}
	}
	r.cache[key] = v
	r.mu.Unlock()
	return v, nil
}

// loadStewardInput is the steward branch of loadInput: the checks, then
// the Input of the elevated viewer. The grant, the principal's roles
// and the settings are read now, on every request: nothing about an
// earlier request is trusted, so an expiry, a revocation, a removed
// role or a lowered cap is the next request's refusal.
func (r *Resolver) loadStewardInput(ctx context.Context, set StewardSettings, principalID, kind, identityID, grantID string) (Input, error) {
	now := r.clock()

	// 1. The grant exists and is the caller's.
	g, err := r.store.StewardGrants().Get(ctx, grantID)
	switch {
	case errors.Is(err, storage.ErrNotFound):
		return Input{}, refuseSteward(StewardReasonGrantUnknown)
	case err != nil:
		return Input{}, stewardUnavailable(err)
	case kind == storage.PrincipalKindOwner || g.StewardID != principalID:
		return Input{}, refuseSteward(StewardReasonNotSteward)
	}

	// 2. Approved, inside its window under today's cap, not revoked.
	switch g.StateAt(now, set.MaxDuration) {
	case storage.StewardStateActive:
	case storage.StewardStateRevoked:
		return Input{}, refuseSteward(StewardReasonGrantRevoked)
	case storage.StewardStateExpired:
		return Input{}, refuseSteward(StewardReasonGrantExpired)
	default:
		return Input{}, refuseSteward(StewardReasonGrantNotActive)
	}

	// 3. The principal still holds request:steward and is active.
	if r.registry == nil {
		return Input{}, stewardUnavailable(errors.New("no principal registry to read the steward's roles from"))
	}
	p, err := r.registry.Principals().Get(ctx, principalID)
	switch {
	case errors.Is(err, storage.ErrNotFound):
		return Input{}, refuseSteward(StewardReasonRoleMissing)
	case err != nil:
		return Input{}, stewardUnavailable(err)
	case !p.Active() || !r.steward.HoldsRequestSteward(p.Roles):
		return Input{}, refuseSteward(StewardReasonRoleMissing)
	}

	// 4. The stored scope is the one that was approved.
	digest, err := StewardScopeDigest(g.Scope)
	if err != nil || digest != g.ScopeDigest {
		return Input{}, refuseSteward(StewardReasonScopeChanged)
	}
	scope, err := ParseStewardScope(g.Scope)
	if err != nil {
		return Input{}, &StewardRefusal{Reason: StewardReasonScopeInvalid, Err: err}
	}

	// What still applies: the principal's ceiling and the credential's
	// key policy intersect with the grant, as with any viewer.
	deny, err := r.store.DenyClasses(ctx, principalID)
	if err != nil {
		return Input{}, stewardUnavailable(fmt.Errorf("ceiling: %w", err))
	}
	keyPolicy, err := r.stewardKeyPolicy(ctx, identityID)
	if err != nil {
		return Input{}, err
	}
	// One grant per spec of the scope, all of them this steward grant's:
	// an object renders through the specs covering it, never through
	// their union.
	policies := scope.Policies(g.ID, set)
	grants := make([]Grant, len(policies))
	for i, p := range policies {
		grants[i] = Grant{ID: g.ID, Audience: "steward:" + g.ID, Policy: p, Attest: stewardAttest(g.ScopeDigest, p)}
	}
	return Input{
		PrincipalID: principalID, IdentityID: identityID, DenyClasses: deny, KeyPolicy: keyPolicy,
		StewardGrant: g.ID, Grants: grants,
	}, nil
}

// stewardKeyPolicy is loadKeyPolicy for the steward branch, with each
// failure classified: an identity the registry does not know is denied
// (identity_unknown), a bound policy whose row is gone or whose spec no
// longer validates is denied (key_policy_invalid), and only a store
// that could not be read is deferred. The policy it returns has been
// validated, its data filter compiled for every dialect, so Compile
// cannot fail on it afterwards.
func (r *Resolver) stewardKeyPolicy(ctx context.Context, identityID string) (*Policy, error) {
	if identityID == "" {
		return nil, nil
	}
	policyID, err := r.store.IdentityPolicy(ctx, identityID)
	switch {
	case errors.Is(err, storage.ErrNotFound):
		return nil, &StewardRefusal{Reason: StewardReasonIdentityUnknown, Err: err}
	case err != nil:
		return nil, stewardUnavailable(fmt.Errorf("identity %s: %w", identityID, err))
	case policyID == "":
		return nil, nil
	}
	row, err := r.store.Policies().Get(ctx, policyID)
	switch {
	case errors.Is(err, storage.ErrNotFound):
		return nil, &StewardRefusal{Reason: StewardReasonKeyPolicyInvalid, Err: err}
	case err != nil:
		return nil, stewardUnavailable(fmt.Errorf("key policy %s: %w", policyID, err))
	}
	p, err := PolicyFromSpec(row)
	if err != nil {
		return nil, &StewardRefusal{Reason: StewardReasonKeyPolicyInvalid, Err: err}
	}
	return &p, nil
}

// stewardAttest is a spec's extra key material: the scope digest and
// what the settings made of the spec, so a change of the withheld
// classes or of allow_export is a new key, not a stale cache hit. With
// the policy's name ("<grant>/<i>") the key names, per spec, the grant,
// the spec's index and the digest.
func stewardAttest(digest string, p Policy) string {
	return digest + ";excluded=" + strings.Join(p.ExcludeClasses, ",") + ";export=" + strconv.FormatBool(p.Capabilities.Export)
}
