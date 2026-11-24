package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	// ErrObjectHeld is returned whenever a mutation or deletion targets a
	// knowledge object with one or more active (unreleased) holds.
	ErrObjectHeld = errors.New("object is under active hold")

	// ErrSourceErased is returned when an ingest or creation targets a
	// source_key that carries an erasure tombstone.
	ErrSourceErased = errors.New("source key was erased and tombstoned")

	// ErrObligationIDReused is returned when a bos.obligations/v1 request
	// reuses an existing obligation id with a different canonical body.
	ErrObligationIDReused = errors.New("obligation id reused with different body")
)

// ObjectHold is one legal or administrative hold placed on an object (or a
// bos source_key). Multiple concurrent holds may cover the same object; the
// object remains held until every active hold on it is released.
type ObjectHold struct {
	ID            string     `json:"id"`
	ObjectID      string     `json:"object_id"`
	SourceKey     string     `json:"source_key,omitempty"`
	HoldID        string     `json:"hold_id"`
	Source        string     `json:"source"`
	Reason        string     `json:"reason,omitempty"`
	PlacedBy      string     `json:"placed_by"`
	PlacedAt      time.Time  `json:"placed_at"`
	ReleasedBy    string     `json:"released_by,omitempty"`
	ReleasedAt    *time.Time `json:"released_at,omitempty"`
	ReleaseReason string     `json:"release_reason,omitempty"`
}

// HoldError wraps ErrObjectHeld and names the active holds blocking a mutation
// or deletion so every surface (CLI, HTTP, logs) reports both hold IDs and sources.
type HoldError struct {
	ObjectID string
	Holds    []ObjectHold
}

func (e *HoldError) Error() string {
	if len(e.Holds) == 0 {
		return fmt.Sprintf("object %s: %v", e.ObjectID, ErrObjectHeld)
	}
	parts := make([]string, 0, len(e.Holds))
	for _, h := range e.Holds {
		part := fmt.Sprintf("%s (source=%s)", h.HoldID, h.Source)
		if h.Reason != "" {
			part += fmt.Sprintf(" [%s]", h.Reason)
		}
		parts = append(parts, part)
	}
	return fmt.Sprintf("object %s: %v: %s", e.ObjectID, ErrObjectHeld, strings.Join(parts, ", "))
}

func (e *HoldError) Unwrap() error { return ErrObjectHeld }

// HoldFilter restricts active hold listings.
type HoldFilter struct {
	ObjectID  string
	SourceKey string
	HoldID    string
	Source    string
	Limit     int
	Offset    int
}

// HoldStore persists legal and operator holds on knowledge objects.
type HoldStore interface {
	// Place records an active hold on (h.ObjectID, h.HoldID). Calling Place
	// again while an active hold for the same pair already exists is a no-op.
	Place(ctx context.Context, h *ObjectHold) error
	// Release marks the active hold for (objectID, holdID) released at `at`
	// with releasedBy and reason. Returns true if an active hold was released,
	// false if no active hold matched. Released rows are retained for audit.
	Release(ctx context.Context, objectID, holdID, releasedBy, reason string, at time.Time) (bool, error)
	// ListActive returns all active (unreleased) holds on objectID, ordered by placed_at asc.
	ListActive(ctx context.Context, objectID string) ([]ObjectHold, error)
	// ListActiveBySourceKey returns active holds matching sourceKey (including
	// address-level holds placed before or alongside an object row).
	ListActiveBySourceKey(ctx context.Context, sourceKey string) ([]ObjectHold, error)
	// ListAllActive returns active holds matching filter, ordered by placed_at asc.
	ListAllActive(ctx context.Context, filter HoldFilter) ([]ObjectHold, error)
	// IsHeld reports whether objectID has at least one active hold.
	IsHeld(ctx context.Context, objectID string) (bool, error)
}

// ErasureTombstone records that a source_key was erased so feed replay or
// re-import never resurrects it. Stores addresses and metadata only — never content.
type ErasureTombstone struct {
	SourceKey    string    `json:"source_key"`
	Source       string    `json:"source"`
	ObligationID string    `json:"obligation_id,omitempty"`
	ErasedAt     time.Time `json:"erased_at"`
	ErasedBy     string    `json:"erased_by,omitempty"`
}

// TombstoneStore persists erasure tombstones keyed by source_key.
type TombstoneStore interface {
	// Put inserts or keeps an existing tombstone for t.SourceKey (idempotent).
	Put(ctx context.Context, t *ErasureTombstone) error
	// Exists reports whether sourceKey has been erased and tombstoned.
	Exists(ctx context.Context, sourceKey string) (bool, error)
	// Get returns the tombstone for sourceKey, or an error wrapping ErrNotFound.
	Get(ctx context.Context, sourceKey string) (*ErasureTombstone, error)
	// List returns tombstones ordered by erased_at desc.
	List(ctx context.Context, limit, offset int) ([]ErasureTombstone, error)
}

// ObligationRecord stores one bos.obligations/v1 request's idempotency fingerprint
// and its last computed status/receipt document.
type ObligationRecord struct {
	ID            string    `json:"id"`
	Kind          string    `json:"kind"`
	RequestSHA256 string    `json:"request_sha256"`
	State         string    `json:"state"`
	StatusJSON    string    `json:"status_json"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// ObligationStore persists bos.obligations/v1 requests and their status/receipts.
type ObligationStore interface {
	// Get returns the record for id, or an error wrapping ErrNotFound.
	Get(ctx context.Context, id string) (*ObligationRecord, error)
	// Save inserts rec if id does not exist; if id exists with the same
	// RequestSHA256 it updates state/status_json/updated_at; if id exists with
	// a different RequestSHA256 it returns ErrObligationIDReused.
	Save(ctx context.Context, rec *ObligationRecord) error
}
