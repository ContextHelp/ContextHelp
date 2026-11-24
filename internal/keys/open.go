package keys

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/secrets"
)

// DefaultSecretName is the secret entry name read from keyring or agefile
// backends when storage.encryption.secret_name is unset.
const DefaultSecretName = "storage_kek"

// Open constructs and health-checks the configured KEK Provider.
// When encryption is disabled, it returns (nil, nil). When enabled, any
// configuration, backend, or health-check failure returns an error wrapping
// ErrUnavailable so callers fail closed.
func Open(ctx context.Context, enc config.StorageEncryptionConfig, secCfg config.SecretsConfig) (Provider, error) {
	if !enc.Enabled {
		return nil, nil
	}
	var (
		p   Provider
		err error
	)
	switch enc.Provider {
	case config.EncryptionProviderFile:
		p, err = NewLocalFromFile(enc.KeyFile, enc.CurrentKey)
	case config.EncryptionProviderEnv:
		if enc.KeyEnv == "" {
			return nil, fmt.Errorf("%w: storage.encryption.key_env is empty", ErrUnavailable)
		}
		raw := strings.TrimSpace(os.Getenv(enc.KeyEnv))
		if raw == "" {
			return nil, fmt.Errorf("%w: environment variable %s is empty", ErrUnavailable, enc.KeyEnv)
		}
		p, err = NewLocalFromEnv(raw, enc.CurrentKey)
	case config.EncryptionProviderKeyring:
		cfg := secCfg
		cfg.Backend = "keyring"
		p, err = openFromSecrets(ctx, cfg, enc.SecretName, enc.CurrentKey)
	case config.EncryptionProviderAgeFile:
		cfg := secCfg
		cfg.Backend = "agefile"
		p, err = openFromSecrets(ctx, cfg, enc.SecretName, enc.CurrentKey)
	case config.EncryptionProviderTransit:
		p, err = openTransit(ctx, enc.Transit, secCfg)
	default:
		return nil, fmt.Errorf("%w: unknown storage.encryption.provider %q", ErrUnavailable, enc.Provider)
	}
	if err != nil {
		return nil, err
	}
	if err := p.Health(ctx); err != nil {
		return nil, fmt.Errorf("%w: key provider health check: %w", ErrUnavailable, err)
	}
	return p, nil
}

func openFromSecrets(ctx context.Context, secCfg config.SecretsConfig, secretName, currentKey string) (Provider, error) {
	if strings.TrimSpace(secretName) == "" {
		secretName = DefaultSecretName
	}
	store, err := secrets.New(secCfg)
	if err != nil {
		return nil, fmt.Errorf("%w: open secrets backend %s: %w", ErrUnavailable, secCfg.Backend, err)
	}
	return NewFromSecretStore(ctx, store, secretName, currentKey)
}

func openTransit(ctx context.Context, tc config.TransitEncryptionConfig, secCfg config.SecretsConfig) (Provider, error) {
	var token string
	switch {
	case tc.TokenEnv != "":
		token = strings.TrimSpace(os.Getenv(tc.TokenEnv))
	case tc.TokenSecret != "":
		store, err := secrets.New(secCfg)
		if err != nil {
			return nil, fmt.Errorf("%w: open secrets backend for transit token: %w", ErrUnavailable, err)
		}
		sec, err := store.Get(ctx, tc.TokenSecret)
		if err != nil {
			return nil, fmt.Errorf("%w: read transit token secret %q: %w", ErrUnavailable, tc.TokenSecret, err)
		}
		token = strings.TrimSpace(string(sec.Value))
	default:
		token = strings.TrimSpace(os.Getenv("VAULT_TOKEN"))
	}
	return NewTransit(TransitConfig{
		Addr:    tc.Addr,
		Mount:   tc.Mount,
		KeyName: tc.KeyName,
		Token:   token,
	})
}
