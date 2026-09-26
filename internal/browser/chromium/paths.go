package chromium

import (
	"errors"
	"fmt"
	"os"
	"path"
	"runtime"
	"strings"
)

// Browser identifies a Chromium-family browser.
type Browser string

// Supported browsers.
const (
	Chrome   Browser = "chrome"
	Brave    Browser = "brave"
	Edge     Browser = "edge"
	Arc      Browser = "arc"
	Chromium Browser = "chromium"
	Vivaldi  Browser = "vivaldi"
)

var (
	// ErrUnknownBrowser reports a browser name this package does not know.
	ErrUnknownBrowser = errors.New("unknown browser")
	// ErrUnsupported reports a browser with no known default location on
	// the given OS (e.g. Arc on linux).
	ErrUnsupported = errors.New("browser not supported on this OS")
	// ErrNoHome reports that the user's home directory could not be found.
	ErrNoHome = errors.New("home directory unknown")
)

// userDataPaths holds a browser's default user data directory per OS,
// relative to the OS-specific root:
//
//	darwin:  ~/Library/Application Support
//	linux:   $CHROME_CONFIG_HOME (chrome, chromium only), else
//	         $XDG_CONFIG_HOME, else ~/.config
//	windows: %LOCALAPPDATA%
//
// An empty entry means the browser has no known location on that OS.
type userDataPaths struct {
	darwin, linux, windows string
	// chromeConfigHome: honours $CHROME_CONFIG_HOME on linux.
	chromeConfigHome bool
}

// Sources:
//   - chrome, chromium: https://chromium.googlesource.com/chromium/src/+/HEAD/docs/user_data_dir.md
//   - vivaldi: https://help.vivaldi.com/desktop/privacy/preventing-vivaldi-profiles-from-being-uploaded-to-git-repositories/
//   - brave, edge, arc: vendor defaults; each browser's chrome://version page
//     shows the active "Profile Path" for confirmation.
//
// Arc ships on Windows as a packaged (MSIX) app whose data lives under a
// package-virtualised path; it is left unsupported there until verified.
var browserPaths = map[Browser]userDataPaths{
	Chrome: {
		darwin:           "Google/Chrome",
		linux:            "google-chrome",
		windows:          "Google/Chrome/User Data",
		chromeConfigHome: true,
	},
	Chromium: {
		darwin:           "Chromium",
		linux:            "chromium",
		windows:          "Chromium/User Data",
		chromeConfigHome: true,
	},
	Brave: {
		darwin:  "BraveSoftware/Brave-Browser",
		linux:   "BraveSoftware/Brave-Browser",
		windows: "BraveSoftware/Brave-Browser/User Data",
	},
	Edge: {
		darwin:  "Microsoft Edge",
		linux:   "microsoft-edge",
		windows: "Microsoft/Edge/User Data",
	},
	Arc: {
		darwin: "Arc/User Data",
	},
	Vivaldi: {
		darwin:  "Vivaldi",
		linux:   "vivaldi",
		windows: "Vivaldi/User Data",
	},
}

// Browsers returns the supported browsers in a stable order.
func Browsers() []Browser {
	return []Browser{Chrome, Brave, Edge, Arc, Chromium, Vivaldi}
}

// ParseBrowser converts a user-supplied name (case-insensitive) to a Browser.
func ParseBrowser(s string) (Browser, error) {
	b := Browser(strings.ToLower(strings.TrimSpace(s)))
	if _, ok := browserPaths[b]; !ok {
		return "", unknownBrowser(b)
	}
	return b, nil
}

// Option configures path and profile lookups.
type Option func(*options)

type options struct {
	userDataDir string
}

// WithUserDataDir uses dir as the browser's user data directory instead of
// the environment override or the OS default.
func WithUserDataDir(dir string) Option {
	return func(o *options) { o.userDataDir = dir }
}

func applyOptions(opts []Option) options {
	var o options
	for _, fn := range opts {
		fn(&o)
	}
	return o
}

// EnvUserDataDir returns the environment variable that overrides b's user
// data directory, e.g. CTXT_BRAVE_USER_DATA_DIR.
func EnvUserDataDir(b Browser) string {
	return "CTXT_" + strings.ToUpper(string(b)) + "_USER_DATA_DIR"
}

// UserDataDir returns the user data directory for b.
//
// Resolution: WithUserDataDir > $CTXT_<BROWSER>_USER_DATA_DIR > the vendor
// default for runtime.GOOS. The directory is not required to exist.
func UserDataDir(b Browser, opts ...Option) (string, error) {
	if _, ok := browserPaths[b]; !ok {
		return "", unknownBrowser(b)
	}
	if o := applyOptions(opts); o.userDataDir != "" {
		return o.userDataDir, nil
	}
	if v := os.Getenv(EnvUserDataDir(b)); v != "" {
		return v, nil
	}
	home, _ := os.UserHomeDir()
	return defaultUserDataDir(b, runtime.GOOS, os.Getenv, home)
}

// defaultUserDataDir computes the vendor default for b on goos. It is pure
// so every OS can be tested from any host.
func defaultUserDataDir(b Browser, goos string, getenv func(string) string, home string) (string, error) {
	p, ok := browserPaths[b]
	if !ok {
		return "", unknownBrowser(b)
	}

	var root, rel string
	needHome := false
	switch goos {
	case "darwin":
		rel = p.darwin
		root, needHome = joinFor(goos, home, "Library", "Application Support"), true
	case "linux":
		rel = p.linux
		switch {
		case p.chromeConfigHome && getenv("CHROME_CONFIG_HOME") != "":
			root = getenv("CHROME_CONFIG_HOME")
		case getenv("XDG_CONFIG_HOME") != "":
			root = getenv("XDG_CONFIG_HOME")
		default:
			root, needHome = joinFor(goos, home, ".config"), true
		}
	case "windows":
		rel = p.windows
		root = getenv("LOCALAPPDATA")
		if root == "" {
			root, needHome = joinFor(goos, home, "AppData", "Local"), true
		}
	}
	if rel == "" {
		return "", fmt.Errorf("%w: %s on %s", ErrUnsupported, b, goos)
	}
	if needHome && home == "" {
		return "", fmt.Errorf("%s user data dir: %w", b, ErrNoHome)
	}
	return joinFor(goos, root, strings.Split(rel, "/")...), nil
}

// joinFor joins path elements with goos's separator, independent of the
// host running the code. Windows elements are joined verbatim (no
// cleaning) so drive and UNC prefixes survive.
func joinFor(goos, root string, elem ...string) string {
	if goos != "windows" {
		return path.Join(append([]string{root}, elem...)...)
	}
	out := strings.TrimRight(root, `\/`)
	for _, e := range elem {
		out += `\` + e
	}
	return out
}

func unknownBrowser(b Browser) error {
	names := make([]string, 0, len(browserPaths))
	for _, x := range Browsers() {
		names = append(names, string(x))
	}
	return fmt.Errorf("%w %q (supported: %s)", ErrUnknownBrowser, b, strings.Join(names, ", "))
}
