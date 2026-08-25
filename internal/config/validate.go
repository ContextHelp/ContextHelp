package config

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// ValidationError is a single field-level validation failure.
type ValidationError struct {
	Field   string
	Message string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// Validate returns a slice of field-level errors for the given Config.
func Validate(c *Config) []ValidationError {
	var errs []ValidationError

	switch c.Storage.Type {
	case "", "sqlite", "postgres":
		// valid
	default:
		errs = append(errs, ValidationError{Field: "storage.type", Message: fmt.Sprintf("unknown storage type %q", c.Storage.Type)})
	}

	if c.Server.Port != 0 && (c.Server.Port < 1024 || c.Server.Port > 65535) {
		errs = append(errs, ValidationError{Field: "server.port", Message: fmt.Sprintf("must be 1024–65535, got %d", c.Server.Port)})
	}

	if c.Server.GRPCPort != 0 && (c.Server.GRPCPort < 1024 || c.Server.GRPCPort > 65535) {
		errs = append(errs, ValidationError{Field: "server.grpc_port", Message: fmt.Sprintf("must be 1024–65535, got %d", c.Server.GRPCPort)})
	}

	errs = append(errs, validateAccess(c)...)

	if c.Jobs.PollInterval != 0 && c.Jobs.PollInterval < 50*time.Millisecond {
		errs = append(errs, ValidationError{Field: "jobs.poll_interval", Message: fmt.Sprintf("must be ≥50ms, got %s", c.Jobs.PollInterval)})
	}

	if c.Profile.Default != "" {
		if _, ok := c.Profile.Profiles[c.Profile.Default]; !ok {
			errs = append(errs, ValidationError{Field: "profile.default", Message: fmt.Sprintf("profile %q not defined in profiles map", c.Profile.Default)})
		}
	}

	nsRe := regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)
	for i, ns := range c.Conventions.AllowedMentionNamespaces {
		if !nsRe.MatchString(ns) {
			errs = append(errs, ValidationError{Field: fmt.Sprintf("conventions.allowed_mention_namespaces[%d]", i), Message: fmt.Sprintf("invalid namespace %q (must match [a-z][a-z0-9_-]*)", ns)})
		}
	}

	switch c.Secrets.Backend {
	case "", "env",
		// kit canonical names (preferred)
		"keyring", "agefile", "onepassword", "ghsecrets",
		// deprecated aliases (still accepted; emit deprecation warning at runtime)
		"keychain", "age-file", "1password", "gh-secrets":
		// valid
	default:
		errs = append(errs, ValidationError{Field: "secrets.backend", Message: fmt.Sprintf("unknown backend %q; must be one of env, keyring, agefile, onepassword, ghsecrets", c.Secrets.Backend)})
	}

	return errs
}

// validateAccess enforces the server.access class rules:
//
//   - the class must be one of private | protected | public (empty = unset,
//     resolved to private by EffectiveAccess);
//   - an explicit access: private combined with server.public / --public is
//     a hard error (the two contradict each other);
//   - a non-private instance must have inbound authentication configured —
//     an exposed listener with zero auth is exactly the failure mode the
//     class exists to delete.
func validateAccess(c *Config) []ValidationError {
	var errs []ValidationError

	switch c.Server.Access {
	case "", AccessPrivate, AccessProtected, AccessPublic:
		// valid
	default:
		errs = append(errs, ValidationError{
			Field:   "server.access",
			Message: fmt.Sprintf("unknown access class %q; must be one of %s, %s, %s", c.Server.Access, AccessPrivate, AccessProtected, AccessPublic),
		})
		return errs // effective-access rules below assume a valid class
	}

	if c.Server.Access == AccessPrivate && c.Server.Public {
		errs = append(errs, ValidationError{
			Field:   "server.public",
			Message: "conflicts with server.access: private — drop server.public / --public, or set access: protected|public",
		})
	}

	if eff := c.Server.EffectiveAccess(); eff != AccessPrivate && !c.Server.Auth.HasInboundAuth() {
		errs = append(errs, ValidationError{
			Field:   "server.access",
			Message: fmt.Sprintf("%s instance requires inbound authentication — set server.auth.provider (e.g. \"static\" with at least one token under server.auth.static.tokens)", eff),
		})
	}

	return errs
}

// ValidateAccess returns the access-class misconfiguration for c, if
// any, as a single error. dpkms serve calls it (with the --public flag
// folded into Server.Public) so a protected/public instance with no
// credentials refuses to start instead of silently exposing 0.0.0.0
// with zero auth.
func (c *Config) ValidateAccess() error {
	errs := validateAccess(c)
	if len(errs) == 0 {
		return nil
	}
	msgs := make([]string, len(errs))
	for i, e := range errs {
		msgs[i] = e.Error()
	}
	return fmt.Errorf("config: %s", strings.Join(msgs, "; "))
}

// Validate checks that the Config is internally consistent.
// Called after Load() when strict validation is desired.
func (c *Config) Validate() error {
	if err := c.validateDuplicates(); err != nil {
		return err
	}
	if err := c.validateFederations(); err != nil {
		return err
	}
	return nil
}

// validateFederations checks federation entries for valid sync modes, required
// intervals, and duplicate names (which would cause ambiguous routing).
// Full topology-level cycle detection (A→B→A across remote configs) is deferred
// to Phase 2 when remote config discovery is available.
func (c *Config) validateFederations() error {
	seen := make(map[string]struct{}, len(c.Federations))
	for i, f := range c.Federations {
		if f.Name == "" {
			return fmt.Errorf("config: federations[%d].name must not be empty", i)
		}
		if _, dup := seen[f.Name]; dup {
			return fmt.Errorf("config: federations: duplicate name %q (names must be unique)", f.Name)
		}
		seen[f.Name] = struct{}{}

		switch f.SyncMode {
		case "async", "inline":
			// valid
		case "bidirectional":
			return fmt.Errorf("config: federations[%d] (%q): bidirectional sync mode is not yet implemented (Phase 3)", i, f.Name)
		default:
			return fmt.Errorf("config: federations[%d] (%q): sync_mode must be \"async\" or \"inline\"; got %q", i, f.Name, f.SyncMode)
		}

		if f.SyncMode == "async" && f.Interval <= 0 {
			return fmt.Errorf("config: federations[%d] (%q): interval is required and must be >0 for async sync_mode", i, f.Name)
		}
	}
	return nil
}

func (c *Config) validateDuplicates() error {
	d := c.Duplicates
	policy := d.Policy
	if policy == "" {
		policy = "warn" // treat empty as default
	}
	switch policy {
	case "warn", "drop", "keep":
		// valid
	default:
		return fmt.Errorf("config: duplicates.policy must be one of \"warn\", \"drop\", \"keep\"; got %q", d.Policy)
	}
	if d.SimilarityThreshold < 0.0 || d.SimilarityThreshold > 1.0 {
		return fmt.Errorf("config: duplicates.similarity_threshold must be 0.0–1.0; got %f", d.SimilarityThreshold)
	}
	return nil
}
