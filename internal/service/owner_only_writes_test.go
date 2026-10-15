package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/sharing"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// restrictedCtx is a restricted viewer's context: the zero viewer, what
// a principal no grant applies to resolves to.
func restrictedCtx() context.Context {
	return sharing.WithViewer(context.Background(), sharing.Viewer{})
}

// The service methods that write an owner-only store through Store(ctx)
// refuse a restricted viewer with ErrRestrictedViewer and leave the
// driver untouched, while the owner's call lands. The routes reaching
// them are closed to restricted viewers; this pins the storage backstop
// behind the route table, so a route opened by mistake cannot write
// rows no share policy covers.
func TestOwnerOnlyServiceWritesRefuseRestrictedViewer(t *testing.T) {
	svc := newTestService(t)
	drv := svc.store
	ctx := ownerCtx()
	now := time.Now().UTC().Truncate(time.Second)

	if err := drv.Registries().CacheManifest(ctx, &storage.RegistryCache{RegistryURL: "https://reg.example", LastFetched: now}); err != nil {
		t.Fatal(err)
	}
	job := &storage.Job{ID: "owner-only-job", Type: "analyze", Status: storage.JobPending, Pipeline: "text.short", CreatedAt: now, UpdatedAt: now}
	if err := drv.Jobs().Create(ctx, job); err != nil {
		t.Fatal(err)
	}
	det := &storage.DetectorRecord{ID: "det_owner", Kind: storage.DetectorKindURLPattern, Name: "d", Pattern: "example.com", PipelineName: "text.short", Enabled: true, CreatedAt: now, UpdatedAt: now}
	if err := drv.Detectors().Create(ctx, det); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name      string
		call      func(context.Context) error
		untouched func() bool
	}{
		{
			name: "RemoveRegistry",
			call: func(c context.Context) error { return svc.RemoveRegistry(c, "https://reg.example") },
			untouched: func() bool {
				got, err := drv.Registries().GetCachedManifest(ctx, "https://reg.example")
				return err == nil && got != nil
			},
		},
		{
			name: "CancelJob",
			call: func(c context.Context) error { return svc.CancelJob(c, job.ID) },
			untouched: func() bool {
				got, err := drv.Jobs().Get(ctx, job.ID)
				return err == nil && got.Status == storage.JobPending
			},
		},
		{
			name: "DisableDetector",
			call: func(c context.Context) error { return svc.DisableDetector(c, det.ID) },
			untouched: func() bool {
				got, err := drv.Detectors().Get(ctx, det.ID)
				return err == nil && got.Enabled
			},
		},
		{
			name: "CreateFeed",
			call: func(c context.Context) error {
				_, err := svc.CreateFeed(c, "https://feed.example/rss", "rss")
				return err
			},
			untouched: func() bool {
				got, err := drv.Feeds().GetByURL(ctx, "https://feed.example/rss")
				return err != nil || got == nil
			},
		},
		{
			name: "RecordMeteringEvent",
			call: func(c context.Context) error {
				return svc.RecordMeteringEvent(c, "owner-only-reg", storage.MeteringEventEntityResolve, "ns")
			},
			untouched: func() bool {
				got, err := drv.Metering().List(ctx, storage.MeteringFilter{RegistryName: "owner-only-reg"})
				return err == nil && len(got) == 0
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(restrictedCtx()); !errors.Is(err, storage.ErrRestrictedViewer) {
				t.Fatalf("restricted viewer: err = %v, want ErrRestrictedViewer", err)
			}
			if !tc.untouched() {
				t.Fatal("restricted viewer's write reached the driver")
			}
			if err := tc.call(ownerCtx()); err != nil {
				t.Fatalf("owner: %v", err)
			}
			if tc.untouched() {
				t.Fatal("owner's write did not reach the driver")
			}
		})
	}
}

// A registry sync run under a restricted viewer must not store the
// entitlement the registry answers: entitlement rows are keyed by
// registry or principal name and gate what entities a caller may pull,
// so writing one is granting namespaces. The owner's sync stores it.
func TestSyncRegistryEntitiesRestrictedViewerStoresNoEntitlement(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/entitlements" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"plan":"pro","namespaces":["*"]}`))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	svc := newTestService(t)
	svc.Cfg.Registries = []config.RegistryConfig{{
		Name: "paid", URL: srv.URL, EntitlementURL: srv.URL + "/entitlements",
		SyncMode: config.RegistrySyncModeFull,
	}}
	stored := func() bool {
		got, err := svc.store.Entitlements().Get(ownerCtx(), storage.EntitlementOutbound, "paid")
		return err == nil && got != nil
	}

	if _, err := svc.SyncRegistryEntities(restrictedCtx(), srv.URL); !errors.Is(err, storage.ErrRestrictedViewer) {
		t.Fatalf("restricted sync: err = %v, want ErrRestrictedViewer", err)
	}
	if stored() {
		t.Fatal("restricted viewer's sync stored the entitlement")
	}

	// The owner's sync stores the entitlement before it fetches the
	// entity index, which this server does not serve: the sync fails
	// after the write.
	_, _ = svc.SyncRegistryEntities(ownerCtx(), srv.URL)
	if !stored() {
		t.Fatal("owner's sync did not store the entitlement")
	}
}
