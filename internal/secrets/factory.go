package secrets

import (
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/config"
)

// NewResolver creates the appropriate Resolver based on cfg.
func NewResolver(cfg config.SecretsConfig) (Resolver, error) {
	switch cfg.Backend {
	case "env", "":
		return NewEnvResolver(), nil
	case "keychain":
		svc := cfg.KeychainService
		if svc == "" {
			svc = "ctxt"
		}
		return NewKeychainResolver(svc), nil
	case "age-file":
		if cfg.AgeFile == "" {
			return nil, fmt.Errorf("secrets: age-file backend requires secrets.age_file to be set")
		}
		if cfg.AgeIdentityFile == "" {
			return nil, fmt.Errorf("secrets: age-file backend requires secrets.age_identity_file to be set")
		}
		return NewAgeFileResolver(cfg.AgeFile, cfg.AgeIdentityFile), nil
	default:
		return nil, fmt.Errorf("secrets: unknown backend %q", cfg.Backend)
	}
}
