package chromium

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// LocalStateFile is the name of the JSON file, at the root of a user data
// directory, that lists the browser's profiles.
const LocalStateFile = "Local State"

// defaultProfileDir is the folder Chromium treats as last used when
// profile.last_used is absent.
const defaultProfileDir = "Default"

var (
	// ErrProfileNotFound reports that no profile matched the query.
	ErrProfileNotFound = errors.New("profile not found")
	// ErrAmbiguousProfile reports a display name shared by several profiles.
	ErrAmbiguousProfile = errors.New("ambiguous profile name")
	// ErrProfileDirMissing reports a profile listed in Local State whose
	// directory does not exist on disk.
	ErrProfileDirMissing = errors.New("profile directory missing")
	// ErrMalformedLocalState reports a Local State file that is not valid JSON.
	ErrMalformedLocalState = errors.New("malformed Local State")
)

// Profile is one browser profile listed in Local State.
type Profile struct {
	Browser Browser
	// DirName is the profile folder name, e.g. "Default" or "Profile 1".
	DirName string
	// Name is the display name shown in the browser's profile picker.
	Name string
	// Dir is the absolute profile directory (user data dir + DirName).
	Dir string
	// LastUsed marks the profile the browser opens by default.
	LastUsed bool
	// Missing marks a profile listed in Local State whose Dir is absent.
	Missing bool
}

// localState is the subset of Chromium's Local State file read here.
type localState struct {
	Profile struct {
		InfoCache map[string]struct {
			Name string `json:"name"`
		} `json:"info_cache"`
		LastUsed      string   `json:"last_used"`
		ProfilesOrder []string `json:"profiles_order"`
	} `json:"profile"`
}

// Profiles lists b's profiles from its Local State file, in the browser's
// profile-picker order (profiles_order), then by folder name.
//
// Profiles whose directory is absent are returned with Missing set.
// A missing Local State file yields an error wrapping os.ErrNotExist.
func Profiles(b Browser, opts ...Option) ([]Profile, error) {
	base, err := UserDataDir(b, opts...)
	if err != nil {
		return nil, err
	}
	lsPath := filepath.Join(base, LocalStateFile)
	data, err := os.ReadFile(lsPath) // #nosec G304 -- path derived from browser user data dir
	if err != nil {
		return nil, fmt.Errorf("%s: read %s: %w", b, LocalStateFile, err)
	}
	var ls localState
	if err := json.Unmarshal(data, &ls); err != nil {
		return nil, fmt.Errorf("%s: %w: %s: %w", b, ErrMalformedLocalState, lsPath, err)
	}

	lastUsed := ls.Profile.LastUsed
	if lastUsed == "" {
		lastUsed = defaultProfileDir
	}

	rank := make(map[string]int, len(ls.Profile.ProfilesOrder))
	for i, d := range ls.Profile.ProfilesOrder {
		if _, seen := rank[d]; !seen {
			rank[d] = i
		}
	}

	out := make([]Profile, 0, len(ls.Profile.InfoCache))
	for dirName, info := range ls.Profile.InfoCache {
		if !validDirName(dirName) {
			continue
		}
		dir := filepath.Join(base, dirName)
		fi, statErr := os.Stat(dir)
		out = append(out, Profile{
			Browser:  b,
			DirName:  dirName,
			Name:     info.Name,
			Dir:      dir,
			LastUsed: dirName == lastUsed,
			Missing:  statErr != nil || !fi.IsDir(),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		ri, iok := rank[out[i].DirName]
		rj, jok := rank[out[j].DirName]
		switch {
		case iok && jok:
			return ri < rj
		case iok != jok:
			return iok
		default:
			return out[i].DirName < out[j].DirName
		}
	})
	return out, nil
}

// ResolveProfile finds b's profile named nameOrDir.
//
// An exact folder-name match ("Profile 1") wins; otherwise the display name
// is matched case-insensitively. A display name shared by several profiles
// is ErrAmbiguousProfile, no match is ErrProfileNotFound (both list the
// candidates), and a match whose directory is absent is
// ErrProfileDirMissing.
func ResolveProfile(b Browser, nameOrDir string, opts ...Option) (Profile, error) {
	ps, err := Profiles(b, opts...)
	if err != nil {
		return Profile{}, err
	}
	query := strings.TrimSpace(nameOrDir)

	var matches []Profile
	for _, p := range ps {
		if p.DirName == query {
			matches = []Profile{p}
			break
		}
		if query != "" && strings.EqualFold(p.Name, query) {
			matches = append(matches, p)
		}
	}

	switch len(matches) {
	case 0:
		return Profile{}, fmt.Errorf("%w: %s has no profile %q (available: %s)",
			ErrProfileNotFound, b, nameOrDir, describe(ps))
	case 1:
		p := matches[0]
		if p.Missing {
			return Profile{}, fmt.Errorf("%w: %s profile %q: %s",
				ErrProfileDirMissing, b, p.Name, p.Dir)
		}
		return p, nil
	default:
		return Profile{}, fmt.Errorf("%w: %s has %d profiles named %q (use a folder name: %s)",
			ErrAmbiguousProfile, b, len(matches), nameOrDir, describe(matches))
	}
}

// validDirName rejects info_cache keys that are not a single path element,
// so a crafted Local State cannot point outside the user data dir.
func validDirName(s string) bool {
	return s != "" && s != "." && s != ".." && !strings.ContainsAny(s, `/\`)
}

// describe renders profiles as `"Name" (DirName)`, comma separated.
func describe(ps []Profile) string {
	if len(ps) == 0 {
		return "none"
	}
	parts := make([]string, len(ps))
	for i, p := range ps {
		parts[i] = fmt.Sprintf("%q (%s)", p.Name, p.DirName)
	}
	return strings.Join(parts, ", ")
}
