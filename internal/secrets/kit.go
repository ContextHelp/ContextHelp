// Package secrets is a thin adapter that translates ctxt's
// SecretsConfig into kit/go/storage/secret backend selection.
//
// All actual backend implementations live in hop.top/kit. This file
// imports them for side-effect registration so the kit factory
// (secret.Open) recognises them.
package secrets

import (
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"hop.top/kit/go/storage/secret"

	// Side-effect imports register backends with the kit factory.
	_ "hop.top/kit/go/storage/secret/agefile"
	_ "hop.top/kit/go/storage/secret/env"
	_ "hop.top/kit/go/storage/secret/ghsecrets"
	_ "hop.top/kit/go/storage/secret/keyring"
	_ "hop.top/kit/go/storage/secret/onepassword"
)

// defaultKeychainService is used when the user does not configure one.
const defaultKeychainService = "ctxt"

// New returns a kit secret store configured from cfg. Backend names
// are translated to kit's canonical names:
//
//	"" / "env"   -> env (read-only; Set/Delete return ErrNotSupported)
//	"keychain"   -> keyring
//	"age-file"   -> agefile (Set/Delete return ErrNotSupported)
//	"1password"  -> onepassword (CLI mode; vault from cfg.OnePasswordVault)
//	"gh-secrets" -> ghsecrets
func New(cfg config.SecretsConfig) (secret.MutableStore, error) {
	switch cfg.Backend {
	case "", "env":
		return secret.Open(secret.Config{Backend: "env", Prefix: ""})
	case "keychain":
		svc := cfg.KeychainService
		if svc == "" {
			svc = defaultKeychainService
		}
		return secret.Open(secret.Config{Backend: "keyring", Service: svc})
	case "age-file":
		if cfg.AgeFile == "" {
			return nil, fmt.Errorf("secrets: age-file backend requires secrets.age_file")
		}
		if cfg.AgeIdentityFile == "" {
			return nil, fmt.Errorf("secrets: age-file backend requires secrets.age_identity_file")
		}
		return secret.Open(secret.Config{
			Backend:      "agefile",
			Path:         cfg.AgeFile,
			IdentityFile: cfg.AgeIdentityFile,
		})
	case "1password":
		if cfg.OnePasswordVault == "" {
			return nil, fmt.Errorf("secrets: 1password backend requires secrets.onepassword_vault")
		}
		return secret.Open(secret.Config{
			Backend: "onepassword",
			Vault:   cfg.OnePasswordVault,
		})
	case "gh-secrets":
		return secret.Open(secret.Config{
			Backend: "ghsecrets",
			Repo:    cfg.GHRepo,
		})
	default:
		return nil, fmt.Errorf("secrets: unknown backend %q", cfg.Backend)
	}
}
