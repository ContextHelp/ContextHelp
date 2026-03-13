package config

import (
	"fmt"
	"regexp"
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

	return errs
}

// Validate checks that the Config is internally consistent.
// Called after Load() when strict validation is desired.
func (c *Config) Validate() error {
	if err := c.validateDuplicates(); err != nil {
		return err
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
