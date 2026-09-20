// Package auth defines the provider-agnostic inbound authentication
// boundary for dpkms servers.
//
// A Provider validates a transport-level Credential and resolves it to a
// Principal. The concrete backend (static tokens today; OIDC/JWT and mTLS
// later) is selected purely via server config (server.auth.provider) —
// HTTP middleware, gRPC interceptors, and the MCP mount consume the
// Provider interface and never a concrete scheme, so switching identity
// backends is an ops decision, not a rebuild.
//
// The Principal shape is stable across providers: downstream consumers
// (policy engine, entitlements, metering, security events) key off
// Principal fields only.
package auth

import (
	"context"
	"crypto/tls"
	"errors"
	"slices"
)

// Provider names accepted in server.auth.provider.
const (
	// ProviderStatic validates against a fixed token→principal table
	// from server config.
	ProviderStatic = "static"
	// ProviderOIDC is reserved for the OIDC/JWT backend.
	ProviderOIDC = "oidc"
	// ProviderMTLS is reserved for the mutual-TLS client-cert backend.
	ProviderMTLS = "mtls"
)

// Credential schemes describing how the secret arrived on the wire.
const (
	// SchemeBearer is an RFC 6750 Authorization: Bearer header.
	SchemeBearer = "bearer"
	// SchemeAPIKey is an X-API-Key style header.
	SchemeAPIKey = "apikey"
)

// Sentinel errors returned by Provider.Authenticate. Transport layers
// map both to 401/UNAUTHENTICATED; they differ only for logging and
// security-event granularity.
var (
	// ErrNoCredential means the request presented no credential at all.
	ErrNoCredential = errors.New("auth: no credential presented")
	// ErrInvalidCredential means a credential was presented but did not
	// resolve to a principal.
	ErrInvalidCredential = errors.New("auth: invalid credential")
)

// Principal is the authenticated caller identity. Its shape is stable
// across providers: downstream code must not depend on provider-specific
// extras outside Meta.
type Principal struct {
	// ID is the stable unique identifier for the caller
	// (config principal name, OIDC subject, certificate CN, ...).
	ID string
	// Name is an optional human-readable display name.
	Name string
	// Provider records which backend authenticated the caller
	// (ProviderStatic, ProviderOIDC, ProviderMTLS).
	Provider string
	// Roles carries coarse role grants (e.g. "admin", "reader").
	Roles []string
	// Meta holds provider-specific attributes (claims, cert fields).
	Meta map[string]string
}

// HasRole reports whether the principal carries the given role.
func (p *Principal) HasRole(role string) bool {
	return p != nil && slices.Contains(p.Roles, role)
}

// Credential is the transport-level secret material extracted by the
// HTTP middleware / gRPC interceptor, normalized so providers never
// parse headers or metadata themselves.
type Credential struct {
	// Scheme says how the credential arrived: SchemeBearer, SchemeAPIKey,
	// or "" when nothing was presented.
	Scheme string
	// Token is the opaque secret (bearer token, API key, JWT compact form).
	Token string
	// TLS carries the connection state for mTLS providers; nil otherwise.
	TLS *tls.ConnectionState
}

// Empty reports whether no credential material was presented.
func (c Credential) Empty() bool {
	return c.Token == "" && (c.TLS == nil || len(c.TLS.PeerCertificates) == 0)
}

// Provider validates credentials against one identity backend.
// Implementations must be safe for concurrent use.
type Provider interface {
	// Name identifies the backend (ProviderStatic, ...).
	Name() string
	// Authenticate resolves cred to a Principal. It returns
	// ErrNoCredential when cred is empty and ErrInvalidCredential when
	// cred does not resolve; any other error is an internal provider
	// failure (e.g. issuer unreachable).
	Authenticate(ctx context.Context, cred Credential) (*Principal, error)
}
