package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// The sharing registry (ADR-080 §1, §2, §9): audiences of principals,
// named share policies, the grants that bind an audience to a policy,
// each principal's class ceiling, the policy an API key narrows its
// principal to, and the read audit of non-owner requests.
//
// Scopes govern these rows, not share policies: the auth chain reads a
// principal's grants to compile its viewer before one exists, and a
// restricted viewer lists its own audiences and policies. The accessor
// therefore passes through ScopedDriver like Auth does.

// Audience is a named, owner-managed group of principals.
type Audience struct {
	// ID is aud_<ULID>.
	ID string `json:"id"`
	// Name is unique per instance ("colleagues", "partners").
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// AudienceMember is one principal's membership of an audience, with
// its optional expiry.
type AudienceMember struct {
	AudienceID  string     `json:"audience_id"`
	PrincipalID string     `json:"principal_id"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	// AddedBy is the principal that added the member.
	AddedBy string    `json:"added_by"`
	AddedAt time.Time `json:"added_at"`
}

// ActiveAt reports whether the membership is still in force at now.
func (m *AudienceMember) ActiveAt(now time.Time) bool {
	return m != nil && (m.ExpiresAt == nil || m.ExpiresAt.After(now))
}

// SharePolicy is a named, versioned, reusable visibility rule. Spec is
// the policy's JSON (the YAML of ADR-080 §2 encoded by internal/sharing);
// the store keeps it opaque beyond checking it is a JSON object.
type SharePolicy struct {
	// ID is pol_<ULID>.
	ID string `json:"id"`
	// Name is unique per instance.
	Name string `json:"name"`
	// Version starts at 1 and the store bumps it on every Update;
	// compiled viewers key their cache on it.
	Version   int             `json:"version"`
	Spec      json.RawMessage `json:"spec"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// ShareGrant gives an audience a policy, until it expires or is revoked.
type ShareGrant struct {
	// ID is grt_<ULID>.
	ID         string     `json:"id"`
	AudienceID string     `json:"audience_id"`
	PolicyID   string     `json:"policy_id"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	// CreatedBy is the principal that granted.
	CreatedBy string     `json:"created_by"`
	CreatedAt time.Time  `json:"created_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}

// ActiveAt reports whether the grant is in force at now: not revoked
// and not expired.
func (g *ShareGrant) ActiveAt(now time.Time) bool {
	return g != nil && g.RevokedAt == nil && (g.ExpiresAt == nil || g.ExpiresAt.After(now))
}

// ActiveGrant is one grant that applies to a principal at a moment:
// the grant, the audience the principal reaches it through, and the
// policy at its current version.
type ActiveGrant struct {
	Grant    ShareGrant  `json:"grant"`
	Audience Audience    `json:"audience"`
	Policy   SharePolicy `json:"policy"`
}

// GrantFilter narrows ShareGrantStore.List.
type GrantFilter struct {
	// AudienceID and PolicyID, when set, keep matching grants only.
	AudienceID string
	PolicyID   string
	// IncludeRevoked keeps revoked grants; by default they are left out.
	IncludeRevoked bool
}

// AccessEntry is one non-owner request in the read audit (ADR-080 §9),
// shaped as a receipt: who asked for what through which route, the
// decision, the ordered checks behind it, and how much the answer
// carried, under which policy versions.
type AccessEntry struct {
	// ID is acc_<ULID>.
	ID          string    `json:"id"`
	At          time.Time `json:"at"`
	PrincipalID string    `json:"principal_id"`
	// IdentityID is the credential the request authenticated with;
	// empty when the method carries none.
	IdentityID string `json:"identity_id,omitempty"`
	Route      string `json:"route"`
	// Decision is the verdict on the request.
	Decision AccessDecision `json:"decision"`
	// Checks are the checks the request met, in evaluation order. A
	// check it never reached is absent; none is recorded as PASS
	// without having run.
	Checks []AccessCheck `json:"checks"`
	// OperationDigest identifies the operation asked for: a versioned
	// hash over the method, the route template and the sorted params
	// (sharing.OperationDigest). It holds no raw value.
	OperationDigest string `json:"operation_digest"`
	// PolicyVersions is the compiled viewer's policy key.
	PolicyVersions string `json:"policy_versions"`
	ObjectCount    int    `json:"object_count"`
	EntityCount    int    `json:"entity_count"`
	// ObjectIDs are the objects the response carried. They live in
	// access_log_objects and are pruned by retention; the counts stay.
	ObjectIDs []string `json:"object_ids,omitempty"`
	// StewardGrantID is the steward grant an elevated request named
	// (ADR-083 §5), whether it was used or refused; empty for every
	// other request. A steward entry is durable and chained
	// (AppendSteward), and sharing.audit.retention never prunes its
	// object ids: steward.audit_retention does.
	StewardGrantID string `json:"steward_grant_id,omitempty"`
	// Chain is a steward entry's place in the tamper-evident chain;
	// nil for every other entry. Not part of the API answer.
	Chain *AccessChainLink `json:"-"`
}

// AccessFilter narrows AccessLogStore.Query.
type AccessFilter struct {
	// PrincipalID keeps entries of that principal; ObjectID keeps
	// entries whose ID list names that object. Both may be set.
	PrincipalID string
	ObjectID    string
	// Decision keeps entries with that decision; empty keeps all.
	Decision AccessDecision
	// StewardGrantID keeps the entries of requests elevated into that
	// steward grant.
	StewardGrantID string
	// HideStewardObjects is for a caller that may not see the object id
	// lists of steward entries (ADR-083 §5: approve:steward holders and
	// the owner only): ObjectID does not match through them, and steward
	// entries come back without ObjectIDs. Their routes and counts stay.
	HideStewardObjects bool
	// Since and Until bound At: Since inclusive, Until exclusive; zero
	// means unbounded.
	Since time.Time
	Until time.Time
	// Limit caps the result; 0 means no cap.
	Limit int
}

// AudienceStore keeps audiences and their members.
type AudienceStore interface {
	// Create stores a. An empty ID is assigned (NewAudienceID) and a
	// zero CreatedAt becomes now. An empty name is an error; a taken ID
	// or name is ErrAlreadyExists.
	Create(ctx context.Context, a *Audience) error
	// Get returns the audience with id, or ErrNotFound.
	Get(ctx context.Context, id string) (*Audience, error)
	// GetByName returns the audience named name, or ErrNotFound.
	GetByName(ctx context.Context, name string) (*Audience, error)
	// List returns every audience, by name.
	List(ctx context.Context) ([]*Audience, error)
	// AddMember stores m, replacing an existing membership of the same
	// principal (its expiry, adder and time). A zero AddedAt becomes
	// now; an empty AddedBy is an error; an unknown audience or
	// principal is ErrNotFound.
	AddMember(ctx context.Context, m *AudienceMember) error
	// RemoveMember ends the membership; a membership that does not
	// exist is ErrNotFound.
	RemoveMember(ctx context.Context, audienceID, principalID string) error
	// ListMembers returns the audience's memberships, expired ones
	// included, oldest first.
	ListMembers(ctx context.Context, audienceID string) ([]*AudienceMember, error)
	// ListMemberships returns the principal's memberships in force at
	// now, oldest first.
	ListMemberships(ctx context.Context, principalID string, now time.Time) ([]*AudienceMember, error)
}

// SharePolicyStore keeps share policies.
type SharePolicyStore interface {
	// Create stores p at version 1. An empty ID is assigned
	// (NewSharePolicyID) and a zero UpdatedAt becomes now. An empty
	// name or a spec that is not a JSON object is an error; a taken ID
	// or name is ErrAlreadyExists.
	Create(ctx context.Context, p *SharePolicy) error
	// Get returns the policy with id, or ErrNotFound.
	Get(ctx context.Context, id string) (*SharePolicy, error)
	// GetByName returns the policy named name, or ErrNotFound.
	GetByName(ctx context.Context, name string) (*SharePolicy, error)
	// List returns every policy, by name.
	List(ctx context.Context) ([]*SharePolicy, error)
	// Update replaces the spec of policy id at now, bumps its version
	// and returns the row. A spec that is not a JSON object is an
	// error; an unknown id is ErrNotFound.
	Update(ctx context.Context, id string, spec json.RawMessage, now time.Time) (*SharePolicy, error)
}

// ShareGrantStore keeps grants.
type ShareGrantStore interface {
	// Create stores g. An empty ID is assigned (NewShareGrantID) and a
	// zero CreatedAt becomes now. An empty CreatedBy is an error; an
	// unknown audience or policy is ErrNotFound; a taken ID is
	// ErrAlreadyExists.
	Create(ctx context.Context, g *ShareGrant) error
	// Get returns the grant with id, or ErrNotFound.
	Get(ctx context.Context, id string) (*ShareGrant, error)
	// List returns grants matching f, oldest first.
	List(ctx context.Context, f GrantFilter) ([]*ShareGrant, error)
	// Revoke ends the grant at at. A revoked grant keeps its first
	// revocation; an unknown id is ErrNotFound.
	Revoke(ctx context.Context, id string, at time.Time) error
	// ListActiveForPrincipal returns the grants in force for principal
	// at now: through a membership that has not expired, on a grant
	// that is neither revoked nor expired, oldest grant first. An
	// unknown principal has none.
	ListActiveForPrincipal(ctx context.Context, principalID string, now time.Time) ([]*ActiveGrant, error)
}

// AccessLogStore keeps the read audit of non-owner requests.
type AccessLogStore interface {
	// Append stores entries in one transaction. An empty ID is assigned
	// (NewAccessID) and a zero At becomes now; an empty principal or
	// route, a decision other than allowed, denied or deferred, an empty
	// operation digest, a check without id or reason or with an unknown
	// outcome, a denied entry with no FAIL check and a deferred entry
	// with no UNKNOWN check are errors, and so is a steward entry
	// (StewardGrantID set), which only AppendSteward writes. Nothing is
	// written for a batch with an invalid entry.
	Append(ctx context.Context, entries []*AccessEntry) error
	// AppendSteward stores one steward entry (ADR-083 §5) durably and
	// chained: in one transaction it takes the chain head (a row lock on
	// Postgres; SQLite's single writer already serialises), links e after
	// it (LinkAccessEntry, which sets e.Chain and keeps At to the
	// millisecond), writes e with its object ids and moves the head. When
	// it returns nil the entry is committed. The entry is validated as
	// Append validates, its object ids sorted and deduplicated; an entry
	// without StewardGrantID is an error.
	AppendSteward(ctx context.Context, e *AccessEntry) error
	// Query returns entries matching f, newest first, their ObjectIDs
	// filled from what retention has kept, StewardGrantID and Chain
	// filled for steward entries.
	Query(ctx context.Context, f AccessFilter) ([]*AccessEntry, error)
	// Prune deletes the object ID lists of entries logged before
	// before and returns how many IDs it removed. The entries and their
	// counts stay. Steward entries are exempt (sharing.audit.retention
	// does not govern them); PruneSteward prunes theirs.
	Prune(ctx context.Context, before time.Time) (int64, error)
	// PruneSteward deletes the object ID lists of steward entries logged
	// before before (steward.audit_retention) and returns how many IDs it
	// removed. The entries, their counts and their chain stay.
	PruneSteward(ctx context.Context, before time.Time) (int64, error)
	// StewardChain returns the chain head and every steward entry in
	// append order, with its link and the object ids retention kept, for
	// VerifyAccessChain.
	StewardChain(ctx context.Context) (AccessChainHead, []*AccessEntry, error)
}

// SharingStore gathers the sharing registry stores of a driver.
type SharingStore interface {
	Audiences() AudienceStore
	Policies() SharePolicyStore
	Grants() ShareGrantStore
	AccessLog() AccessLogStore
	// StewardGrants keeps the grant half of the steward grants
	// (ADR-083 §2): the approved requests' frozen scopes and windows.
	StewardGrants() StewardGrantStore
	// DenyClasses returns the principal's ceiling (principals.deny_classes):
	// the classes it may never see, whatever its grants say. An unknown
	// principal is ErrNotFound.
	DenyClasses(ctx context.Context, principalID string) ([]string, error)
	// SetDenyClasses replaces the principal's ceiling. An invalid class
	// name is an error; an unknown principal is ErrNotFound.
	SetDenyClasses(ctx context.Context, principalID string, classes []string) error
	// IdentityPolicy returns the share policy the identity is bound to
	// (principal_identities.share_policy_id), "" when unbound. An
	// unknown identity is ErrNotFound.
	IdentityPolicy(ctx context.Context, identityID string) (string, error)
	// BindIdentityPolicy binds the identity to policy id policyID, or
	// unbinds it when policyID is "". An unknown identity or policy is
	// ErrNotFound.
	BindIdentityPolicy(ctx context.Context, identityID, policyID string) error
	// InTx runs fn in one database transaction and commits when fn
	// returns nil; an error or a panic rolls back every write fn made.
	// The context fn receives carries the transaction: the audience,
	// policy and grant stores, the ceiling and the key-policy binding
	// of this store, and the driver's AuditLog and Approvals, called
	// with it, read and write inside it, and an InTx called with it
	// joins it. This is how a sharing change is classified and written
	// in one transaction (ADR-083 §10 B), so a concurrent change cannot
	// slip between the two. The access log is not part of it.
	InTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// NewAudienceID returns a fresh aud_<ULID>.
func NewAudienceID() string { return "aud_" + NewULID() }

// NewSharePolicyID returns a fresh pol_<ULID>.
func NewSharePolicyID() string { return "pol_" + NewULID() }

// NewShareGrantID returns a fresh grt_<ULID>.
func NewShareGrantID() string { return "grt_" + NewULID() }

// NewAccessID returns a fresh acc_<ULID>.
func NewAccessID() string { return "acc_" + NewULID() }

// sharingNamePattern bounds audience and policy names: a lowercase
// slug, as the CLI and policy YAML name them.
var sharingNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// ValidateSharingName reports whether name may name an audience or a
// policy.
func ValidateSharingName(what, name string) error {
	if !sharingNamePattern.MatchString(name) {
		return fmt.Errorf("%s name %q: want 1-64 chars of [a-z0-9._-], starting alphanumeric", what, name)
	}
	return nil
}

// PrepareNewAudience fills the defaults of an audience about to be
// created and validates it, as AudienceStore.Create documents.
func PrepareNewAudience(a *Audience, now time.Time) error {
	if a == nil {
		return errors.New("audience: nil")
	}
	a.Name = strings.TrimSpace(a.Name)
	if err := ValidateSharingName("audience", a.Name); err != nil {
		return err
	}
	if a.ID == "" {
		a.ID = NewAudienceID()
	}
	if a.CreatedAt.IsZero() {
		a.CreatedAt = now
	}
	a.CreatedAt = a.CreatedAt.UTC()
	return nil
}

// PrepareNewMember fills the defaults of a membership about to be
// stored and validates it, as AudienceStore.AddMember documents.
func PrepareNewMember(m *AudienceMember, now time.Time) error {
	if m == nil {
		return errors.New("audience member: nil")
	}
	if m.AudienceID == "" || m.PrincipalID == "" {
		return errors.New("audience member: audience id and principal id are required")
	}
	if m.AddedBy == "" {
		return errors.New("audience member: added_by is required")
	}
	if m.AddedAt.IsZero() {
		m.AddedAt = now
	}
	m.AddedAt = m.AddedAt.UTC()
	if m.ExpiresAt != nil {
		t := m.ExpiresAt.UTC()
		m.ExpiresAt = &t
	}
	return nil
}

// ValidatePolicySpec reports whether spec is a JSON object, the only
// shape the store accepts for a policy spec.
func ValidatePolicySpec(spec json.RawMessage) error {
	trimmed := strings.TrimSpace(string(spec))
	if !json.Valid([]byte(trimmed)) || !strings.HasPrefix(trimmed, "{") {
		return errors.New("share policy: spec must be a JSON object")
	}
	return nil
}

// PrepareNewSharePolicy fills the defaults of a policy about to be
// created and validates it, as SharePolicyStore.Create documents.
func PrepareNewSharePolicy(p *SharePolicy, now time.Time) error {
	if p == nil {
		return errors.New("share policy: nil")
	}
	p.Name = strings.TrimSpace(p.Name)
	if err := ValidateSharingName("share policy", p.Name); err != nil {
		return err
	}
	if err := ValidatePolicySpec(p.Spec); err != nil {
		return err
	}
	if p.ID == "" {
		p.ID = NewSharePolicyID()
	}
	p.Version = 1
	if p.UpdatedAt.IsZero() {
		p.UpdatedAt = now
	}
	p.UpdatedAt = p.UpdatedAt.UTC()
	return nil
}

// PrepareNewShareGrant fills the defaults of a grant about to be
// created and validates it, as ShareGrantStore.Create documents.
func PrepareNewShareGrant(g *ShareGrant, now time.Time) error {
	if g == nil {
		return errors.New("share grant: nil")
	}
	if g.AudienceID == "" || g.PolicyID == "" {
		return errors.New("share grant: audience id and policy id are required")
	}
	if g.CreatedBy == "" {
		return errors.New("share grant: created_by is required")
	}
	if g.ID == "" {
		g.ID = NewShareGrantID()
	}
	if g.CreatedAt.IsZero() {
		g.CreatedAt = now
	}
	g.CreatedAt = g.CreatedAt.UTC()
	if g.ExpiresAt != nil {
		t := g.ExpiresAt.UTC()
		g.ExpiresAt = &t
	}
	g.RevokedAt = nil
	return nil
}

// PrepareAccessEntry fills the defaults of an entry about to be
// appended and validates it, as AccessLogStore.Append documents.
func PrepareAccessEntry(e *AccessEntry, now time.Time) error {
	if e != nil && e.StewardGrantID != "" {
		return errors.New("access entry: a steward entry is appended durably and chained (AppendSteward)")
	}
	return prepareAccessEntry(e, now)
}

// prepareAccessEntry is the validation and defaults both kinds of entry
// share.
func prepareAccessEntry(e *AccessEntry, now time.Time) error {
	if err := ValidateAccessEntry(e); err != nil {
		return err
	}
	if e.Checks == nil {
		e.Checks = []AccessCheck{}
	}
	if e.ID == "" {
		e.ID = NewAccessID()
	}
	if e.At.IsZero() {
		e.At = now
	}
	e.At = e.At.UTC()
	return nil
}
