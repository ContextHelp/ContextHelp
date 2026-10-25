//go:build !windows

package accessmarker

import (
	"os"
	"path/filepath"
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
