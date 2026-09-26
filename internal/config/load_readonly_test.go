package config

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

// fileSnap is a file's content and mtime at a point in time.
type fileSnap struct {
	body  string
	mtime time.Time
}

// snapshotTree records every regular file under root. Comparing two
// snapshots catches rewrites, touched mtimes, and stray new files (such as
// an atomic-write .tmp left behind).
func snapshotTree(t *testing.T, root string) map[string]fileSnap {
	t.Helper()
	out := map[string]fileSnap{}
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		body, err := os.ReadFile(p) // #nosec G304 -- test tempdir
		if err != nil {
			return err
		}
		out[p] = fileSnap{body: string(body), mtime: info.ModTime()}
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot %s: %v", root, err)
	}
	return out
}

func assertTreeUnchanged(t *testing.T, before, after map[string]fileSnap) {
	t.Helper()
	var paths []string
	for p := range after {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		b, ok := before[p]
		if !ok {
			t.Errorf("config load created %s:\n%s", p, after[p].body)
			continue
		}
		if a := after[p]; a.body != b.body {
			t.Errorf("config load rewrote %s:\nbefore:\n%s\nafter:\n%s", p, b.body, a.body)
		} else if !a.mtime.Equal(b.mtime) {
			t.Errorf("config load touched %s: mtime %v -> %v", p, b.mtime, a.mtime)
		}
	}
	for p := range before {
		if _, ok := after[p]; !ok {
			t.Errorf("config load removed %s", p)
		}
	}
}

// writeAged writes body to path and backdates its mtime so a rewrite within
// the same clock tick still shows up as an mtime change.
func writeAged(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
}

// TestLoadNeverWritesConfigFiles: loading config is read-only. An
// unversioned file at every source the loader reads (user cascade slot,
// CTXT_CONFIG, legacy single-file override, -c path) plus -c key=value
// overrides must leave every file byte-identical with its mtime unchanged.
func TestLoadNeverWritesConfigFiles(t *testing.T) {
	overrides := map[string]any{
		"server":  map[string]any{"urls": []any{"http://127.0.0.1:19991", "http://127.0.0.1:19992"}},
		"storage": map[string]any{"path": "/nonexistent/overlay/db.sqlite"},
	}

	cases := map[string]func(t *testing.T, dir string) (cfgFile string, extra []string){
		"user cascade slot": func(t *testing.T, dir string) (string, []string) {
			writeAged(t, filepath.Join(dir, "contexthelp", "ctxt.yaml"), "server:\n  url: http://127.0.0.1:19990\n")
			return "", nil
		},
		"CTXT_CONFIG": func(t *testing.T, dir string) (string, []string) {
			p := filepath.Join(dir, "env", "ctxt.yaml")
			writeAged(t, p, "server:\n  port: 4343\n")
			t.Setenv(EnvConfigPath, p)
			return "", nil
		},
		"legacy cfgFile": func(t *testing.T, dir string) (string, []string) {
			p := filepath.Join(dir, "legacy", "ctxt.yaml")
			writeAged(t, p, "server:\n  port: 4343\n")
			return p, nil
		},
		"-c path over user slot": func(t *testing.T, dir string) (string, []string) {
			writeAged(t, filepath.Join(dir, "contexthelp", "ctxt.yaml"), "server:\n  url: http://127.0.0.1:19990\n")
			p := filepath.Join(dir, "extra", "overlay.yaml")
			writeAged(t, p, "server:\n  port: 5454\n")
			return "", []string{p}
		},
	}

	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			dir := hermeticEnv(t)
			cfgFile, extra := setup(t, dir)
			before := snapshotTree(t, dir)

			cfg, err := LoadWithOverrides("ctxt", cfgFile, extra, overrides)
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			if len(cfg.Server.URLs) != 2 || cfg.Storage.Path != "/nonexistent/overlay/db.sqlite" {
				t.Fatalf("overrides not applied: urls=%v storage.path=%q", cfg.Server.URLs, cfg.Storage.Path)
			}

			assertTreeUnchanged(t, before, snapshotTree(t, dir))
		})
	}
}

// TestLoadToleratesVersionKey: a file still carrying the retired top-level
// `version: 1` stamp (and any other unknown key) loads as if it were absent.
func TestLoadToleratesVersionKey(t *testing.T) {
	for name, body := range map[string]string{
		"version stamp": "version: 1\nserver:\n  port: 4343\n",
		"no version":    "server:\n  port: 4343\n",
		"unknown key":   "version: 7\nnot_a_real_key: x\nserver:\n  port: 4343\n",
	} {
		t.Run(name, func(t *testing.T) {
			dir := hermeticEnv(t)
			p := filepath.Join(dir, "ctxt.yaml")
			writeAged(t, p, body)
			before := snapshotTree(t, dir)

			cfg, err := Load("ctxt", p)
			if err != nil {
				t.Fatalf("load %q: %v", body, err)
			}
			if cfg.Server.Port != 4343 {
				t.Errorf("server.port = %d; want 4343", cfg.Server.Port)
			}
			assertTreeUnchanged(t, before, snapshotTree(t, dir))
		})
	}
}
