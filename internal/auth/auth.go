// Package auth is the inbound authentication boundary of dpkms servers
// (ADR-079).
//
// A request is authenticated by an ordered Chain of Methods (chain.go):
// each method resolves one kind of credential to a Principal, a row of
// the instance's principal registry. The transports (HTTP middleware,
// gRPC interceptors) only lift credentials out of the wire into a
// Credential and hand it to the chain; which methods run is server
// config (server.auth.methods), never a rebuild.
//
// The Principal shape is stable across methods: downstream consumers
// (policy engine, entitlements, metering, security events) key off
// Principal fields only.
package auth

import (
	"crypto/tls"
	"errors"
	"slices"
)

// Credential schemes describing how a carried secret arrived on the wire.
const (
	// SchemeBearer is an RFC 6750 Authorization: Bearer header.
	SchemeBearer = "bearer"
	// SchemeAPIKey is an X-API-Key style header.
	SchemeAPIKey = "apikey"
)

// Sentinel errors the chain and its methods return. Transport layers
// map both to 401/UNAUTHENTICATED; they differ only for logging and
// security-event granularity.
var (
	// ErrNoCredential means the request presented no credential at all.
	ErrNoCredential = errors.New("auth: no credential presented")
	// ErrInvalidCredential means a credential was presented but did not
	// resolve to a principal: it is unknown, wrong, expired or revoked.
	ErrInvalidCredential = errors.New("auth: invalid credential")
)

// Principal is the authenticated caller identity. Its shape is stable
// across methods: downstream code must not depend on method-specific
// extras outside Meta.
type Principal struct {
	// ID is the principal's registry id: storage.OwnerPrincipal for the
	// owner, prn_<ULID> otherwise.
	ID string
	// Name is an optional human-readable display name.
	Name string
	// Kind is the principal kind (storage.PrincipalKind*): owner,
	// person, device or peer.
	Kind string
	// Provider records which method authenticated the caller on this
	// request (MethodAPIKey, MethodSession), or "none" on a private
	// instance.
	Provider string
	// Roles carries the role grants (RoleAdmin, RoleWriter,
	// RoleReader). Only the entity-gate bypass reads them; route
	// authorization reads Scopes.
	Roles []string
	// Scopes is the effective scope set every route and RPC is checked
	// against: the principal's role scopes, narrowed by the key or the
	// session kind that authenticated it.
	Scopes []Scope
	// Meta holds method-specific attributes (key id, identity id,
	// session id).
	Meta map[string]string
}

// HasRole reports whether the principal carries the given role.
func (p *Principal) HasRole(role string) bool {
	return p != nil && slices.Contains(p.Roles, role)
}

// Credential is the transport-level secret material extracted by the
// HTTP middleware / gRPC interceptor, normalized so methods never parse
// headers or metadata themselves. A request can carry a header
// credential and a session cookie at once; the chain decides which one
// counts (carried before ambient).
type Credential struct {
	// Scheme says how the carried credential arrived: SchemeBearer,
	// SchemeAPIKey, or "" when no header was presented.
	Scheme string
	// Token is the opaque carried secret (bearer token, API key).
	Token string
	// TLS carries the connection state for mTLS methods; nil otherwise.
	TLS *tls.ConnectionState
	// Cookie is the value of the instance's own web UI session cookie,
	// "" when the request carries none. The HTTP middleware reads it
	// only from that cookie, never from a header, so a session secret
	// pasted into Authorization is just an invalid bearer.
	Cookie string
}

// Empty reports whether no credential material was presented at all.
func (c Credential) Empty() bool {
	return c.Token == "" && c.Cookie == "" && (c.TLS == nil || len(c.TLS.PeerCertificates) == 0)
}
