//go:build cursor_e2e

package cursor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/cursor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCursor_FileLocationOverrideViaEnv: setting CTXT_CURSOR_FILE redirects
// reads/writes to that path.
func TestCursor_FileLocationOverrideViaEnv(t *testing.T) {
	dir := t.TempDir()
	custom := filepath.Join(dir, "alt", "cursors.yaml")
	t.Setenv(cursor.EnvFile, custom)

	resolved, err := cursor.Path()
	require.NoError(t, err)
	assert.Equal(t, custom, resolved)

	mgr := cursor.NewWithPath(custom)
	_, err = mgr.Advance("feed", time.Now().UTC(), "ko_1", cursor.QuerySnapshot{})
	require.NoError(t, err)

	_, err = os.Stat(custom)
	require.NoError(t, err, "cursor file must materialize at $CTXT_CURSOR_FILE")
}

// TestCursor_FilePermissionsLockedDown: file 0600, dir 0700.
func TestCursor_FilePermissionsLockedDown(t *testing.T) {
	env := newCursorEnv(t)
	_, err := env.mgr.Advance("feed", time.Now().UTC(), "ko_1", cursor.QuerySnapshot{})
	require.NoError(t, err)

	st, err := os.Stat(env.path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), st.Mode().Perm(), "cursor file must be 0600")

	dst, err := os.Stat(filepath.Dir(env.path))
	require.NoError(t, err)
	// Directory may already have been created by t.TempDir() with 0700/0755;
	// the cursor package only enforces 0700 when it has to mkdir. Accept
	// either as long as group/other have no write.
	assert.True(t, dst.Mode().Perm()&0o022 == 0,
		"cursor dir must not be group/other-writable; got %o", dst.Mode().Perm())
}

// TestCursor_SchemaVersionPersisted: file must contain schema_version: 1.
func TestCursor_SchemaVersionPersisted(t *testing.T) {
	env := newCursorEnv(t)
	_, err := env.mgr.Advance("feed", time.Now().UTC(), "ko_1", cursor.QuerySnapshot{})
	require.NoError(t, err)

	data, err := os.ReadFile(env.path) // #nosec G304 -- temp test file
	require.NoError(t, err)
	assert.True(t, strings.Contains(string(data), "schema_version: 1"),
		"cursor file must declare schema_version: 1; got:\n%s", string(data))
}
