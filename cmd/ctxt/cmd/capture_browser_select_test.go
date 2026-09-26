package cmd

import (
	"path/filepath"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/browser/chromium"
	"github.com/ideacrafterslabs/ctxt/internal/browser/chromium/chromiumtest"
)

// useFakeBrowsers points every capture command at synthetic installs
// and a fixed OS default browser. Browsers absent from installs have no
// Local State, so nothing on the host is ever read.
func useFakeBrowsers(t *testing.T, def chromium.DefaultBrowser, installs map[chromium.Browser]string) {
	t.Helper()
	absent := filepath.Join(t.TempDir(), "absent")
	prev := captureBrowserSelector
	captureBrowserSelector = func() chromium.Selector {
		return chromium.Selector{
			UserDataDir: func(b chromium.Browser) (string, error) {
				if d, ok := installs[b]; ok {
					return d, nil
				}
				return filepath.Join(absent, string(b)), nil
			},
			DefaultBrowser: func() (chromium.DefaultBrowser, error) { return def, nil },
		}
	}
	t.Cleanup(func() { captureBrowserSelector = prev })
}

// Folders of the standard schedule fixture profiles.
const (
	workDir = "Profile 1" // chrome "Work", last used
	oddDir  = "Profile 2" // chrome oddProfile
)

// useStandardBrowsers installs chrome (Work, oddProfile) and brave
// (oddProfile), with brave as the OS default.
func useStandardBrowsers(t *testing.T) (chrome, brave string) {
	t.Helper()
	chrome = chromiumtest.WriteUserDataDir(t,
		chromiumtest.Profile{DirName: workDir, Name: "Work"},
		chromiumtest.Profile{DirName: oddDir, Name: oddProfile})
	brave = chromiumtest.WriteUserDataDir(t, chromiumtest.Profile{DirName: "Default", Name: oddProfile})
	useFakeBrowsers(t, chromium.DefaultBrowser{ID: "com.brave.browser", Browser: chromium.Brave},
		map[chromium.Browser]string{chromium.Chrome: chrome, chromium.Brave: brave})
	return chrome, brave
}

func TestSelectionNote(t *testing.T) {
	p := chromium.Profile{Browser: chromium.Brave, Name: "Work", DirName: "Profile 1"}
	cases := []struct {
		b, p chromium.Source
		want string
	}{
		{chromium.SourceFlag, chromium.SourceFlag, ""},
		{chromium.SourceConfig, chromium.SourceFlag, ""},
		{chromium.SourceOSDefault, chromium.SourceFlag, `using brave profile "Work" (Profile 1): the OS default browser`},
		{chromium.SourceProfileMatch, chromium.SourceFlag, `using brave profile "Work" (Profile 1): the only browser with that profile`},
		{chromium.SourceFlag, chromium.SourceLastUsed, `using brave profile "Work" (Profile 1): its last-used profile`},
		{chromium.SourceConfig, chromium.SourceOnlyProfile, `using brave profile "Work" (Profile 1): its only profile`},
		{chromium.SourceOSDefault, chromium.SourceLastUsed, `using brave profile "Work" (Profile 1): the OS default browser, its last-used profile`},
	}
	for _, tc := range cases {
		got := selectionNote(chromium.Selection{Profile: p, BrowserSource: tc.b, ProfileSource: tc.p})
		if got != tc.want {
			t.Errorf("%s/%s: %q, want %q", tc.b, tc.p, got, tc.want)
		}
	}
}
