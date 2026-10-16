package blob

import (
	"context"
	"fmt"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// DefaultSweepGrace is how old an unreferenced blob must be before Sweep
// deletes it, and before DeleteObject and UpdateObject delete one they
// free (they leave a younger one to Sweep).
//
// Ingest puts a blob before it writes the row that refers to it, so a
// young unreferenced blob may be one an ingest is still writing. Keys
// are content hashes: re-ingesting content an object being deleted also
// holds puts the blob again (refreshing its age) before the new row
// lands, and the grace keeps the sweep off it too.
const DefaultSweepGrace = time.Hour

// SweepOptions tunes Sweep. A zero Grace is DefaultSweepGrace; a zero
// Now is time.Now.
type SweepOptions struct {
	Grace  time.Duration
	Now    time.Time
	DryRun bool
}

// SweepResult counts what Sweep found.
type SweepResult struct {
	// Scanned is the number of blobs in the store.
	Scanned int
	// Deleted lists the unreferenced blobs older than the grace period
	// that were deleted, or would be on a dry run. A blob an object
	// refers to again by the time it is deleted is put back, and not
	// listed.
	Deleted []string
	// Young counts unreferenced blobs kept because they are younger than
	// the grace period, or of unknown age.
	Young int
	// Failed lists the blobs that could not be deleted.
	Failed []Orphan
}

// Sweep deletes the orphan blobs in drv's blob store: blobs no object
// refers to (CountBlobRefs) that are older than the grace period. Hard
// deletes and updates delete the blobs they free as they go; Sweep
// collects the rest: blobs they freed inside DefaultSweepGrace, blobs
// whose delete failed, blobs left by earlier releases, and blobs an
// ingest put but never wrote a row for.
//
// err reports that the store could not be listed or a reference count
// could not be read; a blob that can't be deleted is in Failed.
func Sweep(ctx context.Context, drv storage.StorageDriver, opts SweepOptions) (SweepResult, error) {
	var res SweepResult
	store := drv.Blobs()
	if store == nil {
		return res, ErrNoStore
	}
	grace := opts.Grace
	if grace <= 0 {
		grace = DefaultSweepGrace
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	items, err := store.List(ctx, "")
	if err != nil {
		return res, fmt.Errorf("sweep blobs: list: %w", err)
	}
	res.Scanned = len(items)
	for _, item := range items {
		n, err := drv.Objects().CountBlobRefs(ctx, item.Key)
		if err != nil {
			return res, fmt.Errorf("sweep blobs: %w", err)
		}
		if n > 0 {
			continue
		}
		if item.UpdatedAt.IsZero() || now.Sub(item.UpdatedAt) < grace {
			res.Young++
			continue
		}
		if !opts.DryRun {
			// The age was read at the listing: an ingest of the same
			// content may have put the blob since; remove catches it.
			removed, err := remove(ctx, drv.Objects(), store, item.Key)
			if err != nil {
				res.Failed = append(res.Failed, Orphan{Key: item.Key, Err: err})
				continue
			}
			if !removed {
				continue
			}
		}
		res.Deleted = append(res.Deleted, item.Key)
	}
	return res, nil
}
