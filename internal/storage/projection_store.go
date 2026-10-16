package storage

import "context"

// ProjectionStore re-derives what ObjectStore.Create and Update store from
// the index projection (projection.ProjectIndex): projected_fts_body, the
// FTS index entry built from it, and fts_indexed. Each object row carries
// the projection version it was projected under (objects.projection_version);
// Create, Update and Reproject stamp the build-time version
// (indexsig.ProjectionVersion), so a row is stale exactly when its stamp
// differs. Both drivers' ObjectStore implement it.
//
// The per-row stamp is the re-projection job's progress record: a run that
// stops part-way leaves re-projected rows stamped, and the next run lists
// only the rest.
type ProjectionStore interface {
	// CountStaleProjections counts the objects not projected under the
	// current projection version.
	CountStaleProjections(ctx context.Context) (int, error)
	// ListStaleProjections returns up to limit stale object IDs greater than
	// after, in ID order.
	ListStaleProjections(ctx context.Context, after string, limit int) ([]string, error)
	// Reproject re-projects one object from its stored fields, rewrites its
	// FTS body and index entry, and stamps the current version, in one
	// transaction; updated_at is left alone. It reports whether the object
	// was stale: false, with a nil error, when the object is gone or
	// already current (a concurrent write re-projected it).
	Reproject(ctx context.Context, id string) (bool, error)
}

// RefTextStore repairs objects whose text_content is a blob:// ref.
// Before projection v3, storage copied an externalized object's ref into
// an empty text_content, so such a row has a pointer, not text, where
// search and embeddings read its text. The re-projection job resolves
// each ref and writes the text back through ReplaceRefText. Both
// drivers' ObjectStore implement it.
type RefTextStore interface {
	// ListRefText returns up to limit objects with an ID greater than
	// after, in ID order, whose text_content is a blob:// ref. Only ID,
	// RawContent, TextContent and Metadata are filled in.
	ListRefText(ctx context.Context, after string, limit int) ([]*KnowledgeObject, error)
	// ReplaceRefText sets object id's text_content to text, provided it
	// is still ref, and with truncated sets metadata
	// text_content_truncated (blobref.MetaTextTruncated). It marks the
	// row's projection stale so the re-projection re-indexes it, and
	// leaves updated_at alone: a repair is not an edit. It reports
	// whether it wrote; false when the object is gone or its text has
	// changed since it was listed.
	ReplaceRefText(ctx context.Context, id, ref, text string, truncated bool) (bool, error)
}
