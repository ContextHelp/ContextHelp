package config

import (
	"fmt"
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

// secretFieldNames are substrings that, when found in a field name, flag the
// value. Redaction reuses the list: connection-string parameters matching
// it are masked, and every Config string field whose name matches must
// carry a `secret` tag (see redact.go).
var secretFieldNames = []string{"token", "password", "secret", "key"}

// shellEnvRef is a shell-style ${VAR} or ${VAR:-default} reference
// anywhere in a value. GitHub Actions ${{ ... }} and CloudFormation
// ${AWS::Region} do not match.
var shellEnvRef = regexp.MustCompile(`\$\{[A-Za-z_][A-Za-z0-9_]*(:?[-?+=][^}]*)?\}`)

// bareEnvRef is a whole value that is a bare $VAR reference.
var bareEnvRef = regexp.MustCompile(`^\$[A-Z_][A-Z0-9_]*$`)

// isEnvRef reports whether a config value is written as an env-var
// reference. Load never expands one, so the value is used literally;
// Validate rejects it with envRefMessage.
func isEnvRef(val string) bool {
	return shellEnvRef.MatchString(val) || bareEnvRef.MatchString(val)
}

// SecretWarning describes a potentially exposed plaintext secret.
type SecretWarning struct {
	// Field is the dot-separated config path (e.g. "storage.blob.s3.secret_key").
	Field string
	// Hint is the value as RedactSecret masks it.
	Hint string
	// Reason explains why this field was flagged.
	Reason string
}

// ScanSecrets walks cfg via reflection and returns warnings for any string
// fields that appear to contain plaintext secrets.
//
// Env-var references are skipped: they are not plaintext secrets, and
// Validate already rejects them because config never expands them.
func ScanSecrets(cfg *Config) []SecretWarning {
	var warnings []SecretWarning
	walkStrings(reflect.ValueOf(cfg), "", func(path, val string) {
		if isEnvRef(val) {
			return
		}
		if reason := secretReason(path, val); reason != "" {
			warnings = append(warnings, SecretWarning{
				Field:  path,
				Hint:   RedactSecret(val),
				Reason: reason,
			})
		}
	})
	return warnings
}

// walkStrings recursively walks v and calls fn with the dot-separated
// config path of every non-empty string value. Slice elements carry
// their index: server.urls[0].token.
func walkStrings(v reflect.Value, path string, fn func(path, val string)) {
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

			walkStrings(fieldVal, childPath, fn)
		}

	case reflect.String:
		if val := v.String(); val != "" {
			fn(path, val)
		}

	case reflect.Map:
		for _, key := range v.MapKeys() {
			keyStr := key.String()
			childPath := keyStr
			if path != "" {
				childPath = path + "." + keyStr
			}
			walkStrings(v.MapIndex(key), childPath, fn)
		}

	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			walkStrings(v.Index(i), fmt.Sprintf("%s[%d]", path, i), fn)
		}

	case reflect.Interface:
		if !v.IsNil() {
			walkStrings(v.Elem(), path, fn)
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
	if keyword := secretKeyword(fieldPath); keyword != "" {
		// Only flag if it looks non-trivial (>8 chars, not a path/backend name).
		if len(value) > 8 && !looksLikeFilePath(value) && !looksLikeBackendName(value) {
			return "field name suggests secret (" + keyword + ")"
		}
	}

	return ""
}

// looksLikeFilePath returns true for values that look like filesystem paths.
func looksLikeFilePath(s string) bool {
	return strings.HasPrefix(s, "/") || strings.HasPrefix(s, "~/") ||
		strings.HasPrefix(s, "./") || strings.Contains(s, string([]byte{0x2f}))
}

// looksLikeBackendName returns true for short identifiers used as backend names.
func looksLikeBackendName(s string) bool {
	// These are known safe non-secret values that happen to appear in "key"-named fields.
	knownSafe := []string{"env",
		// kit canonical secret backend names
		"keyring", "agefile", "onepassword", "ghsecrets",
		// deprecated dpkms aliases (still accepted, emit warning at runtime)
		"keychain", "age-file", "1password", "gh-secrets",
		"sqlite", "postgres", "local", "s3", "auto", "ctxt"}
	lower := strings.ToLower(s)
	for _, safe := range knownSafe {
		if lower == safe {
			return true
		}
	}
	return false
}
