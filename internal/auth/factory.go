package auth

import (
	"fmt"

	"github.com/ideacrafterslabs/ctxt/internal/config"
)

// FromConfig builds the Provider selected by server.auth. An empty
// provider name returns (nil, nil): no inbound auth is configured, which
// is only acceptable for private instances (enforced by config
// validation and dpkms serve).
func FromConfig(cfg config.AuthConfig) (Provider, error) {
	switch cfg.Provider {
	case "":
		return nil, nil
	case ProviderStatic:
		tokens := make([]StaticToken, 0, len(cfg.Static.Tokens))
		for _, t := range cfg.Static.Tokens {
			tokens = append(tokens, StaticToken{
				Token:     t.Token,
				Principal: t.Principal,
				Roles:     t.Roles,
			})
		}
		return NewStatic(tokens)
	case ProviderOIDC, ProviderMTLS:
		return nil, fmt.Errorf("auth: provider %q is reserved but not yet implemented", cfg.Provider)
	default:
		return nil, fmt.Errorf("auth: unknown provider %q (valid: %s, %s, %s)",
			cfg.Provider, ProviderStatic, ProviderOIDC, ProviderMTLS)
	}
}
