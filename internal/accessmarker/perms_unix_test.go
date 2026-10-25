//go:build !windows

package accessmarker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckPermissions(t *testing.T) {
	cases := map[string]struct {
		dbMode, walMode, blobMode os.FileMode
		want                      []string // base names flagged
	}{
		"locked down":        {0o600, 0o600, 0o700, nil},
		"world-readable db":  {0o644, 0o600, 0o700, []string{"db.sqlite"}},
		"group-writable wal": {0o600, 0o620, 0o700, []string{"db.sqlite-wal"}},
		"open blob dir":      {0o600, 0o600, 0o755, []string{"blobs"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			db := filepath.Join(dir, "db.sqlite")
			blobs := filepath.Join(dir, "blobs")
			for p, mode := range map[string]os.FileMode{db: tc.dbMode, db + "-wal": tc.walMode} {
				if err := os.WriteFile(p, nil, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(p, mode); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Mkdir(blobs, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(blobs, tc.blobMode); err != nil {
				t.Fatal(err)
			}
			got, err := CheckPermissions(db, blobs)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("findings %v; want %v", got, tc.want)
			}
			for i, f := range got {
				if filepath.Base(f.Path) != tc.want[i] {
					t.Errorf("finding %d = %s; want %s", i, f.Path, tc.want[i])
				}
			}
		})
	}
}

// Missing instance files (no WAL yet, no marker) are not findings.
func TestCheckPermissions_MissingSkipped(t *testing.T) {
	got, err := CheckPermissions(filepath.Join(t.TempDir(), "absent.sqlite"), "")
	if err != nil || len(got) != 0 {
		t.Fatalf("CheckPermissions(missing) = %v, %v", got, err)
	}
}

func TestOwnedByCurrentUser(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	owned, uid, err := OwnedByCurrentUser(p)
	if err != nil || !owned || uid != os.Getuid() {
		t.Fatalf("OwnedByCurrentUser(own file) = %v, %d, %v", owned, uid, err)
	}
}

// Blobs written before the store created them closed (0750 dirs, 0644
// files) keep their modes: the check walks the blob tree and reports
// each, with the command that fixes it.
func TestCheckPermissions_LooseBlobsInClosedDir(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "db.sqlite")
	blobs := filepath.Join(dir, "blobs")
	fan := filepath.Join(blobs, "ab", "cd")
	if err := os.MkdirAll(fan, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(blobs, "ab"), 0o750); err != nil {
		t.Fatal(err)
	}
	blob := filepath.Join(fan, "abcdef")
	if err := os.WriteFile(blob, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(blob, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := CheckPermissions(db, blobs)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("findings %v; want the 0750 fan-out dir and the 0644 blob", got)
	}
	for _, f := range got {
		want := "chmod 0600"
		if f.Mode.IsDir() {
			want = "chmod 0700"
		}
		if !strings.Contains(f.Fix(), want) || !strings.Contains(f.Fix(), f.Path) {
			t.Errorf("fix for %s = %q; want %q naming the path", f.Path, f.Fix(), want)
		}
	}
	if fix := BlobTreeFix(blobs); !strings.Contains(fix, "find "+blobs) {
		t.Errorf("blob tree fix %q", fix)
	}
}
