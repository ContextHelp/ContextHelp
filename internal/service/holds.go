package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

var (
	// ErrInvalidHold is returned when a hold request is missing required
	// fields or violates reason length bounds (1..1000 chars).
	ErrInvalidHold = errors.New("invalid hold request")

	// ErrHoldAuthority is returned when a caller attempts to release a hold
	// owned by another authority (e.g., an operator releasing a bos hold).
	ErrHoldAuthority = errors.New("caller lacks authority over this hold")
)

const (
	maxHoldReasonLen      = 1000
	defaultOperatorSource = "operator"
)

// PlaceHoldOptions configures placing an object or source_key hold.
type PlaceHoldOptions struct {
	ObjectID  string `json:"object_id,omitempty"`
	SourceKey string `json:"source_key,omitempty"`
	HoldID    string `json:"hold_id"`
	Source    string `json:"source,omitempty"`
	Reason    string `json:"reason"`
	PlacedBy  string `json:"placed_by,omitempty"`
}

// ReleaseHoldOptions configures releasing an active hold.
type ReleaseHoldOptions struct {
	ObjectID   string `json:"object_id,omitempty"`
	SourceKey  string `json:"source_key,omitempty"`
	HoldID     string `json:"hold_id"`
	Source     string `json:"source,omitempty"`
	ReleasedBy string `json:"released_by,omitempty"`
	Reason     string `json:"reason"`
}

// PlaceHold validates and places an active hold on an object (by ObjectID)
// or all objects matching SourceKey. Returns the placed hold record(s).
func (s *Service) PlaceHold(ctx context.Context, opts PlaceHoldOptions) ([]storage.ObjectHold, error) {
	holdID := strings.TrimSpace(opts.HoldID)
	reason := strings.TrimSpace(opts.Reason)
	if holdID == "" {
		return nil, fmt.Errorf("%w: hold_id is required", ErrInvalidHold)
	}
	if reason == "" || len(reason) > maxHoldReasonLen {
		return nil, fmt.Errorf("%w: reason must be 1..%d characters", ErrInvalidHold, maxHoldReasonLen)
	}
	objID := strings.TrimSpace(opts.ObjectID)
	srcKey := strings.TrimSpace(opts.SourceKey)
	if objID == "" && srcKey == "" {
		return nil, fmt.Errorf("%w: object_id or source_key is required", ErrInvalidHold)
	}
	src := strings.TrimSpace(opts.Source)
	if src == "" {
		src = defaultOperatorSource
	}
	placedBy := strings.TrimSpace(opts.PlacedBy)
	if placedBy == "" {
		placedBy = src
	}

	drv := s.Store(ctx)
	targets, err := resolveHoldTargets(ctx, drv, objID, srcKey)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	placed := make([]storage.ObjectHold, 0, len(targets))
	for _, target := range targets {
		h := &storage.ObjectHold{
			ObjectID:  target.id,
			SourceKey: target.sourceKey,
			HoldID:    holdID,
			Source:    src,
			Reason:    reason,
			PlacedBy:  placedBy,
			PlacedAt:  now,
		}
		if err := drv.Holds().Place(ctx, h); err != nil {
			return nil, fmt.Errorf("place hold: %w", err)
		}
		placed = append(placed, *h)
		s.logHoldAudit(ctx, "hold.placed", h.ObjectID, placedBy, map[string]any{
			"hold_id":    holdID,
			"source":     src,
			"source_key": h.SourceKey,
			"reason":     reason,
		})
	}
	return placed, nil
}

// ReleaseHold releases matching active hold(s) after verifying authority:
// a hold placed by a non-operator authority (such as "bos") can only be
// released by that same authority.
func (s *Service) ReleaseHold(ctx context.Context, opts ReleaseHoldOptions) (int, error) {
	holdID := strings.TrimSpace(opts.HoldID)
	if holdID == "" {
		return 0, fmt.Errorf("%w: hold_id is required", ErrInvalidHold)
	}
	objID := strings.TrimSpace(opts.ObjectID)
	srcKey := strings.TrimSpace(opts.SourceKey)
	src := strings.TrimSpace(opts.Source)
	if src == "" {
		src = defaultOperatorSource
	}
	releasedBy := strings.TrimSpace(opts.ReleasedBy)
	if releasedBy == "" {
		releasedBy = src
	}
	reason := strings.TrimSpace(opts.Reason)
	if len(reason) > maxHoldReasonLen {
		return 0, fmt.Errorf("%w: reason exceeds %d characters", ErrInvalidHold, maxHoldReasonLen)
	}

	drv := s.Store(ctx)
	active, err := drv.Holds().ListAllActive(ctx, storage.HoldFilter{
		ObjectID:  objID,
		SourceKey: srcKey,
		HoldID:    holdID,
	})
	if err != nil {
		return 0, fmt.Errorf("release hold: list active: %w", err)
	}
	for _, h := range active {
		if h.Source != "" && h.Source != defaultOperatorSource && h.Source != src {
			return 0, fmt.Errorf("%w: hold %s belongs to authority %q", ErrHoldAuthority, h.HoldID, h.Source)
		}
	}

	now := time.Now().UTC()
	released := 0
	for _, h := range active {
		ok, rerr := drv.Holds().Release(ctx, h.ObjectID, h.HoldID, releasedBy, reason, now)
		if rerr != nil {
			return released, fmt.Errorf("release hold %s on %s: %w", h.HoldID, h.ObjectID, rerr)
		}
		if ok {
			released++
			s.logHoldAudit(ctx, "hold.released", h.ObjectID, releasedBy, map[string]any{
				"hold_id":        h.HoldID,
				"source":         src,
				"source_key":     h.SourceKey,
				"release_reason": reason,
			})
		}
	}
	return released, nil
}

// ListHolds lists active holds matching filter.
func (s *Service) ListHolds(ctx context.Context, filter storage.HoldFilter) ([]storage.ObjectHold, error) {
	return s.Store(ctx).Holds().ListAllActive(ctx, filter)
}

type holdTarget struct {
	id        string
	sourceKey string
}

func resolveHoldTargets(ctx context.Context, drv storage.StorageDriver, objID, srcKey string) ([]holdTarget, error) {
	if objID != "" {
		obj, err := drv.Objects().Get(ctx, objID)
		if err != nil {
			return nil, err
		}
		sk := srcKey
		if sk == "" {
			sk = obj.SourceKey
		}
		return []holdTarget{{id: obj.ID, sourceKey: sk}}, nil
	}
	objs, _, err := drv.Objects().List(ctx, storage.ObjectFilter{
		SourceKey: srcKey,
		Status:    storage.ObjectStatusAll,
	})
	if err != nil {
		return nil, fmt.Errorf("resolve hold targets for %q: %w", srcKey, err)
	}
	if len(objs) == 0 {
		// Record an address-level hold anchored on source_key even if no object exists yet.
		return []holdTarget{{id: "source:" + srcKey, sourceKey: srcKey}}, nil
	}
	out := make([]holdTarget, 0, len(objs))
	for _, o := range objs {
		out = append(out, holdTarget{id: o.ID, sourceKey: srcKey})
	}
	return out, nil
}

func (s *Service) logHoldAudit(ctx context.Context, eventType, objectID, actor string, payload map[string]any) {
	auditStore := s.Store(ctx).AuditLog()
	if auditStore == nil {
		return
	}
	entry := &storage.AuditEntry{
		ID:        uuid.New().String(),
		EventType: eventType,
		ObjectID:  objectID,
		Actor:     actor,
		Payload:   payload,
		CreatedAt: time.Now().UTC(),
	}
	if err := auditStore.Append(ctx, entry); err != nil {
		slog.Warn("hold: audit append (non-fatal)", "event", eventType, "object_id", objectID, "err", err)
	}
}
