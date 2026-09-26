package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/browser/chromium/chromiumtest"
)

// Auto-selection through a built binary: exit codes, stderr notes and
// the browser actually read. Every browser other than the ones a test
// wires in resolves under the temp HOME, where nothing is installed.

// writeLaunchServicesDefault makes bundleID the https handler in a
// LaunchServices plist under home (XML; plutil reads both formats).
func writeLaunchServicesDefault(t *testing.T, home, bundleID string) {
	t.Helper()
	p := filepath.Join(home, "Library", "Preferences", "com.apple.LaunchServices", "com.apple.launchservices.secure.plist")
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	body := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict><key>LSHandlers</key><array>
<dict><key>LSHandlerRoleAll</key><string>com.apple.mail</string><key>LSHandlerURLScheme</key><string>mailto</string></dict>
<dict><key>LSHandlerRoleAll</key><string>` + bundleID + `</string><key>LSHandlerURLScheme</key><string>https</string></dict>
</array></dict></plist>`
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (e *tabsEnv) runJSON(args ...string) (tabsDoc, string, int) {
	e.t.Helper()
	out, errOut, code := e.run(append(args, "--dry-run", "--format", "json")...)
	var doc tabsDoc
	if code == 0 {
		if err := json.Unmarshal([]byte(out), &doc); err != nil {
			e.t.Fatalf("decode %q: %v\nstderr:\n%s", out, err, errOut)
		}
	}
	return doc, errOut, code
}

func personalAndWork(now time.Time) []chromiumtest.Profile {
	return []chromiumtest.Profile{
		{DirName: "Default", Name: "Personal", ModTime: now, Session: chromiumtest.Session(tabB)},
		workProfile(now, tabA),
	}
}

func TestCaptureTabs_ProfileOnlyFindsTheBrowser(t *testing.T) {
	e := newTabsEnv(t, "", personalAndWork(time.Now())...)
	doc, errOut, code := e.runJSON("capture", "tabs", "--browser-profile", "work")
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, errOut)
	}
	if doc.Browser != "brave" || doc.Profile.Dir != "Profile 1" {
		t.Errorf("read %s/%s, want brave/Profile 1", doc.Browser, doc.Profile.Dir)
	}
	if !strings.Contains(errOut, `using brave profile "Work" (Profile 1): the only browser with that profile`) {
		t.Errorf("stderr does not name the pick:\n%s", errOut)
	}
	if len(e.requests()) != 0 {
		t.Error("dry run sent requests")
	}
}

func TestCaptureTabs_ProfileOnlyAmbiguousAcrossBrowsers(t *testing.T) {
	now := time.Now()
	e := newTabsEnv(t, "", personalAndWork(now)...)
	chrome := chromiumtest.WriteUserDataDir(t, chromiumtest.Profile{DirName: "Profile 3", Name: "Work", ModTime: now, Session: chromiumtest.Session(tabB)})
	e.extraEnv = []string{"CTXT_CHROME_USER_DATA_DIR=" + chrome}

	_, errOut, code := e.run("capture", "tabs", "--browser-profile", "Work", "--dry-run")
	if code != 2 {
		t.Fatalf("exit %d, want 2\n%s", code, errOut)
	}
	for _, want := range []string{`chrome:"Work" (Profile 3)`, `brave:"Work" (Profile 1)`} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr missing candidate %s:\n%s", want, errOut)
		}
	}
}

func TestCaptureTabs_BrowserOnlyReadsLastUsedProfile(t *testing.T) {
	e := newTabsEnv(t, "", personalAndWork(time.Now())...)
	doc, errOut, code := e.runJSON("capture", "tabs", "--browser", "brave")
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, errOut)
	}
	// chromiumtest marks the first profile last used.
	if doc.Profile.Dir != "Default" {
		t.Errorf("read %s, want the last-used Default", doc.Profile.Dir)
	}
	if !strings.Contains(errOut, `using brave profile "Personal" (Default): its last-used profile`) {
		t.Errorf("stderr does not name the profile:\n%s", errOut)
	}
}

func TestCaptureTabs_ConfigBrowser(t *testing.T) {
	now := time.Now()
	e := newTabsEnv(t, "capture:\n  browser: chrome\n", personalAndWork(now)...)
	chrome := chromiumtest.WriteUserDataDir(t, chromiumtest.Profile{DirName: "Profile 3", Name: "Work", ModTime: now, Session: chromiumtest.Session(tabB)})
	e.extraEnv = []string{"CTXT_CHROME_USER_DATA_DIR=" + chrome}

	// capture.browser beats the profile search that would be ambiguous.
	doc, errOut, code := e.runJSON("capture", "tabs", "--browser-profile", "Work")
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, errOut)
	}
	if doc.Browser != "chrome" || doc.Profile.Dir != "Profile 3" {
		t.Errorf("read %s/%s, want chrome/Profile 3", doc.Browser, doc.Profile.Dir)
	}
	if strings.Contains(errOut, "using ") {
		t.Errorf("configured browser and named profile need no note:\n%s", errOut)
	}
	// The flag beats the config.
	doc, errOut, code = e.runJSON("capture", "tabs", "--browser", "brave", "--browser-profile", "Work")
	if code != 0 || doc.Browser != "brave" {
		t.Errorf("flag over config: exit %d, browser %q\n%s", code, doc.Browser, errOut)
	}
}

func TestCaptureTabs_NoBrowserToPick(t *testing.T) {
	e := newTabsEnv(t, "", personalAndWork(time.Now())...)
	_, errOut, code := e.run("capture", "tabs", "--dry-run")
	if code != 2 {
		t.Fatalf("exit %d, want 2\n%s", code, errOut)
	}
	for _, want := range []string{"no browser selected", "installed: brave", "--browser"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr missing %q:\n%s", want, errOut)
		}
	}
}

func TestCaptureTabs_OSDefaultBrowser(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("reads a LaunchServices plist with plutil")
	}
	e := newTabsEnv(t, "", personalAndWork(time.Now())...)
	writeLaunchServicesDefault(t, e.home, "com.brave.browser")

	doc, errOut, code := e.runJSON("capture", "tabs")
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, errOut)
	}
	if doc.Browser != "brave" || doc.Profile.Dir != "Default" {
		t.Errorf("read %s/%s, want brave/Default", doc.Browser, doc.Profile.Dir)
	}
	if !strings.Contains(errOut, "the OS default browser, its last-used profile") {
		t.Errorf("stderr does not explain the pick:\n%s", errOut)
	}

	// A default that is not Chromium-family is skipped, and named.
	writeLaunchServicesDefault(t, e.home, "com.apple.safari")
	_, errOut, code = e.run("capture", "tabs", "--dry-run")
	if code != 2 || !strings.Contains(errOut, "com.apple.safari") {
		t.Errorf("safari default: exit %d, want 2 naming it\n%s", code, errOut)
	}
}
