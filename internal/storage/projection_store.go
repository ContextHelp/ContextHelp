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
