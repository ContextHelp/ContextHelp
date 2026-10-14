package service

import (
	"context"
	"fmt"
	"maps"

	psteps "github.com/ideacrafterslabs/ctxt/internal/pipeline/steps"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blobref"
)

// MetaContentTruncated is the metadata flag a list-style read sets on an
// object whose content it returned as a preview. GetObject returns the
// whole content; metadata.blob_original_size is its length.
const MetaContentTruncated = "content_truncated"

// defaultPreviewBytes caps a list preview when storage.blob.threshold is
// unset (it matches the config default).
const defaultPreviewBytes = 65536

// Content above storage.blob.threshold is stored as a blob:// ref
// (externalize_content). The service is the one place that turns refs
// back into content: single-object reads get the whole content, list
// and search reads a preview of at most the threshold — the size an
// inline object can have — flagged MetaContentTruncated, so no client
// ever sees a ref and a list stays bounded. The write path re-applies the
// threshold, so content read here and written back stays externalized.
//
// text_content is never a ref: it is what search and embeddings index.
// When it is the content, the externalizer keeps a threshold-sized prefix
// of it inline, flagged psteps.MetaTextTruncated, and a single-object read
// hands back the whole content as the text.

// resolveContent replaces obj's blob refs with the content they point to.
func (s *Service) resolveContent(ctx context.Context, obj *storage.KnowledgeObject) error {
	if obj == nil || !hasBlobRef(obj) {
		return nil
	}
	raw, err := blob.Content(ctx, s.Store.Blobs(), obj.RawContent)
	if err != nil {
		return fmt.Errorf("object %s: %w", obj.ID, err)
	}
	text, err := blob.Content(ctx, s.Store.Blobs(), obj.TextContent)
	if err != nil {
		return fmt.Errorf("object %s: %w", obj.ID, err)
	}
	// The text is the content when it is a prefix of it, or when there
	// is none: the default storage applies to inline content.
	if truncated, _ := obj.Metadata[psteps.MetaTextTruncated].(bool); truncated || text == "" {
		text = raw
		delete(obj.Metadata, psteps.MetaTextTruncated)
	}
	obj.RawContent, obj.TextContent = raw, text
	return nil
}

// previewContent replaces each object's blob refs with a preview of at
// most previewBytes of the content, flagging the ones it cut short.
func (s *Service) previewContent(ctx context.Context, objs []*storage.KnowledgeObject) error {
	limit := s.previewBytes()
	for _, obj := range objs {
		if obj == nil || !hasBlobRef(obj) {
			continue
		}
		raw, rawCut, err := blob.Preview(ctx, s.Store.Blobs(), obj.RawContent, limit)
		if err != nil {
			return fmt.Errorf("object %s: %w", obj.ID, err)
		}
		text, textCut, err := blob.Preview(ctx, s.Store.Blobs(), obj.TextContent, limit)
		if err != nil {
			return fmt.Errorf("object %s: %w", obj.ID, err)
		}
		obj.RawContent, obj.TextContent = raw, text
		if rawCut || textCut {
			if obj.Metadata == nil {
				obj.Metadata = map[string]any{}
			}
			obj.Metadata[MetaContentTruncated] = true
		}
	}
	return nil
}

func (s *Service) previewBytes() int64 {
	if t := s.Cfg.Storage.Blob.Threshold; t > 0 {
		return t
	}
	return defaultPreviewBytes
}

func hasBlobRef(obj *storage.KnowledgeObject) bool {
	return blob.IsBlobRef(obj.RawContent) || blob.IsBlobRef(obj.TextContent)
}

// storable returns the row to write for obj: a copy whose content is
// externalized again when it is above the threshold, and whose preview
// (MetaContentTruncated) is replaced by the ref it was read from, so
// writing back what a read returned never inlines or truncates content.
// obj itself is left as the caller has it.
func (s *Service) storable(ctx context.Context, obj *storage.KnowledgeObject) (*storage.KnowledgeObject, error) {
	cp := *obj
	cp.Metadata = maps.Clone(obj.Metadata)

	textTruncated, _ := cp.Metadata[psteps.MetaTextTruncated].(bool)
	delete(cp.Metadata, psteps.MetaTextTruncated)

	if truncated, _ := cp.Metadata[MetaContentTruncated].(bool); truncated {
		key, _ := cp.Metadata["blob_key"].(string)
		if key == "" {
			return nil, fmt.Errorf("object %s: content is a truncated preview with no blob_key; write the full content", obj.ID)
		}
		// The text a preview carries is the stored prefix, or the
		// content's own preview: a prefix of the content either way.
		if textTruncated || cp.TextContent == cp.RawContent {
			cp.Metadata[psteps.MetaTextTruncated] = true
		}
		cp.RawContent = blobref.For(key)
		delete(cp.Metadata, MetaContentTruncated)
		return &cp, nil
	}
	delete(cp.Metadata, MetaContentTruncated)

	threshold := s.Cfg.Storage.Blob.Threshold
	if threshold <= 0 || s.Store.Blobs() == nil || blob.IsBlobRef(cp.RawContent) || int64(len(cp.RawContent)) <= threshold {
		if textTruncated && blob.IsBlobRef(cp.RawContent) {
			cp.Metadata[psteps.MetaTextTruncated] = true
		}
		return &cp, nil
	}
	out, err := psteps.NewExternalizer(s.Store.Blobs(), threshold).Run(ctx, &cp)
	if err != nil {
		return nil, fmt.Errorf("object %s: %w", obj.ID, err)
	}
	return out, nil
}
