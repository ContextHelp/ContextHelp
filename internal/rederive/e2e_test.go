package rederive

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// The scheduling path end to end on the real store and registry: two
// pipelines, one step of one of them is bumped; the job is scheduled the
// way the daemon does, run as its handler, and only the objects of the
// bumped pipeline are re-derived and stamped.
func TestEndToEnd_OnlyTheBumpedPipelineIsReDerived(t *testing.T) {
	e := newRealEnv(t)
	ctx := context.Background()
	page1 := e.ingest("url.generic", e.srv.URL+"/a", "")
	page2 := e.ingest("url.generic", e.srv.URL+"/b", "")
	short := e.ingest("text.short", "", "a short note about nothing in particular")
	e.record()
	require.Contains(t, e.get(short.ID).Pipeline, "text.short")
	before := e.snapshot(short.ID)
	shortSig := e.sigOf(short.ID)
	hits := e.hits.Load()

	// Nothing is stale yet: nothing is scheduled.
	q := jobs.NewQueue(e.driver.Jobs())
	j, err := e.job().Schedule(ctx, q)
	require.NoError(t, err)
	require.Nil(t, j)

	newSig := e.bump("url.generic", "sectioner")
	scheduled, err := e.job().Schedule(ctx, q)
	require.NoError(t, err)
	require.NotNil(t, scheduled)

	msg, err := e.job().Handle(ctx, scheduled)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(msg, "re-derived 2 and stamped 0 of 2 objects"), msg)

	for _, id := range []string{page1.ID, page2.ID} {
		require.Equal(t, newSig, e.sigOf(id), "bumped pipeline: re-derived and stamped")
	}
	require.Equal(t, shortSig, e.sigOf(short.ID))
	require.Equal(t, before, e.snapshot(short.ID), "the other pipeline's object is untouched")
	require.Equal(t, hits, e.hits.Load(), "stored-content mode: no fetch")
	require.Zero(t, staleObjects(t, e.store(), e.reg))
	again, err := e.job().Schedule(ctx, q)
	require.NoError(t, err)
	require.Nil(t, again, "nothing left to do: nothing scheduled")
	_ = storage.JobPending
}

// After a start, every signature the registry can produce has recorded
// entries: each pipeline's current one and its baseline, pipelines left
// out for a missing provider included.
func TestRecordSignatures_EverySignatureHasEntries(t *testing.T) {
	e := newRealEnv(t)
	n, err := RecordSignatures(context.Background(), e.store(), e.reg)
	require.NoError(t, err)
	require.Positive(t, n)
	want := map[string]bool{}
	for _, s := range pipeline.CurrentSignatures(e.reg) {
		want[s] = true
	}
	for _, s := range pipeline.Baselines(e.reg) {
		want[s] = true
	}
	require.NotEmpty(t, want)
	for sig := range want {
		entries, ok, err := e.store().DerivationSignatureEntries(context.Background(), sig)
		require.NoError(t, err)
		require.True(t, ok, "signature %s has no recorded entries", sig)
		require.NotEmpty(t, entries, sig)
	}
	// The recorded entries are the ones the signature hashes.
	for _, name := range e.reg.List() {
		p, err := e.reg.Get(name)
		require.NoError(t, err)
		got, _, _ := e.store().DerivationSignatureEntries(context.Background(), p.Signature)
		require.Equal(t, p.Entries, got, name)
		base, _, _ := e.store().DerivationSignatureEntries(context.Background(), p.BaselineSignature)
		require.Equal(t, p.BaselineEntries, base, name)
	}
	// Recording twice changes nothing.
	n2, err := RecordSignatures(context.Background(), e.store(), e.reg)
	require.NoError(t, err)
	require.Equal(t, n, n2)
}

// Once a step is bumped, the baseline signature (every step at version 1)
// differs from the current one and keeps its recorded entries: it is what
// an object derived before signatures were recorded carries.
func TestRecordSignatures_BaselineStaysRecordedAfterABump(t *testing.T) {
	e := newRealEnv(t)
	cur := e.bump("url.generic", "sectioner")
	p, err := e.reg.Get("url.generic")
	require.NoError(t, err)
	require.Equal(t, cur, p.Signature)
	require.NotEqual(t, p.Signature, p.BaselineSignature)
	got, ok, err := e.store().DerivationSignatureEntries(context.Background(), p.BaselineSignature)
	require.NoError(t, err)
	require.True(t, ok, "the baseline signature has no recorded entries")
	require.Equal(t, p.BaselineEntries, got)
	cur2, ok, err := e.store().DerivationSignatureEntries(context.Background(), p.Signature)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, p.Entries, cur2)
}
