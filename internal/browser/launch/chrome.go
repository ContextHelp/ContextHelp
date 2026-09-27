package launch

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// EnvChrome names the Chrome or Chromium executable to run headless, as a
// path or a command on PATH. "off", "0" or "false" disable headless runs.
const EnvChrome = "CTXT_CHROME"

var (
	// ErrNotFound means no Chrome or Chromium was found.
	ErrNotFound = errors.New("no Chrome or Chromium found; set " + EnvChrome + " to one")
	// ErrDisabled means EnvChrome turned headless runs off.
	ErrDisabled = errors.New("headless Chrome disabled by " + EnvChrome)
	// ErrReservedFlag means a caller passed a switch the package owns.
	ErrReservedFlag = errors.New("switch is set by package launch and cannot be overridden")
)

// mandatoryFlags go on every headless run, after any caller switch so
// that Chrome, which keeps the last value of a repeated switch, honours
// them even if a reserved name slipped through.
var mandatoryFlags = []string{
	"--headless=new",
	// Never touch the OS credential store: no macOS keychain prompt
	// ("Chrome Safe Storage"), no Linux keyring.
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

// userDataDirFlag carries the throwaway profile the package creates.
const userDataDirFlag = "--user-data-dir"

// FindChrome returns the Chrome or Chromium executable for headless runs:
// EnvChrome when set, else the first standard install location or PATH
// command that exists. It returns ErrDisabled or ErrNotFound otherwise.
func FindChrome() (string, error) {
	return defaultLocator().find()
}

// locator is FindChrome with its environment injected, for tests.
type locator struct {
	goos     string
	getenv   func(string) string
	exists   func(string) bool
	lookPath func(string) (string, error)
}

func defaultLocator() locator {
	return locator{
		goos:   runtime.GOOS,
		getenv: os.Getenv,
		exists: func(p string) bool {
			fi, err := os.Stat(p)
			return err == nil && !fi.IsDir()
		},
		lookPath: exec.LookPath,
	}
}

func (l locator) find() (string, error) {
	switch v := strings.TrimSpace(l.getenv(EnvChrome)); strings.ToLower(v) {
	case "off", "0", "false":
		return "", ErrDisabled
	case "":
	default:
		p, err := l.lookPath(v)
		if err != nil {
			return "", fmt.Errorf("%s=%q: %w", EnvChrome, v, err)
		}
		return p, nil
	}
	for _, p := range l.installPaths() {
		if l.exists(p) {
			return p, nil
		}
	}
	for _, name := range chromeCommands(l.goos) {
		if p, err := l.lookPath(name); err == nil {
			return p, nil
		}
	}
	return "", ErrNotFound
}

// GOOS values the platform tables switch on.
const (
	goosDarwin  = "darwin"
	goosLinux   = "linux"
	goosWindows = "windows"
)

// installPaths lists the standard install locations on l.goos.
func (l locator) installPaths() []string {
	switch l.goos {
	case goosDarwin:
		var out []string
		roots := []string{"/Applications"}
		if home := l.getenv("HOME"); home != "" {
			roots = append(roots, filepath.Join(home, "Applications"))
		}
		for _, root := range roots {
			out = append(out,
				root+"/Google Chrome.app/Contents/MacOS/Google Chrome",
				root+"/Chromium.app/Contents/MacOS/Chromium",
			)
		}
		return out
	case goosWindows:
		var out []string
		for _, env := range []string{"ProgramFiles", "ProgramFiles(x86)", "LocalAppData"} {
			root := l.getenv(env)
			if root == "" {
				continue
			}
			out = append(out,
				filepath.Join(root, "Google", "Chrome", "Application", "chrome.exe"),
				filepath.Join(root, "Chromium", "Application", "chrome.exe"),
			)
		}
		return out
	}
	return nil
}

// chromeCommands lists the command names Chrome and Chromium install
// under on PATH.
func chromeCommands(goos string) []string {
	if goos == goosWindows {
		return []string{"chrome", "chromium"}
	}
	return []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser"}
}

// needsNoSandbox reports whether Chrome must run without its sandbox:
// Chrome refuses it as root, and Linux CI runners may deny the
// unprivileged user namespaces it needs. Headless runs only render
// throwaway local pages in a throwaway profile.
func needsNoSandbox(euid int, goos string, ci bool) bool {
	return euid == 0 || (goos == goosLinux && ci)
}

// headlessArgs returns the switches for one headless run with profile as
// its user data directory: the caller's extra switches, then --no-sandbox
// where needed, then the mandatory ones and owned, then targets. An extra
// switch naming a mandatory or owned switch is ErrReservedFlag.
func headlessArgs(profile string, noSandbox bool, extra, owned, targets []string) ([]string, error) {
	if profile == "" {
		return nil, errors.New("launch: empty profile dir")
	}
	reserved := map[string]bool{switchName(userDataDirFlag): true}
	for _, f := range mandatoryFlags {
		reserved[switchName(f)] = true
	}
	for _, f := range owned {
		reserved[switchName(f)] = true
	}
	for _, a := range extra {
		if n := switchName(a); n != "" && reserved[n] {
			return nil, fmt.Errorf("%q: %w", a, ErrReservedFlag)
		}
	}
	argv := make([]string, 0, len(extra)+len(mandatoryFlags)+len(owned)+len(targets)+2)
	argv = append(argv, extra...)
	if noSandbox {
		argv = append(argv, "--no-sandbox")
	}
	argv = append(argv, mandatoryFlags...)
	argv = append(argv, userDataDirFlag+"="+profile)
	argv = append(argv, owned...)
	return append(argv, targets...), nil
}

// switchName returns the lower-cased name of a Chrome switch, which
// Chrome accepts with one or two leading dashes, and "" for anything
// else.
func switchName(arg string) string {
	if !strings.HasPrefix(arg, "-") {
		return ""
	}
	name, _, _ := strings.Cut(strings.TrimLeft(arg, "-"), "=")
	return strings.ToLower(name)
}
