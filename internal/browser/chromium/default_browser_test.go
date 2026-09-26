package chromium

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBrowserForHandler(t *testing.T) {
	cases := []struct {
		goos, id string
		want     Browser
	}{
		// macOS LaunchServices stores bundle ids lowercased; match either case.
		{"darwin", "com.brave.browser", Brave},
		{"darwin", "com.brave.Browser", Brave},
		{"darwin", "com.google.chrome", Chrome},
		{"darwin", "com.microsoft.edgemac", Edge},
		{"darwin", "company.thebrowser.browser", Arc},
		{"darwin", "org.chromium.chromium", Chromium},
		{"darwin", "com.vivaldi.vivaldi", Vivaldi},
		// Channels keep their data elsewhere; never map them to stable.
		{"darwin", "com.google.chrome.canary", ""},
		{"darwin", "com.microsoft.edgemac.beta", ""},
		{"darwin", "com.apple.safari", ""},
		{"linux", "google-chrome.desktop", Chrome},
		{"linux", "brave-browser.desktop", Brave},
		{"linux", "microsoft-edge.desktop", Edge},
		{"linux", "chromium.desktop", Chromium},
		{"linux", "chromium-browser.desktop", Chromium},
		{"linux", "vivaldi-stable.desktop", Vivaldi},
		{"linux", "firefox.desktop", ""},
		{"linux", "google-chrome-beta.desktop", ""},
		{"windows", "ChromeHTML", Chrome},
		{"windows", "BraveHTML", Brave},
		{"windows", "MSEdgeHTM", Edge},
		{"windows", "ChromiumHTM.ABCDEFGHIJ", Chromium},
		{"windows", "VivaldiHTM.XYZ", Vivaldi},
		{"windows", "ChromeBHTML", ""},
		{"windows", "FirefoxURL-308046B0AF4A39CB", ""},
		{"darwin", "", ""},
		{"plan9", "com.google.chrome", ""},
	}
	for _, tc := range cases {
		if got := BrowserForHandler(tc.goos, tc.id); got != tc.want {
			t.Errorf("BrowserForHandler(%s, %q) = %q, want %q", tc.goos, tc.id, got, tc.want)
		}
	}
}

// launchServicesJSON is `plutil -convert json` output for a secure
// LaunchServices plist, trimmed to the keys that matter.
const launchServicesJSON = `{"LSHandlers":[
 {"LSHandlerContentType":"public.html","LSHandlerRoleViewer":"com.apple.safari"},
 {"LSHandlerURLScheme":"mailto","LSHandlerRoleAll":"com.apple.mail"},
 {"LSHandlerURLScheme":"http","LSHandlerRoleAll":"com.google.chrome"},
 {"LSHandlerURLScheme":"https","LSHandlerRoleAll":"com.brave.browser","LSHandlerPreferredVersions":{"LSHandlerRoleAll":"-"}}
]}`

func TestHTTPSHandlerFromLaunchServices(t *testing.T) {
	id, err := httpsHandlerFromLaunchServices([]byte(launchServicesJSON))
	if err != nil || id != "com.brave.browser" {
		t.Fatalf("got %q, %v; want the https LSHandlerRoleAll", id, err)
	}
	id, err = httpsHandlerFromLaunchServices([]byte(`{"LSHandlers":[{"LSHandlerURLScheme":"http","LSHandlerRoleAll":"x"}]}`))
	if err != nil || id != "" {
		t.Errorf("no https entry: got %q, %v; want empty (the system default, Safari)", id, err)
	}
	if _, err := httpsHandlerFromLaunchServices([]byte("not json")); err == nil {
		t.Error("malformed JSON: want error")
	}
}

func TestProgIDFromRegQuery(t *testing.T) {
	out := "\r\nHKEY_CURRENT_USER\\Software\\Microsoft\\Windows\\Shell\\Associations\\UrlAssociations\\https\\UserChoice\r\n" +
		"    Hash    REG_SZ    abc=\r\n    ProgId    REG_SZ    BraveHTML\r\n\r\n"
	if got := progIDFromRegQuery(out); got != "BraveHTML" {
		t.Errorf("progIDFromRegQuery = %q, want BraveHTML", got)
	}
	if got := progIDFromRegQuery("ERROR: The system was unable to find the specified registry key or value.\r\n"); got != "" {
		t.Errorf("error output: got %q, want empty", got)
	}
}

// fakeRunner records commands and answers from a table.
type fakeRunner struct {
	calls []string
	out   map[string]string
	err   map[string]error
}

func (f *fakeRunner) run(name string, args ...string) ([]byte, error) {
	key := strings.Join(append([]string{name}, args...), " ")
	f.calls = append(f.calls, key)
	for prefix, err := range f.err {
		if strings.HasPrefix(key, prefix) {
			return nil, err
		}
	}
	for prefix, out := range f.out {
		if strings.HasPrefix(key, prefix) {
			return []byte(out), nil
		}
	}
	return nil, errors.New("unexpected command: " + key)
}

func TestDetectDefaultBrowser_Darwin(t *testing.T) {
	home := t.TempDir()
	plist := filepath.Join(home, "Library", "Preferences", "com.apple.LaunchServices", "com.apple.launchservices.secure.plist")
	r := &fakeRunner{out: map[string]string{"plutil -convert json -o - " + plist: launchServicesJSON}}
	h := defaultBrowserHost{goos: "darwin", home: home, run: r.run}

	// No prefs file: the user never picked a browser, so Safari is the
	// default and plutil is not run.
	got, err := detectDefaultBrowser(h)
	if err != nil || got != (DefaultBrowser{}) || len(r.calls) != 0 {
		t.Fatalf("no plist: got %+v, %v, calls %v", got, err, r.calls)
	}

	if err := os.MkdirAll(filepath.Dir(plist), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plist, []byte("bplist00"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err = detectDefaultBrowser(h)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "com.brave.browser" || got.Browser != Brave {
		t.Errorf("got %+v, want brave", got)
	}

	r.err = map[string]error{"plutil": errors.New("plutil: exit 1")}
	if _, err := detectDefaultBrowser(h); err == nil {
		t.Error("plutil failure: want error")
	}
}

func TestDetectDefaultBrowser_Linux(t *testing.T) {
	r := &fakeRunner{out: map[string]string{"xdg-settings get default-web-browser": "brave-browser.desktop\n"}}
	got, err := detectDefaultBrowser(defaultBrowserHost{goos: "linux", run: r.run})
	if err != nil || got.ID != "brave-browser.desktop" || got.Browser != Brave {
		t.Fatalf("got %+v, %v", got, err)
	}
	r = &fakeRunner{out: map[string]string{"xdg-settings get default-web-browser": "firefox.desktop\n"}}
	got, err = detectDefaultBrowser(defaultBrowserHost{goos: "linux", run: r.run})
	if err != nil || got.ID != "firefox.desktop" || got.Browser != "" {
		t.Fatalf("firefox: got %+v, %v", got, err)
	}
}

func TestDetectDefaultBrowser_Windows(t *testing.T) {
	const key = `HKCU\Software\Microsoft\Windows\Shell\Associations\UrlAssociations\https\`
	latest := "reg query " + key + "UserChoiceLatest /v ProgId"
	legacy := "reg query " + key + "UserChoice /v ProgId"

	// Windows 11 25H2 writes UserChoiceLatest and stops updating
	// UserChoice, so UserChoiceLatest wins when present.
	r := &fakeRunner{out: map[string]string{
		latest: "    ProgId    REG_SZ    ChromeHTML\r\n",
		legacy: "    ProgId    REG_SZ    MSEdgeHTM\r\n",
	}}
	got, err := detectDefaultBrowser(defaultBrowserHost{goos: "windows", run: r.run})
	if err != nil || got.Browser != Chrome {
		t.Fatalf("got %+v, %v; want UserChoiceLatest's chrome", got, err)
	}

	r = &fakeRunner{
		out: map[string]string{legacy: "    ProgId    REG_SZ    BraveHTML\r\n"},
		err: map[string]error{latest: errors.New("exit status 1")},
	}
	got, err = detectDefaultBrowser(defaultBrowserHost{goos: "windows", run: r.run})
	if err != nil || got.ID != "BraveHTML" || got.Browser != Brave {
		t.Fatalf("legacy fallback: got %+v, %v", got, err)
	}
}

func TestDetectDefaultBrowser_OtherOS(t *testing.T) {
	r := &fakeRunner{}
	got, err := detectDefaultBrowser(defaultBrowserHost{goos: "plan9", run: r.run})
	if err == nil || got != (DefaultBrowser{}) || len(r.calls) != 0 {
		t.Errorf("got %+v, %v, calls %v; want an unsupported error and no commands", got, err, r.calls)
	}
}
