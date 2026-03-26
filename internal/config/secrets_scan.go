package config

import (
	"reflect"
	"regexp"
	"strings"
)

// secretPatterns are value prefixes that indicate a plaintext secret.
var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`^sk-ant-`),
	regexp.MustCompile(`^sk-`),
	regexp.MustCompile(`^eyJ`),
	regexp.MustCompile(`^ghp_`),
}

// secretFieldNames are substrings that, when found in a field name, flag the value.
var secretFieldNames = []string{"token", "password", "secret", "key"}

// envRefPattern matches env-var references that should be skipped.
var envRefPattern = regexp.MustCompile(`^\$\{[^}]+\}$|^\$[A-Z_][A-Z0-9_]*$`)

// SecretWarning describes a potentially exposed plaintext secret.
type SecretWarning struct {
	// Field is the dot-separated config path (e.g. "storage.blob.s3.secret_key").
	Field string
	// Hint is the first 4 chars of the value followed by "****".
	Hint string
	// Reason explains why this field was flagged.
	Reason string
}

// ScanSecrets walks cfg via reflection and returns warnings for any string
// fields that appear to contain plaintext secrets.
func ScanSecrets(cfg *Config) []SecretWarning {
	var warnings []SecretWarning
	walkStruct(reflect.ValueOf(cfg), "", &warnings)
	return warnings
}

// walkStruct recursively walks a struct value, collecting secret warnings.
func walkStruct(v reflect.Value, path string, warnings *[]SecretWarning) {
	// Dereference pointers.
	for v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return
		}
		v = v.Elem()
	}

	switch v.Kind() {
	case reflect.Struct:
		t := v.Type()
		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			fieldVal := v.Field(i)

			// Determine field path from mapstructure tag, fallback to lowercase name.
			name := fieldNameFromTag(field)
			childPath := name
			if path != "" {
				childPath = path + "." + name
			}

			walkStruct(fieldVal, childPath, warnings)
		}

	case reflect.String:
		val := v.String()
		if val == "" {
			return
		}
		// Skip env-var references.
		if envRefPattern.MatchString(val) {
			return
		}
		reason := secretReason(path, val)
		if reason != "" {
			*warnings = append(*warnings, SecretWarning{
				Field:  path,
				Hint:   sanitise(val),
				Reason: reason,
			})
		}

	case reflect.Map:
		for _, key := range v.MapKeys() {
			keyStr := key.String()
			childPath := keyStr
			if path != "" {
				childPath = path + "." + keyStr
			}
			walkStruct(v.MapIndex(key), childPath, warnings)
		}

	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			walkStruct(v.Index(i), path, warnings)
		}

	case reflect.Interface:
		if !v.IsNil() {
			walkStruct(v.Elem(), path, warnings)
		}
	}
}

// fieldNameFromTag extracts the mapstructure tag name, or falls back to
// lowercased struct field name.
func fieldNameFromTag(f reflect.StructField) string {
	tag := f.Tag.Get("mapstructure")
	if tag == "" {
		return strings.ToLower(f.Name)
	}
	parts := strings.SplitN(tag, ",", 2)
	if parts[0] == "" || parts[0] == "-" {
		return strings.ToLower(f.Name)
	}
	return parts[0]
}

// secretReason returns the reason string if the field looks like a secret,
// or empty string if it's safe.
func secretReason(fieldPath, value string) string {
	// Check value against known secret prefixes.
	for _, re := range secretPatterns {
		if re.MatchString(value) {
			return "value matches known secret pattern"
		}
	}

	// Check field name for secret-looking substrings.
	lower := strings.ToLower(fieldPath)
	for _, keyword := range secretFieldNames {
		if strings.Contains(lower, keyword) {
			// Only flag if it looks non-trivial (>8 chars, not a path/backend name).
			if len(value) > 8 && !looksLikeFilePath(value) && !looksLikeBackendName(value) {
				return "field name suggests secret (" + keyword + ")"
			}
		}
	}

	return ""
}

// sanitise returns the first 4 chars of s followed by "****".
func sanitise(s string) string {
	if len(s) <= 4 {
		return "****"
	}
	return s[:4] + "****"
}

// looksLikeFilePath returns true for values that look like filesystem paths.
func looksLikeFilePath(s string) bool {
	return strings.HasPrefix(s, "/") || strings.HasPrefix(s, "~/") ||
		strings.HasPrefix(s, "./") || strings.Contains(s, string([]byte{0x2f}))
}

// looksLikeBackendName returns true for short identifiers used as backend names.
func looksLikeBackendName(s string) bool {
	// These are known safe non-secret values that happen to appear in "key"-named fields.
	knownSafe := []string{"env", "keychain", "age-file", "1password", "gh-secrets",
		"sqlite", "postgres", "local", "s3", "auto", "ctxt"}
	lower := strings.ToLower(s)
	for _, safe := range knownSafe {
		if lower == safe {
			return true
		}
	}
	return false
}
