package chromium

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newUserDataDir builds a fake user data dir in a temp dir: the named
// Local State fixture plus one directory per entry in dirs.
func newUserDataDir(t *testing.T, fixture string, dirs ...string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "localstate", fixture))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, LocalStateFile), data, 0o600); err != nil {
		t.Fatalf("write Local State: %v", err)
	}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(base, d), 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	return base
}

var basicDirs = []string{"Default", "Profile 1", "Profile 3"}

func TestProfilesListsInfoCache(t *testing.T) {
	base := newUserDataDir(t, "basic.json", basicDirs...)

	got, err := Profiles(Brave, WithUserDataDir(base))
	if err != nil {
		t.Fatalf("Profiles: %v", err)
	}

	want := []Profile{
		{Browser: Brave, DirName: "Default", Name: "Personal", Dir: filepath.Join(base, "Default")},
		{Browser: Brave, DirName: "Profile 3", Name: "Side Project", Dir: filepath.Join(base, "Profile 3")},
		{Browser: Brave, DirName: "Profile 1", Name: "Work", Dir: filepath.Join(base, "Profile 1"), LastUsed: true},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d profiles, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("profile[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestResolveProfile(t *testing.T) {
	base := newUserDataDir(t, "basic.json", basicDirs...)

	tests := []struct {
		name    string
		query   string
		wantDir string
	}{
		{"by display name", "Personal", "Default"},
		{"by folder name", "Profile 3", "Profile 3"},
		{"display name case-insensitive", "WORK", "Profile 1"},
		{"display name mixed case", "side project", "Profile 3"},
		{"folder name Default", "Default", "Default"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := ResolveProfile(Brave, tt.query, WithUserDataDir(base))
			if err != nil {
				t.Fatalf("ResolveProfile(%q): %v", tt.query, err)
			}
			if p.DirName != tt.wantDir {
				t.Fatalf("DirName = %q, want %q", p.DirName, tt.wantDir)
			}
			if p.Dir != filepath.Join(base, tt.wantDir) {
				t.Fatalf("Dir = %q, want %q", p.Dir, filepath.Join(base, tt.wantDir))
			}
		})
	}
}

func TestResolveProfileFolderNameIsExact(t *testing.T) {
	base := newUserDataDir(t, "basic.json", basicDirs...)

	_, err := ResolveProfile(Brave, "profile 3", WithUserDataDir(base))
	if !errors.Is(err, ErrProfileNotFound) {
		t.Fatalf("err = %v, want ErrProfileNotFound (folder match must be exact)", err)
	}
}

func TestResolveProfileLastUsed(t *testing.T) {
	base := newUserDataDir(t, "basic.json", basicDirs...)

	p, err := ResolveProfile(Brave, "Work", WithUserDataDir(base))
	if err != nil {
		t.Fatalf("ResolveProfile: %v", err)
	}
	if !p.LastUsed {
		t.Fatal("Work should be marked LastUsed")
	}
	p, err = ResolveProfile(Brave, "Personal", WithUserDataDir(base))
	if err != nil {
		t.Fatalf("ResolveProfile: %v", err)
	}
	if p.LastUsed {
		t.Fatal("Personal should not be marked LastUsed")
	}
}

func TestLastUsedDefaultsToDefaultFolder(t *testing.T) {
	// Chromium treats an absent profile.last_used as "Default".
	base := newUserDataDir(t, "no_last_used.json", "Default", "Profile 1")

	ps, err := Profiles(Chrome, WithUserDataDir(base))
	if err != nil {
		t.Fatalf("Profiles: %v", err)
	}
	for _, p := range ps {
		if p.LastUsed != (p.DirName == "Default") {
			t.Errorf("%s LastUsed = %v", p.DirName, p.LastUsed)
		}
	}
}

func TestResolveProfileAmbiguous(t *testing.T) {
	base := newUserDataDir(t, "ambiguous.json", "Default", "Profile 1", "Profile 2")

	_, err := ResolveProfile(Chrome, "Work", WithUserDataDir(base))
	if !errors.Is(err, ErrAmbiguousProfile) {
		t.Fatalf("err = %v, want ErrAmbiguousProfile", err)
	}
	for _, s := range []string{"Profile 1", "Profile 2"} {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("error %q should list candidate %q", err, s)
		}
	}

	// A folder name still disambiguates.
	p, err := ResolveProfile(Chrome, "Profile 2", WithUserDataDir(base))
	if err != nil {
		t.Fatalf("ResolveProfile by folder: %v", err)
	}
	if p.Name != "work" {
		t.Fatalf("Name = %q, want %q", p.Name, "work")
	}
}

func TestResolveProfileNotFound(t *testing.T) {
	base := newUserDataDir(t, "basic.json", basicDirs...)

	_, err := ResolveProfile(Brave, "Nope", WithUserDataDir(base))
	if !errors.Is(err, ErrProfileNotFound) {
		t.Fatalf("err = %v, want ErrProfileNotFound", err)
	}
	for _, s := range []string{"Personal", "Work", "Side Project"} {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("error %q should list available profile %q", err, s)
		}
	}
}

func TestResolveProfileEmptyQuery(t *testing.T) {
	base := newUserDataDir(t, "basic.json", basicDirs...)

	_, err := ResolveProfile(Brave, "  ", WithUserDataDir(base))
	if !errors.Is(err, ErrProfileNotFound) {
		t.Fatalf("err = %v, want ErrProfileNotFound", err)
	}
}

func TestMissingProfileDir(t *testing.T) {
	// Profile 3 is listed in Local State but absent on disk.
	base := newUserDataDir(t, "basic.json", "Default", "Profile 1")

	ps, err := Profiles(Brave, WithUserDataDir(base))
	if err != nil {
		t.Fatalf("Profiles: %v", err)
	}
	var found bool
	for _, p := range ps {
		if p.DirName == "Profile 3" {
			found = true
			if !p.Missing {
				t.Error("Profile 3 should be flagged Missing")
			}
		} else if p.Missing {
			t.Errorf("%s wrongly flagged Missing", p.DirName)
		}
	}
	if !found {
		t.Fatal("Profiles should still list Profile 3")
	}

	_, err = ResolveProfile(Brave, "Side Project", WithUserDataDir(base))
	if !errors.Is(err, ErrProfileDirMissing) {
		t.Fatalf("err = %v, want ErrProfileDirMissing", err)
	}
	if !strings.Contains(err.Error(), filepath.Join(base, "Profile 3")) {
		t.Errorf("error %q should name the missing directory", err)
	}
}

func TestMalformedLocalState(t *testing.T) {
	base := newUserDataDir(t, "malformed.json")

	if _, err := Profiles(Brave, WithUserDataDir(base)); !errors.Is(err, ErrMalformedLocalState) {
		t.Fatalf("Profiles err = %v, want ErrMalformedLocalState", err)
	}
	if _, err := ResolveProfile(Brave, "Personal", WithUserDataDir(base)); !errors.Is(err, ErrMalformedLocalState) {
		t.Fatalf("ResolveProfile err = %v, want ErrMalformedLocalState", err)
	}
}

func TestMissingLocalState(t *testing.T) {
	_, err := Profiles(Brave, WithUserDataDir(t.TempDir()))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err = %v, want os.ErrNotExist", err)
	}
}

func TestUnsafeFolderNamesSkipped(t *testing.T) {
	base := newUserDataDir(t, "unsafe_dir.json", "Default")

	ps, err := Profiles(Brave, WithUserDataDir(base))
	if err != nil {
		t.Fatalf("Profiles: %v", err)
	}
	if len(ps) != 1 || ps[0].DirName != "Default" {
		t.Fatalf("got %+v, want only Default", ps)
	}
	if _, err := ResolveProfile(Brave, "Escape", WithUserDataDir(base)); !errors.Is(err, ErrProfileNotFound) {
		t.Fatalf("err = %v, want ErrProfileNotFound", err)
	}
}
