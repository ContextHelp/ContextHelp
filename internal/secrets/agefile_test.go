package secrets

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgeFileResolverImplementsLister(t *testing.T) {
	r := NewAgeFileResolver("/tmp/secrets.age", "/tmp/identity.txt")
	_, ok := any(r).(Lister)
	assert.True(t, ok, "AgeFileResolver should implement Lister")
}

func TestAgeFileResolverListKeys(t *testing.T) {
	// Skip if age tooling not available.
	if _, err := exec.LookPath("age-keygen"); err != nil {
		t.Skip("age-keygen not in PATH")
	}
	if _, err := exec.LookPath("age"); err != nil {
		t.Skip("age not in PATH")
	}

	dir := t.TempDir()
	identityFile := filepath.Join(dir, "identity.txt")
	secretsAge := filepath.Join(dir, "secrets.age")

	// Generate identity.
	require.NoError(t, exec.Command("age-keygen", "-o", identityFile).Run())

	// Extract public key.
	idBytes, err := os.ReadFile(identityFile)
	require.NoError(t, err)
	var pubkey string
	for _, line := range splitLines(string(idBytes)) {
		if len(line) > 14 && line[:14] == "# public key: " {
			pubkey = line[14:]
		}
	}
	require.NotEmpty(t, pubkey, "could not extract public key")

	// Write plaintext YAML.
	plaintext := filepath.Join(dir, "secrets.yaml")
	require.NoError(t, os.WriteFile(plaintext, []byte("ALPHA: val1\nBETA: val2\n"), 0600))

	// Encrypt.
	require.NoError(t, exec.Command("age", "-r", pubkey, "-o", secretsAge, plaintext).Run())

	r := NewAgeFileResolver(secretsAge, identityFile)
	lister, ok := any(r).(Lister)
	require.True(t, ok)

	keys, err := lister.Keys()
	require.NoError(t, err)
	sort.Strings(keys)
	assert.Equal(t, []string{"ALPHA", "BETA"}, keys)
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i, c := range s {
		if c == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
