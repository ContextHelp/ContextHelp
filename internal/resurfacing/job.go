package resurfacing

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// Backend is the narrow storage interface needed by the resurfacing job.
type Backend interface {
	Objects() storage.ObjectStore
	Resurfacing() storage.ResurfacingQueueStore
}

// Job periodically scores all knowledge objects against the active profile
// and maintains the resurfacing_queue table.
type Job struct {
	store    Backend
	cfg      config.ResurfacingConfig
	interval time.Duration
}

// NewJob creates a new resurfacing job.
func NewJob(store Backend, cfg config.ResurfacingConfig) *Job {
	interval := cfg.RunInterval
	if interval <= 0 {
		interval = time.Hour
	}
	return &Job{store: store, cfg: cfg, interval: interval}
}

// Run starts the background loop; blocks until ctx is cancelled.
// Profile name is used as the profile_id for queue entries.
func (j *Job) Run(ctx context.Context, profileName string, profile config.FocusProfile) error {
	if !j.cfg.Enabled {
		return nil
	}
	ticker := time.NewTicker(j.interval)
	defer ticker.Stop()

	// Score immediately on start.
	if err := j.score(ctx, profileName, profile); err != nil {
		fmt.Printf("resurfacing job: initial score: %v\n", err)
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := j.score(ctx, profileName, profile); err != nil {
				fmt.Printf("resurfacing job: score: %v\n", err)
			}
		}
	}
}

// RunOnce scores once synchronously; useful for testing and one-shot refresh.
func (j *Job) RunOnce(ctx context.Context, profileName string, profile config.FocusProfile) error {
	return j.score(ctx, profileName, profile)
}

func (j *Job) score(ctx context.Context, profileID string, profile config.FocusProfile) error {
	// Gather all active objects.
	objs, _, err := j.store.Objects().List(ctx, storage.ObjectFilter{
		Limit:  0, // 0 = no limit in most stores; caller should handle large sets
		Status: "active",
	})
	if err != nil {
		return fmt.Errorf("list objects: %w", err)
	}

	// Build entity slug list from profile hints and mention_namespaces.
	profileSlugs := profileEntitySlugs(profile)

	now := time.Now()
	for _, obj := range objs {
		res := Score(ScoreInput{
			Object:             obj,
			Profile:            profile,
			ProfileEntitySlugs: profileSlugs,
			Now:                now,
		})

		if res.Score < j.cfg.MinScore {
			continue
		}

		entry := &storage.ResurfacingEntry{
			ID:        deterministicID(obj.ID, profileID),
			ObjectID:  obj.ID,
			ProfileID: profileID,
			Score:     res.Score,
			Reason:    res.Reason,
			CreatedAt: now,
		}
		if err := j.store.Resurfacing().Upsert(ctx, entry); err != nil {
			// Non-fatal; log and continue.
			fmt.Printf("resurfacing job: upsert %s: %v\n", obj.ID, err)
		}
	}
	return nil
}

// profileEntitySlugs extracts entity slug hints from a profile's Tags and
// MentionNamespaces fields. These drive entity overlap scoring.
func profileEntitySlugs(profile config.FocusProfile) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, ns := range profile.MentionNamespaces {
		if _, ok := seen[ns]; !ok {
			seen[ns] = struct{}{}
			out = append(out, ns)
		}
	}
	for k := range profile.RerankBoosts {
		if _, ok := seen[k]; !ok {
			seen[k] = struct{}{}
			out = append(out, k)
		}
	}
	return out
}

// deterministicID derives a stable entry ID from object + profile IDs so that
// re-scoring an object updates the existing row rather than creating duplicates.
func deterministicID(objectID, profileID string) string {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(objectID+"|"+profileID)).String()
}
