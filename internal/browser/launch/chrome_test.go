package launch

import (
	"errors"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

// The switches every headless run must carry, spelled out here rather
// than read from mandatoryFlags so that dropping one from the package
// fails this test.
var wantMandatory = []string{
	"--headless=new",
	"--use-mock-keychain",
	"--password-store=basic",
	"--no-first-run",
	"--no-default-browser-check",
	"--disable-sync",
	"--disable-extensions",
	"--disable-background-networking",
	"--disable-component-update",
	"--disable-default-apps",
	"--mute-audio",
}

// assertMandatory checks argv carries every mandatory switch and the
// profile, after the last of the caller's extra switches.
func assertMandatory(t *testing.T, argv []string, profile string, extra []string) {
	t.Helper()
	after := 0
	for _, e := range extra {
		if i := slices.Index(argv, e); i >= after {
			after = i + 1
		}
	}
	for _, f := range append(slices.Clone(wantMandatory), "--user-data-dir="+profile) {
		i := slices.Index(argv, f)
		switch {
		case i < 0:
			t.Errorf("argv lacks %s: %q", f, argv)
		case i < after:
			t.Errorf("%s at %d precedes a caller switch (last at %d): %q", f, i, after-1, argv)
		}
	}
}

func TestHeadlessArgs_MandatoryAlwaysPresent(t *testing.T) {
	for _, extra := range [][]string{
		nil,
		{"--window-size=800,600"},
		{"--window-size=800,600", "--lang=fr", "--disable-gpu"},
	} {
		argv, err := headlessArgs("/tmp/p", false, extra, []string{"--dump-dom"}, []string{"http://x/"})
		if err != nil {
			t.Fatalf("extra %q: %v", extra, err)
		}
		assertMandatory(t, argv, "/tmp/p", extra)
		for _, e := range extra {
			if !slices.Contains(argv, e) {
				t.Errorf("caller switch %s dropped: %q", e, argv)
			}
		}
		if got := argv[len(argv)-1]; got != "http://x/" {
			t.Errorf("last arg = %q, want the target", got)
		}
		if !slices.Contains(argv, "--dump-dom") {
			t.Errorf("owned switch missing: %q", argv)
		}
	}
}

// A caller cannot override or duplicate a switch the package sets: the
// run is refused before Chrome starts rather than silently keeping one of
// two values.
func TestHeadlessArgs_ReservedSwitchRefused(t *testing.T) {
	for _, arg := range []string{
		"--headless=old",
		"--headless",
		"-headless",
		"--use-mock-keychain",
		"--USE-MOCK-KEYCHAIN",
		"--password-store=gnome-libsecret",
		"--password-store",
		"--user-data-dir=/Users/me/Library/Application Support/Google/Chrome",
		"--disable-sync=false",
		"--dump-dom",
	} {
		_, err := headlessArgs("/tmp/p", false, []string{"--lang=fr", arg}, []string{"--dump-dom"}, nil)
		if !errors.Is(err, ErrReservedFlag) {
			t.Errorf("%s: err = %v, want ErrReservedFlag", arg, err)
		}
	}
}

func TestHeadlessArgs_NoSandbox(t *testing.T) {
	with, _ := headlessArgs("/tmp/p", true, nil, nil, nil)
	without, _ := headlessArgs("/tmp/p", false, nil, nil, nil)
	if !slices.Contains(with, "--no-sandbox") || slices.Contains(without, "--no-sandbox") {
		t.Errorf("with=%q without=%q", with, without)
	}
	assertMandatory(t, with, "/tmp/p", nil)
}

func TestHeadlessArgs_EmptyProfile(t *testing.T) {
	if _, err := headlessArgs("", false, nil, nil, nil); err == nil {
		t.Error("empty profile accepted")
	}
}

func TestNeedsNoSandbox(t *testing.T) {
	for _, tc := range []struct {
		euid int
		goos string
		ci   bool
		want bool
	}{
		{0, "darwin", false, true},
		{0, "linux", false, true},
		{501, "linux", true, true},
		{501, "linux", false, false},
		{501, "darwin", true, false},
		{-1, "windows", true, false},
	} {
		if got := needsNoSandbox(tc.euid, tc.goos, tc.ci); got != tc.want {
			t.Errorf("needsNoSandbox(%d, %s, %v) = %v", tc.euid, tc.goos, tc.ci, got)
		}
	}
}

// slashed normalises separators so Windows paths compare on any host.
func slashed(p string) string { return strings.ReplaceAll(p, `\`, "/") }

// fakeLocator is a locator over a fixed environment, file set and PATH.
func fakeLocator(goos string, env map[string]string, files []string, path map[string]string) locator {
	return locator{
		goos:   goos,
		getenv: func(k string) string { return env[k] },
		exists: func(p string) bool { return slices.Contains(files, slashed(p)) },
		lookPath: func(name string) (string, error) {
			if p, ok := path[name]; ok {
				return p, nil
			}
			if strings.Contains(name, "/") && slices.Contains(files, name) {
				return name, nil
			}
			return "", exec.ErrNotFound
		},
	}
}

func TestFindChrome(t *testing.T) {
	const macChrome = "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
	for _, tc := range []struct {
		name    string
		loc     locator
		want    string
		wantErr error
	}{
		{"env path wins over install", fakeLocator("darwin",
			map[string]string{EnvChrome: "/opt/chrome"}, []string{macChrome, "/opt/chrome"}, nil), "/opt/chrome", nil},
		{"env command resolved on PATH", fakeLocator("linux",
			map[string]string{EnvChrome: "chromium"}, nil, map[string]string{"chromium": "/usr/bin/chromium"}), "/usr/bin/chromium", nil},
		{"env off", fakeLocator("darwin", map[string]string{EnvChrome: "off"}, []string{macChrome}, nil), "", ErrDisabled},
		{"env 0", fakeLocator("darwin", map[string]string{EnvChrome: "0"}, []string{macChrome}, nil), "", ErrDisabled},
		{"env FALSE", fakeLocator("darwin", map[string]string{EnvChrome: "FALSE"}, []string{macChrome}, nil), "", ErrDisabled},
		{"env missing executable", fakeLocator("darwin", map[string]string{EnvChrome: "/nope/chrome"}, []string{macChrome}, nil), "", exec.ErrNotFound},
		{"mac install", fakeLocator("darwin", nil, []string{macChrome}, map[string]string{"chromium": "/usr/local/bin/chromium"}), macChrome, nil},
		{"mac user Applications", fakeLocator("darwin", map[string]string{"HOME": "/Users/me"},
			[]string{"/Users/me/Applications/Chromium.app/Contents/MacOS/Chromium"}, nil),
			"/Users/me/Applications/Chromium.app/Contents/MacOS/Chromium", nil},
		{"linux PATH order", fakeLocator("linux", nil, nil,
			map[string]string{"chromium": "/usr/bin/chromium", "google-chrome-stable": "/usr/bin/google-chrome-stable"}), "/usr/bin/google-chrome-stable", nil},
		{"windows program files", fakeLocator("windows", map[string]string{"ProgramFiles": "C:/PF", "LocalAppData": "C:/LA"},
			[]string{"C:/LA/Chromium/Application/chrome.exe", "C:/PF/Google/Chrome/Application/chrome.exe"}, nil),
			"C:/PF/Google/Chrome/Application/chrome.exe", nil},
		{"windows PATH", fakeLocator("windows", nil, nil, map[string]string{"chrome": `C:\bin\chrome.exe`}), `C:\bin\chrome.exe`, nil},
		{"not found", fakeLocator("linux", nil, nil, nil), "", ErrNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.loc.find()
			if !errors.Is(err, tc.wantErr) || (tc.wantErr == nil && err != nil) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if slashed(got) != slashed(tc.want) {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFindChrome_EnvOverride(t *testing.T) {
	t.Setenv(EnvChrome, "off")
	if _, err := FindChrome(); !errors.Is(err, ErrDisabled) {
		t.Errorf("err = %v, want ErrDisabled", err)
	}
	t.Setenv(EnvChrome, "/definitely/not/a/chrome")
	if _, err := FindChrome(); err == nil || !strings.Contains(err.Error(), EnvChrome) {
		t.Errorf("err = %v, want one naming %s", err, EnvChrome)
	}
}
