package config

import (
	"fmt"
	"slices"
)

// Storage encryption provider names.
const (
	EncryptionProviderKeyring = "keyring"
	EncryptionProviderAgeFile = "agefile"
	EncryptionProviderFile    = "file"
	EncryptionProviderEnv     = "env"
	EncryptionProviderTransit = "transit"
)

// Postgres column encryption modes.
const (
	ColumnEncryptionOff       = "off"
	ColumnEncryptionSensitive = "sensitive"
	ColumnEncryptionAll       = "all"
)

// Postgres FTS indexing behavior for encrypted columns.
const (
	FTSOnEncryptedOmit       = "omit"
	FTSOnEncryptedRequireTDE = "require_tde"
)

// StorageEncryptionConfig configures encryption at rest and KEK custody.
type StorageEncryptionConfig struct {
	Enabled    bool                    `mapstructure:"enabled" yaml:"enabled"`
	Provider   string                  `mapstructure:"provider" yaml:"provider"`
	KeyFile    string                  `mapstructure:"key_file" yaml:"key_file" secret:"false"`
	KeyEnv     string                  `mapstructure:"key_env" yaml:"key_env" secret:"false"`
	SecretName string                  `mapstructure:"secret_name" yaml:"secret_name" secret:"false"`
	CurrentKey string                  `mapstructure:"current_key" yaml:"current_key" secret:"false"`
	Transit    TransitEncryptionConfig `mapstructure:"transit" yaml:"transit"`
}

// TransitEncryptionConfig configures OpenBao / Vault Transit KEK wrapping.
type TransitEncryptionConfig struct {
	Addr        string `mapstructure:"addr" yaml:"addr"`
	Mount       string `mapstructure:"mount" yaml:"mount"`
	KeyName     string `mapstructure:"key_name" yaml:"key_name" secret:"false"`
	TokenEnv    string `mapstructure:"token_env" yaml:"token_env" secret:"false"`
	TokenSecret string `mapstructure:"token_secret" yaml:"token_secret" secret:"false"`
	Namespace   string `mapstructure:"namespace" yaml:"namespace"`
}

// PostgresStorageConfig configures Postgres encryption-at-rest posture.
// RequireTDE and ColumnEncryption are orthogonal controls that may be
// enabled independently or together.
type PostgresStorageConfig struct {
	RequireTDE       bool   `mapstructure:"require_tde" yaml:"require_tde"`
	ColumnEncryption string `mapstructure:"column_encryption" yaml:"column_encryption"`
	FTSOnEncrypted   string `mapstructure:"fts_on_encrypted" yaml:"fts_on_encrypted"`
}

// EffectiveColumnEncryption returns the resolved column encryption mode.
func (p PostgresStorageConfig) EffectiveColumnEncryption() string {
	if p.ColumnEncryption == "" {
		return ColumnEncryptionOff
	}
	return p.ColumnEncryption
}

// EffectiveFTSOnEncrypted returns the resolved FTS policy for encrypted columns.
func (p PostgresStorageConfig) EffectiveFTSOnEncrypted() string {
	if p.FTSOnEncrypted == "" {
		return FTSOnEncryptedOmit
	}
	return p.FTSOnEncrypted
}

// ValidateEncryption checks storage.encryption and storage.postgres settings.
func (s StorageConfig) ValidateEncryption() []ValidationError {
	var errs []ValidationError

	enc := s.Encryption
	validProviders := []string{
		EncryptionProviderKeyring,
		EncryptionProviderAgeFile,
		EncryptionProviderFile,
		EncryptionProviderEnv,
		EncryptionProviderTransit,
	}
	if enc.Provider != "" && !slices.Contains(validProviders, enc.Provider) {
		errs = append(errs, ValidationError{
			Field:   "storage.encryption.provider",
			Message: fmt.Sprintf("unknown provider %q (want one of keyring, agefile, file, env, transit)", enc.Provider),
		})
	}
	if enc.Enabled {
		if enc.Provider == "" {
			errs = append(errs, ValidationError{
				Field:   "storage.encryption.provider",
				Message: "must be set when storage.encryption.enabled is true",
			})
		}
		switch enc.Provider {
		case EncryptionProviderFile:
			if enc.KeyFile == "" {
				errs = append(errs, ValidationError{
					Field:   "storage.encryption.key_file",
					Message: "must be set when storage.encryption.provider is file",
				})
			}
		case EncryptionProviderEnv:
			if enc.KeyEnv == "" {
				errs = append(errs, ValidationError{
					Field:   "storage.encryption.key_env",
					Message: "must be set when storage.encryption.provider is env",
				})
			}
		case EncryptionProviderTransit:
			if enc.Transit.Addr == "" {
				errs = append(errs, ValidationError{
					Field:   "storage.encryption.transit.addr",
					Message: "must be set when storage.encryption.provider is transit",
				})
			}
			if enc.Transit.KeyName == "" {
				errs = append(errs, ValidationError{
					Field:   "storage.encryption.transit.key_name",
					Message: "must be set when storage.encryption.provider is transit",
				})
			}
		}
	}

	colMode := s.Postgres.EffectiveColumnEncryption()
	switch colMode {
	case ColumnEncryptionOff, ColumnEncryptionSensitive, ColumnEncryptionAll:
	default:
		errs = append(errs, ValidationError{
			Field:   "storage.postgres.column_encryption",
			Message: fmt.Sprintf("unknown mode %q (want off, sensitive, or all)", s.Postgres.ColumnEncryption),
		})
	}
	if (colMode == ColumnEncryptionSensitive || colMode == ColumnEncryptionAll) && !enc.Enabled {
		errs = append(errs, ValidationError{
			Field:   "storage.encryption.enabled",
			Message: fmt.Sprintf("must be true when storage.postgres.column_encryption is %q", colMode),
		})
	}

	ftsMode := s.Postgres.EffectiveFTSOnEncrypted()
	switch ftsMode {
	case FTSOnEncryptedOmit, FTSOnEncryptedRequireTDE:
	default:
		errs = append(errs, ValidationError{
			Field:   "storage.postgres.fts_on_encrypted",
			Message: fmt.Sprintf("unknown mode %q (want omit or require_tde)", s.Postgres.FTSOnEncrypted),
		})
	}
	if ftsMode == FTSOnEncryptedRequireTDE && !s.Postgres.RequireTDE {
		errs = append(errs, ValidationError{
			Field:   "storage.postgres.fts_on_encrypted",
			Message: "require_tde requires storage.postgres.require_tde to be true",
		})
	}

	return errs
}
