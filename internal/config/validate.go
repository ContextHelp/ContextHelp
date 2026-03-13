package config

import (
	"fmt"
	"regexp"
	"time"
)

// ValidationError describes a single config validation failure.
type ValidationError struct {
	Field   string
	Message string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("config: %s: %s", e.Field, e.Message)
}

var namespaceRE = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

var validStorageTypes = map[string]bool{
	"sqlite":   true,
	"postgres": true,
	"memory":   true,
}

// Validate runs all validation rules against cfg and returns a slice of
// ValidationErrors. An empty slice means the config is valid.
func Validate(cfg *Config) []ValidationError {
	var errs []ValidationError

	// storage.type enum check
	if cfg.Storage.Type != "" && !validStorageTypes[cfg.Storage.Type] {
		errs = append(errs, ValidationError{
			Field:   "storage.type",
			Message: fmt.Sprintf("must be one of sqlite, postgres, memory; got %q", cfg.Storage.Type),
		})
	}

	// server.port range
	if cfg.Server.Port != 0 && (cfg.Server.Port < 1024 || cfg.Server.Port > 65535) {
		errs = append(errs, ValidationError{
			Field:   "server.port",
			Message: fmt.Sprintf("must be between 1024 and 65535; got %d", cfg.Server.Port),
		})
	}

	// server.grpc_port range
	if cfg.Server.GRPCPort != 0 && (cfg.Server.GRPCPort < 1024 || cfg.Server.GRPCPort > 65535) {
		errs = append(errs, ValidationError{
			Field:   "server.grpc_port",
			Message: fmt.Sprintf("must be between 1024 and 65535; got %d", cfg.Server.GRPCPort),
		})
	}

	// jobs.poll_interval minimum
	if cfg.Jobs.PollInterval > 0 && cfg.Jobs.PollInterval < 50*time.Millisecond {
		errs = append(errs, ValidationError{
			Field:   "jobs.poll_interval",
			Message: fmt.Sprintf("must be >= 50ms; got %s", cfg.Jobs.PollInterval),
		})
	}

	// profile.default must exist in profile.profiles (if profiles defined)
	if cfg.Profile.Default != "" && len(cfg.Profile.Profiles) > 0 {
		if _, ok := cfg.Profile.Profiles[cfg.Profile.Default]; !ok {
			errs = append(errs, ValidationError{
				Field:   "profile.default",
				Message: fmt.Sprintf("profile %q not found in profile.profiles", cfg.Profile.Default),
			})
		}
	}

	// allowed_mention_namespaces format
	for i, ns := range cfg.Conventions.AllowedMentionNamespaces {
		if !namespaceRE.MatchString(ns) {
			errs = append(errs, ValidationError{
				Field:   fmt.Sprintf("conventions.allowed_mention_namespaces[%d]", i),
				Message: fmt.Sprintf("must match ^[a-z][a-z0-9_]*$; got %q", ns),
			})
		}
	}

	// secrets.backend enum
	validSecretBackends := map[string]bool{"env": true, "keychain": true, "age-file": true}
	if cfg.Secrets.Backend != "" && !validSecretBackends[cfg.Secrets.Backend] {
		errs = append(errs, ValidationError{
			Field:   "secrets.backend",
			Message: fmt.Sprintf("must be one of env, keychain, age-file; got %q", cfg.Secrets.Backend),
		})
	}

	return errs
}
