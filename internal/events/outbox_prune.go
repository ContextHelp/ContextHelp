package events

import (
	"context"
	"log/slog"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// DefaultPruneEvery is how often the outbox is pruned.
const DefaultPruneEvery = time.Hour

// Pruner deletes event_outbox rows older than Retention (server.events_retention,
// default 7 days). Run prunes once at start, then every Every, until ctx ends.
type Pruner struct {
	Store     storage.OutboxStore
	Retention time.Duration
	Every     time.Duration
	// Now is the clock; time.Now when nil.
	Now func() time.Time
}

// PruneOnce deletes the rows past retention and returns how many.
func (p *Pruner) PruneOnce(ctx context.Context) (int64, error) {
	now := time.Now
	if p.Now != nil {
		now = p.Now
	}
	return p.Store.Prune(ctx, now().Add(-p.Retention))
}

// Run prunes now and on every tick. A failed prune is logged and tried
// again on the next tick; Run returns when ctx ends.
func (p *Pruner) Run(ctx context.Context) error {
	every := p.Every
	if every <= 0 {
		every = DefaultPruneEvery
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if n, err := p.PruneOnce(ctx); err != nil {
			slog.Warn("events: outbox prune", "err", err)
		} else if n > 0 {
			slog.Info("events: outbox pruned", "rows", n)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
	}
}
