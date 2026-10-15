package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authn "github.com/ideacrafterslabs/ctxt/internal/auth"
	"github.com/ideacrafterslabs/ctxt/internal/storage"
)

// runSplit runs dpkms against db like testDB.run, but keeps stdout and
// stderr apart: the key create contract is that the key is the only
// thing on stdout.
func (db *testDB) runSplit(args ...string) (stdout, stderr string, err error) {
	resetAllFlags(rootCmd)
	for _, k := range []string{"format", "output", "output.format", "verbose", "quiet", "no-color", "no-hints", "offline", "offline.enabled", "data-dir", "server-url", "storage.path", "server.url"} {
		viper.Set(k, "")
	}
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	var errBuf bytes.Buffer
	rootCmd.SetArgs(append([]string{"--config", db.ConfigPath}, args...))
	rootCmd.SetOut(w)
	rootCmd.SetErr(&errBuf)
	done := make(chan []byte, 1)
	go func() {
		data, _ := io.ReadAll(r)
		done <- data
	}()
	err = rootCmd.Execute()
	w.Close()
	os.Stdout = oldStdout
	out := <-done
	return string(out), errBuf.String(), err
}

func TestAuthKeyCreate(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	stdout, stderr, err := db.runSplit("auth", "key", "create", "--principal", "owner", "--name", "laptop")
	require.NoError(t, err, stdout+stderr)
	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	require.Len(t, lines, 1, "stdout carries the key and nothing else: %q", stdout)
	secret := lines[0]
	assert.True(t, strings.HasPrefix(secret, authn.APIKeyPrefix+"_"), "key %q", secret)

	keys := authn.NewKeysOver(db.Driver.Auth(), nil)
	p, idn, err := keys.Verify(ctx, secret)
	require.NoError(t, err)
	assert.Equal(t, storage.OwnerPrincipal, p.ID)
	assert.Equal(t, authn.ProviderAPIKey, p.Provider)
	assert.True(t, p.HasRole(authn.RoleAdmin))
	assert.Equal(t, "laptop", idn.Name)
	assert.Equal(t, storage.IdentityMethodAPIKey, idn.Method)

	assert.Contains(t, stderr, "Created API key "+idn.Subject+" for principal owner (laptop), no expiry")
	assert.NotContains(t, stderr, secret, "the secret never reaches stderr")
}

func TestAuthKeyCreateExpiry(t *testing.T) {
	db := setupTestDB(t)
	before := time.Now().UTC()

	stdout, _, err := db.runSplit("auth", "key", "create", "--principal", "owner", "--expires", "1h", "--format", "json")
	require.NoError(t, err, stdout)
	var doc struct {
		Key       string    `json:"key"`
		ID        string    `json:"id"`
		Principal string    `json:"principal_id"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	require.NoError(t, json.Unmarshal(jsonDoc(t, stdout), &doc), stdout)
	assert.Equal(t, "owner", doc.Principal)
	assert.WithinRange(t, doc.ExpiresAt, before.Add(time.Hour), time.Now().UTC().Add(time.Hour+time.Second))
	rec, err := authn.NewLocalKeys(db.Driver.Auth().KV(), nil).Get(context.Background(), doc.ID)
	require.NoError(t, err)
	assert.Equal(t, doc.ExpiresAt.Unix(), rec.ExpiresAt.Unix())
	_, _, err = authn.NewKeysOver(db.Driver.Auth(), nil).Verify(context.Background(), doc.Key)
	assert.NoError(t, err)

	stdout, _, err = db.runSplit("auth", "key", "create", "--principal", "owner", "--expires", "2026-12-31", "--format", "json")
	require.NoError(t, err, stdout)
	require.NoError(t, json.Unmarshal(jsonDoc(t, stdout), &doc), stdout)
	// The issuer stores now + TTL (kit's NewAPIKey shape), so an absolute
	// expiry lands within the clock reads of one call.
	assert.WithinDuration(t, time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), doc.ExpiresAt, time.Second, "a date expires at the end of that day")

	for _, bad := range []string{"soon", "-1h", "0s"} {
		stdout, _, err := db.runSplit("auth", "key", "create", "--principal", "owner", "--expires", bad)
		assert.Error(t, err, bad)
		assert.Empty(t, stdout, "nothing printed on refusal: %q", stdout)
	}
	ids, err := db.Driver.Auth().Identities().ListByPrincipal(context.Background(), storage.OwnerPrincipal)
	require.NoError(t, err)
	assert.Len(t, ids, 2, "refused runs mint nothing")
}

func TestAuthKeyCreateRefusals(t *testing.T) {
	db := setupTestDB(t)

	stdout, _, err := db.runSplit("auth", "key", "create", "--principal", "prn_nobody")
	require.Error(t, err)
	assert.ErrorIs(t, err, storage.ErrNotFound)
	assert.Empty(t, stdout)

	_, _, err = db.runSplit("auth", "key", "create")
	assert.Error(t, err, "--principal is required")

	_, _, err = db.runSplit("auth", "key", "create", "--principal", "owner", "extra")
	assert.Error(t, err, "no positional arguments")

	ids, err := db.Driver.Auth().Identities().ListByPrincipal(context.Background(), storage.OwnerPrincipal)
	require.NoError(t, err)
	assert.Empty(t, ids)
}

func TestParseKeyExpiry(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	at, err := parseKeyExpiry("", now)
	require.NoError(t, err)
	assert.True(t, at.IsZero())
	at, err = parseKeyExpiry(" 90m ", now)
	require.NoError(t, err)
	assert.Equal(t, now.Add(90*time.Minute), at)
	at, err = parseKeyExpiry("2026-12-31T10:00:00+02:00", now)
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 12, 31, 8, 0, 0, 0, time.UTC), at)
	for _, bad := range []string{"tomorrow", "-5m", "0h", "2026-13-01"} {
		_, err := parseKeyExpiry(bad, now)
		assert.Error(t, err, bad)
	}
}
