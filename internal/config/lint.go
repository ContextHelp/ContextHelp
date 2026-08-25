package config

import (
	"fmt"
	"os"
)

// LintSeverity indicates the severity of a lint finding.
type LintSeverity string

const (
	// SeverityWarn is a non-blocking issue that should be addressed.
	SeverityWarn LintSeverity = "WARN"
	// SeverityError is a blocking issue that must be fixed.
	SeverityError LintSeverity = "ERROR"
)

// LintFinding is a single linter finding with severity, field path, message,
// and an optional fix description.
type LintFinding struct {
	Severity LintSeverity
	// Field is the dot-separated config path, or "@file" for file-level findings.
	Field   string
	Message string
	// Fix describes the auto-fix that will be applied when --fix is used.
	// Empty means no auto-fix available.
	Fix string
}

func (f LintFinding) String() string {
	if f.Fix != "" {
		return fmt.Sprintf("%-5s %s: %s (fix: %s)", f.Severity, f.Field, f.Message, f.Fix)
	}
	return fmt.Sprintf("%-5s %s: %s", f.Severity, f.Field, f.Message)
}

// deprecatedKeys maps old config field paths to their replacement.
// Add entries here when fields are removed or renamed.
var deprecatedKeys = map[string]string{
	// Example (uncomment when deprecated):
	// "storage.blob.s3.endpoint_url": "storage.blob.s3.endpoint",
}

// LintConfig runs all lint checks on the given Config loaded from configPath.
// It combines schema validation, secret scanning, file permission checks,
// and deprecated key detection.
//
// configPath may be empty if the config was not loaded from a file
// (e.g. defaults only), in which case permission checks are skipped.
func LintConfig(cfg *Config, configPath string) []LintFinding {
	var findings []LintFinding

	findings = append(findings, lintSchema(cfg)...)
	findings = append(findings, lintSecrets(cfg)...)
	findings = append(findings, lintPermissions(configPath)...)
	findings = append(findings, lintDeprecated(cfg)...)
	findings = append(findings, lintAccess(cfg)...)

	return findings
}

// lintAccess surfaces access-class smells that are not hard errors:
// the deprecated server.public shorthand, and an inbound federation
// token on a private instance (probably meant to be protected). Neither
// flips the class — validation owns the hard rules.
func lintAccess(cfg *Config) []LintFinding {
	var findings []LintFinding
	if cfg.Server.Access == "" && cfg.Server.Public {
		findings = append(findings, LintFinding{
			Severity: SeverityWarn,
			Field:    "server.public",
			Message:  "deprecated shorthand for server.access: public; set server.access explicitly",
			Fix:      "set server.access: public and drop server.public",
		})
	}
	if cfg.Server.EffectiveAccess() == AccessPrivate && cfg.Federation.Token != "" {
		findings = append(findings, LintFinding{
			Severity: SeverityWarn,
			Field:    "federation.token",
			Message:  "inbound federation token configured on a private instance — remote pushers cannot reach it; did you mean server.access: protected?",
		})
	}
	return findings
}

// lintSchema converts ValidationErrors into LintFindings at ERROR severity.
func lintSchema(cfg *Config) []LintFinding {
	errs := Validate(cfg)
	findings := make([]LintFinding, 0, len(errs))
	for _, e := range errs {
		findings = append(findings, LintFinding{
			Severity: SeverityError,
			Field:    e.Field,
			Message:  e.Message,
		})
	}
	return findings
}

// lintSecrets converts SecretWarnings into LintFindings at WARN severity.
func lintSecrets(cfg *Config) []LintFinding {
	warnings := ScanSecrets(cfg)
	findings := make([]LintFinding, 0, len(warnings))
	for _, w := range warnings {
		findings = append(findings, LintFinding{
			Severity: SeverityWarn,
			Field:    w.Field,
			Message:  fmt.Sprintf("looks like a plaintext secret (%s) — %s; use ${ENV_VAR} instead", w.Hint, w.Reason),
		})
	}
	return findings
}

// lintPermissions checks that the config file is not world-readable.
// Recommends chmod 600. Returns empty slice when configPath is "".
func lintPermissions(configPath string) []LintFinding {
	if configPath == "" {
		return nil
	}
	info, err := os.Stat(configPath)
	if err != nil {
		// File may not exist yet; not a lint error.
		return nil
	}
	mode := info.Mode().Perm()
	// World-readable: others read bit (0004) set.
	if mode&0004 != 0 {
		return []LintFinding{{
			Severity: SeverityWarn,
			Field:    "@file",
			Message: fmt.Sprintf(
				"config file is world-readable (%s); recommend chmod 600 — secrets may be exposed",
				mode,
			),
			Fix: "chmod 600 " + configPath,
		}}
	}
	// Group-readable: group read bit (0040) set.
	if mode&0040 != 0 {
		return []LintFinding{{
			Severity: SeverityWarn,
			Field:    "@file",
			Message: fmt.Sprintf(
				"config file is group-readable (%s); recommend chmod 600",
				mode,
			),
			Fix: "chmod 600 " + configPath,
		}}
	}
	return nil
}

// lintDeprecated checks for deprecated config keys (by checking known field paths).
// Currently a stub — add deprecated key mappings to deprecatedKeys above.
func lintDeprecated(_ *Config) []LintFinding {
	// deprecatedKeys is checked against raw YAML in RunE; here we surface any
	// entries that are still non-empty in the struct (future use).
	// When fields are actually removed, add struct checks here.
	var findings []LintFinding
	for old, replacement := range deprecatedKeys {
		// Placeholder — future deprecated fields will be detected via reflection.
		_ = old
		_ = replacement
	}
	return findings
}

// FixPermissions applies the safe auto-fix for world/group-readable config files.
// Returns nil on success, or an error if chmod fails.
func FixPermissions(configPath string) error {
	if configPath == "" {
		return nil
	}
	info, err := os.Stat(configPath)
	if err != nil {
		return nil // file doesn't exist, nothing to fix
	}
	mode := info.Mode().Perm()
	if mode&0044 == 0 {
		return nil // already safe
	}
	return os.Chmod(configPath, 0600)
}
