package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	registrysync "github.com/ideacrafterslabs/ctxt/internal/registry"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// ErrPendingSandboxed refuses approval of a queued entity whose registry
// is now sandboxed: a sandboxed registry never writes to the local
// graph, approved or not.
var ErrPendingSandboxed = errors.New("registry is sandboxed")

// ErrPendingSelection is a malformed approval selection: no IDs and no
// registry, or IDs together with a registry-wide selection.
var ErrPendingSelection = errors.New("invalid pending entity selection")

// ListPendingEntities returns the entities untrusted registries served
// that wait for approval; registry, when set, keeps that registry's.
func (s *Service) ListPendingEntities(ctx context.Context, registry string) ([]*storage.PendingEntity, error) {
	return s.Store.PendingEntities().List(ctx, storage.PendingEntityFilter{RegistryName: registry})
}

// PendingSelection picks queued entities: IDs, or every one a registry
// has queued (All with Registry).
type PendingSelection struct {
	IDs      []string
	All      bool
	Registry string
}

// SelectPendingEntities resolves sel to queued entities without acting
// on them (what approve or reject would touch). Every ID must be
// queued, or nothing is returned.
func (s *Service) SelectPendingEntities(ctx context.Context, sel PendingSelection) ([]*storage.PendingEntity, error) {
	switch {
	case sel.All && sel.Registry == "":
		return nil, fmt.Errorf("%w: selecting all needs a registry", ErrPendingSelection)
	case sel.All && len(sel.IDs) > 0:
		return nil, fmt.Errorf("%w: pass IDs or all of a registry, not both", ErrPendingSelection)
	case !sel.All && len(sel.IDs) == 0:
		return nil, fmt.Errorf("%w: no pending entity IDs", ErrPendingSelection)
	}
	if sel.All {
		return s.ListPendingEntities(ctx, sel.Registry)
	}
	q := s.Store.PendingEntities()
	out := make([]*storage.PendingEntity, 0, len(sel.IDs))
	for _, id := range dedupe(sel.IDs) {
		p, err := q.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// ApprovePendingEntities writes the selected queued entities to the
// entity store the way a sync of a trusted registry would (the
// registry's sync_mode; full when it is no longer configured), with
// provenance "approved from <registry>", and drops them from the
// queue. Nothing is written when an ID is not queued or an entity's
// registry is now sandboxed. It returns the entities written, which on
// a write error are those written before it.
func (s *Service) ApprovePendingEntities(ctx context.Context, sel PendingSelection) ([]*storage.PendingEntity, error) {
	pending, err := s.SelectPendingEntities(ctx, sel)
	if err != nil {
		return nil, err
	}
	modes := make([]config.RegistrySyncMode, len(pending))
	for i, p := range pending {
		reg, ok := s.registryByName(p.RegistryName)
		if ok && reg.EffectiveTrustLevel() == config.RegistryTrustLevelSandboxed {
			return nil, fmt.Errorf("approve %s (%s from %s): %w", p.ID, p.Slug, p.RegistryName, ErrPendingSandboxed)
		}
		modes[i] = reg.SyncMode
		if modes[i] == "" {
			modes[i] = config.RegistrySyncModeFull
		}
	}

	syncer := registrysync.New(s.Store.Entities()).WithPendingQueue(s.Store.PendingEntities())
	done := make([]*storage.PendingEntity, 0, len(pending))
	for i, p := range pending {
		if err := syncer.Approve(ctx, p, modes[i]); err != nil {
			return done, fmt.Errorf("approve %s (%s from %s): %w", p.ID, p.Slug, p.RegistryName, err)
		}
		done = append(done, p)
	}
	return done, nil
}

// RejectPendingEntities drops the selected queued entities without
// writing them. Nothing is dropped when an ID is not queued. A rejected
// entity is queued again if its registry still serves it at the next
// sync.
func (s *Service) RejectPendingEntities(ctx context.Context, sel PendingSelection) ([]*storage.PendingEntity, error) {
	pending, err := s.SelectPendingEntities(ctx, sel)
	if err != nil {
		return nil, err
	}
	q := s.Store.PendingEntities()
	done := make([]*storage.PendingEntity, 0, len(pending))
	for _, p := range pending {
		if err := q.Delete(ctx, p.ID); err != nil {
			return done, fmt.Errorf("reject %s: %w", p.ID, err)
		}
		done = append(done, p)
	}
	return done, nil
}

// registryByName is the configured registry named name.
func (s *Service) registryByName(name string) (config.RegistryConfig, bool) {
	for _, r := range s.Cfg.Registries {
		if r.Name == name || (r.Name == "" && r.URL == name) {
			return r, true
		}
	}
	return config.RegistryConfig{}, false
}

func dedupe(ids []string) []string {
	seen := make(map[string]bool, len(ids))
	out := ids[:0:0]
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// RedactPending returns copies of ps with credentials stripped from the
// registry URLs, for output.
func RedactPending(ps []*storage.PendingEntity) []*storage.PendingEntity {
	out := make([]*storage.PendingEntity, len(ps))
	for i, p := range ps {
		c := *p
		c.RegistryURL = config.RedactDSN(p.RegistryURL)
		if p.Entity != nil {
			e := *p.Entity
			e.RegistryURL = config.RedactDSN(p.Entity.RegistryURL)
			c.Entity = &e
		}
		out[i] = &c
	}
	return out
}
