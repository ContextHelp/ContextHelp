package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	kitconfig "hop.top/kit/go/core/config"
)

// storage.migrate_snapshot defaults to true, and a file or a -c
// override can turn it off.
func TestLoad_StorageMigrateSnapshot(t *testing.T) {
	t.Run("default on", func(t *testing.T) {
		hermeticDataDir(t)
		cfg, err := Load("dpkms", "")
		require.NoError(t, err)
		assert.True(t, cfg.Storage.MigrateSnapshot)
	})
	t.Run("file turns it off", func(t *testing.T) {
		root := hermeticDataDir(t)
		writeUserConfig(t, root, "dpkms", "storage:\n  migrate_snapshot: false\n")
		cfg, err := Load("dpkms", "")
		require.NoError(t, err)
		assert.False(t, cfg.Storage.MigrateSnapshot)
	})
	t.Run("-c override turns it off", func(t *testing.T) {
		hermeticDataDir(t)
		// The overrides -c storage.migrate_snapshot=false hands Load.
		_, overrides, err := kitconfig.ParseConfigArgs([]string{"storage.migrate_snapshot=false"})
		require.NoError(t, err)
		cfg, err := LoadWithOverrides("dpkms", "", nil, overrides)
		require.NoError(t, err)
		assert.False(t, cfg.Storage.MigrateSnapshot)
	})
}
