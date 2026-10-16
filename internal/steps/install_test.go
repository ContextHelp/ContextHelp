package steps_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/steps"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

func TestInstallArchiveRecordsStepAndDigest(t *testing.T) {
	f := newFixture(t)
	rs := f.install(t, "masker", skill("masker"), emit(`{"object":{}}`))

	assert.Equal(t, filepath.Join(f.stepsDir, "masker"), rs.Path)
	assert.Equal(t, "local", rs.Source)
	exe := filepath.Join(rs.Path, "run.sh")
	b, err := os.ReadFile(exe)
	require.NoError(t, err)
	sum := sha256.Sum256(b)
	require.NotNil(t, rs.Metadata.Execution)
	assert.Equal(t, hex.EncodeToString(sum[:]), rs.Metadata.Execution.ExecutableSHA256)
	fi, err := os.Stat(exe)
	require.NoError(t, err)
	assert.NotZero(t, fi.Mode().Perm()&0o111, "executable bit kept")

	got, err := f.store.Steps().Get(context.Background(), "masker")
	require.NoError(t, err)
	assert.Equal(t, rs.Metadata.Execution.ExecutableSHA256, got.Metadata.Execution.ExecutableSHA256)

	// Staging directories never stay behind.
	entries, err := os.ReadDir(f.stepsDir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "masker", entries[0].Name())
}

func TestInstallArchiveRejects(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name    string
		step    string
		archive func(t *testing.T) []byte
		want    string
	}{
		{"name mismatch", "other", packed(skill("masker"), "true"), `SKILL.md names the step "masker"`},
		{"bad name", "../x", packed(skill("masker"), "true"), "step name"},
		{"no execution block", "masker", packed("---\nname: masker\n---\n", "true"), "no execution block"},
		{"executable outside dir", "masker", packed("---\nname: masker\nexecution:\n  type: external\n  executable: ../run.sh\n---\n", "true"), "leaves the step directory"},
		{"container without image", "masker", packed(skill("masker", "minimum_isolation: container"), "true"), "container_image"},
		{"traversal entry", "masker", rawTar(map[string]string{"SKILL.md": skill("masker"), "../evil": "x"}, nil), "leaves the step directory"},
		{"symlink entry", "masker", rawTar(map[string]string{"SKILL.md": skill("masker")}, map[string]string{"run.sh": "/bin/sh"}), "only regular files and directories"},
		{"not executable", "masker", rawTar(map[string]string{"SKILL.md": skill("masker"), "run.sh": "#!/bin/sh\n"}, nil), "not an executable regular file"},
		{"not gzip", "masker", func(*testing.T) []byte { return []byte("plain") }, "not gzip"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			_, err := f.installer.InstallArchive(ctx, tc.step, tc.archive(t), "local", "")
			require.Error(t, err)
			assert.ErrorIs(t, err, steps.ErrInvalidStep)
			assert.Contains(t, err.Error(), tc.want)
			_, gerr := f.store.Steps().Get(ctx, tc.step)
			assert.ErrorIs(t, gerr, storage.ErrNotFound, "nothing recorded")
			entries, _ := os.ReadDir(f.stepsDir)
			assert.Empty(t, entries, "nothing left in the steps directory")
		})
	}
}

func TestInstallArchiveRefusesReservedAndExisting(t *testing.T) {
	f := newFixture(t)
	f.installer.Reserved = func(n string) bool { return n == "tagger" }
	_, err := f.installer.InstallArchive(context.Background(), "tagger", packed(skill("tagger"), "true")(t), "local", "")
	assert.ErrorIs(t, err, steps.ErrInvalidStep)

	f.install(t, "masker", skill("masker"), "true")
	_, err = f.installer.InstallArchive(context.Background(), "masker", packed(skill("masker"), "true")(t), "local", "")
	assert.ErrorIs(t, err, steps.ErrStepExists)
}

func TestUninstallRemovesFiles(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	rs := f.install(t, "masker", skill("masker"), "true")
	require.DirExists(t, rs.Path)

	require.NoError(t, f.installer.Uninstall(ctx, "masker"))
	assert.NoDirExists(t, rs.Path)
	_, err := f.store.Steps().Get(ctx, "masker")
	assert.ErrorIs(t, err, storage.ErrNotFound)

	assert.ErrorIs(t, f.installer.Uninstall(ctx, "masker"), steps.ErrStepNotFound)
}

// A record pointing outside the steps directory loses its row, never
// someone else's files.
func TestUninstallLeavesForeignPaths(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	foreign := t.TempDir()
	now := time.Now()
	require.NoError(t, f.store.Steps().Create(ctx, &storage.RegisteredStep{
		Name: "old", Source: "local", Path: foreign, InstalledAt: now, UpdatedAt: now,
	}))
	require.NoError(t, f.installer.Uninstall(ctx, "old"))
	assert.DirExists(t, foreign)
}

// fakeRegistry serves a manifest listing one step archive with the digest
// declared, counting archive downloads.
type fakeRegistry struct {
	srv       *httptest.Server
	downloads atomic.Int32
}

func newRegistry(t *testing.T, name string, archive []byte, declared string) *fakeRegistry {
	t.Helper()
	r := &fakeRegistry{}
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/", "":
			_, _ = w.Write([]byte(`{"name":"reg","version":"1","steps":[{"name":"` + name +
				`","version":"1.0.0","archive":"steps/` + name + `.tar.gz","sha256":"` + declared + `"}]}`))
		case "/steps/" + name + ".tar.gz":
			r.downloads.Add(1)
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, req)
		}
	}))
	t.Cleanup(r.srv.Close)
	return r
}

func digest(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func TestInstallFromRegistryTrustGate(t *testing.T) {
	ctx := context.Background()
	archive := packed(skill("masker", "container_image: alpine:3.20"), emit(`{"object":{}}`))(t)

	t.Run("untrusted refuses before downloading", func(t *testing.T) {
		f := newFixture(t)
		reg := newRegistry(t, "masker", archive, digest(archive))
		for _, trust := range []config.RegistryTrustLevel{"", config.RegistryTrustLevelUntrusted} {
			_, err := f.installer.InstallFromRegistry(ctx, "masker", config.RegistryConfig{URL: reg.srv.URL, TrustLevel: trust})
			require.ErrorIs(t, err, steps.ErrUntrustedRegistry)
			assert.Contains(t, err.Error(), "trust_level")
		}
		assert.Zero(t, reg.downloads.Load())
		_, err := f.store.Steps().Get(ctx, "masker")
		assert.ErrorIs(t, err, storage.ErrNotFound)
	})

	t.Run("sandboxed installs pinned to container isolation", func(t *testing.T) {
		f := newFixture(t)
		reg := newRegistry(t, "masker", archive, digest(archive))
		rs, err := f.installer.InstallFromRegistry(ctx, "masker", config.RegistryConfig{URL: reg.srv.URL, TrustLevel: config.RegistryTrustLevelSandboxed})
		require.NoError(t, err)
		assert.Equal(t, "registry:"+reg.srv.URL, rs.Source)
		assert.Equal(t, "sandboxed", rs.Metadata.Execution.TrustLevel)
		assert.Equal(t, storage.StepIsolationContainer, steps.EffectiveIsolation(rs.Metadata.Execution, ""))
	})

	t.Run("sandboxed without an image is refused", func(t *testing.T) {
		f := newFixture(t)
		plain := packed(skill("masker"), "true")(t)
		reg := newRegistry(t, "masker", plain, digest(plain))
		_, err := f.installer.InstallFromRegistry(ctx, "masker", config.RegistryConfig{URL: reg.srv.URL, TrustLevel: config.RegistryTrustLevelSandboxed})
		require.ErrorIs(t, err, steps.ErrInvalidStep)
		assert.Contains(t, err.Error(), "container_image")
	})

	t.Run("trusted installs for process isolation", func(t *testing.T) {
		f := newFixture(t)
		plain := packed(skill("masker"), "true")(t)
		reg := newRegistry(t, "masker", plain, digest(plain))
		rs, err := f.installer.InstallFromRegistry(ctx, "masker", config.RegistryConfig{URL: reg.srv.URL, TrustLevel: config.RegistryTrustLevelTrusted})
		require.NoError(t, err)
		assert.Equal(t, storage.StepIsolationProcess, steps.EffectiveIsolation(rs.Metadata.Execution, ""))
		assert.Equal(t, storage.StepIsolationContainer, steps.EffectiveIsolation(rs.Metadata.Execution, "container"))
	})
}

func TestInstallFromRegistryChecksDigest(t *testing.T) {
	ctx := context.Background()
	archive := packed(skill("masker"), "true")(t)
	trusted := func(url string) config.RegistryConfig {
		return config.RegistryConfig{URL: url, TrustLevel: config.RegistryTrustLevelTrusted}
	}

	t.Run("mismatch", func(t *testing.T) {
		f := newFixture(t)
		reg := newRegistry(t, "masker", archive, digest([]byte("something else")))
		_, err := f.installer.InstallFromRegistry(ctx, "masker", trusted(reg.srv.URL))
		require.ErrorIs(t, err, steps.ErrDigestMismatch)
		entries, _ := os.ReadDir(f.stepsDir)
		assert.Empty(t, entries)
	})
	t.Run("undeclared", func(t *testing.T) {
		f := newFixture(t)
		reg := newRegistry(t, "masker", archive, "")
		_, err := f.installer.InstallFromRegistry(ctx, "masker", trusted(reg.srv.URL))
		require.ErrorIs(t, err, steps.ErrInvalidStep)
		assert.Contains(t, err.Error(), "sha256")
		assert.Zero(t, reg.downloads.Load())
	})
	t.Run("not listed", func(t *testing.T) {
		f := newFixture(t)
		reg := newRegistry(t, "other", archive, digest(archive))
		_, err := f.installer.InstallFromRegistry(ctx, "masker", trusted(reg.srv.URL))
		require.ErrorIs(t, err, steps.ErrNotInRegistry)
	})
}

func TestDownloadArchiveStaysOnRegistry(t *testing.T) {
	sd := steps.NewStepDiscovery(newFixture(t).store)
	for _, rel := range []string{"https://elsewhere.example/x.tar.gz", "/abs.tar.gz", "../up.tar.gz", "x.tar.gz?y=1"} {
		_, err := sd.DownloadArchive(context.Background(), "http://127.0.0.1:1", rel)
		assert.ErrorIs(t, err, steps.ErrInvalidStep, rel)
	}
}

// packed is a step archive with SKILL.md and an executable run.sh.
func packed(skillMD, script string) func(t *testing.T) []byte {
	return func(t *testing.T) []byte {
		t.Helper()
		b, err := steps.PackDir(stepDir(t, skillMD, script))
		require.NoError(t, err)
		return b
	}
}

// rawTar is a gzipped tar with the given files (mode 0644) and symlinks.
func rawTar(files, links map[string]string) func(t *testing.T) []byte {
	return func(t *testing.T) []byte {
		t.Helper()
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gz)
		for name, body := range files {
			require.NoError(t, tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(body))}))
			_, err := tw.Write([]byte(body))
			require.NoError(t, err)
		}
		for name, target := range links {
			require.NoError(t, tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeSymlink, Linkname: target}))
		}
		require.NoError(t, tw.Close())
		require.NoError(t, gz.Close())
		return buf.Bytes()
	}
}
