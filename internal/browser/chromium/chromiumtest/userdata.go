package chromiumtest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// SessionFileName is the Sessions/ file name WriteUserDataDir uses.
const SessionFileName = "Session_13370000000000000"

// Tab is one tab for Session.
type Tab struct {
	URL   string
	Title string
}

// Session encodes a complete version-3 session file holding tabs, all in
// window 1, left to right.
func Session(tabs ...Tab) []byte {
	b := NewSNSS(VersionWithMarker)
	var index int32
	for _, t := range tabs {
		b.Tab(1, 100+index, index, t.URL, t.Title)
		index++
	}
	return b.Marker().Bytes()
}

// Profile describes one profile for WriteUserDataDir.
type Profile struct {
	// DirName is the profile folder, e.g. "Default" or "Profile 1".
	DirName string
	// Name is the display name listed in Local State.
	Name string
	// Session, when non-nil, is written to Sessions/SessionFileName.
	Session []byte
	// ModTime is the session file's modification time; zero means now.
	ModTime time.Time
	// Missing lists the profile in Local State without creating its
	// directory.
	Missing bool
}

// WriteUserDataDir creates a user data directory under a fresh temp dir:
// a Local State file listing profiles (in order; the first is last used)
// and each profile's folder with its session file. It returns the
// directory, suitable for chromium.WithUserDataDir or the
// CTXT_<BROWSER>_USER_DATA_DIR environment variable.
func WriteUserDataDir(t testing.TB, profiles ...Profile) string {
	t.Helper()
	base := t.TempDir()

	type info struct {
		Name string `json:"name"`
	}
	var ls struct {
		Profile struct {
			InfoCache     map[string]info `json:"info_cache"`
			LastUsed      string          `json:"last_used,omitempty"`
			ProfilesOrder []string        `json:"profiles_order"`
		} `json:"profile"`
	}
	ls.Profile.InfoCache = make(map[string]info, len(profiles))
	for i, p := range profiles {
		ls.Profile.InfoCache[p.DirName] = info{Name: p.Name}
		ls.Profile.ProfilesOrder = append(ls.Profile.ProfilesOrder, p.DirName)
		if i == 0 {
			ls.Profile.LastUsed = p.DirName
		}
	}
	data, err := json.Marshal(ls)
	if err != nil {
		t.Fatalf("chromiumtest: marshal Local State: %v", err)
	}
	if err := os.WriteFile(filepath.Join(base, "Local State"), data, 0o600); err != nil {
		t.Fatalf("chromiumtest: write Local State: %v", err)
	}

	for _, p := range profiles {
		if p.Missing {
			continue
		}
		dir := filepath.Join(base, p.DirName)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("chromiumtest: mkdir %s: %v", p.DirName, err)
		}
		if p.Session == nil {
			continue
		}
		WriteSession(t, dir, SessionFileName, p.Session, p.ModTime)
	}
	return base
}

// WriteSession writes data to <profileDir>/Sessions/<name> and sets its
// modification time (zero means now).
func WriteSession(t testing.TB, profileDir, name string, data []byte, mtime time.Time) {
	t.Helper()
	p := filepath.Join(profileDir, "Sessions", name)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatalf("chromiumtest: mkdir Sessions: %v", err)
	}
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatalf("chromiumtest: write session: %v", err)
	}
	if mtime.IsZero() {
		return
	}
	if err := os.Chtimes(p, mtime, mtime); err != nil {
		t.Fatalf("chromiumtest: chtimes: %v", err)
	}
}
