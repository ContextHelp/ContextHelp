package keys

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/config"
)

func TestOpenFromConfig(t *testing.T) {
	ctx := context.Background()

	// Disabled returns nil, nil.
	p, err := Open(ctx, config.StorageEncryptionConfig{Enabled: false}, config.SecretsConfig{})
	if err != nil || p != nil {
		t.Fatalf("Open(disabled) = (%v, %v), want (nil, nil)", p, err)
	}

	// Enabled with missing key file fails closed with ErrUnavailable.
	_, err = Open(ctx, config.StorageEncryptionConfig{
		Enabled:  true,
		Provider: config.EncryptionProviderFile,
		KeyFile:  filepath.Join(t.TempDir(), "does-not-exist"),
	}, config.SecretsConfig{})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Open(missing file) err = %v, want ErrUnavailable", err)
	}

	// Valid 0600 file opens and passes health check.
	raw := make([]byte, MaterialSize)
	_, _ = rand.Read(raw)
	keyPath := filepath.Join(t.TempDir(), "kek.txt")
	if err := os.WriteFile(keyPath, []byte("k1="+base64.StdEncoding.EncodeToString(raw)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err = Open(ctx, config.StorageEncryptionConfig{
		Enabled:  true,
		Provider: config.EncryptionProviderFile,
		KeyFile:  keyPath,
	}, config.SecretsConfig{})
	if err != nil {
		t.Fatalf("Open(valid file): %v", err)
	}
	if cur, err := p.Current(ctx); err != nil || cur != "k1" {
		t.Fatalf("Current = (%q, %v), want k1", cur, err)
	}

	// Env provider works and fails closed if env var unset.
	envName := "CTXT_TEST_STORAGE_KEK_" + "MATERIAL"
	t.Setenv(envName, "k2="+base64.StdEncoding.EncodeToString(raw))
	p, err = Open(ctx, config.StorageEncryptionConfig{
		Enabled:  true,
		Provider: config.EncryptionProviderEnv,
		KeyEnv:   envName,
	}, config.SecretsConfig{})
	if err != nil {
		t.Fatalf("Open(env): %v", err)
	}
	if cur, err := p.Current(ctx); err != nil || cur != "k2" {
		t.Fatalf("Current(env) = (%q, %v), want k2", cur, err)
	}
	t.Setenv(envName, "")
	if _, err := Open(ctx, config.StorageEncryptionConfig{
		Enabled:  true,
		Provider: config.EncryptionProviderEnv,
		KeyEnv:   envName,
	}, config.SecretsConfig{}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Open(empty env) err = %v, want ErrUnavailable", err)
	}
}
