package rederive

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/federation"
	"github.com/ideacrafterslabs/ctxt/internal/jobs"
	"github.com/ideacrafterslabs/ctxt/internal/pipeline"
	"github.com/ideacrafterslabs/ctxt/internal/search"
	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

func snapshotOf(t *testing.T, drv storage.StorageDriver, id string) string {
	t.Helper()
	o, err := drv.Objects().Get(ctxSystem(), id)
	require.NoError(t, err)
	require.NotNil(t, o)
	b, err := json.Marshal(o)
	require.NoError(t, err)
	return string(b)
}

func inbound(id string) storage.KnowledgeObject {
	now := time.Now().UTC().Truncate(time.Second)
	return storage.KnowledgeObject{
		ID: id, Type: "note", Pipeline: "alpha@v1", RawContent: "replica " + id, TextContent: "replica " + id,
		ContentHash: "hash-" + id, Status: storage.ObjectStatusActive, CreatedAt: now, UpdatedAt: now,
	}
}

// A row received by federation is created unsigned, and the baseline stamp
// ran once at the first start: an unsigned row is never stale, so the job
// never selects it, whatever pipeline it names.
// (The stamp running once is pinned in cmd/dpkms/cmd: TestStampDerivationBaseline_RunsOnce.)
func TestFederatedRowsAreNeverReDerived(t *testing.T) {
	f := newFixture(t)
	ctx := ctxSystem()
	// First start of a signature-aware build: the baseline is stamped.
	f.add("local", "alpha@v1", alphaOld)
	_, err := f.store.StampDerivationBaseline(ctx, pipeline.Baselines(f.reg))
	require.NoError(t, err)

	// Then replicas arrive, by both inbound paths.
	svc := service.New(f.drv, jobs.NewQueue(f.drv.Jobs()), f.reg, search.NewEngine(f.drv), "", nil)
	n, err := svc.FederationAccept(ctx, []storage.KnowledgeObject{inbound("fed-http")}, nil, nil)
	require.NoError(t, err)
	require.Equal(t, 1, n)

	targetPath := filepath.Join(t.TempDir(), "target.db")
	target, err := storageutil.NewDriver("sqlite", targetPath, storageutil.NewTestBlobStore(t))
	require.NoError(t, err)
	require.NoError(t, target.Init(ctx))
	t.Cleanup(func() { target.Close(ctx) })
	pusher := federation.NewLocalPusher("fed", targetPath, nil, storageutil.NewTestBlobStore(t))
	require.NoError(t, pusher.Push(ctx, []storage.KnowledgeObject{inbound("fed-local")}, nil, nil))

	require.Empty(t, f.sigOf("fed-http"), "FederationAccept must leave the signature empty")
	require.Empty(t, sigOfRow(t, target, "fed-local"), "LocalPusher.Push must leave the signature empty")

	// The release bumps a step: the local row is stale, the replicas are not.
	httpBefore := snapshotOf(t, f.drv, "fed-http")
	res, err := f.run()
	require.NoError(t, err)
	require.Equal(t, 1, res.Processed, "%+v", res)
	require.Equal(t, []string{"local"}, f.svc.called(), "only the local row is re-derived")
	require.Equal(t, httpBefore, snapshotOf(t, f.drv, "fed-http"), "a replica is byte-identical after the run")
	require.Empty(t, f.sigOf("fed-http"))

	// The pushed replica, in its own store, with the same registry.
	tstore := target.Objects().(Store)
	localBefore := snapshotOf(t, target, "fed-local")
	require.Zero(t, staleObjects(t, tstore, f.reg))
	job := &Job{Store: tstore, Svc: f.svc, Registry: f.reg}
	res, err = job.Run(context.Background(), "j")
	require.NoError(t, err)
	require.Zero(t, res.Total)
	require.Equal(t, localBefore, snapshotOf(t, target, "fed-local"))
}
