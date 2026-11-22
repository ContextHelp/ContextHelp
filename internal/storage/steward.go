package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// The grant half of a steward grant (ADR-083 §2): what an approved
// `steward.grant` request wrote, a frozen share-policy scope for one
// named principal and a window. The request half (who asked, why, who
// approved) lives on the approval mechanism's tables; this row has its
// own lifecycle. Only `approved` and `revoked` are stored. `active`
// and `expired` are computed from the clock on every use, so no sweeper
// whose lateness could extend a grant exists.

// StewardGrantStatus is the stored status of a steward grant.
type StewardGrantStatus string

// The stored statuses.
const (
	// StewardGrantApproved: written by the final approval. Whether it
	// is in force is the clock's to say (StewardGrant.StateAt).
	StewardGrantApproved StewardGrantStatus = "approved"
	// StewardGrantRevoked: ended by a revocation. Terminal.
	StewardGrantRevoked StewardGrantStatus = "revoked"
)

// StewardGrantState is a grant's state at a moment: its stored status
// refined by the clock.
type StewardGrantState string

// The states a grant moves through.
const (
	// StewardStatePending: approved, and not_before is still ahead.
	StewardStatePending StewardGrantState = "approved"
	// StewardStateActive: approved, inside the window.
	StewardStateActive StewardGrantState = "active"
	// StewardStateExpired: approved, and the effective expiry has
	// passed. Computed, so not terminal: raising the duration cap brings
	// back a grant the cap expired. The stored status is still approved,
	// so the grant can still be revoked.
	StewardStateExpired StewardGrantState = "expired"
	// StewardStateRevoked: revoked. Terminal.
	StewardStateRevoked StewardGrantState = "revoked"
)

// ErrStewardGrantNotApproved is a revocation of a grant that is no
// longer approved: an earlier revocation closed it first.
var ErrStewardGrantNotApproved = errors.New("steward grant is not approved")

// StewardGrant is one approved steward grant.
type StewardGrant struct {
	// ID is stg_<ULID>.
	ID string `json:"id"`
	// RequestID is the approved request it came from; one grant per
	// request.
	RequestID string `json:"request_id"`
	// StewardID is the principal who reads under it.
	StewardID string `json:"steward_id"`
	// MatterRef is an optional external reference (a case or ticket
	// number), free text.
	MatterRef string `json:"matter_ref,omitempty"`
	// Scope is the frozen share-policy spec (JSON) the grant opens;
	// ScopeDigest is the digest of its canonical form. The store keeps
	// both verbatim and never recomputes either: the reader compares
	// them on every use, so a row edited behind the application's back
	// is refused, not trusted.
	Scope       json.RawMessage    `json:"scope"`
	ScopeDigest string             `json:"scope_digest"`
	Status      StewardGrantStatus `json:"status"`
	// NotBefore and ExpiresAt bound the window; the effective expiry
	// may be earlier (EffectiveExpiry).
	NotBefore time.Time `json:"not_before"`
	ExpiresAt time.Time `json:"expires_at"`
	// ApprovedAt is when the final approval wrote the grant.
	ApprovedAt time.Time `json:"approved_at"`
	// RevokedAt, RevokedBy and RevokeReason are set by Revoke.
	RevokedAt    *time.Time `json:"revoked_at,omitempty"`
	RevokedBy    string     `json:"revoked_by,omitempty"`
	RevokeReason string     `json:"revoke_reason,omitempty"`
	// RenewsID names the grant this one renews, when it does.
	RenewsID string `json:"renews_id,omitempty"`
}

// EffectiveExpiry is the moment the grant ends: min(expires_at,
// not_before + maxDuration). A cap lowered after approval shortens a
// live grant, since the cap is applied on every use. maxDuration <= 0
// means no cap.
func (g *StewardGrant) EffectiveExpiry(maxDuration time.Duration) time.Time {
	end := g.ExpiresAt
	if maxDuration > 0 {
		if capped := g.NotBefore.Add(maxDuration); capped.Before(end) {
			end = capped
		}
	}
	return end
}

// StateAt returns the grant's state at now under the duration cap
// maxDuration (steward.max_duration): revoked wins, then the window
// decides. An unknown stored status is not in force: it reads as
// pending, never active.
func (g *StewardGrant) StateAt(now time.Time, maxDuration time.Duration) StewardGrantState {
	switch {
	case g.Status == StewardGrantRevoked:
		return StewardStateRevoked
	case g.Status != StewardGrantApproved:
		return StewardStatePending
	case !now.Before(g.EffectiveExpiry(maxDuration)):
		return StewardStateExpired
	case now.Before(g.NotBefore):
		return StewardStatePending
	}
	return StewardStateActive
}

// StewardGrantFilter narrows StewardGrantStore.List. Every set field
// must hold.
type StewardGrantFilter struct {
	StewardID string
	Status    StewardGrantStatus
	// Limit caps the result; 0 means no cap.
	Limit int
}

// StewardGrantStore keeps steward grants.
type StewardGrantStore interface {
	// Create stores g in status approved. Every statement joins the
	// transaction the context carries (ApprovalStore.InTx), so the
	// approval kind writes the grant in the final approval's
	// transaction. An empty ID is assigned (NewStewardGrantID) and a
	// zero ApprovedAt becomes now. A missing request or steward, scope
	// or digest, a scope that is not a JSON object, a window that does
	// not end after it starts, or a status other than approved is an
	// error; an unknown request, steward or renewed grant is
	// ErrNotFound; a taken ID or a second grant for one request is
	// ErrAlreadyExists.
	Create(ctx context.Context, g *StewardGrant) error
	// Get returns the grant with id, or ErrNotFound.
	Get(ctx context.Context, id string) (*StewardGrant, error)
	// GetByRequest returns the grant request requestID produced, or
	// ErrNotFound.
	GetByRequest(ctx context.Context, requestID string) (*StewardGrant, error)
	// List returns the grants matching f, newest approval first.
	List(ctx context.Context, f StewardGrantFilter) ([]*StewardGrant, error)
	// Revoke ends grant id at at, by principal by, with a note.
	// Only an approved grant is revoked: a revoked one keeps its first
	// revocation and answers ErrStewardGrantNotApproved; an unknown id
	// is ErrNotFound. The window is the clock's: Revoke does not look
	// at it.
	Revoke(ctx context.Context, id, by, reason string, at time.Time) error
}

// NewStewardGrantID returns a fresh stg_<ULID>.
func NewStewardGrantID() string { return "stg_" + NewULID() }

// StewardGrantStatuses lists the stored statuses.
var StewardGrantStatuses = []StewardGrantStatus{StewardGrantApproved, StewardGrantRevoked}

// PrepareNewStewardGrant fills the defaults of a grant about to be
// created and validates it, as StewardGrantStore.Create documents.
func PrepareNewStewardGrant(g *StewardGrant, now time.Time) error {
	if g == nil {
		return errors.New("steward grant: nil")
	}
	g.RequestID, g.StewardID = strings.TrimSpace(g.RequestID), strings.TrimSpace(g.StewardID)
	g.MatterRef, g.RenewsID = strings.TrimSpace(g.MatterRef), strings.TrimSpace(g.RenewsID)
	switch {
	case g.RequestID == "":
		return errors.New("steward grant: request_id is required")
	case g.StewardID == "":
		return errors.New("steward grant: steward_id is required")
	case g.ScopeDigest == "":
		return errors.New("steward grant: scope_digest is required")
	case g.Status != "" && g.Status != StewardGrantApproved:
		return fmt.Errorf("steward grant: a new grant is %s, not %s", StewardGrantApproved, g.Status)
	}
	if err := validateScopeObject(g.Scope); err != nil {
		return err
	}
	g.NotBefore, g.ExpiresAt = g.NotBefore.UTC(), g.ExpiresAt.UTC()
	if g.NotBefore.IsZero() || !g.ExpiresAt.After(g.NotBefore) {
		return errors.New("steward grant: expires_at must be after not_before")
	}
	if g.ID == "" {
		g.ID = NewStewardGrantID()
	}
	if g.ApprovedAt.IsZero() {
		g.ApprovedAt = now
	}
	g.ApprovedAt = g.ApprovedAt.UTC()
	g.Status = StewardGrantApproved
	g.RevokedAt, g.RevokedBy, g.RevokeReason = nil, "", ""
	return nil
}

// validateScopeObject checks scope is a JSON object, the only shape a
// scope has. Whether it is a valid share-policy spec is internal/sharing's
// to say; the store keeps it opaque, as it keeps a policy's spec.
func validateScopeObject(scope json.RawMessage) error {
	trimmed := strings.TrimSpace(string(scope))
	if !json.Valid([]byte(trimmed)) || !strings.HasPrefix(trimmed, "{") {
		return errors.New("steward grant: scope must be a JSON object")
	}
	return nil
}

// ValidateStewardGrantRevocation checks the note of a revocation: who
// revoked it is required.
func ValidateStewardGrantRevocation(by string) error {
	if strings.TrimSpace(by) == "" {
		return errors.New("steward grant: revoked_by is required")
	}
	return nil
}

// ValidStewardGrantStatus reports whether s is a stored steward grant
// status.
func ValidStewardGrantStatus(s StewardGrantStatus) bool {
	return slices.Contains(StewardGrantStatuses, s)
}
