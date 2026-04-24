package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// Runner executes an adapter and persists results.
type Runner struct {
	store storage.ObjectStore
}

// NewRunner creates a runner wired to the given object store.
func NewRunner(store storage.ObjectStore) *Runner {
	return &Runner{store: store}
}

// Run executes the adapter once, deduplicates by source_key
// (adapter.Name() + ":" + obj.ID), and creates new objects.
func (r *Runner) Run(ctx context.Context, adapter Adapter) (*Result, error) {
	start := time.Now()
	objects, err := adapter.Fetch(ctx)
	if err != nil {
		return nil, fmt.Errorf("adapter %s: fetch: %w", adapter.Name(), err)
	}

	res := &Result{
		Source: adapter.Name(),
		Total: len(objects),
	}

	for _, obj := range objects {
		sourceKey := adapter.Name() + ":" + obj.ID
		if err := r.upsert(ctx, adapter.Name(), sourceKey, obj); err != nil {
			res.Errors++
			continue
		}
		// upsert determines created vs skipped internally;
		// count optimistically as created (dedup returns early)
		res.Created++
	}

	res.Elapsed = time.Since(start)
	return res, nil
}

// RunFromStdin reads a JSON array of Objects from r and ingests them
// using the given source name for dedup keys.
func (r *Runner) RunFromStdin(
	ctx context.Context, source string, reader io.Reader,
) (*Result, error) {
	start := time.Now()

	var objects []Object
	if err := json.NewDecoder(reader).Decode(&objects); err != nil {
		return nil, fmt.Errorf("decode stdin: %w", err)
	}

	res := &Result{
		Source: source,
		Total: len(objects),
	}

	for _, obj := range objects {
		sourceKey := source + ":" + obj.ID
		if err := r.upsert(ctx, source, sourceKey, obj); err != nil {
			res.Errors++
			continue
		}
		res.Created++
	}

	res.Elapsed = time.Since(start)
	return res, nil
}

func (r *Runner) upsert(
	ctx context.Context, source, sourceKey string, obj Object,
) error {
	existing, err := r.store.GetBySourceKey(ctx, sourceKey)
	if err != nil {
		return err
	}
	if existing != nil {
		return nil // already ingested
	}

	now := time.Now().Truncate(time.Second)
	tags := make([]storage.Tag, 0, len(obj.Tags))
	for _, t := range obj.Tags {
		tags = append(tags, storage.Tag{Label: t})
	}

	ko := &storage.KnowledgeObject{
		ID:          uuid.New().String(),
		Type:        obj.Type,
		RawContent:  obj.Content,
		TextContent: obj.Content,
		Source:      source,
		SourceKey:   sourceKey,
		ContentHash: storageutil.ContentHash(obj.Content, source),
		Tags:        tags,
		Metadata:    obj.Metadata,
		Status:      "raw",
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	return r.store.Create(ctx, ko)
}
