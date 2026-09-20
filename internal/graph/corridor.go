package graph

import (
	"context"
	"log/slog"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Corridor is a background job that periodically validates graph consistency.
// It checks for orphaned edges and logs violations; does not auto-repair.
type Corridor struct {
	es        storage.EntityStore
	edgeStore storage.EdgeStore
	interval  time.Duration
	log       *slog.Logger
}

// NewCorridor creates a Corridor. interval controls how often the check runs.
// Pass slog.Default() if you have no custom logger.
func NewCorridor(es storage.EntityStore, edgeStore storage.EdgeStore, interval time.Duration, log *slog.Logger) *Corridor {
	if log == nil {
		log = slog.Default()
	}
	return &Corridor{
		es:        es,
		edgeStore: edgeStore,
		interval:  interval,
		log:       log,
	}
}

// Run starts the corridor loop. It blocks until ctx is cancelled.
func (c *Corridor) Run(ctx context.Context) {
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.runOnce(ctx)
		}
	}
}

// runOnce performs a single consistency pass.
func (c *Corridor) runOnce(ctx context.Context) {
	entities, err := c.es.List(ctx, storage.EntityFilter{})
	if err != nil {
		c.log.ErrorContext(ctx, "corridor: list entities", "err", err)
		return
	}

	total := 0
	for _, e := range entities {
		orphans, err := FindOrphanedEdges(ctx, c.es, c.edgeStore, "entity", e.Slug)
		if err != nil {
			c.log.WarnContext(ctx, "corridor: orphan check failed", "entity", e.Slug, "err", err)
			continue
		}
		for _, id := range orphans {
			c.log.WarnContext(ctx, "corridor: orphaned edge", "edge_id", id, "entity", e.Slug)
			total++
		}

		// Cycle detection: outgoing edges from this entity.
		err = DetectCycles(ctx, e.Slug, func(ctx context.Context, id string) ([]string, error) {
			edges, err := c.edgeStore.ListFrom(ctx, "entity", id)
			if err != nil {
				return nil, err
			}
			var targets []string
			for _, edge := range edges {
				if edge.ToType == "entity" {
					targets = append(targets, mentionToSlug(edge.ToID))
				}
			}
			return targets, nil
		})
		if err != nil {
			c.log.WarnContext(ctx, "corridor: cycle detected", "entity", e.Slug, "err", err)
			total++
		}
	}

	c.log.InfoContext(ctx, "corridor: pass complete",
		"entities_checked", len(entities),
		"violations", total,
	)
}
