package federation

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/accessmarker"
	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/dblock"
	"github.com/ideacrafterslabs/ctxt/internal/directopen"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// LocalPusher pushes objects to a local SQLite target through internal/directopen.
// Dedup: if object with same ContentHash already exists at target, skip.
// Edges: inserted for each successfully inserted object.
type LocalPusher struct {
	name       string
	targetPath string
	targetBlob *config.BlobConfig // the entry's blob setting; nil = default
	blobs      storage.BlobStore
}

// NewLocalPusher returns a LocalPusher that writes to targetPath. blobs
// is the source's store: each pushed object's blob is copied from it
// into the target's store, the one targetBlob (the entry's blob setting)
// configures — config.FileTargetBlob, blobs/ beside the target database
// when nil — so the target's blob:// refs resolve without the source.
func NewLocalPusher(name, targetPath string, targetBlob *config.BlobConfig, blobs storage.BlobStore) *LocalPusher {
	return &LocalPusher{name: name, targetPath: targetPath, targetBlob: targetBlob, blobs: blobs}
}

// copyBlobs copies the blobs obj's content refs name from src to dst,
// skipping those dst already holds. A blob src does not hold is an
// *ObjectError; any other read failure is the store's, not the
// object's.
func copyBlobs(ctx context.Context, src, dst storage.BlobStore, obj *storage.KnowledgeObject) error {
	for _, content := range []string{obj.RawContent, obj.TextContent} {
		key, ok := blob.BlobKey(content)
		if !ok {
			continue
		}
		if have, err := dst.Exists(ctx, key); err != nil {
			return fmt.Errorf("object %q: blob %s: check target: %w", obj.ID, key, err)
		} else if have {
			continue
		}
		if src == nil {
			return fmt.Errorf("object %q: blob %s: %w", obj.ID, key, blob.ErrNoStore)
		}
		rc, meta, err := src.Get(ctx, key)
		if err != nil {
			return objectBlobError(obj.ID, fmt.Errorf("blob %s: read source: %w", key, err))
		}
		err = dst.Put(ctx, key, rc, meta)
		rc.Close()
		if err != nil {
			return fmt.Errorf("object %q: blob %s: write target: %w", obj.ID, key, err)
		}
	}
	return nil
}

// writeObject writes obj's row into objs, its blobs into dst first: a
// row never lands without the blob its ref names. Once the row is
// written it copies again any blob dst no longer holds.
//
// A blob dst already holds is not copied, and its age is not refreshed:
// no target object may refer to it, and a delete or sweep at the target
// may be deleting it as this row lands. A delete deleting it after the
// second copy finds the row and puts it back (blob.Ensure has the same
// contract on ingest).
func writeObject(ctx context.Context, objs storage.ObjectStore, src, dst storage.BlobStore, obj *storage.KnowledgeObject) error {
	if err := copyBlobs(ctx, src, dst, obj); err != nil {
		return err
	}
	if err := objs.Create(ctx, obj); err != nil {
		return fmt.Errorf("create object %q: %w", obj.ID, err)
	}
	return copyBlobs(ctx, src, dst, obj)
}

// ErrLocalTargetNotPrivate is wrapped by every refusal to push into a
// local target whose access class is not private.
var ErrLocalTargetNotPrivate = errors.New("local federation target is not private")

// CheckLocalTarget refuses a local push into the database at targetPath
// unless its access marker is private or absent. A protected or public
// target is reached only through its daemon (a URL entry), whose
// federation token, policy engine and database lock a direct write would
// bypass. The refusal wraps both ErrLocalTargetNotPrivate and
// accessmarker.ErrRefused.
func CheckLocalTarget(targetPath string) error {
	err := accessmarker.Gate(targetPath)
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %w; configure the federation entry with the target daemon's URL (federation push over its API) instead of a path",
		ErrLocalTargetNotPrivate, err)
}

// Name returns the federation entry name from config.
func (p *LocalPusher) Name() string { return p.name }

// targetFor resolves the target database as a direct-open target.
// federation entries may declare targets as file:// URIs; canonicalPath
// strips the scheme and resolves the absolute path (same helper as
// detectCycles in worker.go).
func (p *LocalPusher) targetFor() (directopen.Target, error) {
	targetPath, err := canonicalPath(p.targetPath)
	if err != nil {
		return directopen.Target{}, fmt.Errorf("resolve target %q: %w", p.targetPath, err)
	}
	return directopen.Target{
		Type:  "sqlite",
		Path:  targetPath,
		Blob:  config.FileTargetBlob(targetPath, p.targetBlob),
		Label: "federation push " + p.name,
	}, nil
}

// openTarget opens the target database for writing like any direct
// writer (internal/directopen): access gate, the target's database lock
// (so never beside its daemon), migration. The caller closes the
// handle, which releases the lock.
func (p *LocalPusher) openTarget(ctx context.Context) (*directopen.Handle, error) {
	t, err := p.targetFor()
	if err != nil {
		return nil, err
	}
	if err := CheckLocalTarget(t.Path); err != nil {
		return nil, err
	}
	h, err := directopen.Write(ctx, t)
	if errors.Is(err, dblock.ErrHeld) {
		return nil, fmt.Errorf("target %q: %w; while its daemon runs, configure the federation entry with the daemon's URL instead of a path", t.Path, err)
	}
	if err != nil {
		return nil, fmt.Errorf("open target %q: %w", t.Path, err)
	}
	return h, nil
}

// Watermark returns the watermark the target database keeps for this
// federation; 0 when the database does not exist yet or has no schema.
// It only reads: no lock, so it answers beside the target's daemon.
func (p *LocalPusher) Watermark(ctx context.Context) (int64, error) {
	t, err := p.targetFor()
	if err != nil {
		return 0, fmt.Errorf("federation local watermark: %w", err)
	}
	if _, err := os.Stat(t.Path); errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err := CheckLocalTarget(t.Path); err != nil {
		return 0, fmt.Errorf("federation local watermark: %w", err)
	}
	h, err := directopen.Read(ctx, t)
	if errors.Is(err, storageutil.ErrNoSchema) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("federation local watermark: %w", err)
	}
	defer func() { _ = h.Close(ctx) }()
	return h.Driver.Watermarks().GetWatermark(ctx, p.name)
}

// Push upserts objects, edges, and entities into the target SQLite database.
// Objects with a matching ContentHash at the target are skipped (idempotent).
// Edges are inserted only for objects that were newly created. Entities are
// upserted by slug (UpsertThin) so a 'full' record at the target is never
// clobbered by a thin federation row — see ADR-049 + EntityStore.UpsertThin.
//
// On a successful non-empty batch, the federation_watermarks row at the
// target moves to the ChangeSeq of the last object (US-0319 AC #3).
// Empty batches are a no-op: no writes, no watermark advance (US-0319 AC #7).
//
// The watermark is written as the last step after all objects + edges +
// entities have been committed. If any per-row call returns an error the
// function returns early and the watermark is left untouched, so the next
// tick re-pushes the failed batch (US-0319 AC #4 crash recovery).
func (p *LocalPusher) Push(
	ctx context.Context,
	objects []storage.KnowledgeObject,
	edges []storage.Edge,
	entities []storage.Entity,
) error {
	if len(objects) == 0 {
		return nil
	}

	h, err := p.openTarget(ctx)
	if err != nil {
		return fmt.Errorf("federation local push: %w", err)
	}
	defer func() { _ = h.Close(ctx) }()
	drv, targetBlobs := h.Driver, h.Driver.Blobs()

	objs := drv.Objects()
	edgeStore := drv.Edges()
	entityStore := drv.Entities()
	insertedEdges := make(map[string]bool)

	// Upsert entities first so mention edges (object→entity) have valid
	// targets when ListFrom queries traverse them at the receiver.
	// UpsertThin preserves any 'full' record already at the target.
	for i := range entities {
		ent := &entities[i]
		if ent.Slug == "" {
			continue
		}
		if err := entityStore.UpsertThin(ctx, ent); err != nil {
			return fmt.Errorf("federation local push: upsert entity %q: %w",
				ent.Slug, err)
		}
	}

	for i := range objects {
		obj := &objects[i]
		if obj.ContentHash != "" {
			existing, _ := objs.GetByContentHash(ctx, obj.ContentHash, obj.ProfileID)
			if existing != nil {
				continue
			}
		}
		// rows with empty ContentHash (entity_page, etc.) skip the
		// content-hash dedup above and would fall through to Create, which
		// fails with UNIQUE-id when the same id already exists at the
		// target. Pre-check by id so we treat dupes as no-op skips instead
		// of aborting the whole batch.
		if obj.ContentHash == "" {
			if existing, _ := objs.Get(ctx, obj.ID); existing != nil {
				slog.Debug("federation local push: skipping pre-existing object id",
					"federation", p.name, "id", obj.ID, "type", obj.Type)
				continue
			}
		}
		if err := writeObject(ctx, objs, p.blobs, targetBlobs, obj); err != nil {
			// Defensive: a concurrent peer may have inserted this row
			// between our Get and Create. Treat UNIQUE-constraint races as
			// no-op skips so the rest of the batch + watermark still land.
			if isUniqueConstraintErr(err) {
				slog.Warn("federation local push: UNIQUE collision, skipping row",
					"federation", p.name, "id", obj.ID, "err", err)
				continue
			}
			return fmt.Errorf("federation local push: %w", err)
		}
		// Insert edges that belong to this object (dedup within this batch).
		for j := range edges {
			e := &edges[j]
			if e.FromID != obj.ID && e.ToID != obj.ID {
				continue
			}
			if insertedEdges[e.ID] {
				continue
			}
			if err := edgeStore.Create(ctx, e); err != nil {
				if isUniqueConstraintErr(err) {
					slog.Warn("federation local push: UNIQUE collision on edge, skipping",
						"federation", p.name, "edge", e.ID, "err", err)
					insertedEdges[e.ID] = true
					continue
				}
				return fmt.Errorf("federation local push: create edge %q: %w", e.ID, err)
			}
			insertedEdges[e.ID] = true
		}
	}

	// Advance watermark only after the batch has fully committed. If any
	// upsert above failed we returned early — watermark stays at last value.
	last := objects[len(objects)-1].ChangeSeq
	if err := drv.Watermarks().SetWatermark(ctx, p.name, last); err != nil {
		return fmt.Errorf("federation local push: advance watermark for %q: %w", p.name, err)
	}
	return nil
}

// Advance moves the watermark the target database keeps to seq.
func (p *LocalPusher) Advance(ctx context.Context, seq int64) error {
	h, err := p.openTarget(ctx)
	if err != nil {
		return fmt.Errorf("federation local advance: %w", err)
	}
	defer func() { _ = h.Close(ctx) }()
	if err := h.Driver.Watermarks().SetWatermark(ctx, p.name, seq); err != nil {
		return fmt.Errorf("federation local advance %q: %w", p.name, err)
	}
	return nil
}

// isUniqueConstraintErr reports whether the error is a SQLite UNIQUE
// constraint violation. The sqlite driver (mattn/go-sqlite3)
// surfaces the violation as text in the error string. We string-match
// instead of typing on the driver-specific error to keep this package
// driver-agnostic — same approach used by jobs/worker.go.
func isUniqueConstraintErr(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}
