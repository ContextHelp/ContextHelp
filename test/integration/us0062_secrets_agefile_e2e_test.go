package integration

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"filippo.io/age"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/secrets"
	"hop.top/kit/go/storage/secret"
)

// writeAgeYAMLForTest encrypts payload with a fresh X25519 identity and
// writes both files to dir. Returns paths.
func writeAgeYAMLForTest(t *testing.T, dir, payload string) (cipherPath, identPath string) {
	t.Helper()
	id, err := age.GenerateX25519Identity()
	require.NoError(t, err, "generate identity")

	identPath = filepath.Join(dir, "identity.txt")
	require.NoError(t, os.WriteFile(identPath, []byte(id.String()+"\n"), 0o600))

	cipherPath = filepath.Join(dir, "secrets.age")
	f, err := os.Create(cipherPath)
	require.NoError(t, err)
	w, err := age.Encrypt(f, id.Recipient())
	require.NoError(t, err)
	_, err = w.Write([]byte(payload))
	require.NoError(t, err)
	require.NoError(t, w.Close())
	require.NoError(t, f.Close())
	return cipherPath, identPath
}

// TestUS0062_AgeFileBackendRoundTrip verifies the age-file backend decrypts a
// real age-encrypted YAML and returns values for the expected keys.
func TestUS0062_AgeFileBackendRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cipher, ident := writeAgeYAMLForTest(t, dir,
		"openai_api_key: sk-test-1234\nanthropic_api_key: sk-ant-9876\n")

	r, err := secrets.New(config.SecretsConfig{
		Backend:         "age-file",
		AgeFile:         cipher,
		AgeIdentityFile: ident,
	})
	require.NoError(t, err)

	got, err := r.Get(context.Background(), "openai_api_key")
	require.NoError(t, err)
	assert.Equal(t, "sk-test-1234", string(got.Value))

	got, err = r.Get(context.Background(), "anthropic_api_key")
	require.NoError(t, err)
	assert.Equal(t, "sk-ant-9876", string(got.Value))
}

// TestUS0062_AgeFileBackendListKeys verifies List enumerates every key in the
// decrypted YAML map (kit's agefile differs from ctxt's old impl: lists are
// supported here even though Set/Delete are not).
func TestUS0062_AgeFileBackendListKeys(t *testing.T) {
	dir := t.TempDir()
	cipher, ident := writeAgeYAMLForTest(t, dir,
		"k1: v1\nk2: v2\nk3: v3\n")

	r, err := secrets.New(config.SecretsConfig{
		Backend:         "age-file",
		AgeFile:         cipher,
		AgeIdentityFile: ident,
	})
	require.NoError(t, err)

	keys, err := r.List(context.Background(), "")
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"k1", "k2", "k3"}, keys)
}

// TestUS0062_AgeFileBackendMissingKeyReturnsErrNotFound verifies the sentinel
// error contract for missing keys (callers errors.Is against secret.ErrNotFound).
func TestUS0062_AgeFileBackendMissingKeyReturnsErrNotFound(t *testing.T) {
	dir := t.TempDir()
	cipher, ident := writeAgeYAMLForTest(t, dir, "present: yes\n")

	r, err := secrets.New(config.SecretsConfig{
		Backend:         "age-file",
		AgeFile:         cipher,
		AgeIdentityFile: ident,
	})
	require.NoError(t, err)

	_, err = r.Get(context.Background(), "absent")
	require.Error(t, err)
	assert.True(t, errors.Is(err, secret.ErrNotFound), "want ErrNotFound, got %v", err)
}

// TestUS0062_AgeFileBackendSetReturnsNotSupported verifies write operations
// surface ErrNotSupported (re-encrypt the file out-of-band to mutate).
func TestUS0062_AgeFileBackendSetReturnsNotSupported(t *testing.T) {
	dir := t.TempDir()
	cipher, ident := writeAgeYAMLForTest(t, dir, "k: v\n")

	r, err := secrets.New(config.SecretsConfig{
		Backend:         "age-file",
		AgeFile:         cipher,
		AgeIdentityFile: ident,
	})
	require.NoError(t, err)

	err = r.Set(context.Background(), "k2", []byte("v2"))
	require.Error(t, err)
	assert.True(t, errors.Is(err, secret.ErrNotSupported), "want ErrNotSupported, got %v", err)
}
