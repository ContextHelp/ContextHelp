package federation

import (
	"context"
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// LocalPusher pushes objects to a local SQLite target via storageutil.NewDriver.
// Dedup: if object with same ContentHash already exists at target, skip.
// Edges: inserted for each successfully inserted object.
type LocalPusher struct {
	name       string
	targetPath string
}

// NewLocalPusher returns a LocalPusher that writes to targetPath.
func NewLocalPusher(name, targetPath string) *LocalPusher {
	return &LocalPusher{name: name, targetPath: targetPath}
}

// Name returns the federation entry name from config.
func (p *LocalPusher) Name() string { return p.name }

// Push upserts objects and their edges into the target SQLite database.
// Objects with a matching ContentHash at the target are skipped (idempotent).
// Edges are inserted only for objects that were newly created.
func (p *LocalPusher) Push(ctx context.Context, objects []storage.KnowledgeObject, edges []storage.Edge) error {
	drv, err := storageutil.NewDriver("sqlite", p.targetPath)
	if err != nil {
		return fmt.Errorf("federation local push: open target %q: %w", p.targetPath, err)
	}
	defer drv.Close(ctx)

	if err := drv.Init(ctx); err != nil {
		return fmt.Errorf("federation local push: init target %q: %w", p.targetPath, err)
	}

	objs := drv.Objects()
	edgeStore := drv.Edges()
	insertedEdges := make(map[string]bool)

	for i := range objects {
		obj := &objects[i]
		if obj.ContentHash != "" {
			existing, _ := objs.GetByContentHash(ctx, obj.ContentHash)
			if existing != nil {
				continue
			}
		}
		if err := objs.Create(ctx, obj); err != nil {
			return fmt.Errorf("federation local push: create object %q: %w", obj.ID, err)
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
				return fmt.Errorf("federation local push: create edge %q: %w", e.ID, err)
			}
			insertedEdges[e.ID] = true
		}
	}
	return nil
}
