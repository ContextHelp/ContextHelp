package chromium

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

var (
	// ErrNoBrowser reports that no browser was named and none could be
	// picked automatically.
	ErrNoBrowser = errors.New("no browser selected")
	// ErrNoProfile reports that no profile was named and the browser's
	// last-used profile is not unambiguous.
	ErrNoProfile = errors.New("no profile selected")
)

// NotInstalledError reports a selected browser whose user data
// directory holds no Local State file. It unwraps to fs.ErrNotExist.
type NotInstalledError struct {
	Err         error
	Browser     Browser
	UserDataDir string
}

func (e *NotInstalledError) Error() string {
	return fmt.Sprintf("%s: no %q in %s; is %s installed?", e.Browser, LocalStateFile, e.UserDataDir, e.Browser)
}

func (e *NotInstalledError) Unwrap() error { return e.Err }

// noneListed stands in for an empty candidate list in messages.
const noneListed = "none"

// Source says how a browser or profile was chosen.
type Source string

// Selection sources, in resolution order.
const (
	// SourceFlag: named on the command line.
	SourceFlag Source = "flag"
	// SourceConfig: capture.browser in the config.
	SourceConfig Source = "config"
	// SourceProfileMatch: the only installed browser holding the named
	// profile.
	SourceProfileMatch Source = "profile_match"
	// SourceOSDefault: the OS default browser.
	SourceOSDefault Source = "os_default"
	// SourceLastUsed: the browser's last-used profile.
	SourceLastUsed Source = "last_used"
	// SourceOnlyProfile: the browser's only profile.
	SourceOnlyProfile Source = "only_profile"
)

// Request is what the caller was told: an explicit browser (a flag), a
// configured browser, and a profile name or folder. Any may be empty.
type Request struct {
	Browser       string
	ConfigBrowser string
	Profile       string
}

// Selection is the profile Select picked and how.
type Selection struct {
	Profile       Profile
	BrowserSource Source
	ProfileSource Source
}

// Installation is one browser found on disk: its user data directory
// holds a Local State file.
type Installation struct {
	// Err is set when Local State exists but cannot be read.
	Err         error
	Browser     Browser
	UserDataDir string
	Profiles    []Profile
}

// Selector picks a browser profile when the caller does not name both a
// browser and a profile. The zero value inspects the real host; tests
// set the hooks.
type Selector struct {
	// UserDataDir resolves a browser's user data directory. Nil means
	// the package-level UserDataDir.
	UserDataDir func(Browser) (string, error)
	// DefaultBrowser reports the OS default browser. Nil means
	// DetectDefaultBrowser.
	DefaultBrowser func() (DefaultBrowser, error)
}

func (s Selector) userDataDir(b Browser) (string, error) {
	if s.UserDataDir != nil {
		return s.UserDataDir(b)
	}
	return UserDataDir(b)
}

// Default reports the OS default browser through s.DefaultBrowser.
func (s Selector) Default() (DefaultBrowser, error) {
	if s.DefaultBrowser != nil {
		return s.DefaultBrowser()
	}
	return DetectDefaultBrowser()
}

// Installed lists the supported browsers whose user data directory
// holds a Local State file, in Browsers() order.
func (s Selector) Installed() []Installation {
	out := make([]Installation, 0, len(Browsers()))
	for _, b := range Browsers() {
		dir, err := s.userDataDir(b)
		if err != nil {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, LocalStateFile)); errors.Is(err, fs.ErrNotExist) {
			continue
		}
		in := Installation{Browser: b, UserDataDir: dir}
		in.Profiles, in.Err = Profiles(b, WithUserDataDir(dir))
		out = append(out, in)
	}
	return out
}

// Select resolves req to one profile. First hit wins:
//
//  1. req.Browser;
//  2. req.ConfigBrowser;
//  3. with only req.Profile set: the one installed browser holding a
//     profile of that display name or folder (several is
//     ErrAmbiguousProfile, none ErrProfileNotFound);
//  4. the OS default browser, when it is a supported browser and
//     installed;
//  5. otherwise ErrNoBrowser, listing the installed browsers.
//
// With req.Profile empty the browser's last-used profile is taken, or
// its only profile; anything else is ErrNoProfile.
func (s Selector) Select(req Request) (Selection, error) {
	query := strings.TrimSpace(req.Profile)

	var (
		b   Browser
		src Source
		err error
	)
	switch {
	case strings.TrimSpace(req.Browser) != "":
		if b, err = ParseBrowser(req.Browser); err != nil {
			return Selection{}, err
		}
		src = SourceFlag
	case strings.TrimSpace(req.ConfigBrowser) != "":
		if b, err = ParseBrowser(req.ConfigBrowser); err != nil {
			return Selection{}, fmt.Errorf("capture.browser: %w", err)
		}
		src = SourceConfig
	case query != "":
		return s.selectByProfile(query)
	default:
		if b, err = s.osDefault(); err != nil {
			return Selection{}, err
		}
		src = SourceOSDefault
	}

	dir, err := s.userDataDir(b)
	if err != nil {
		return Selection{}, err
	}
	opt := WithUserDataDir(dir)
	notInstalled := func(err error) error {
		if errors.Is(err, fs.ErrNotExist) {
			return &NotInstalledError{Err: err, Browser: b, UserDataDir: dir}
		}
		return err
	}
	if query != "" {
		p, err := ResolveProfile(b, query, opt)
		if err != nil {
			return Selection{}, notInstalled(err)
		}
		return Selection{Profile: p, BrowserSource: src, ProfileSource: SourceFlag}, nil
	}
	p, psrc, err := pickProfile(b, opt)
	if err != nil {
		return Selection{}, notInstalled(err)
	}
	return Selection{Profile: p, BrowserSource: src, ProfileSource: psrc}, nil
}

// osDefault returns the OS default browser when it is supported and
// installed, else ErrNoBrowser explaining why.
func (s Selector) osDefault() (Browser, error) {
	installed := s.Installed()
	names := make([]string, 0, len(installed))
	for _, in := range installed {
		names = append(names, string(in.Browser))
	}
	list := strings.Join(names, ", ")
	if list == "" {
		list = noneListed
	}

	def, derr := s.Default()
	var why string
	switch {
	case derr != nil:
		why = fmt.Sprintf("OS default browser unknown: %v", derr)
	case def.ID == "":
		why = "no OS default browser set"
	case def.Browser == "":
		why = fmt.Sprintf("OS default %s is not a supported Chromium-family browser", def.ID)
	default:
		for _, in := range installed {
			if in.Browser == def.Browser {
				return def.Browser, nil
			}
		}
		why = fmt.Sprintf("OS default %s has no %s in its user data directory", def.Browser, LocalStateFile)
	}
	return "", fmt.Errorf("%w (installed: %s; %s)", ErrNoBrowser, list, why)
}

// selectByProfile finds the one installed browser holding query.
func (s Selector) selectByProfile(query string) (Selection, error) {
	installed := s.Installed()
	var all, matches []Profile
	for _, in := range installed {
		if in.Err != nil {
			continue
		}
		all = append(all, in.Profiles...)
		matches = append(matches, matchProfiles(in.Profiles, query)...)
	}
	switch len(matches) {
	case 0:
		if len(installed) == 0 {
			return Selection{}, fmt.Errorf("%w (installed: %s)", ErrNoBrowser, noneListed)
		}
		return Selection{}, fmt.Errorf("%w: no installed browser has profile %q (available: %s)",
			ErrProfileNotFound, query, describeQualified(all))
	case 1:
		m := matches[0]
		dir, err := s.userDataDir(m.Browser)
		if err != nil {
			return Selection{}, err
		}
		// Re-resolve by folder so a missing directory reports as it
		// does for an explicit browser.
		p, err := ResolveProfile(m.Browser, m.DirName, WithUserDataDir(dir))
		if err != nil {
			return Selection{}, err
		}
		return Selection{Profile: p, BrowserSource: SourceProfileMatch, ProfileSource: SourceFlag}, nil
	default:
		return Selection{}, fmt.Errorf("%w: %q matches %d profiles across installed browsers (%s)",
			ErrAmbiguousProfile, query, len(matches), describeQualified(matches))
	}
}

// matchProfiles applies ResolveProfile's rule to one browser's
// profiles: an exact folder name wins, else display names match
// case-insensitively.
func matchProfiles(ps []Profile, query string) []Profile {
	var out []Profile
	for _, p := range ps {
		if p.DirName == query {
			return []Profile{p}
		}
		if strings.EqualFold(p.Name, query) {
			out = append(out, p)
		}
	}
	return out
}

// pickProfile returns b's last-used profile, or its only profile, when
// that is unambiguous.
func pickProfile(b Browser, opts ...Option) (Profile, Source, error) {
	ps, err := Profiles(b, opts...)
	if err != nil {
		return Profile{}, "", err
	}
	present := make([]Profile, 0, len(ps))
	for _, p := range ps {
		if p.Missing {
			continue
		}
		if p.LastUsed {
			return p, SourceLastUsed, nil
		}
		present = append(present, p)
	}
	if len(present) == 1 {
		return present[0], SourceOnlyProfile, nil
	}
	return Profile{}, "", fmt.Errorf("%w: %s has no unambiguous last-used profile (available: %s)",
		ErrNoProfile, b, describe(ps))
}

// describeQualified renders profiles as `browser:"Name" (DirName)`.
func describeQualified(ps []Profile) string {
	if len(ps) == 0 {
		return noneListed
	}
	parts := make([]string, len(ps))
	for i, p := range ps {
		parts[i] = fmt.Sprintf("%s:%q (%s)", p.Browser, p.Name, p.DirName)
	}
	return strings.Join(parts, ", ")
}
