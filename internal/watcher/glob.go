package watcher

import (
	"path/filepath"
	"strings"
)

// MatchGlob reports whether path matches the given glob pattern.
// "**" matches any number of path segments (including zero).
func MatchGlob(pattern, path string) bool {
	// Normalise separators to forward slash.
	path = filepath.ToSlash(path)
	pattern = filepath.ToSlash(pattern)

	// Fast-path: no double-star, delegate directly.
	if !strings.Contains(pattern, "**") {
		ok, _ := filepath.Match(pattern, path)
		return ok
	}

	// Split pattern into segments separated by "/".
	// Treat "**" as a special wildcard that can match zero or more segments.
	patParts := strings.Split(pattern, "/")
	pathParts := strings.Split(path, "/")

	return matchSegments(patParts, pathParts)
}

// matchSegments implements double-star matching segment by segment.
func matchSegments(pat, path []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			// Remove consecutive "**" segments.
			for len(pat) > 0 && pat[0] == "**" {
				pat = pat[1:]
			}
			// "**" at end: matches everything remaining.
			if len(pat) == 0 {
				return true
			}
			// "**" in middle: try consuming 0 to N path segments.
			for i := 0; i <= len(path); i++ {
				if matchSegments(pat, path[i:]) {
					return true
				}
			}
			return false
		}

		if len(path) == 0 {
			return false
		}

		// Match single segment using filepath.Match (handles *, ?, ranges).
		ok, _ := filepath.Match(pat[0], path[0])
		if !ok {
			return false
		}
		pat = pat[1:]
		path = path[1:]
	}
	return len(path) == 0
}

// MatchAny reports whether path matches any of the given glob patterns.
// If patterns is empty, it returns true (match all).
func MatchAny(patterns []string, path string) bool {
	if len(patterns) == 0 {
		return true
	}
	for _, p := range patterns {
		if MatchGlob(p, path) {
			return true
		}
	}
	return false
}
