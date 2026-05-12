package sqlite

import "context"

// RunEmbeddingsBackfillForTest exposes migrate033EmbeddingsBackfill so the
// internal/embeddings/registry test harness can re-run the backfill on a
// driver whose schema_version table already records the migration as
// applied. Production code paths must not call this — they get the
// backfill via Driver.Init / Migrate.
func RunEmbeddingsBackfillForTest(ctx context.Context, d *Driver) error {
	return migrate033EmbeddingsBackfill(ctx, d)
}
