// Package secrets is a thin adapter that translates ctxt's
// SecretsConfig into kit/go/storage/secret backend selection.
//
// All actual backend implementations live in hop.top/kit. This file
// imports them for side-effect registration so the kit factory
// (secret.Open) recognises them.
package secrets

import (
	"fmt"
	"log/slog"
	"os"
	"sync"

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

// CanonicalBackendName returns the kit-canonical backend name for the
// dpkms-/ctxt-facing alias, or the input unchanged when no alias matches.
//
// The aliases are kept for backwards compatibility — pre-T-0469 configs
// referenced backends by these names. New code should always use the
// canonical kit names directly.
//
//	"keychain"   -> "keyring"
//	"age-file"   -> "agefile"
//	"1password"  -> "onepassword"
//	"gh-secrets" -> "ghsecrets"
func CanonicalBackendName(name string) string {
	if canon, ok := backendAliases[name]; ok {
		return canon
	}
	return name
}

// backendAliases maps deprecated dpkms backend names to kit canonical names.
var backendAliases = map[string]string{
	"keychain":   "keyring",
	"age-file":   "agefile",
	"1password":  "onepassword",
	"gh-secrets": "ghsecrets",
}

// IsDeprecatedBackendAlias reports whether name is a deprecated alias
// (i.e. distinct from its canonical form).
func IsDeprecatedBackendAlias(name string) bool {
	_, ok := backendAliases[name]
	return ok
}

// deprecationOnce ensures we warn at most once per process per alias,
// to avoid noisy logs when New is called repeatedly with the same config.
var deprecationOnce sync.Map // map[string]*sync.Once

func warnDeprecatedAlias(alias, canonical string) {
	v, _ := deprecationOnce.LoadOrStore(alias, &sync.Once{})
	v.(*sync.Once).Do(func() {
		fmt.Fprintf(os.Stderr,
			"warning: secrets backend %q is deprecated; use canonical name %q (will be removed in a future release)\n",
			alias, canonical)
		slog.Warn("secrets: deprecated backend alias",
			"alias", alias,
			"canonical", canonical)
	})
}

// New returns a kit secret store configured from cfg. Backend names
// accept both the canonical kit names and the deprecated dpkms aliases:
//
//	canonical    deprecated alias
//	---------    ----------------
//	env          (none)
//	keyring      keychain
//	agefile      age-file
//	onepassword  1password
//	ghsecrets    gh-secrets
//
// When a deprecated alias is used, a one-time warning is emitted to
// stderr and the request is routed to the canonical backend.
func New(cfg config.SecretsConfig) (secret.MutableStore, error) {
	canonical := CanonicalBackendName(cfg.Backend)
	if IsDeprecatedBackendAlias(cfg.Backend) {
		warnDeprecatedAlias(cfg.Backend, canonical)
	}

	switch canonical {
	case "", "env":
		return secret.Open(secret.Config{Backend: "env", Prefix: ""})
	case "keyring":
		svc := cfg.KeychainService
		if svc == "" {
			svc = defaultKeychainService
		}
		return secret.Open(secret.Config{Backend: "keyring", Service: svc})
	case "agefile":
		if cfg.AgeFile == "" {
			return nil, fmt.Errorf("secrets: agefile backend requires secrets.age_file")
		}
		if cfg.AgeIdentityFile == "" {
			return nil, fmt.Errorf("secrets: agefile backend requires secrets.age_identity_file")
		}
		return secret.Open(secret.Config{
			Backend:      "agefile",
			Path:         cfg.AgeFile,
			IdentityFile: cfg.AgeIdentityFile,
		})
	case "onepassword":
		if cfg.OnePasswordVault == "" {
			return nil, fmt.Errorf("secrets: onepassword backend requires secrets.onepassword_vault")
		}
		return secret.Open(secret.Config{
			Backend: "onepassword",
			Vault:   cfg.OnePasswordVault,
		})
	case "ghsecrets":
		return secret.Open(secret.Config{
			Backend: "ghsecrets",
			Repo:    cfg.GHRepo,
		})
	default:
		return nil, fmt.Errorf("secrets: unknown backend %q", cfg.Backend)
	}
}
