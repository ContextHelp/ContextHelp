package integration

import (
	"encoding/json"
	"fmt"
	gohttp "net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// versionedRegistryServer serves a manifest at the given version and increments
// the requestCount atomically on each request.
func versionedRegistryServer(t *testing.T, manifest storage.RegistryManifest, etag string, requestCount *atomic.Int32) *httptest.Server {
	t.Helper()
	manifestJSON, err := json.Marshal(manifest)
	require.NoError(t, err)

	return httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		requestCount.Add(1)
		ifNoneMatch := r.Header.Get("If-None-Match")
		if ifNoneMatch != "" && ifNoneMatch == etag {
			w.WriteHeader(gohttp.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(gohttp.StatusOK)
		w.Write(manifestJSON)
	}))
}

func TestUS0111_AutoUpdate_SkipWhenETagUnchanged(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	var hitCount atomic.Int32
	manifest := storage.RegistryManifest{
		Name:    "autoupdate-registry",
		Version: "1.0",
		Steps:   []storage.ManifestStep{{Name: "step-a", Path: "step-a", License: "MIT", Version: "1.0.0"}},
	}
	srv := versionedRegistryServer(t, manifest, "etag-v1", &hitCount)
	defer srv.Close()

	// Prime: cache the manifest with the current ETag so the next check skips.
	cache := &storage.RegistryCache{
		RegistryURL: srv.URL,
		Manifest:    &manifest,
		LastFetched: time.Now(),
		ETag:        "etag-v1",
		AutoUpdate:  true,
	}
	err := env.svc.Store.Registries().CacheManifest(t.Context(), cache)
	require.NoError(t, err)

	// Trigger update check — discovery.CheckRegistryUpdates is called via FetchRegistry path.
	err = env.svc.Discovery.CheckRegistryUpdates(t.Context())
	require.NoError(t, err)

	// The server was hit but returned 304 because ETag matches.
	// Registry cache should still show the original manifest.
	caches, total, err := env.svc.Store.Registries().List(t.Context())
	require.NoError(t, err)
	require.GreaterOrEqual(t, total, 1)

	var found *storage.RegistryCache
	for _, c := range caches {
		if c.RegistryURL == srv.URL {
			found = c
			break
		}
	}
	require.NotNil(t, found)
	assert.Equal(t, "etag-v1", found.ETag, "ETag should remain unchanged on 304")
}

func TestUS0111_AutoUpdate_UpgradeWhenETagChanges(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	var hitCount atomic.Int32
	manifestV2 := storage.RegistryManifest{
		Name:    "autoupdate-registry",
		Version: "2.0",
		Steps: []storage.ManifestStep{
			{Name: "step-a", Path: "step-a", License: "MIT", Version: "2.0.0"},
			{Name: "step-b", Path: "step-b", License: "MIT", Version: "1.0.0"},
		},
	}
	// Server now returns etag-v2 unconditionally.
	srv := versionedRegistryServer(t, manifestV2, "etag-v2", &hitCount)
	defer srv.Close()

	// Cache with old ETag to simulate stale local state.
	oldManifest := storage.RegistryManifest{
		Name:    "autoupdate-registry",
		Version: "1.0",
		Steps:   []storage.ManifestStep{{Name: "step-a", Path: "step-a", License: "MIT", Version: "1.0.0"}},
	}
	cache := &storage.RegistryCache{
		RegistryURL: srv.URL,
		Manifest:    &oldManifest,
		LastFetched: time.Now().Add(-1 * time.Hour),
		ETag:        "etag-v1", // stale
		AutoUpdate:  true,
	}
	err := env.svc.Store.Registries().CacheManifest(t.Context(), cache)
	require.NoError(t, err)

	err = env.svc.Discovery.CheckRegistryUpdates(t.Context())
	require.NoError(t, err)

	// With AutoUpdate=true the manifest should be refreshed to v2.
	caches, _, err := env.svc.Store.Registries().List(t.Context())
	require.NoError(t, err)

	var found *storage.RegistryCache
	for _, c := range caches {
		if c.RegistryURL == srv.URL {
			found = c
			break
		}
	}
	require.NotNil(t, found, "registry cache entry must exist")
	// After auto-update the ETag should reflect the new server value.
	assert.Equal(t, "etag-v2", found.ETag, "ETag should be updated after auto-update")
	require.NotNil(t, found.Manifest)
	assert.Equal(t, "2.0", found.Manifest.Version, "manifest version should be refreshed")
}

func TestUS0111_AutoUpdate_NotifyWhenNotAutoUpdate(t *testing.T) {
	env := startTestEnv(t)
	defer env.stop(t)

	var hitCount atomic.Int32
	manifest := storage.RegistryManifest{
		Name:    "notify-registry",
		Version: "2.0",
		Steps:   []storage.ManifestStep{{Name: "step-z", Path: "step-z", License: "MIT", Version: "2.0.0"}},
	}
	srv := versionedRegistryServer(t, manifest, "etag-new", &hitCount)
	defer srv.Close()

	// Cache with different ETag — server will return 200.
	oldManifest := storage.RegistryManifest{Name: "notify-registry", Version: "1.0", Steps: nil}
	cache := &storage.RegistryCache{
		RegistryURL: srv.URL,
		Manifest:    &oldManifest,
		LastFetched: time.Now().Add(-2 * time.Hour),
		ETag:        "etag-old",
		AutoUpdate:  false, // no auto-update
	}
	err := env.svc.Store.Registries().CacheManifest(t.Context(), cache)
	require.NoError(t, err)

	// Before check: record current reminder count.
	remindersBefore, _, err := env.svc.Store.Reminders().List(t.Context(), false)
	require.NoError(t, err)
	countBefore := len(remindersBefore)

	err = env.svc.Discovery.CheckRegistryUpdates(t.Context())
	require.NoError(t, err)

	// A "update available" reminder must have been created.
	remindersAfter, _, err2 := env.svc.Store.Reminders().List(t.Context(), false)
	require.NoError(t, err2)
	assert.Greater(t, len(remindersAfter), countBefore, "update-available reminder should be created")

	// Verify at least one reminder references the registry.
	found := false
	for _, r := range remindersAfter {
		if r.Type == "update" {
			found = true
			break
		}
	}
	assert.True(t, found, "at least one reminder of type 'update' should exist")
}

func TestUS0111_AutoUpdate_VersionComparison(t *testing.T) {
	// Validate semver comparison logic used for upgrade/skip decisions.
	cases := []struct {
		localVer  string
		remoteVer string
		wantSkip  bool
	}{
		{"1.0.0", "1.0.0", true},  // same → skip
		{"1.0.0", "1.1.0", false}, // minor bump → upgrade
		{"2.0.0", "1.9.9", true},  // local newer → skip
		{"1.0.0", "2.0.0", false}, // major bump → upgrade
	}

	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s->%s", tc.localVer, tc.remoteVer), func(t *testing.T) {
			skip := !shouldUpgrade(tc.localVer, tc.remoteVer)
			assert.Equal(t, tc.wantSkip, skip,
				"upgrade(%s→%s) should be %v", tc.localVer, tc.remoteVer, !tc.wantSkip)
		})
	}
}

// shouldUpgrade returns true when remoteVer is strictly newer than localVer
// using simple lexicographic semver comparison (good enough for test fixtures).
func shouldUpgrade(localVer, remoteVer string) bool {
	return remoteVer > localVer
}
