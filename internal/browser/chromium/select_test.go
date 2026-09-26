package chromium

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeProfile is one profile for writeInstall.
type fakeProfile struct {
	dir, name string
	missing   bool
}

// writeInstall creates a user data dir with a Local State listing
// profiles (in order) and lastUsed as profile.last_used ("" omits it).
func writeInstall(t *testing.T, lastUsed string, profiles ...fakeProfile) string {
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
	ls.Profile.InfoCache = map[string]info{}
	ls.Profile.LastUsed = lastUsed
	for _, p := range profiles {
		ls.Profile.InfoCache[p.dir] = info{Name: p.name}
		ls.Profile.ProfilesOrder = append(ls.Profile.ProfilesOrder, p.dir)
		if !p.missing {
			if err := os.MkdirAll(filepath.Join(base, p.dir), 0o700); err != nil {
				t.Fatal(err)
			}
		}
	}
	data, err := json.Marshal(ls)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, LocalStateFile), data, 0o600); err != nil {
		t.Fatal(err)
	}
	return base
}

// fakeHost is a selector over a fixed set of installs. Browsers absent
// from installs resolve to a directory that does not exist.
func fakeHost(t *testing.T, def DefaultBrowser, defErr error, installs map[Browser]string) Selector {
	t.Helper()
	absent := filepath.Join(t.TempDir(), "absent")
	return Selector{
		UserDataDir: func(b Browser) (string, error) {
			if b == Arc && installs[Arc] == "" {
				return "", ErrUnsupported
			}
			if d, ok := installs[b]; ok {
				return d, nil
			}
			return filepath.Join(absent, string(b)), nil
		},
		DefaultBrowser: func() (DefaultBrowser, error) { return def, defErr },
	}
}

func workAndPersonal(t *testing.T) string {
	return writeInstall(t, "Default",
		fakeProfile{dir: "Default", name: "Personal"},
		fakeProfile{dir: "Profile 1", name: "Work"})
}

func TestSelect_ResolutionOrder(t *testing.T) {
	brave := workAndPersonal(t)
	chrome := writeInstall(t, "Profile 2",
		fakeProfile{dir: "Default", name: "Person 1"},
		fakeProfile{dir: "Profile 2", name: "Research"})
	braveDefault := DefaultBrowser{ID: "com.brave.browser", Browser: Brave}
	installs := map[Browser]string{Brave: brave, Chrome: chrome}

	cases := []struct {
		name          string
		def           DefaultBrowser
		req           Request
		browser       Browser
		dir           string
		browserSource Source
		profileSource Source
	}{
		{
			name: "flag beats config and default", def: braveDefault,
			req:     Request{Browser: "Chrome", ConfigBrowser: "brave", Profile: "Research"},
			browser: Chrome, dir: "Profile 2", browserSource: SourceFlag, profileSource: SourceFlag,
		},
		{
			name: "config beats profile search and default", def: braveDefault,
			req:     Request{ConfigBrowser: "chrome", Profile: "Person 1"},
			browser: Chrome, dir: "Default", browserSource: SourceConfig, profileSource: SourceFlag,
		},
		{
			name: "profile search beats default", def: braveDefault,
			req:     Request{Profile: "research"},
			browser: Chrome, dir: "Profile 2", browserSource: SourceProfileMatch, profileSource: SourceFlag,
		},
		{
			name: "profile search by folder name", def: DefaultBrowser{},
			req:     Request{Profile: "Profile 1"},
			browser: Brave, dir: "Profile 1", browserSource: SourceProfileMatch, profileSource: SourceFlag,
		},
		{
			name: "OS default with named profile", def: braveDefault,
			req:     Request{},
			browser: Brave, dir: "Default", browserSource: SourceOSDefault, profileSource: SourceLastUsed,
		},
		{
			name: "explicit browser, last used profile", def: braveDefault,
			req:     Request{Browser: "chrome"},
			browser: Chrome, dir: "Profile 2", browserSource: SourceFlag, profileSource: SourceLastUsed,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sel, err := fakeHost(t, tc.def, nil, installs).Select(tc.req)
			if err != nil {
				t.Fatalf("Select: %v", err)
			}
			if sel.Profile.Browser != tc.browser || sel.Profile.DirName != tc.dir {
				t.Errorf("picked %s/%s, want %s/%s", sel.Profile.Browser, sel.Profile.DirName, tc.browser, tc.dir)
			}
			if sel.BrowserSource != tc.browserSource || sel.ProfileSource != tc.profileSource {
				t.Errorf("sources = %s/%s, want %s/%s", sel.BrowserSource, sel.ProfileSource, tc.browserSource, tc.profileSource)
			}
			if want := filepath.Join(installs[tc.browser], tc.dir); sel.Profile.Dir != want {
				t.Errorf("Dir = %s, want %s", sel.Profile.Dir, want)
			}
		})
	}
}

func TestSelect_ProfileSearchAmbiguousAcrossBrowsers(t *testing.T) {
	brave := workAndPersonal(t)
	chrome := writeInstall(t, "Default", fakeProfile{dir: "Default", name: "Work"})
	s := fakeHost(t, DefaultBrowser{Browser: Brave}, nil, map[Browser]string{Brave: brave, Chrome: chrome})

	_, err := s.Select(Request{Profile: "work"})
	if !errors.Is(err, ErrAmbiguousProfile) {
		t.Fatalf("err = %v, want ErrAmbiguousProfile", err)
	}
	for _, want := range []string{`chrome:"Work" (Default)`, `brave:"Work" (Profile 1)`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not list %s", err, want)
		}
	}
	// "Default" is a folder in both: also ambiguous, never the OS default.
	if _, err := s.Select(Request{Profile: "Default"}); !errors.Is(err, ErrAmbiguousProfile) {
		t.Errorf("folder in several browsers: err = %v, want ErrAmbiguousProfile", err)
	}
}

func TestSelect_ProfileSearchNoMatch(t *testing.T) {
	s := fakeHost(t, DefaultBrowser{}, nil, map[Browser]string{Brave: workAndPersonal(t)})
	_, err := s.Select(Request{Profile: "Nope"})
	if !errors.Is(err, ErrProfileNotFound) {
		t.Fatalf("err = %v, want ErrProfileNotFound", err)
	}
	if !strings.Contains(err.Error(), `brave:"Work" (Profile 1)`) {
		t.Errorf("error %q does not list the candidates", err)
	}
}

func TestSelect_ProfileSearchMissingDir(t *testing.T) {
	brave := writeInstall(t, "Default",
		fakeProfile{dir: "Default", name: "Personal"},
		fakeProfile{dir: "Profile 4", name: "Archive", missing: true})
	s := fakeHost(t, DefaultBrowser{}, nil, map[Browser]string{Brave: brave})
	if _, err := s.Select(Request{Profile: "Archive"}); !errors.Is(err, ErrProfileDirMissing) {
		t.Errorf("err = %v, want ErrProfileDirMissing", err)
	}
}

func TestSelect_NoBrowser(t *testing.T) {
	brave := workAndPersonal(t)
	cases := []struct {
		name     string
		def      DefaultBrowser
		defErr   error
		installs map[Browser]string
		wants    []string
	}{
		{"default not chromium", DefaultBrowser{ID: "com.apple.safari"}, nil, map[Browser]string{Brave: brave}, []string{"brave", "com.apple.safari"}},
		{"default chromium but not installed", DefaultBrowser{ID: "com.google.chrome", Browser: Chrome}, nil, map[Browser]string{Brave: brave}, []string{"brave", "chrome"}},
		{"default lookup failed", DefaultBrowser{}, errors.New("plutil exploded"), map[Browser]string{Brave: brave}, []string{"brave", "plutil exploded"}},
		{"nothing installed", DefaultBrowser{ID: "com.brave.browser", Browser: Brave}, nil, nil, []string{"none"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := fakeHost(t, tc.def, tc.defErr, tc.installs).Select(Request{})
			if !errors.Is(err, ErrNoBrowser) {
				t.Fatalf("err = %v, want ErrNoBrowser", err)
			}
			for _, w := range tc.wants {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error %q missing %q", err, w)
				}
			}
		})
	}
}

func TestSelect_ProfileOmitted(t *testing.T) {
	t.Run("only one profile, no last_used entry for it", func(t *testing.T) {
		d := writeInstall(t, "Gone", fakeProfile{dir: "Profile 7", name: "Solo"})
		sel, err := fakeHost(t, DefaultBrowser{}, nil, map[Browser]string{Edge: d}).Select(Request{Browser: "edge"})
		if err != nil || sel.Profile.DirName != "Profile 7" || sel.ProfileSource != SourceOnlyProfile {
			t.Fatalf("got %+v, %v", sel, err)
		}
	})
	t.Run("last used names no listed profile", func(t *testing.T) {
		d := writeInstall(t, "Gone",
			fakeProfile{dir: "Default", name: "Personal"},
			fakeProfile{dir: "Profile 1", name: "Work"})
		_, err := fakeHost(t, DefaultBrowser{}, nil, map[Browser]string{Edge: d}).Select(Request{Browser: "edge"})
		if !errors.Is(err, ErrNoProfile) {
			t.Fatalf("err = %v, want ErrNoProfile", err)
		}
		if !strings.Contains(err.Error(), `"Work" (Profile 1)`) || !strings.Contains(err.Error(), `"Personal" (Default)`) {
			t.Errorf("error %q does not list the profiles", err)
		}
	})
	t.Run("last used profile dir missing", func(t *testing.T) {
		d := writeInstall(t, "Default",
			fakeProfile{dir: "Default", name: "Personal", missing: true},
			fakeProfile{dir: "Profile 1", name: "Work"},
			fakeProfile{dir: "Profile 2", name: "Other"})
		_, err := fakeHost(t, DefaultBrowser{}, nil, map[Browser]string{Edge: d}).Select(Request{Browser: "edge"})
		if !errors.Is(err, ErrNoProfile) {
			t.Fatalf("err = %v, want ErrNoProfile", err)
		}
	})
	t.Run("absent Local State", func(t *testing.T) {
		_, err := fakeHost(t, DefaultBrowser{}, nil, nil).Select(Request{Browser: "vivaldi"})
		var nie *NotInstalledError
		if !errors.Is(err, fs.ErrNotExist) || !errors.As(err, &nie) || nie.Browser != Vivaldi || nie.UserDataDir == "" {
			t.Fatalf("err = %v, want a vivaldi NotInstalledError wrapping fs.ErrNotExist", err)
		}
	})
}

func TestSelect_BadNames(t *testing.T) {
	s := fakeHost(t, DefaultBrowser{}, nil, map[Browser]string{Brave: workAndPersonal(t)})
	if _, err := s.Select(Request{Browser: "netscape"}); !errors.Is(err, ErrUnknownBrowser) {
		t.Errorf("flag: err = %v", err)
	}
	_, err := s.Select(Request{ConfigBrowser: "netscape", Profile: "Work"})
	if !errors.Is(err, ErrUnknownBrowser) || !strings.Contains(err.Error(), "capture.browser") {
		t.Errorf("config: err = %v, want ErrUnknownBrowser naming capture.browser", err)
	}
	if _, err := s.Select(Request{Browser: "brave", Profile: "Nope"}); !errors.Is(err, ErrProfileNotFound) {
		t.Errorf("unknown profile: err = %v", err)
	}
}

func TestInstalled(t *testing.T) {
	brave := workAndPersonal(t)
	broken := t.TempDir()
	if err := os.WriteFile(filepath.Join(broken, LocalStateFile), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := fakeHost(t, DefaultBrowser{}, nil, map[Browser]string{Brave: brave, Vivaldi: broken}).Installed()
	if len(got) != 2 || got[0].Browser != Brave || got[1].Browser != Vivaldi {
		t.Fatalf("Installed = %+v, want brave then vivaldi", got)
	}
	if got[0].Err != nil || len(got[0].Profiles) != 2 || got[0].UserDataDir != brave {
		t.Errorf("brave = %+v", got[0])
	}
	if !errors.Is(got[1].Err, ErrMalformedLocalState) {
		t.Errorf("vivaldi Err = %v, want ErrMalformedLocalState", got[1].Err)
	}
}
