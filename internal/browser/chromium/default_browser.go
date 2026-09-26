package chromium

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// GOOS values default-browser lookup handles.
const (
	goosDarwin  = "darwin"
	goosLinux   = "linux"
	goosWindows = "windows"
)

// ErrNoDefaultBrowserLookup reports an OS where the default browser is
// not looked up.
var ErrNoDefaultBrowserLookup = errors.New("default browser lookup not supported on this OS")

// DefaultBrowser is the OS's default web browser: the handler
// registered for https URLs.
type DefaultBrowser struct {
	// ID is the handler as the OS names it: a bundle id on macOS
	// ("com.brave.browser"), a .desktop file on Linux
	// ("brave-browser.desktop"), a ProgId on Windows ("BraveHTML").
	// Empty when the user never chose one (the system browser).
	ID string
	// Browser is the supported Chromium-family browser ID names, or ""
	// when ID is another browser (Safari, Firefox) or a channel this
	// package does not read (Beta, Canary, Dev).
	Browser Browser
}

// Handler ids per OS. Only stable channels are listed: Beta, Dev and
// Canary builds keep their profiles in other user data directories.
//
// Sources:
//   - macOS bundle ids (CFBundleIdentifier): com.google.Chrome and
//     com.brave.Browser read from the installed apps' Info.plist;
//     org.chromium.Chromium from chrome/app/theme/chromium/BRANDING
//     (MAC_BUNDLE_ID); company.thebrowser.Browser from Arc's group
//     policy guide; com.microsoft.edgemac and com.vivaldi.Vivaldi from
//     the vendors' published app ids. LaunchServices stores them
//     lowercased, so matching ignores case.
//   - Linux .desktop names: the vendors' Linux packages.
//   - Windows ProgId prefixes: browser_prog_id_prefix in
//     chrome/install_static/{google_chrome,chromium}_install_modes.h
//     (ChromeHTML, ChromiumHTM) and brave-core's copy (BraveHTML); Edge
//     MSEdgeHTM; Vivaldi VivaldiHTM. Per-user installs append
//     ".<suffix>", which is ignored.
var (
	darwinHandlers = map[string]Browser{
		"com.google.chrome":          Chrome,
		"com.brave.browser":          Brave,
		"com.microsoft.edgemac":      Edge,
		"company.thebrowser.browser": Arc,
		"org.chromium.chromium":      Chromium,
		"com.vivaldi.vivaldi":        Vivaldi,
	}
	linuxHandlers = map[string]Browser{
		"google-chrome.desktop":    Chrome,
		"brave-browser.desktop":    Brave,
		"microsoft-edge.desktop":   Edge,
		"chromium.desktop":         Chromium,
		"chromium-browser.desktop": Chromium,
		"vivaldi-stable.desktop":   Vivaldi,
	}
	windowsHandlers = map[string]Browser{
		"chromehtml":  Chrome,
		"bravehtml":   Brave,
		"msedgehtm":   Edge,
		"chromiumhtm": Chromium,
		"vivaldihtm":  Vivaldi,
	}
)

// BrowserForHandler maps a default-browser handler id reported on goos
// to a supported browser, or "" when it names none.
func BrowserForHandler(goos, id string) Browser {
	id = strings.ToLower(strings.TrimSpace(id))
	switch goos {
	case goosDarwin:
		return darwinHandlers[id]
	case goosLinux:
		return linuxHandlers[id]
	case goosWindows:
		prefix, _, _ := strings.Cut(id, ".")
		return windowsHandlers[prefix]
	}
	return ""
}

// DetectDefaultBrowser asks the host OS for its default web browser.
//
//   - macOS: the https handler in LaunchServices' secure preferences,
//     read with plutil (shipped with every macOS).
//   - Linux: `xdg-settings get default-web-browser`.
//   - Windows: the https UserChoiceLatest (Windows 11 25H2+), then
//     UserChoice, ProgId under HKCU, read with reg.exe.
//
// A zero DefaultBrowser with a nil error means no browser was chosen.
func DetectDefaultBrowser() (DefaultBrowser, error) {
	home, _ := os.UserHomeDir()
	return detectDefaultBrowser(defaultBrowserHost{goos: runtime.GOOS, home: home, run: runCommand})
}

// defaultBrowserHost is everything detection touches on the host, so
// tests never read the real preferences or registry.
type defaultBrowserHost struct {
	run  func(name string, args ...string) ([]byte, error)
	goos string
	home string
}

func runCommand(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).Output() // #nosec G204 -- fixed tool names; args are constants or paths under $HOME
}

// LaunchServicesPrefs is the macOS file, relative to $HOME, holding the
// user's URL-scheme handlers.
const LaunchServicesPrefs = "Library/Preferences/com.apple.LaunchServices/com.apple.launchservices.secure.plist"

// windowsURLAssociations is the HKCU key holding per-scheme choices.
const windowsURLAssociations = `HKCU\Software\Microsoft\Windows\Shell\Associations\UrlAssociations\https\`

func detectDefaultBrowser(h defaultBrowserHost) (DefaultBrowser, error) {
	var id string
	switch h.goos {
	case goosDarwin:
		if h.home == "" {
			return DefaultBrowser{}, fmt.Errorf("default browser: %w", ErrNoHome)
		}
		plist := filepath.Join(h.home, filepath.FromSlash(LaunchServicesPrefs))
		if _, err := os.Stat(plist); errors.Is(err, fs.ErrNotExist) {
			return DefaultBrowser{}, nil
		}
		// plutil turns the binary plist into JSON the standard library
		// reads; no plist dependency for one lookup.
		out, err := h.run("plutil", "-convert", "json", "-o", "-", plist)
		if err != nil {
			return DefaultBrowser{}, fmt.Errorf("default browser: plutil %s: %w", plist, err)
		}
		if id, err = httpsHandlerFromLaunchServices(out); err != nil {
			return DefaultBrowser{}, fmt.Errorf("default browser: %s: %w", plist, err)
		}
	case goosLinux:
		out, err := h.run("xdg-settings", "get", "default-web-browser")
		if err != nil {
			return DefaultBrowser{}, fmt.Errorf("default browser: xdg-settings: %w", err)
		}
		id = strings.TrimSpace(string(out))
	case goosWindows:
		for _, key := range []string{"UserChoiceLatest", "UserChoice"} {
			out, err := h.run("reg", "query", windowsURLAssociations+key, "/v", "ProgId")
			if err != nil {
				continue
			}
			if id = progIDFromRegQuery(string(out)); id != "" {
				break
			}
		}
	default:
		return DefaultBrowser{}, fmt.Errorf("%w: %s", ErrNoDefaultBrowserLookup, h.goos)
	}
	return DefaultBrowser{ID: id, Browser: BrowserForHandler(h.goos, id)}, nil
}

// httpsHandlerFromLaunchServices returns the LSHandlerRoleAll bundle id
// registered for the https scheme, or "" when there is none.
func httpsHandlerFromLaunchServices(data []byte) (string, error) {
	var prefs struct {
		LSHandlers []struct {
			Scheme  string `json:"LSHandlerURLScheme"`
			RoleAll string `json:"LSHandlerRoleAll"`
		} `json:"LSHandlers"`
	}
	if err := json.Unmarshal(data, &prefs); err != nil {
		return "", err
	}
	for _, h := range prefs.LSHandlers {
		if strings.EqualFold(h.Scheme, "https") && h.RoleAll != "" {
			return h.RoleAll, nil
		}
	}
	return "", nil
}

// progIDFromRegQuery extracts the ProgId value from `reg query ... /v
// ProgId` output ("    ProgId    REG_SZ    BraveHTML").
func progIDFromRegQuery(out string) string {
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) >= 3 && strings.EqualFold(f[0], "ProgId") && f[1] == "REG_SZ" {
			return f[2]
		}
	}
	return ""
}
