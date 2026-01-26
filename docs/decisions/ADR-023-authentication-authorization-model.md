# ADR-023 – Authentication and Authorization Model

> **Status:** Accepted
> **Date:** 2026-01-26
> **Author:** @jadb
> **Applies to:** dPKMS, ctxt
> **Supersedes:** None
> **Superseded by:** N/A

---

## Context

ContextHelp's local-first philosophy prioritizes offline operation and user sovereignty, but real-world usage demands network capabilities—remote registry access, API endpoints, multi-device sync, plugin installations, and agent integrations. These interactions introduce authentication and authorization requirements.

**Local-Only Operation (No Auth Required):**
- Single-user machine access
- Filesystem permissions sufficient
- No network exposure
- No authentication needed

**Remote Registry Access:**
- Public registries (no auth)
- Private registries (auth required)
- Commercial registries (paid subscriptions)
- Rate-limited registries (token-based)
- User must authenticate to query/sync

**API Exposure:**
- REST API for agent integrations
- gRPC API for performance-critical clients
- Default: localhost-only, no auth
- Optional: expose publicly with auth
- Multi-user or multi-agent scenarios

**Multi-Device Sync (Future):**
- Same user, multiple devices
- Device authentication and pairing
- Sync token distribution
- Trust establishment between devices

**Plugin Installation:**
- Plugin marketplace or registry
- Signature verification (trust)
- Capability declarations (permissions)
- User approval before installation

**Security Concerns:**
- Unauthorized access to local knowledge via API
- Registry credential theft or leakage
- Man-in-the-middle attacks on registry connections
- Confused deputy attacks (agent acting beyond scope)
- Token leakage in logs or error messages
- Privilege escalation through plugins
- Replay attacks on auth tokens

**Design Constraints:**
- Local-first remains default (no forced auth)
- Authentication must be optional and configurable
- No central authority or vendor lock-in
- Support multiple auth methods (tokens, OAuth, device codes)
- Capability-based authorization (principle of least privilege)
- Profile-scoped permissions (per focus profile)
- Clear security boundaries between local/remote
- Audit trails for auth events

**Affected Subsystems:**
- API layer (REST + gRPC auth interceptors)
- Registry connectors (token management, OAuth flows)
- Plugin system (capability enforcement)
- Agent execution (scope enforcement)
- Profile system (profile-scoped access control)
- CLI commands (authentication commands)
- Configuration management (secure credential storage)

**Goals:**
- Enable secure remote registry access
- Protect API endpoints when exposed
- Support multi-device sync (future)
- Enforce capability-based plugin permissions
- Maintain sovereignty (user controls credentials)
- Preserve simplicity for local-only use
- Provide clear security boundaries

---

## Decision

**ContextHelp will implement an optional, layered authentication and authorization model with support for token-based authentication, OAuth/OIDC for registry connections, capability-based permissions for plugins and agents, and profile-scoped access control, while preserving the default local-only operation requiring no authentication.**

The authentication and authorization system provides:

1. **Authentication Layers:**

   **Layer 1: No Authentication (Default)**
   - Local-only operation
   - Localhost-bound API
   - Filesystem permissions sufficient
   - No network exposure

   **Layer 2: Token-Based Authentication**
   - API access tokens for REST/gRPC
   - Personal Access Tokens (PATs) for agents
   - Short-lived session tokens
   - Token rotation and revocation

   **Layer 3: OAuth/OIDC for Registries**
   - OAuth 2.0 for commercial registries
   - OIDC for federated identity
   - PKCE flow for CLI applications
   - Token refresh and expiry handling

   **Layer 4: Device Authentication (Future)**
   - Device pairing for multi-device sync
   - Device-specific keys
   - Trust establishment protocol
   - Device revocation support

2. **Authentication Configuration:**
   ```yaml
   authentication:
     # API authentication (REST + gRPC)
     api:
       enabled: false  # Default: no auth for localhost
       mode: none | token | session | mtls
       token:
         issuer: self-signed | external
         algorithm: HS256 | RS256
         ttl_minutes: 60
         refresh_enabled: true
       bind:
         address: 127.0.0.1  # localhost only by default
         port: 8080
         tls:
           enabled: false
           cert: /path/to/cert.pem
           key: /path/to/key.pem

     # Registry authentication
     registries:
       - name: uxpatterns
         url: https://registry.example.com
         auth:
           method: none | token | oauth | basic
           token: env:REGISTRY_TOKEN
           oauth:
             provider: auth0 | github | google | custom
             client_id: env:OAUTH_CLIENT_ID
             scopes: ["read:taxonomy", "read:entities"]
             token_cache: ~/.config/ctxt/tokens/uxpatterns.json

       - name: private-work
         url: https://work.internal.corp
         auth:
           method: token
           token: env:WORK_REGISTRY_TOKEN
           headers:
             X-API-Version: "2"

     # Device authentication (future)
     devices:
       enabled: false
       pairing_method: code | qr | link
       trust_policy: manual | automatic
   ```

3. **Token Management:**

   **Personal Access Tokens (PATs):**
   ```bash
   # Generate PAT for agent access
   ctxt auth token create --name "agent-leann" --scope "read:objects,write:inbox"

   # Output:
   # Token: ctxt_pat_abc123...
   # Scopes: read:objects, write:inbox
   # Expires: 2026-12-31
   # Store this token securely. It will not be shown again.

   # List tokens
   ctxt auth token list

   # Revoke token
   ctxt auth token revoke ctxt_pat_abc123...
   ```

   **Token Format:**
   ```
   ctxt_pat_<base62(random(20))>

   Stored as:
   {
     "id": "token-uuid",
     "name": "agent-leann",
     "token_hash": "sha256(token)",
     "scopes": ["read:objects", "write:inbox"],
     "created_at": "2026-01-26T10:00:00Z",
     "expires_at": "2026-12-31T23:59:59Z",
     "last_used_at": null,
     "revoked": false
   }
   ```

   **Token Storage:**
   ```
   ~/.config/ctxt/auth/
     tokens.db          # Local PAT database (SQLite)
     registry_tokens/   # Registry OAuth tokens
       uxpatterns.json
       private-work.json
   ```

4. **OAuth Flow for Registries:**

   **Authorization Code Flow with PKCE:**
   ```bash
   # User adds registry requiring OAuth
   ctxt registry add https://registry.example.com

   # CLI detects OAuth requirement
   # 1. Generate PKCE challenge
   # 2. Open browser to authorization URL
   # 3. User authenticates with registry
   # 4. Registry redirects to localhost callback
   # 5. CLI exchanges code for token
   # 6. Token stored securely

   Output:
   Opening browser for authentication...
   Authorization successful.
   Registry 'registry.example.com' configured.
   ```

   **Token Refresh Flow:**
   ```go
   // Automatic token refresh when expired
   if tokenExpired(registryToken) {
       newToken, err := refreshToken(registryToken.RefreshToken)
       if err != nil {
           // Re-authentication required
           return promptReAuth(registry)
       }
       updateStoredToken(registry, newToken)
   }
   ```

5. **Authorization Model (Capability-Based):**

   **Scope Definitions:**
   ```yaml
   scopes:
     # Object operations
     read:objects      # Query and retrieve objects
     write:objects     # Create/update objects
     delete:objects    # Delete objects

     # Inbox operations
     read:inbox        # View inbox items
     write:inbox       # Add to inbox
     process:inbox     # Process inbox items

     # Registry operations
     read:registries   # Query remote registries
     sync:registries   # Sync registry data locally

     # Configuration
     read:config       # View configuration
     write:config      # Modify configuration

     # Export/Import
     export:data       # Export bundles
     import:data       # Import bundles

     # Admin operations
     admin:tokens      # Manage API tokens
     admin:profiles    # Manage profiles
     admin:plugins     # Install/manage plugins
   ```

   **Token Scopes Example:**
   ```json
   {
     "token": "ctxt_pat_abc123",
     "scopes": [
       "read:objects",
       "write:inbox",
       "read:registries"
     ],
     "profile": "work"  // Optional: scope to specific profile
   }
   ```

6. **Plugin Permission Model:**

   **Plugin Manifest (Capability Declaration):**
   ```yaml
   # plugin.yaml
   name: price-monitor
   version: 1.0.0
   author: "@example"

   capabilities:
     network:
       - domains: ["api.example.com"]
         reason: "Fetch product prices"

     filesystem:
       read:
         - ~/.config/ctxt/plugins/price-monitor/
       write:
         - ~/.config/ctxt/plugins/price-monitor/cache/

     api:
       scopes:
         - read:objects
         - write:inbox
       reason: "Add price alerts to inbox"

     cron:
       - schedule: "0 */6 * * *"
         reason: "Check prices every 6 hours"
   ```

   **Installation Approval:**
   ```bash
   ctxt plugin install price-monitor

   # User prompted:
   Plugin 'price-monitor' requests the following permissions:

   Network Access:
     - api.example.com (Fetch product prices)

   Filesystem Access:
     - Read: ~/.config/ctxt/plugins/price-monitor/
     - Write: ~/.config/ctxt/plugins/price-monitor/cache/

   API Access:
     - read:objects (Access your knowledge base)
     - write:inbox (Add price alerts to inbox)

   Background Jobs:
     - Every 6 hours (Check prices every 6 hours)

   Approve? [y/N]:
   ```

   **Runtime Capability Enforcement:**
   ```go
   // Plugin runtime sandbox
   type PluginSandbox struct {
       AllowedDomains []string
       AllowedPaths   []string
       APIScopes      []string
   }

   func (s *PluginSandbox) CheckNetworkAccess(url string) error {
       domain := extractDomain(url)
       if !contains(s.AllowedDomains, domain) {
           return fmt.Errorf("plugin not authorized to access %s", domain)
       }
       return nil
   }
   ```

7. **Profile-Scoped Authorization:**

   **Profile-Based Access Control:**
   ```yaml
   profiles:
     - name: founder
       auth:
         tokens:
           - token_id: token-abc
             scopes: ["read:objects", "write:objects"]

         registries:
           - name: work-registry
             allowed: true

       plugins:
         allowed:
           - price-monitor
           - slack-notifier
         blocked:
           - experimental-plugin

     - name: personal
       auth:
         tokens:
           - token_id: token-xyz
             scopes: ["read:objects"]

         registries:
           - name: public-registry
             allowed: true
```

8. **API Authentication:**

   **REST API Token Usage:**
   ```bash
   # Client makes authenticated request
   curl -H "Authorization: Bearer ctxt_pat_abc123" \
        http://localhost:8080/api/v1/objects

   # Response includes rate limit headers
   HTTP/1.1 200 OK
   X-RateLimit-Limit: 1000
   X-RateLimit-Remaining: 999
   X-RateLimit-Reset: 1640000000
   ```

   **gRPC Metadata Authentication:**
   ```go
   // Client attaches token in metadata
   ctx := metadata.AppendToOutgoingContext(ctx,
       "authorization", "Bearer ctxt_pat_abc123")

   resp, err := client.QueryObjects(ctx, req)
   ```

   **Authentication Interceptor:**
   ```go
   func AuthInterceptor(ctx context.Context, req interface{},
                        info *grpc.UnaryServerInfo,
                        handler grpc.UnaryHandler) (interface{}, error) {

       // Extract token from metadata
       token := extractToken(ctx)

       // Validate token
       claims, err := validateToken(token)
       if err != nil {
           return nil, status.Errorf(codes.Unauthenticated, "invalid token")
       }

       // Check scopes for endpoint
       if !hasRequiredScope(claims.Scopes, info.FullMethod) {
           return nil, status.Errorf(codes.PermissionDenied, "insufficient scope")
       }

       // Attach claims to context
       ctx = withClaims(ctx, claims)
       return handler(ctx, req)
   }
   ```

9. **CLI Authentication Commands:**
   ```bash
   # Authentication management
   ctxt auth status
   ctxt auth login <registry>
   ctxt auth logout <registry>
   ctxt auth refresh <registry>

   # Token management
   ctxt auth token create --name <name> --scope <scopes>
   ctxt auth token list
   ctxt auth token revoke <token-id>
   ctxt auth token rotate <token-id>

   # Device management (future)
   ctxt auth device pair
   ctxt auth device list
   ctxt auth device revoke <device-id>

   # API server
   ctxt serve --auth-required --bind 0.0.0.0:8080 --tls
   ```

10. **Audit Trail:**

    **Authentication Events:**
    ```sql
    CREATE TABLE auth_events (
        id UUID PRIMARY KEY,
        event_type TEXT NOT NULL,  -- login, logout, token_created, etc.
        entity_type TEXT NOT NULL, -- token, registry, device
        entity_id TEXT NOT NULL,
        success BOOLEAN NOT NULL,
        ip_address TEXT,
        user_agent TEXT,
        details JSONB,
        created_at TIMESTAMP NOT NULL
    );

    CREATE INDEX idx_auth_events_created ON auth_events(created_at DESC);
    CREATE INDEX idx_auth_events_entity ON auth_events(entity_id, event_type);
    ```

    **Audit Log Query:**
    ```bash
    ctxt auth audit --since "7 days ago" --type token_created
    ctxt auth audit --entity <token-id>
    ```

---

## Rationale

### Alternatives Considered

#### 1. **Always Require Authentication (Rejected)**
Force authentication even for local-only use.

**Rejected because:**
- Violates local-first principle
- Unnecessary friction for majority use case
- Adds complexity for single-user scenarios
- User sovereignty compromised (forced credentials)
- Defeats simplicity goal

#### 2. **Username/Password Authentication (Rejected)**
Traditional username/password for API access.

**Rejected because:**
- Password management burden
- Password storage security concerns
- Not suitable for agent/API integrations
- Rotation and revocation complex
- Token-based auth more appropriate for APIs

#### 3. **JWT with Centralized Issuer (Rejected)**
Require external JWT issuer for token validation.

**Rejected because:**
- Violates sovereignty (dependency on external service)
- Network dependency for local operation
- Single point of failure
- Complexity for local-only users
- Self-signed tokens sufficient for local API

#### 4. **Per-Plugin User Approval (Too Granular) (Rejected)**
Require user approval for every plugin operation.

**Rejected because:**
- Excessive friction (alert fatigue)
- Defeats background automation
- Users approve once at install time
- Runtime enforcement without prompts preferred

#### 5. **Role-Based Access Control (RBAC) (Rejected)**
Define roles (admin, user, viewer) instead of capability-based.

**Rejected because:**
- Too coarse-grained for API tokens
- Doesn't map well to agent use cases
- Capability-based more flexible
- Principle of least privilege harder to enforce

### Benefits of Chosen Approach

**Progressive Complexity:**
- Simple default (no auth)
- Optional authentication when needed
- Layered approach (tokens → OAuth → device)
- Users adopt incrementally

**User Sovereignty:**
- No forced authentication
- User controls credentials completely
- Self-signed tokens for local API
- No vendor lock-in

**Capability-Based Authorization:**
- Principle of least privilege
- Fine-grained scope control
- Explicit permission declarations
- Clear security boundaries

**Profile Integration:**
- Profile-scoped permissions natural fit
- Different auth for work vs personal profiles
- Registry access per profile
- Plugin permissions per profile

**Registry Federation:**
- OAuth enables commercial registries
- Token-based for simple registries
- No authentication for public registries
- Flexible per-registry configuration

**Plugin Safety:**
- Explicit capability declarations
- User approval before installation
- Runtime enforcement (sandboxing)
- Clear permission reasoning

### Drawbacks / Risks

**Credential Management Burden:**
- Users must manage tokens
- Token rotation required
- Token leakage risk
- Secure storage essential

**OAuth Complexity:**
- OAuth flow complex for CLI
- Browser interaction required
- Token refresh logic
- Registry-specific configuration

**Scope Granularity Trade-offs:**
- Too coarse → excessive permissions
- Too fine → configuration complexity
- Finding right balance hard

**Plugin Sandboxing Enforcement:**
- Runtime enforcement complex
- Plugin may bypass restrictions
- Requires careful implementation
- Testing coverage critical

---

## Consequences

### Positive

**Preserves Local-First:**
- No forced authentication
- Default remains simple
- Optional when needed
- Sovereignty maintained

**Enables Remote Access:**
- Registry authentication works
- API exposure with security
- Multi-device sync possible (future)
- Commercial registries supported

**Clear Security Boundaries:**
- Capability-based authorization
- Profile-scoped permissions
- Explicit plugin permissions
- Audit trail for accountability

**Flexible Integration:**
- Token-based for agents
- OAuth for registries
- Device auth for sync (future)
- mTLS option for enterprises

### Negative

**Implementation Complexity:**
- Multiple auth methods
- Token management infrastructure
- OAuth flow handling
- Capability enforcement engine

**User Experience Burden:**
- Token management required for API exposure
- OAuth flows interrupt CLI experience
- Permission approval at plugin install
- Configuration complexity for advanced scenarios

**Security Maintenance:**
- Token rotation procedures
- Vulnerability monitoring
- Cryptographic library updates
- Audit log retention

### Neutral / Considerations

**Token Storage Security:**
- Filesystem permissions critical
- OS keychain integration preferred
- Encrypted storage recommended
- Clear documentation on security

**Registry Token Expiry:**
- Automatic refresh when possible
- Re-authentication UX when refresh fails
- Token expiry notifications
- Grace period handling

**Multi-User Scenarios (Future):**
- Current design single-user focused
- Multi-user requires RBAC extension
- User management infrastructure needed
- Collaboration features depend on this

**Performance Impact:**
- Token validation overhead minimal
- OAuth refresh adds latency
- Capability checks per request
- Caching strategies mitigate

---

## Implementation Notes

### Core Components

**Authentication Manager (`dPKMS/auth/`):**
```go
type AuthManager interface {
    // Token operations
    CreateToken(name string, scopes []string, ttl time.Duration) (*Token, error)
    ValidateToken(tokenString string) (*Claims, error)
    RevokeToken(tokenID string) error
    ListTokens() ([]*Token, error)

    // Registry auth
    AuthenticateRegistry(name string) error
    RefreshRegistryToken(name string) error
    LogoutRegistry(name string) error
}

type Token struct {
    ID        string
    Name      string
    TokenHash string
    Scopes    []string
    ProfileID string  // Optional: profile-scoped
    CreatedAt time.Time
    ExpiresAt time.Time
    LastUsed  time.Time
    Revoked   bool
}

type Claims struct {
    TokenID   string
    Scopes    []string
    ProfileID string
    IssuedAt  time.Time
    ExpiresAt time.Time
}
```

**OAuth Provider (`dPKMS/oauth/`):**
```go
type OAuthProvider interface {
    Name() string
    AuthorizeURL(state string, codeChallenge string) string
    ExchangeCode(code string, codeVerifier string) (*OAuthToken, error)
    RefreshToken(refreshToken string) (*OAuthToken, error)
}

type OAuthToken struct {
    AccessToken  string
    RefreshToken string
    ExpiresAt    time.Time
    Scopes       []string
}

// Built-in providers
type Auth0Provider struct { ... }
type GitHubProvider struct { ... }
type CustomProvider struct { ... }
```

**Capability Engine (`dPKMS/capabilities/`):**
```go
type CapabilityEngine struct {
    scopes map[string]ScopeDefinition
}

type ScopeDefinition struct {
    Name        string
    Description string
    Category    string  // object, registry, config, admin
    Endpoints   []string
}

func (e *CapabilityEngine) Authorize(scopes []string, endpoint string) error {
    required := e.requiredScopeForEndpoint(endpoint)
    if !contains(scopes, required) {
        return ErrInsufficientScope
    }
    return nil
}
```

**Plugin Sandbox (`dPKMS/plugins/sandbox/`):**
```go
type PluginSandbox struct {
    pluginID       string
    capabilities   *PluginCapabilities
    networkChecker *NetworkChecker
    fsChecker      *FilesystemChecker
}

type PluginCapabilities struct {
    Network    []NetworkCapability
    Filesystem []FilesystemCapability
    API        []string  // Scopes
    Cron       []CronCapability
}

func (s *PluginSandbox) CheckAccess(operation Operation) error {
    switch op := operation.(type) {
    case NetworkOperation:
        return s.networkChecker.Check(op.URL, s.capabilities.Network)
    case FilesystemOperation:
        return s.fsChecker.Check(op.Path, s.capabilities.Filesystem)
    case APIOperation:
        return s.checkAPIScope(op.Endpoint, s.capabilities.API)
    default:
        return ErrUnknownOperation
    }
}
```

**Storage Schema:**
```sql
-- API tokens
CREATE TABLE api_tokens (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL,
    token_hash TEXT NOT NULL UNIQUE,
    scopes TEXT[] NOT NULL,
    profile_id UUID,
    created_at TIMESTAMP NOT NULL,
    expires_at TIMESTAMP NOT NULL,
    last_used_at TIMESTAMP,
    revoked BOOLEAN DEFAULT FALSE,
    FOREIGN KEY(profile_id) REFERENCES profiles(id)
);

CREATE INDEX idx_api_tokens_hash ON api_tokens(token_hash);
CREATE INDEX idx_api_tokens_profile ON api_tokens(profile_id);

-- Registry authentication
CREATE TABLE registry_auth (
    id UUID PRIMARY KEY,
    registry_name TEXT NOT NULL UNIQUE,
    auth_method TEXT NOT NULL,  -- none, token, oauth, basic
    access_token_encrypted BYTEA,
    refresh_token_encrypted BYTEA,
    expires_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

-- Plugin capabilities (from manifest)
CREATE TABLE plugin_capabilities (
    id UUID PRIMARY KEY,
    plugin_id UUID NOT NULL,
    capability_type TEXT NOT NULL,  -- network, filesystem, api, cron
    resource TEXT NOT NULL,
    permission TEXT NOT NULL,  -- read, write, execute
    reason TEXT,
    approved_at TIMESTAMP,
    FOREIGN KEY(plugin_id) REFERENCES plugins(id)
);
```

### Integration Points

**With API Layer:**
1. REST/gRPC auth interceptors validate tokens
2. Scope enforcement per endpoint
3. Rate limiting per token
4. Audit logging for all auth events

**With Registry Connectors:**
1. Registry config includes auth method
2. OAuth flow when registry requires it
3. Token injection in registry requests
4. Automatic token refresh

**With Plugin System:**
1. Capability declarations in plugin manifest
2. User approval during installation
3. Runtime capability enforcement
4. Sandbox isolation for plugin execution

**With Profile System:**
1. Profile-scoped token creation
2. Profile-based registry access control
3. Profile-specific plugin permissions
4. Profile switching updates auth context

### Migration Strategy

**Phase 1: API Token Authentication (Skeleton 7)**
- Basic PAT generation and validation
- Token-based API auth for REST/gRPC
- Token storage and revocation
- CLI token management commands

**Phase 2: Registry OAuth (Skeleton 8)**
- OAuth provider implementations
- PKCE flow for CLI
- Token refresh logic
- Registry-specific auth configuration

**Phase 3: Capability Engine (Skeleton 8)**
- Scope definitions and enforcement
- Plugin capability declarations
- Runtime capability checking
- User approval flows

**Phase 4: Advanced Features (Skeleton 9+)**
- Device authentication for multi-device sync
- mTLS support for enterprise
- Hardware security module (HSM) integration
- Advanced audit and compliance features

**Backward Compatibility:**
- Authentication disabled by default
- No breaking changes to existing API
- Existing configurations work unchanged
- Opt-in authentication per use case

### Security Considerations

**Token Security:**
- SHA-256 hash for storage
- Constant-time comparison
- Secure random generation (crypto/rand)
- No plaintext logging

**OAuth Security:**
- PKCE required for all flows
- State parameter for CSRF protection
- Secure token storage (encrypted at rest)
- Automatic HTTPS enforcement

**API Security:**
- Rate limiting per token
- Request size limits
- Input validation
- TLS enforcement for public exposure

**Plugin Security:**
- Capability declarations mandatory
- Sandbox enforcement at runtime
- Network/filesystem restrictions
- API scope validation

### Testing Requirements

**Unit Tests:**
- Token generation and validation
- OAuth flow simulation
- Capability matching logic
- Scope enforcement

**Integration Tests:**
- End-to-end OAuth flow
- Token refresh scenarios
- Plugin capability enforcement
- API authentication flow

**Security Tests:**
- Token leakage prevention
- Timing attack resistance
- OAuth flow security (CSRF, etc.)
- Capability bypass attempts

**Performance Tests:**
- Token validation overhead
- OAuth refresh performance
- Capability check latency
- Concurrent auth requests

---

## References

- **architecture.md:1245-1256** – Authentication and authorization overview
- **dpkms/security.md:76-99** – API security requirements
- **dpkms/registry-protocol.md** – Registry capabilities and authentication
- ADR-001 – Local-First and Decentralized (sovereignty principle)
- ADR-012 – Plugins Extend Any Layer (plugin permissions)
- ADR-019 – Encryption and Privacy Model (secure credential storage)
- **ROADMAP.md** – Skeleton 7-8: Authentication features

**Related Documents:**
- `dPKMS/auth/` – Authentication implementation (to be created)
- `dPKMS/oauth/` – OAuth provider implementations (to be created)
- `dPKMS/capabilities/` – Capability enforcement engine (to be created)
- `dPKMS/plugins/sandbox/` – Plugin sandboxing (to be created)

**External References:**
- OAuth 2.0 RFC: https://datatracker.ietf.org/doc/html/rfc6749
- PKCE RFC: https://datatracker.ietf.org/doc/html/rfc7636
- OIDC Specification: https://openid.net/specs/openid-connect-core-1_0.html
- JWT RFC: https://datatracker.ietf.org/doc/html/rfc7519

---
