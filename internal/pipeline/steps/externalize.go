package steps

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blobref"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// MetaTextTruncated flags an externalized object whose text_content is
// a prefix of its content rather than all of it (see Externalizer.Run).
// A reader that resolves the content gets the whole text back from it.
const MetaTextTruncated = "text_content_truncated"

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
			Produces:     []string{"RawContent", "TextContent", "Metadata"},
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

	// text_content is what search and embeddings index, so it must stay
	// text: when it is the content (empty or equal), keep a prefix of at
	// most threshold bytes inline, the most an inline object holds, and
	// flag it. Text an earlier step set apart from the content (extracted
	// from a document, say) is left alone.
	if draft.TextContent == "" || draft.TextContent == draft.RawContent {
		draft.TextContent = TextPrefix(draft.RawContent, e.threshold)
		draft.Metadata[MetaTextTruncated] = true
	}

	draft.RawContent = blobref.For(hash)
	return draft, nil
}

// TextPrefix returns at most max bytes of s, cut back to a rune boundary.
func TextPrefix(s string, max int64) string {
	if max <= 0 || int64(len(s)) <= max {
		return s
	}
	cut := s[:max]
	// Drop a rune the cap split; at most utf8.UTFMax-1 trailing bytes.
	for i := 0; i < utf8.UTFMax-1 && cut != "" && !utf8.ValidString(cut); i++ {
		cut = cut[:len(cut)-1]
	}
	return cut
}
