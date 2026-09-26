package cmd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/browser/chromium"
	"github.com/ideacrafterslabs/ctxt/internal/browser/chromium/chromiumtest"
)

// execBrowsers runs args only when they resolve to capture browsers, so
// a missing subcommand never falls through to `ctxt capture <url>`.
func execBrowsers(t *testing.T, args ...string) (string, error) {
	t.Helper()
	target, _, err := rootCmd.Find(args)
	if err != nil || target != captureBrowsersCmd {
		t.Fatalf("%q does not resolve to capture browsers; refusing to run it", args)
	}
	return executeCommand(args...)
}

// browsersDoc mirrors the JSON document.
type browsersDoc struct {
	Browsers []struct {
		Browser     string `json:"browser"`
		UserDataDir string `json:"user_data_dir"`
		Error       string `json:"error"`
		Profiles    []struct {
			Name     string `json:"name"`
			Dir      string `json:"dir"`
			LastUsed bool   `json:"last_used"`
			Missing  bool   `json:"missing"`
		} `json:"profiles"`
		OSDefault bool `json:"os_default"`
	} `json:"browsers"`
	OSDefault struct {
		ID      string `json:"id"`
		Browser string `json:"browser"`
		Error   string `json:"error"`
	} `json:"os_default"`
	Selection struct {
		Browser string `json:"browser"`
		Profile *struct {
			Name string `json:"name"`
			Dir  string `json:"dir"`
		} `json:"profile"`
		BrowserSource string `json:"browser_source"`
		ProfileSource string `json:"profile_source"`
		Error         string `json:"error"`
	} `json:"selection"`
}

func browsersJSON(t *testing.T, args ...string) browsersDoc {
	t.Helper()
	out, err := execBrowsers(t, append([]string{"capture", "browsers", "--format", "json"}, args...)...)
	if err != nil {
		t.Fatalf("capture browsers: %v\n%s", err, out)
	}
	var doc browsersDoc
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("decode %q: %v", out, err)
	}
	return doc
}

func TestCaptureBrowsers_ListsInstallsAndSelection(t *testing.T) {
	chrome, brave := useStandardBrowsers(t)
	doc := browsersJSON(t)

	if len(doc.Browsers) != 2 || doc.Browsers[0].Browser != "chrome" || doc.Browsers[1].Browser != "brave" {
		t.Fatalf("browsers = %+v, want chrome then brave", doc.Browsers)
	}
	c, b := doc.Browsers[0], doc.Browsers[1]
	if c.OSDefault || !b.OSDefault {
		t.Errorf("os_default flags: chrome %v brave %v, want brave only", c.OSDefault, b.OSDefault)
	}
	if c.UserDataDir != chrome || b.UserDataDir != brave {
		t.Errorf("user data dirs = %s, %s", c.UserDataDir, b.UserDataDir)
	}
	if len(c.Profiles) != 2 || c.Profiles[0].Dir != workDir || c.Profiles[0].Name != "Work" || !c.Profiles[0].LastUsed || c.Profiles[1].LastUsed {
		t.Errorf("chrome profiles = %+v", c.Profiles)
	}
	if doc.OSDefault.ID != "com.brave.browser" || doc.OSDefault.Browser != "brave" {
		t.Errorf("os_default = %+v", doc.OSDefault)
	}
	s := doc.Selection
	if s.Browser != "brave" || s.Profile == nil || s.Profile.Dir != "Default" ||
		s.BrowserSource != "os_default" || s.ProfileSource != "last_used" || s.Error != "" {
		t.Errorf("selection = %+v", s)
	}
}

func TestCaptureBrowsers_SelectionFollowsFlags(t *testing.T) {
	useStandardBrowsers(t)
	s := browsersJSON(t, "--browser-profile", "work").Selection
	if s.Browser != "chrome" || s.Profile == nil || s.Profile.Dir != workDir || s.BrowserSource != "profile_match" {
		t.Errorf("profile search selection = %+v", s)
	}
	s = browsersJSON(t, "--browser", "chrome").Selection
	if s.Browser != "chrome" || s.BrowserSource != "flag" || s.ProfileSource != "last_used" {
		t.Errorf("flag selection = %+v", s)
	}
	// A failed selection is reported, not an error: the listing is the point.
	s = browsersJSON(t, "--browser-profile", oddProfile).Selection
	if s.Error == "" || !strings.Contains(s.Error, "ambiguous") || s.Profile != nil {
		t.Errorf("ambiguous selection = %+v", s)
	}
}

func TestCaptureBrowsers_ConfigBrowser(t *testing.T) {
	useStandardBrowsers(t)
	doc := browsersJSON(t, "-c", "capture.browser=chrome")
	if doc.Selection.Browser != "chrome" || doc.Selection.BrowserSource != "config" {
		t.Errorf("selection = %+v, want chrome from config", doc.Selection)
	}
}

func TestCaptureBrowsers_NoneInstalled(t *testing.T) {
	useFakeBrowsers(t, chromium.DefaultBrowser{ID: "com.apple.safari"}, nil)

	out, err := execBrowsers(t, "capture", "browsers")
	if err != nil {
		t.Fatalf("want exit 0 with nothing installed, got %v\n%s", err, out)
	}
	if !strings.Contains(out, "no Chromium-family browser found") {
		t.Errorf("human output missing the notice:\n%s", out)
	}
	doc := browsersJSON(t)
	if doc.Browsers == nil || len(doc.Browsers) != 0 {
		t.Errorf("browsers = %#v, want []", doc.Browsers)
	}
	if doc.Selection.Error == "" {
		t.Error("selection error missing")
	}
}

func TestCaptureBrowsers_HumanOutput(t *testing.T) {
	chrome := chromiumtest.WriteUserDataDir(t,
		chromiumtest.Profile{DirName: workDir, Name: "Work"},
		chromiumtest.Profile{DirName: "Profile 9", Name: "Archive", Missing: true})
	useFakeBrowsers(t, chromium.DefaultBrowser{ID: "com.google.chrome", Browser: chromium.Chrome},
		map[chromium.Browser]string{chromium.Chrome: chrome})

	out, err := execBrowsers(t, "capture", "browsers")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{
		"chrome (OS default)", chrome,
		"* Work", "(Profile 1)",
		"Archive", "missing",
		`would use: chrome profile "Work" (Profile 1)`,
		"browser: OS default", "profile: last used",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestCaptureBrowsers_SignatureClean(t *testing.T) {
	resetAllFlags(rootCmd)
	rootCmd.InitDefaultCompletionCmd()
	applyCommandGroups()
	root.ApplyGroupVisibility()
	applyShapeAnnotations()

	for _, v := range root.ValidateSignature().Violations {
		if strings.Contains(v.Path, "capture browsers") || strings.Contains(v.Path, "capture tabs") {
			t.Errorf("signature violation: %s [%s/%s] %s", v.Path, v.Check, v.Severity, v.Detail)
		}
	}
}
