package storage

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// The principal registry (ADR-079 §1): who an instance knows, one row
// per principal, one row per credential bound to it, single-use
// sign-in tickets and a key-value table for API key records. The
// migration that creates the tables seeds exactly one owner principal
// (OwnerPrincipal); every other principal is created through
// PrincipalStore.
//
// Secrets never reach these stores: a ticket is stored as the SHA-256
// of its secret (TicketHash), an API key record carries the hash of
// its secret, and sessions keep their own hashes (UISessionStore).

// Principal kinds (ADR-079 §1). Agents are keys on the principal they
// act for, not a kind of their own.
const (
	PrincipalKindOwner  = "owner"
	PrincipalKindPerson = "person"
	PrincipalKindDevice = "device"
	PrincipalKindPeer   = "peer"
)

// PrincipalKinds lists every kind a principal row may carry.
var PrincipalKinds = []string{PrincipalKindOwner, PrincipalKindPerson, PrincipalKindDevice, PrincipalKindPeer}

// Principal statuses. Only an active principal authenticates; a
// suspended one keeps its credentials for when it is reinstated, a
// revoked one has lost them all (PrincipalStore.Revoke cascades).
const (
	PrincipalActive    = "active"
	PrincipalSuspended = "suspended"
	PrincipalRevoked   = "revoked"
)

// PrincipalStatuses lists every status a principal row may carry.
var PrincipalStatuses = []string{PrincipalActive, PrincipalSuspended, PrincipalRevoked}

// DefaultPrincipalRoles is the role bundle a principal created without
// roles gets: every read route, nothing else (ADR-079 §1).
var DefaultPrincipalRoles = []string{"reader"}

// Identity methods: the credential kinds an identity row can stand
// for (ADR-079 §1, §2).
const (
	IdentityMethodAPIKey  = "api_key"
	IdentityMethodPasskey = "passkey"
	IdentityMethodProxy   = "proxy"
	IdentityMethodOIDC    = "oidc"
	IdentityMethodMTLS    = "mtls"
	// IdentityMethodLink is a redeemed invite or sign-in link: the
	// subject is the ticket's hash. The browser session the redemption
	// minted is bound to it, so revoking the row ends that session.
	IdentityMethodLink = "link"
)

// IdentityMethods lists every method an identity row may carry.
var IdentityMethods = []string{IdentityMethodAPIKey, IdentityMethodPasskey, IdentityMethodProxy, IdentityMethodOIDC, IdentityMethodMTLS, IdentityMethodLink}

// IdentityTouchEvery bounds how often IdentityStore.Touch writes
// last_used_at: a busy key would otherwise write on every request.
const IdentityTouchEvery = time.Minute

// Ticket kinds (ADR-079 §2): an invite is the first sign-in of a new
// person, a magic link signs an existing principal in again.
const (
	TicketKindInvite    = "invite"
	TicketKindMagicLink = "magic_link"
)

// TicketKinds lists every kind a ticket row may carry.
var TicketKinds = []string{TicketKindInvite, TicketKindMagicLink}

// Sentinels the registry stores return beside ErrNotFound.
var (
	// ErrAlreadyExists is a create whose id, or whose unique
	// (method, issuer, subject) triple, is already taken.
	ErrAlreadyExists = errors.New("already exists")
	// ErrPrincipalInactive is an operation on, or a credential of, a
	// principal that is suspended or revoked: a ticket redeemed for it,
	// a suspension of a revoked one.
	ErrPrincipalInactive = errors.New("principal is not active")
	// ErrOwnerProtected is an attempt to create, suspend or revoke the
	// owner principal: every instance has exactly one, seeded by the
	// migration, and it cannot lose its credentials through the store.
	ErrOwnerProtected = errors.New("the owner principal cannot be created, suspended or revoked")
)

// Principal is one row of the registry.
type Principal struct {
	// ID is OwnerPrincipal for the owner, prn_<ULID> otherwise.
	ID string `json:"id"`
	// Kind is one of PrincipalKinds.
	Kind string `json:"kind"`
	// DisplayName is how the principal is shown; never a credential.
	DisplayName string `json:"display_name"`
	// Roles are the role bundles the principal holds (admin, writer,
	// reader); the auth package expands them into scopes.
	Roles []string `json:"roles"`
	// Status is one of PrincipalStatuses.
	Status    string     `json:"status"`
	CreatedAt time.Time  `json:"created_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}

// Active reports whether the principal may authenticate.
func (p *Principal) Active() bool { return p != nil && p.Status == PrincipalActive }

// PrincipalFilter narrows PrincipalStore.List.
type PrincipalFilter struct {
	// Kind and Status, when set, keep matching principals only.
	Kind   string
	Status string
	// Limit caps the result; 0 means no cap.
	Limit int
}

// PrincipalStore keeps the principals of an instance.
type PrincipalStore interface {
	// Create stores p. An empty ID is assigned (NewPrincipalID); empty
	// Roles become DefaultPrincipalRoles, an empty Status becomes
	// PrincipalActive and a zero CreatedAt becomes now. Kind
	// PrincipalKindOwner is ErrOwnerProtected, an unknown kind, status
	// or role shape is an error, a taken ID is ErrAlreadyExists.
	Create(ctx context.Context, p *Principal) error
	// Get returns the principal with id, or ErrNotFound.
	Get(ctx context.Context, id string) (*Principal, error)
	// List returns principals matching f, oldest first.
	List(ctx context.Context, f PrincipalFilter) ([]*Principal, error)
	// Suspend moves an active principal to PrincipalSuspended. A
	// suspended one is left alone; a revoked one is ErrPrincipalInactive;
	// the owner is ErrOwnerProtected; an unknown id is ErrNotFound.
	Suspend(ctx context.Context, id string) error
	// Revoke moves the principal to PrincipalRevoked at at and revokes
	// every identity of it that is still active and every active web
	// UI session of it (reason RevokeReasonPrincipalRevoked), and
	// deletes its pending login codes, in one transaction. A revoked
	// principal keeps its first revocation; the owner is
	// ErrOwnerProtected; an unknown id is ErrNotFound.
	Revoke(ctx context.Context, id string, at time.Time) error
}

// Identity is one credential bound to a principal: an API key, a
// passkey, a proxy login, an OIDC subject or a client certificate.
type Identity struct {
	// ID is idn_<ULID>.
	ID          string `json:"id"`
	PrincipalID string `json:"principal_id"`
	// Method is one of IdentityMethods.
	Method string `json:"method"`
	// Issuer qualifies Subject: a proxy preset, an OIDC issuer, a CA
	// fingerprint; empty for API keys.
	Issuer string `json:"issuer,omitempty"`
	// Subject is the credential's own identifier: the API key id, the
	// passkey credential id, the proxy login, the OIDC sub, the
	// certificate SAN.
	Subject string `json:"subject"`
	// Name is a label ("laptop CLI", "assistant bot").
	Name       string     `json:"name,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

// Active reports whether the identity may still authenticate.
func (i *Identity) Active() bool { return i != nil && i.RevokedAt == nil }

// IdentityStore keeps the credentials bound to principals.
type IdentityStore interface {
	// Create stores i. An empty ID is assigned (NewIdentityID) and a
	// zero CreatedAt becomes now. An unknown principal is ErrNotFound,
	// a taken (method, issuer, subject) is ErrAlreadyExists, an unknown
	// method or an empty subject is an error.
	Create(ctx context.Context, i *Identity) error
	// Get returns the identity with id, or ErrNotFound.
	Get(ctx context.Context, id string) (*Identity, error)
	// Find returns the identity with the (method, issuer, subject)
	// triple, revoked or not, or ErrNotFound.
	Find(ctx context.Context, method, issuer, subject string) (*Identity, error)
	// ListByPrincipal returns every identity of a principal, oldest
	// first; an unknown principal lists nothing.
	ListByPrincipal(ctx context.Context, principalID string) ([]*Identity, error)
	// Revoke ends the identity at at, revokes every active web UI
	// session it minted (reason RevokeReasonIdentityRevoked) and
	// deletes the login codes it minted, in one transaction. A
	// revoked identity keeps its first revocation; an unknown id is
	// ErrNotFound.
	Revoke(ctx context.Context, id string, at time.Time) error
	// Touch records a use at at, but only when the last recorded use is
	// IdentityTouchEvery or more in the past (or unset), so a busy
	// credential writes at most once a minute. A revoked identity is
	// left alone; an unknown id is ErrNotFound.
	Touch(ctx context.Context, id string, at time.Time) error
}

// Ticket is a redeemed single-use sign-in ticket. The secret is never
// stored, only TicketHash of it, so a Ticket carries no secret either.
type Ticket struct {
	// Kind is one of TicketKinds.
	Kind string `json:"kind"`
	// PrincipalID is the principal the ticket signs in; empty for an
	// invite that creates one on redemption.
	PrincipalID string `json:"principal_id,omitempty"`
	// Payload is what the ticket grants, opaque to the store (the
	// grants column): audiences and roles for an invite.
	Payload json.RawMessage `json:"payload"`
	// CreatedBy is the principal that issued the ticket.
	CreatedBy  string     `json:"created_by"`
	CreatedAt  time.Time  `json:"created_at"`
	ExpiresAt  time.Time  `json:"expires_at"`
	RedeemedAt *time.Time `json:"redeemed_at,omitempty"`
}

// TicketIssue is what TicketStore.Issue mints a ticket for.
type TicketIssue struct {
	Kind string
	// Payload is stored as given; nil becomes {}.
	Payload json.RawMessage
	// TTL is how long the ticket can be redeemed; it must be positive.
	TTL time.Duration
	// CreatedBy is the issuing principal; it must exist.
	CreatedBy string
	// PrincipalID is the principal the ticket signs in; empty for an
	// invite. When set it must exist.
	PrincipalID string
	// Now is the issue time; zero means time.Now.
	Now time.Time
}

// TicketStore keeps invite and magic-link tickets.
type TicketStore interface {
	// Issue stores a ticket and returns its secret, shown once. An
	// unknown kind, a non-positive TTL or an empty CreatedBy is an
	// error; an unknown CreatedBy or PrincipalID is ErrNotFound.
	Issue(ctx context.Context, t TicketIssue) (secret string, err error)
	// Redeem marks the ticket for secret redeemed at now and returns it,
	// in one atomic step, so two redemptions of one secret cannot both
	// succeed. An unknown secret, a kind mismatch, a ticket already
	// redeemed or expired at now is ErrNotFound; a ticket whose
	// principal is suspended or revoked is ErrPrincipalInactive and
	// stays unredeemed.
	Redeem(ctx context.Context, kind, secret string, now time.Time) (*Ticket, error)
}

// KVStore is hop.top/kit's storage/kv.Store contract over the instance
// database: API key records live here under apikey/<id>. The method
// set is kit's verbatim, so the driver stores satisfy kit's interface
// unchanged. Close is a no-op: the driver owns the connection.
type KVStore interface {
	// Put stores value under key, replacing any previous value.
	Put(ctx context.Context, key string, value []byte) error
	// Get returns the value under key and whether one exists.
	Get(ctx context.Context, key string) ([]byte, bool, error)
	// Delete removes key; a missing key is not an error.
	Delete(ctx context.Context, key string) error
	// List returns the keys starting with prefix, sorted.
	List(ctx context.Context, prefix string) ([]string, error)
	// Close releases nothing: the driver's connection outlives the store.
	Close() error
}

// AuthStore gathers the principal registry stores of a driver.
type AuthStore interface {
	Principals() PrincipalStore
	Identities() IdentityStore
	Tickets() TicketStore
	KV() KVStore
}

// crockford is the ULID alphabet: base32 without I, L, O and U.
const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// NewULID returns a ULID (48-bit millisecond timestamp, 80 random
// bits, 26 Crockford base32 characters, lexically time-ordered).
func NewULID() string {
	var b [16]byte
	var ts [8]byte
	binary.BigEndian.PutUint64(ts[:], uint64(time.Now().UnixMilli())) // #nosec G115 -- current time is positive
	copy(b[:6], ts[2:])
	// crypto/rand.Read never returns an error: it crashes the program
	// when the system source is unavailable.
	_, _ = rand.Read(b[6:])
	return encodeULID(b)
}

// encodeULID writes the 128 bits of b as 26 base32 characters, the top
// two bits of the first character zero.
func encodeULID(b [16]byte) string {
	var out [26]byte
	for i := range out {
		var v byte
		for j := range 5 {
			bit := i*5 + j - 2
			var x byte
			if bit >= 0 {
				x = (b[bit/8] >> (7 - bit%8)) & 1
			}
			v = v<<1 | x
		}
		out[i] = crockford[v]
	}
	return string(out[:])
}

// NewPrincipalID returns a fresh prn_<ULID>.
func NewPrincipalID() string { return "prn_" + NewULID() }

// NewIdentityID returns a fresh idn_<ULID>.
func NewIdentityID() string { return "idn_" + NewULID() }

// ticketSecretBytes is the entropy of a ticket secret.
const ticketSecretBytes = 32

// NewTicketSecret returns 32 random bytes, base64url without padding.
func NewTicketSecret() (string, error) {
	b := make([]byte, ticketSecretBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("ticket secret: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// TicketHash is the hex SHA-256 of a ticket secret: the only form in
// which it is stored.
func TicketHash(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// RolesJSON encodes roles as the JSON array the roles column holds.
func RolesJSON(roles []string) (string, error) {
	if roles == nil {
		roles = []string{}
	}
	b, err := json.Marshal(roles)
	if err != nil {
		return "", fmt.Errorf("encode roles: %w", err)
	}
	return string(b), nil
}

// ParseRolesJSON decodes a roles column.
func ParseRolesJSON(s string) ([]string, error) {
	var roles []string
	if err := json.Unmarshal([]byte(s), &roles); err != nil {
		return nil, fmt.Errorf("decode roles %q: %w", s, err)
	}
	if roles == nil {
		roles = []string{}
	}
	return roles, nil
}

// PrepareNewPrincipal fills the defaults of a principal about to be
// created and validates it, as PrincipalStore.Create documents.
func PrepareNewPrincipal(p *Principal, now time.Time) error {
	if p == nil {
		return errors.New("principal: nil")
	}
	if p.Kind == PrincipalKindOwner {
		return ErrOwnerProtected
	}
	if !slices.Contains(PrincipalKinds, p.Kind) {
		return fmt.Errorf("principal: unknown kind %q (valid: %s)", p.Kind, strings.Join(PrincipalKinds, ", "))
	}
	if p.ID == "" {
		p.ID = NewPrincipalID()
	}
	if p.ID == OwnerPrincipal {
		return ErrOwnerProtected
	}
	if strings.TrimSpace(p.DisplayName) == "" {
		return errors.New("principal: display name is required")
	}
	if len(p.Roles) == 0 {
		p.Roles = slices.Clone(DefaultPrincipalRoles)
	}
	for _, r := range p.Roles {
		if r == "" || strings.ContainsAny(r, " \t\r\n\"") {
			return fmt.Errorf("principal: role %q: a role is one non-empty word", r)
		}
	}
	if p.Status == "" {
		p.Status = PrincipalActive
	}
	if !slices.Contains(PrincipalStatuses, p.Status) {
		return fmt.Errorf("principal: unknown status %q (valid: %s)", p.Status, strings.Join(PrincipalStatuses, ", "))
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	p.CreatedAt = p.CreatedAt.UTC()
	return nil
}

// PrepareNewIdentity fills the defaults of an identity about to be
// created and validates it, as IdentityStore.Create documents.
func PrepareNewIdentity(i *Identity, now time.Time) error {
	if i == nil {
		return errors.New("identity: nil")
	}
	if i.PrincipalID == "" {
		return errors.New("identity: principal id is required")
	}
	if !slices.Contains(IdentityMethods, i.Method) {
		return fmt.Errorf("identity: unknown method %q (valid: %s)", i.Method, strings.Join(IdentityMethods, ", "))
	}
	if i.Subject == "" {
		return errors.New("identity: subject is required")
	}
	if i.ID == "" {
		i.ID = NewIdentityID()
	}
	if i.CreatedAt.IsZero() {
		i.CreatedAt = now
	}
	i.CreatedAt = i.CreatedAt.UTC()
	return nil
}

// PrepareTicketIssue fills the defaults of a ticket issue and validates
// it, as TicketStore.Issue documents.
func PrepareTicketIssue(t *TicketIssue) error {
	if !slices.Contains(TicketKinds, t.Kind) {
		return fmt.Errorf("ticket: unknown kind %q (valid: %s)", t.Kind, strings.Join(TicketKinds, ", "))
	}
	if t.TTL <= 0 {
		return errors.New("ticket: ttl must be positive")
	}
	if t.CreatedBy == "" {
		return errors.New("ticket: created_by is required")
	}
	if len(t.Payload) == 0 {
		t.Payload = json.RawMessage("{}")
	}
	if t.Now.IsZero() {
		t.Now = time.Now()
	}
	t.Now = t.Now.UTC()
	return nil
}
