package steps

import (
	"context"
	"fmt"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// Externalizer moves content above a size threshold to a blob store.
type Externalizer struct {
	pipeline.BaseContract
	store     storage.BlobStore
	threshold int64
}

func NewExternalizer(store storage.BlobStore, threshold int64) *Externalizer {
	return &Externalizer{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Requires:     []string{"RawContent"},
			Produces:     []string{"RawContent", "Metadata"},
			Capabilities: []string{"blob-externalize"},
		}),
		store:     store,
		threshold: threshold,
	}
}

func (e *Externalizer) Name() string { return "externalize_content" }

func (e *Externalizer) Run(ctx context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if e.threshold <= 0 {
		return draft, nil
	}

	if int64(len(draft.RawContent)) <= e.threshold {
		return draft, nil
	}

	hash := storageutil.ContentHash(draft.RawContent, draft.Source)

	err := e.store.Put(ctx, hash, strings.NewReader(draft.RawContent), storage.BlobMeta{
		ContentType: draft.ContentType,
		Size:        int64(len(draft.RawContent)),
		ContentHash: hash,
	})
	if err != nil {
		return nil, fmt.Errorf("externalize: blob put: %w", err)
	}

	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	draft.Metadata["blob_key"] = hash
	draft.Metadata["blob_original_size"] = int64(len(draft.RawContent))
	draft.Metadata["blob_content_type"] = draft.ContentType

	draft.RawContent = "blob://" + hash
	return draft, nil
}
