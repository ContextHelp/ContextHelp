package config

import (
	"strings"
	"testing"
)

func TestStorageEncryptionValidation(t *testing.T) {
	tests := []struct {
		name      string
		storage   StorageConfig
		wantField string
	}{
		{
			name:    "disabled defaults ok",
			storage: StorageConfig{Type: "sqlite"},
		},
		{
			name: "enabled without provider fails",
			storage: StorageConfig{
				Type:       "sqlite",
				Encryption: StorageEncryptionConfig{Enabled: true},
			},
			wantField: "storage.encryption.provider",
		},
		{
			name: "unknown provider fails",
			storage: StorageConfig{
				Type:       "sqlite",
				Encryption: StorageEncryptionConfig{Enabled: true, Provider: "bogus"},
			},
			wantField: "storage.encryption.provider",
		},
		{
			name: "file provider without key_file fails",
			storage: StorageConfig{
				Type:       "sqlite",
				Encryption: StorageEncryptionConfig{Enabled: true, Provider: EncryptionProviderFile},
			},
			wantField: "storage.encryption.key_file",
		},
		{
			name: "env provider without key_env fails",
			storage: StorageConfig{
				Type:       "sqlite",
				Encryption: StorageEncryptionConfig{Enabled: true, Provider: EncryptionProviderEnv},
			},
			wantField: "storage.encryption.key_env",
		},
		{
			name: "transit without addr fails",
			storage: StorageConfig{
				Type:       "postgres",
				Encryption: StorageEncryptionConfig{Enabled: true, Provider: EncryptionProviderTransit, Transit: TransitEncryptionConfig{KeyName: "ctxt"}},
			},
			wantField: "storage.encryption.transit.addr",
		},
		{
			name: "postgres column_encryption without encryption enabled fails",
			storage: StorageConfig{
				Type:     "postgres",
				Postgres: PostgresStorageConfig{ColumnEncryption: ColumnEncryptionSensitive},
			},
			wantField: "storage.encryption.enabled",
		},
		{
			name: "postgres fts_on_encrypted require_tde without require_tde fails",
			storage: StorageConfig{
				Type: "postgres",
				Encryption: StorageEncryptionConfig{
					Enabled: true, Provider: EncryptionProviderFile, KeyFile: "/run/keys/kek",
				},
				Postgres: PostgresStorageConfig{
					RequireTDE:       false,
					ColumnEncryption: ColumnEncryptionAll,
					FTSOnEncrypted:   FTSOnEncryptedRequireTDE,
				},
			},
			wantField: "storage.postgres.fts_on_encrypted",
		},
		{
			name: "postgres tde + column_encryption + fts require_tde ok",
			storage: StorageConfig{
				Type: "postgres",
				Encryption: StorageEncryptionConfig{
					Enabled: true, Provider: EncryptionProviderFile, KeyFile: "/run/keys/kek",
				},
				Postgres: PostgresStorageConfig{
					RequireTDE:       true,
					ColumnEncryption: ColumnEncryptionAll,
					FTSOnEncrypted:   FTSOnEncryptedRequireTDE,
				},
			},
		},
		{
			name: "postgres tde only ok",
			storage: StorageConfig{
				Type:     "postgres",
				Postgres: PostgresStorageConfig{RequireTDE: true, ColumnEncryption: ColumnEncryptionOff},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			errs := tc.storage.ValidateEncryption()
			if tc.wantField == "" {
				if len(errs) > 0 {
					t.Fatalf("unexpected validation errors: %v", errs)
				}
				return
			}
			found := false
			for _, e := range errs {
				if strings.Contains(e.Field, tc.wantField) {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("expected error on field %q, got %v", tc.wantField, errs)
			}
		})
	}
}
