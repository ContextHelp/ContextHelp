package accessmarker

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/config"
)

func TestReadWrite_RoundTrip(t *testing.T) {
	db := filepath.Join(t.TempDir(), "db.sqlite")
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	want := Marker{Access: config.AccessProtected, Instance: "home", DaemonAddr: "0.0.0.0:8080", UpdatedAt: at}
	if err := Write(db, want); err != nil {
		t.Fatal(err)
	}
	got, found, err := Read(db)
	if err != nil || !found {
		t.Fatalf("Read = %+v, %v, %v", got, found, err)
	}
	if got != want {
		t.Errorf("Read = %+v; want %+v", got, want)
	}
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(Path(db))
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("marker mode %v; want 0600", fi.Mode().Perm())
		}
	}
}

// No marker is private: the instance was never served protected or
// public.
func TestRead_AbsentIsPrivate(t *testing.T) {
	m, found, err := Read(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil || found || m.Access != config.AccessPrivate {
		t.Fatalf("Read(absent) = %+v, %v, %v; want private, not found", m, found, err)
	}
}

func TestWrite_RejectsInvalidClass(t *testing.T) {
	db := filepath.Join(t.TempDir(), "db.sqlite")
	for _, a := range []string{"", "open", "Private"} {
		if err := Write(db, Marker{Access: a}); err == nil {
			t.Errorf("Write(access %q) succeeded", a)
		}
	}
	if _, err := os.Stat(Path(db)); !os.IsNotExist(err) {
		t.Errorf("invalid write left a marker: %v", err)
	}
}

// Write replaces the marker whole and leaves no temporary file: a
// reader sees the old marker or the new one.
func TestWrite_AtomicReplace(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "db.sqlite")
	if err := Write(db, Marker{Access: config.AccessPublic, Instance: "a-much-longer-instance-name"}); err != nil {
		t.Fatal(err)
	}
	if err := Write(db, Marker{Access: config.AccessPrivate, Instance: "b"}); err != nil {
		t.Fatal(err)
	}
	m, _, err := Read(db)
	if err != nil || m.Access != config.AccessPrivate || m.Instance != "b" {
		t.Fatalf("after replace: %+v, %v", m, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != filepath.Base(Path(db)) {
			t.Errorf("stray file after write: %s", e.Name())
		}
	}
}

// A write that cannot complete leaves the previous marker in place.
func TestWrite_FailureKeepsOldMarker(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory write permission is not a mode bit on Windows")
	}
	dir := t.TempDir()
	db := filepath.Join(dir, "db.sqlite")
	if err := Write(db, Marker{Access: config.AccessProtected, Instance: "home"}); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if err := Write(db, Marker{Access: config.AccessPrivate}); err == nil {
		t.Skip("directory not write-protected (running as root?)")
	}
	m, _, err := Read(db)
	if err != nil || m.Access != config.AccessProtected {
		t.Fatalf("marker after failed write = %+v, %v; want the old protected marker", m, err)
	}
}

func TestGate(t *testing.T) {
	cases := map[string]struct {
		body    string // "" = no marker
		refused bool
	}{
		"absent":    {"", false},
		"private":   {`{"access":"private","instance":"home"}`, false},
		"protected": {`{"access":"protected","instance":"home","daemon_addr":"0.0.0.0:8420"}`, true},
		"public":    {`{"access":"public","instance":"home"}`, true},
		"malformed": {`{"access":`, true},
		"unknown":   {`{"access":"open"}`, true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			db := filepath.Join(t.TempDir(), "db.sqlite")
			if tc.body != "" {
				if err := os.WriteFile(Path(db), []byte(tc.body), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			err := Gate(db)
			if !tc.refused {
				if err != nil {
					t.Fatalf("Gate = %v; want nil", err)
				}
				return
			}
			if !errors.Is(err, ErrRefused) {
				t.Fatalf("Gate = %v; want ErrRefused", err)
			}
			var re *RefusedError
			if !errors.As(err, &re) {
				t.Fatalf("Gate error %T is not *RefusedError", err)
			}
			if re.Cause == nil {
				for _, want := range []string{`"home"`, re.Marker.Access} {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("error %q does not name %s", err, want)
					}
				}
			}
			fix := re.Fix()
			wants := []string{"daemon API", "dpkms serve", "dpkms instance set-access private"}
			if re.Cause != nil {
				// An unreadable marker: repair it, the class is unknown.
				wants = []string{"unreadable", "dpkms instance set-access"}
			}
			for _, want := range wants {
				if !strings.Contains(fix, want) {
					t.Errorf("fix %q does not mention %q", fix, want)
				}
			}
			if name == "protected" && !strings.Contains(fix, "0.0.0.0:8420") {
				t.Errorf("fix %q does not name the daemon address", fix)
			}
		})
	}
}

// A marker written for the real path gates every spelling of the
// database: a symlinked file or directory, and a missing file under a
// symlinked directory.
func TestGate_ResolvesSymlinks(t *testing.T) {
	dir := t.TempDir()
	realDir := filepath.Join(dir, "real")
	if err := os.Mkdir(realDir, 0o700); err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(realDir, "db.sqlite")
	if err := os.WriteFile(db, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	linkDir := filepath.Join(dir, "link")
	linkFile := filepath.Join(dir, "db-link.sqlite")
	if err := os.Symlink(realDir, linkDir); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(db, linkFile); err != nil {
		t.Fatal(err)
	}
	if err := Write(db, Marker{Access: config.AccessProtected, Instance: "home"}); err != nil {
		t.Fatal(err)
	}
	for _, alias := range []string{linkFile, filepath.Join(linkDir, "db.sqlite")} {
		if err := Gate(alias); !errors.Is(err, ErrRefused) {
			t.Errorf("Gate(%s) = %v; want ErrRefused", alias, err)
		}
	}
	if got, want := Path(filepath.Join(linkDir, "new.sqlite")), Path(filepath.Join(realDir, "new.sqlite")); got != want {
		t.Errorf("Path under a symlinked dir = %s; want %s", got, want)
	}
}
