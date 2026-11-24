package steps

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blobref"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// MetaTextTruncated flags an externalized object whose text_content is
// a prefix of its content rather than all of it (see Externalizer.Run).
// A reader that resolves the content gets the whole text back from it.
const MetaTextTruncated = blobref.MetaTextTruncated

// Externalizer moves content above a size threshold to a blob store.
type Externalizer struct {
	pipeline.BaseContract
	store     storage.BlobStore
	threshold int64
}

func NewExternalizer(store storage.BlobStore, threshold int64) *Externalizer {
	return &Externalizer{
		BaseContract: pipeline.NewBaseContract(pipeline.StepContract{
			Binary:       true,
			Requires:     []string{"RawContent"},
			Produces:     []string{"RawContent", "TextContent", "Metadata"},
			Capabilities: []string{"blob-externalize"},
		}),
		store:     store,
		threshold: threshold,
	}
}

func (e *Externalizer) Name() string { return "externalize_content" }

// ExternalizeBinary moves draft's content to store when it cannot live
// in a text column (blobref.NeedsBlob), whatever the size threshold, and
// leaves any other content alone. Ingest calls it after the pipeline so a
// pipeline without externalize_content cannot store binary inline.
func ExternalizeBinary(ctx context.Context, store storage.BlobStore, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	if !blobref.NeedsBlob(draft.RawContent) {
		return draft, nil
	}
	if store == nil {
		return nil, fmt.Errorf("externalize: binary content needs a blob store")
	}
	return NewExternalizer(store, 0).Run(ctx, draft)
}

func (e *Externalizer) Run(ctx context.Context, draft *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	binary := blobref.NeedsBlob(draft.RawContent)
	if !binary && (e.threshold <= 0 || int64(len(draft.RawContent)) <= e.threshold) {
		return draft, nil
	}

	// Binary is an original: keyed by its exact bytes, so it shares a
	// blob with the original stored before any step ran. Text keeps the
	// normalised dedup hash.
	hash := blob.OriginalKey(draft.RawContent)
	if !binary {
		hash = storageutil.ContentHash(draft.RawContent, draft.Source)
	}

	// The original stored before the steps ran is already in the store
	// under this key, with its own content type: do not put it again
	// (blob.Ensure still covers a racing delete, from BlobContent).
	orig, isOrig := blob.OriginalOf(draft)
	isOrig = isOrig && orig.Key == hash
	contentType := draft.ContentType
	if !binary && !strings.HasPrefix(contentType, "text/") {
		// text externalized from a PDF, office file or medium is text,
		// not that file's type
		contentType = "text/plain; charset=utf-8"
	}
	if isOrig {
		contentType = orig.ContentType
	} else {
		err := e.store.Put(ctx, hash, strings.NewReader(draft.RawContent), storage.BlobMeta{
			ContentType: contentType,
			Size:        int64(len(draft.RawContent)),
			ContentHash: hash,
		})
		if err != nil {
			return nil, fmt.Errorf("externalize: blob put: %w", err)
		}
	}

	if draft.Metadata == nil {
		draft.Metadata = make(map[string]any)
	}
	draft.Metadata[blob.MetaBlobKey] = hash
	draft.Metadata["blob_original_size"] = int64(len(draft.RawContent))
	draft.Metadata["blob_content_type"] = contentType

	// text_content is what search and embeddings index, so it must stay
	// text: when it is the content (empty or equal), keep a prefix of at
	// most threshold bytes inline, the most an inline object holds, and
	// flag it. Text an earlier step set apart from the content (extracted
	// from a document, say) is left alone.
	// Binary has no text to keep: its text_content is empty.
	switch {
	case binary && (draft.TextContent == "" || draft.TextContent == draft.RawContent || blobref.NeedsBlob(draft.TextContent)):
		draft.TextContent = ""
	case draft.TextContent == "" || draft.TextContent == draft.RawContent:
		draft.TextContent = TextPrefix(draft.RawContent, e.threshold)
		draft.Metadata[MetaTextTruncated] = true
	}

	// Kept until the row is written: if a delete of the same key
	// removes the blob meanwhile, ingest puts it back (blob.Ensure).
	if draft.BlobContent == nil {
		draft.BlobContent = make(map[string]string)
	}
	draft.BlobContent[hash] = draft.RawContent
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
