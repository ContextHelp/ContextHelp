package pipeline

import (
	"strconv"
	"strings"
)

// ParseVersionedName splits a pipeline name with optional version suffix.
//
//	"text.short@v1" → ("text.short", 1, true)
//	"text.short"    → ("text.short", 0, true)  // unversioned implicitly @v0
//	"text.short@v"  → ("text.short@v", 0, false)
//	"text.short@vfoo" → ("text.short@vfoo", 0, false)
//
// Per ADR-070 §2 the value-format convention is "<name>@vN". Pre-versioning
// rows that lack a "@" parse as @v0 so the registry can resolve them via the
// same code path as explicitly versioned entries. Truly malformed values
// (non-integer N, empty N, missing "v") return ok=false so callers can choose
// to log and skip rather than silently treat them as v0.
func ParseVersionedName(s string) (name string, version int, ok bool) {
	idx := strings.LastIndex(s, "@")
	if idx == -1 {
		return s, 0, true
	}
	suffix := s[idx+1:]
	if !strings.HasPrefix(suffix, "v") {
		return s, 0, false
	}
	digits := suffix[1:]
	if digits == "" {
		return s, 0, false
	}
	v, err := strconv.Atoi(digits)
	if err != nil || v < 0 {
		return s, 0, false
	}
	return s[:idx], v, true
}

// FormatVersionedName returns "<name>@vN".
func FormatVersionedName(name string, version int) string {
	return name + "@v" + strconv.Itoa(version)
}
