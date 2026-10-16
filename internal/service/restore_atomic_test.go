package service_test

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/dblock"
	"github.com/ideacrafterslabs/ctxt/internal/pidfile"
	"github.com/ideacrafterslabs/ctxt/internal/service"
)

// The config file dpkms backup ran with is archived as config.yaml and
// restores to the target's own config file, dpkms.yaml in its config
// dir, in both forms; --skip-configs skips it.
func TestBackupRestoreActiveConfigFile(t *testing.T) {
	src := t.TempDir()
	srcDB := filepath.Join(src, "db.sqlite")
	seedExternalizedStore(t, srcDB, filepath.Join(src, "blobs"))
	srcCfg := filepath.Join(src, "contexthelp", "dpkms.yaml")
	const body = "server:\n  port: 8180\nfederations: []\n"
	if err := os.MkdirAll(filepath.Dir(srcCfg), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(srcCfg, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	bResult, err := service.Backup(ownerCtx(), service.BackupOpts{
		DBPath:     srcDB,
		OutputDir:  filepath.Join(src, "backup"),
		ConfigPath: srcCfg,
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
			opts := l.opts(t, tc.source(t))
			opts.ConfigFile = "dpkms.yaml"
			res, err := service.Restore(ownerCtx(), opts)
			if err != nil {
				t.Fatalf("Restore: %v", err)
			}
			got, err := os.ReadFile(filepath.Join(l.configDir, "dpkms.yaml"))
			if err != nil || string(got) != body {
				t.Fatalf("restored dpkms.yaml = %q, %v; want %q", got, err, body)
			}
			if res.ConfigCount != 1 {
				t.Errorf("config count = %d, want 1", res.ConfigCount)
			}
			if _, err := os.Stat(filepath.Join(l.configDir, "config.yaml")); !os.IsNotExist(err) {
				t.Errorf("archive entry name leaked into the config dir: %v", err)
			}
		})
		t.Run(tc.name+"/skip-configs", func(t *testing.T) {
			l := newRestoreLayout(t)
			opts := l.opts(t, tc.source(t))
			opts.ConfigFile = "dpkms.yaml"
			opts.SkipConfigs = true
			res, err := service.Restore(ownerCtx(), opts)
			if err != nil {
				t.Fatalf("Restore: %v", err)
			}
			if _, err := os.Stat(l.configDir); !os.IsNotExist(err) {
				t.Errorf("--skip-configs still wrote the config dir: %v", err)
			}
			if res.ConfigCount != 0 {
				t.Errorf("config count = %d, want 0", res.ConfigCount)
			}
		})
	}
}

// A backup holding the config file with no config file name to restore
// it to stops before anything is written.
func TestRestoreActiveConfigWithoutTargetFails(t *testing.T) {
	l := newRestoreLayout(t)
	archive := filepath.Join(l.base, "b.tar.gz")
	writeTarGz(t, archive, []tarEntry{
		{name: "ctxt-backup/ctxt.db", body: "new db"},
		{name: "ctxt-backup/config.yaml", body: "port: 1\n"},
	})
	if _, err := service.Restore(ownerCtx(), l.opts(t, archive)); err == nil ||
		!strings.Contains(err.Error(), "config file") {
		t.Fatalf("want missing config file error, got %v", err)
	}
	l.assertNothingWritten(t)
}

// failAfter yields n bytes of the source and then an error, standing in
// for a restore interrupted part-way through the database.
type failAfter struct {
	r io.Reader
	n int
}

func (f *failAfter) Read(p []byte) (int, error) {
	if f.n <= 0 {
		return 0, errors.New("injected write failure")
	}
	if len(p) > f.n {
		p = p[:f.n]
	}
	k, err := f.r.Read(p)
	f.n -= k
	return k, err
}

func seedTargetDB(t *testing.T, l restoreLayout, files map[string]string) {
	t.Helper()
	if err := os.MkdirAll(l.target, 0o750); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(l.target, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func targetFiles(t *testing.T, l restoreLayout) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(l.target)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	lock := filepath.Base(dblock.Path(l.dbPath))
	for _, e := range entries {
		// The database lock's sidecar stays once taken, by design:
		// it is not a restore leftover.
		if e.IsDir() || e.Name() == lock {
			continue
		}
		b, err := os.ReadFile(filepath.Join(l.target, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		got[e.Name()] = string(b)
	}
	return got
}

// A restore that fails while writing the database leaves the original
// database, and its WAL, exactly as they were.
func TestRestoreInterruptedKeepsOriginalDB(t *testing.T) {
	l := newRestoreLayout(t)
	orig := map[string]string{
		"db.sqlite":     "original database bytes",
		"db.sqlite-wal": "original wal",
	}
	seedTargetDB(t, l, orig)
	archive := filepath.Join(l.base, "b.tar.gz")
	writeTarGz(t, archive, []tarEntry{
		{name: "ctxt-backup/ctxt.db", body: strings.Repeat("restored database ", 64)},
	})
	service.SetDBSourceHook(t, func(r io.Reader) io.Reader { return &failAfter{r: r, n: 10} })

	if _, err := service.Restore(ownerCtx(), l.opts(t, archive)); err == nil ||
		!strings.Contains(err.Error(), "injected write failure") {
		t.Fatalf("want the injected failure, got %v", err)
	}
	got := targetFiles(t, l)
	if len(got) != len(orig) {
		t.Errorf("target holds %v after failed restore, want only %v", keys(got), keys(orig))
	}
	for name, body := range orig {
		if got[name] != body {
			t.Errorf("%s = %q after failed restore, want %q", name, got[name], body)
		}
	}
}

// A restored database must not meet the old database's WAL, shared
// memory or rollback journal: SQLite would replay them onto it.
func TestRestoreRemovesStaleWAL(t *testing.T) {
	l := newRestoreLayout(t)
	seedTargetDB(t, l, map[string]string{
		"db.sqlite":         "original database bytes",
		"db.sqlite-wal":     "old wal",
		"db.sqlite-shm":     "old shm",
		"db.sqlite-journal": "old journal",
	})
	archive := filepath.Join(l.base, "b.tar.gz")
	writeTarGz(t, archive, []tarEntry{{name: "ctxt-backup/ctxt.db", body: "restored"}})

	if _, err := service.Restore(ownerCtx(), l.opts(t, archive)); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	got := targetFiles(t, l)
	if len(got) != 1 || got["db.sqlite"] != "restored" {
		t.Errorf("target = %v, want only db.sqlite = restored", got)
	}
}

// sleeper starts a disposable child that stays alive for the test, so
// its pid is a genuinely running process other than this one.
func sleeper(t *testing.T) int {
	t.Helper()
	child := exec.Command("sleep", "60")
	if err := child.Start(); err != nil {
		t.Fatalf("start child: %v", err)
	}
	t.Cleanup(func() {
		_ = child.Process.Kill()
		_ = child.Wait()
	})
	return child.Process.Pid
}

// Restore refuses, with nothing written, while a live dpkms instance in
// the data dir's run dir serves the target database. Instances serving
// another database, and dead pidfiles, do not block it.
func TestRestoreRefusesWhileInstanceServesDB(t *testing.T) {
	l := newRestoreLayout(t)
	seedTargetDB(t, l, map[string]string{"db.sqlite": "live database"})
	archive := filepath.Join(l.base, "b.tar.gz")
	writeTarGz(t, archive, []tarEntry{{name: "ctxt-backup/ctxt.db", body: "restored"}})
	runDir := filepath.Join(l.base, "run")
	if err := os.MkdirAll(runDir, 0o750); err != nil {
		t.Fatal(err)
	}
	pid := sleeper(t)
	// Ports only name the pidfiles; nothing binds them.
	if err := pidfile.Write(runDir, pidfile.Info{
		PID: pid, Name: "home", Port: 47641, DBPath: l.dbPath, StartedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	opts := l.opts(t, archive)
	opts.RunDir = runDir

	_, err := service.Restore(ownerCtx(), opts)
	if err == nil || !strings.Contains(err.Error(), "running") {
		t.Fatalf("want refusal naming the running instance, got %v", err)
	}
	if got := targetFiles(t, l); len(got) != 1 || got["db.sqlite"] != "live database" {
		t.Errorf("target = %v after refusal, want the live database untouched", got)
	}
	dry := opts
	dry.DryRun = true
	if _, err := service.Restore(ownerCtx(), dry); err == nil {
		t.Error("dry run: want the same refusal")
	}

	// A relative db_path is relative to the instance's working
	// directory, unknown here: it may be the target, so it blocks.
	if err := pidfile.Write(runDir, pidfile.Info{
		PID: pid, Name: "home", Port: 47641, DBPath: "unrelated.sqlite", StartedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Restore(ownerCtx(), opts); err == nil {
		t.Fatal("instance with a relative db_path did not block restore")
	}
	if got := targetFiles(t, l); got["db.sqlite"] != "live database" {
		t.Errorf("db.sqlite = %q after refusal, want the live database", got["db.sqlite"])
	}

	// Same instance, another database: restore goes ahead.
	if err := pidfile.Write(runDir, pidfile.Info{
		PID: pid, Name: "home", Port: 47641, DBPath: filepath.Join(l.base, "elsewhere.sqlite"), StartedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Restore(ownerCtx(), opts); err != nil {
		t.Fatalf("instance serving another database blocked restore: %v", err)
	}
	if got := targetFiles(t, l); got["db.sqlite"] != "restored" {
		t.Errorf("db.sqlite = %q, want restored", got["db.sqlite"])
	}
}

func keys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
