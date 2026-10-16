package service_test

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/service"
	"github.com/ideacrafterslabs/ctxt/internal/storage/blob/local"
)

// tarEntry is one hand-made archive entry.
type tarEntry struct {
	name     string
	typeflag byte
	linkname string
	body     string
}

func writeTarGz(t *testing.T, path string, entries []tarEntry) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		typ := e.typeflag
		if typ == 0 {
			typ = tar.TypeReg
		}
		hdr := &tar.Header{Name: e.name, Typeflag: typ, Linkname: e.linkname, Mode: 0o644}
		if typ == tar.TypeReg {
			hdr.Size = int64(len(e.body))
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if typ == tar.TypeReg {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
}

// restoreLayout is a scratch tree: target holds the restore's db and
// config dir, outside is a sibling no restore may write into.
type restoreLayout struct {
	base, target, configDir, dbPath, outside string
}

func newRestoreLayout(t *testing.T) restoreLayout {
	t.Helper()
	base := t.TempDir()
	l := restoreLayout{
		base:      base,
		target:    filepath.Join(base, "target"),
		configDir: filepath.Join(base, "target", "config"),
		dbPath:    filepath.Join(base, "target", "db.sqlite"),
		outside:   filepath.Join(base, "outside"),
	}
	if err := os.MkdirAll(l.outside, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(l.outside, "secret"), []byte("outside secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	return l
}

// assertNothingWritten checks the refused restore left the target
// untouched and wrote nothing into the outside sibling.
func (l restoreLayout) assertNothingWritten(t *testing.T) {
	t.Helper()
	got := map[string]bool{}
	_ = filepath.Walk(l.outside, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			rel, _ := filepath.Rel(l.outside, p)
			got[rel] = true
		}
		return nil
	})
	delete(got, "secret")
	if len(got) > 0 {
		t.Errorf("restore wrote outside its targets: %v", got)
	}
	if b, err := os.ReadFile(filepath.Join(l.outside, "secret")); err != nil || string(b) != "outside secret" {
		t.Errorf("restore changed a file outside its targets: %q, %v", b, err)
	}
	if entries, err := os.ReadDir(l.target); err == nil && len(entries) > 0 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("refused restore still wrote into the target: %v", names)
	}
}

func (l restoreLayout) opts(t *testing.T, source string) service.RestoreOpts {
	t.Helper()
	bs, err := local.New(filepath.Join(l.base, "blobstore"))
	if err != nil {
		t.Fatal(err)
	}
	return service.RestoreOpts{Source: source, DBPath: l.dbPath, ConfigDir: l.configDir, Blobs: bs}
}

// Archive entries that would write outside the config dir, or are not
// regular files, refuse the whole restore before anything is written.
func TestRestoreArchiveRefusesUnsafeEntries(t *testing.T) {
	for _, tc := range []struct {
		name  string
		entry func(l restoreLayout) tarEntry
	}{
		{"dotdot", func(restoreLayout) tarEntry {
			return tarEntry{name: "ctxt-backup/config/../../outside/evil", body: "x"}
		}},
		{"dotdot without prefix", func(restoreLayout) tarEntry {
			return tarEntry{name: "config/../../outside/evil", body: "x"}
		}},
		{"nested dotdot", func(restoreLayout) tarEntry {
			return tarEntry{name: "ctxt-backup/config/keys/../../../outside/evil", body: "x"}
		}},
		{"absolute", func(l restoreLayout) tarEntry {
			return tarEntry{name: "ctxt-backup/config/" + filepath.ToSlash(filepath.Join(l.outside, "evil")), body: "x"}
		}},
		{"backslash", func(restoreLayout) tarEntry {
			return tarEntry{name: `ctxt-backup/config/..\..\outside\evil`, body: "x"}
		}},
		{"config symlink", func(l restoreLayout) tarEntry {
			return tarEntry{name: "ctxt-backup/config/keys", typeflag: tar.TypeSymlink, linkname: l.outside}
		}},
		{"config hardlink", func(l restoreLayout) tarEntry {
			return tarEntry{name: "ctxt-backup/config/keys", typeflag: tar.TypeLink, linkname: filepath.Join(l.outside, "secret")}
		}},
		{"config device", func(restoreLayout) tarEntry {
			return tarEntry{name: "ctxt-backup/config/dev", typeflag: tar.TypeChar}
		}},
		{"config fifo", func(restoreLayout) tarEntry {
			return tarEntry{name: "ctxt-backup/config/fifo", typeflag: tar.TypeFifo}
		}},
		{"db symlink", func(l restoreLayout) tarEntry {
			return tarEntry{name: "ctxt-backup/ctxt.db", typeflag: tar.TypeSymlink, linkname: filepath.Join(l.outside, "secret")}
		}},
		{"blob symlink", func(l restoreLayout) tarEntry {
			return tarEntry{name: "ctxt-backup/blobs/abcdef", typeflag: tar.TypeSymlink, linkname: filepath.Join(l.outside, "secret")}
		}},
		{"blob path", func(restoreLayout) tarEntry {
			return tarEntry{name: "ctxt-backup/blobs/../../outside/evil", body: "x"}
		}},
		{"manifest symlink", func(l restoreLayout) tarEntry {
			return tarEntry{name: "ctxt-backup/manifest.json", typeflag: tar.TypeSymlink, linkname: filepath.Join(l.outside, "secret")}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := newRestoreLayout(t)
			archive := filepath.Join(l.base, "evil.tar.gz")
			writeTarGz(t, archive, []tarEntry{
				// Legitimate entries first: a refusal must undo nothing
				// because nothing was written.
				{name: "ctxt-backup/ctxt.db", body: "db bytes"},
				{name: "ctxt-backup/config/config.yaml", body: "x: 1\n"},
				tc.entry(l),
				// A symlink entry followed by a write through it: the
				// classic link-then-write escape.
				{name: "ctxt-backup/config/keys/evil", body: "through the link"},
			})
			if _, err := service.Restore(ownerCtx(), l.opts(t, archive)); err == nil {
				t.Error("restore of an archive with an unsafe entry: want error")
			}
			l.assertNothingWritten(t)
		})
	}
}

// A symlink already in the target config dir cannot carry a config
// entry outside it.
func TestRestoreArchiveRefusesTargetSymlinkEscape(t *testing.T) {
	l := newRestoreLayout(t)
	if err := os.MkdirAll(l.configDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(l.outside, filepath.Join(l.configDir, "keys")); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(l.base, "a.tar.gz")
	writeTarGz(t, archive, []tarEntry{{name: "ctxt-backup/config/keys/evil", body: "x"}})
	if _, err := service.Restore(ownerCtx(), l.opts(t, archive)); err == nil {
		t.Error("config entry through a symlink out of the config dir: want error")
	}
	if _, err := os.Stat(filepath.Join(l.outside, "evil")); !os.IsNotExist(err) {
		t.Fatalf("restore wrote through the symlink outside the config dir (stat err %v)", err)
	}
}

// An extracted backup's symlinks are refused, not followed: none may
// pull a file from outside the backup into the restore's targets.
func TestRestoreDirRefusesSymlinks(t *testing.T) {
	for _, tc := range []struct{ name, link string }{
		{"config file", "config/leak"},
		{"config nested", "config/keys/leak"},
		{"db", "ctxt.db"},
		{"blob", "blobs/abcdef"},
		{"manifest", "manifest.json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := newRestoreLayout(t)
			src := filepath.Join(l.base, "extracted")
			if err := os.MkdirAll(filepath.Join(src, "config"), 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(src, "config", "config.yaml"), []byte("x: 1\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(src, filepath.FromSlash(tc.link))
			if err := os.MkdirAll(filepath.Dir(link), 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(l.outside, "secret"), link); err != nil {
				t.Fatal(err)
			}
			if _, err := service.Restore(ownerCtx(), l.opts(t, src)); err == nil {
				t.Error("restore of a backup dir holding a symlink: want error")
			}
			l.assertNothingWritten(t)
		})
	}
}

// A symlinked config directory in an extracted backup is not walked.
func TestRestoreDirIgnoresSymlinkedConfigDir(t *testing.T) {
	l := newRestoreLayout(t)
	src := filepath.Join(l.base, "extracted")
	if err := os.MkdirAll(src, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(l.outside, filepath.Join(src, "config")); err != nil {
		t.Fatal(err)
	}
	res, err := service.Restore(ownerCtx(), l.opts(t, src))
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if res.ConfigCount != 0 {
		t.Errorf("config count = %d, want 0", res.ConfigCount)
	}
	l.assertNothingWritten(t)
}

// An unsupported manifest refuses the restore before the database is
// overwritten, in either form.
func TestRestoreRefusesNewerSchemaBeforeWriting(t *testing.T) {
	l := newRestoreLayout(t)
	archive := filepath.Join(l.base, "a.tar.gz")
	writeTarGz(t, archive, []tarEntry{
		{name: "ctxt-backup/ctxt.db", body: "db bytes"},
		{name: "ctxt-backup/config/config.yaml", body: "x: 1\n"},
		{name: "ctxt-backup/manifest.json", body: `{"schema_version": 99}`},
	})
	if _, err := service.Restore(ownerCtx(), l.opts(t, archive)); err == nil ||
		!strings.Contains(err.Error(), "schema version") {
		t.Fatalf("archive: want schema version error, got %v", err)
	}
	l.assertNothingWritten(t)

	src := extractArchive(t, archive)
	if _, err := service.Restore(ownerCtx(), l.opts(t, src)); err == nil ||
		!strings.Contains(err.Error(), "schema version") {
		t.Fatalf("directory: want schema version error, got %v", err)
	}
	l.assertNothingWritten(t)
}

// A real backup's config dir restores into ConfigDir, tree intact, in
// both forms.
func TestBackupRestoreConfigsLandInConfigDir(t *testing.T) {
	src := t.TempDir()
	srcDB := filepath.Join(src, "db.sqlite")
	seedExternalizedStore(t, srcDB, filepath.Join(src, "blobs"))
	cfgDir := filepath.Join(src, "contexthelp")
	files := map[string]string{
		"config.yaml":    "port: 8080\n",
		"keys/abcd.pub":  "pubkey\n",
		"profiles/a.yml": "a: 1\n",
	}
	for rel, body := range files {
		p := filepath.Join(cfgDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	bResult, err := service.Backup(ownerCtx(), service.BackupOpts{
		DBPath:         srcDB,
		OutputDir:      filepath.Join(src, "backup"),
		IncludeConfigs: true,
		ConfigDir:      cfgDir,
	})
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}

	for _, tc := range []struct {
		name   string
		source func(t *testing.T) string
	}{
		{"archive", func(*testing.T) string { return bResult.Path }},
		{"directory", func(t *testing.T) string { return extractArchive(t, bResult.Path) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := newRestoreLayout(t)
			res, err := service.Restore(ownerCtx(), l.opts(t, tc.source(t)))
			if err != nil {
				t.Fatalf("Restore: %v", err)
			}
			if res.ConfigCount != len(files) {
				t.Errorf("config count = %d, want %d", res.ConfigCount, len(files))
			}
			for rel, body := range files {
				got, err := os.ReadFile(filepath.Join(l.configDir, filepath.FromSlash(rel)))
				if err != nil || string(got) != body {
					t.Errorf("config %s = %q, %v; want %q", rel, got, err, body)
				}
			}
			if _, err := os.Stat(l.dbPath); err != nil {
				t.Errorf("db not restored: %v", err)
			}
		})
	}
}
