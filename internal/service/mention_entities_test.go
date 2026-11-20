package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline/steps"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	uri "hop.top/cite/scheme"
)

// mentionURIs returns the ctxt://entity/ mentions of slugs.
func mentionURIs(slugs ...string) []uri.URI {
	out := make([]uri.URI, 0, len(slugs))
	for _, s := range slugs {
		out = append(out, uri.URI{Scheme: "ctxt", Namespace: "entity", ID: s})
	}
	return out
}

// requireThinEntity fails unless slug has a thin entity row.
func requireThinEntity(t *testing.T, svc *Service, slug string) {
	t.Helper()
	e, err := svc.GetEntity(ownerCtx(), slug)
	require.NoError(t, err, "entity row %q", slug)
	assert.Equal(t, storage.ContentStatusThin, e.ContentStatus, "status of %q", slug)
}

// A pipeline ingest, with the entity_resolver step wired as the built-in
// registry wires it (no stores, a no-op), still leaves an entity row for
// every mention: the worker, the writer of the edges, ensures them.
func TestIngestMentionsCreateThinEntityRows(t *testing.T) {
	svc := newFanOutService(t)
	pipes := pipeline.NewRegistry()
	pipes.Register("test.builtin-resolver", &pipeline.Pipeline{
		PipelineName: "test.builtin-resolver",
		Steps:        []pipeline.PipelineStep{steps.NewEntityExtractor(), steps.NewEntityResolver()},
	})
	now := time.Now().Truncate(time.Second)
	runIngest(t, svc, pipes, &storage.Job{
		ID: "job-entity-rows", Type: "ingest:text", Status: storage.JobPending,
		Payload: "Met @person.alice", Pipeline: "test.builtin-resolver",
		Source: "test", MaxRetries: 3, CreatedAt: now, UpdatedAt: now,
		UserMentions: []string{"@product.foo", "@concept.bar"},
	})

	for _, slug := range []string{"product/foo", "concept/bar", "person/alice"} {
		requireThinEntity(t, svc, slug)
	}
}

// Fan-out writes mention edges, so it ensures their entity rows, and a
// richer entity already stored is left as it is.
func TestFanOutEnsuresEntityRowsKeepsFullEntity(t *testing.T) {
	svc := newFanOutService(t)
	ctx := ownerCtx()
	require.NoError(t, svc.store.Entities().Upsert(ctx, &storage.Entity{
		Slug: "org/acme", Title: "Acme Corp", Namespace: "org", Description: "the real one",
		ContentStatus: storage.ContentStatusFull, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}))
	seedObjectWithMentions(t, svc, "obj-ent-fan", mentionURIs("person/alice", "org/acme"))

	_, err := svc.FanOut(ctx, "obj-ent-fan")
	require.NoError(t, err)

	requireThinEntity(t, svc, "person/alice")
	acme, err := svc.GetEntity(ctx, "org/acme")
	require.NoError(t, err)
	assert.Equal(t, storage.ContentStatusFull, acme.ContentStatus)
	assert.Equal(t, "Acme Corp", acme.Title)
	assert.Equal(t, "the real one", acme.Description)
}

// GetEntity reads the dot form the CLI shows (@ns.name, ns.name) and the
// URI form as the namespace/name row, while a stored slug that itself
// holds a dot still matches exactly.
func TestGetEntityAcceptsMentionForms(t *testing.T) {
	svc := newFanOutService(t)
	ctx := ownerCtx()
	now := time.Now()
	for _, slug := range []string{"product/foo", "lib/v1.2"} {
		require.NoError(t, svc.store.Entities().UpsertThin(ctx, &storage.Entity{
			Slug: slug, Title: slug, Namespace: "x", ContentStatus: storage.ContentStatusThin, CreatedAt: now, UpdatedAt: now,
		}))
	}
	for _, in := range []string{"product/foo", "product.foo", "@product.foo", "ctxt://entity/product/foo"} {
		e, err := svc.GetEntity(ctx, in)
		require.NoError(t, err, in)
		assert.Equal(t, "product/foo", e.Slug, in)
	}
	e, err := svc.GetEntity(ctx, "lib/v1.2")
	require.NoError(t, err)
	assert.Equal(t, "lib/v1.2", e.Slug)

	_, err = svc.GetEntity(ctx, "product.nope")
	assert.ErrorIs(t, err, storage.ErrNotFound)
}
