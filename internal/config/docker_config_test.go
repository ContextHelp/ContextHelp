package config

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/pelletier/go-toml/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// dockerConfigPath is the config both container images copy to
// /app/config.yaml and serve with --config.
var dockerConfigPath = filepath.Join("..", "..", "docker", "config.docker.yaml")

// The container's config must land in the typed Config: a key Load does
// not read leaves serve on its built-in default, silently.
func TestDockerConfig_LandsInTypedConfig(t *testing.T) {
	clearPortEnv(t)
	// The images and compose set these; clear them so every value
	// asserted below comes from the file.
	for _, k := range []string{"CH_PUBLIC", EnvDPKMSWorkers, "DPKMS_MAX_RETRIES", "CH_STORAGE_TYPE", EnvDataDir} {
		t.Setenv(k, "")
	}
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv(EnvConfigPath, "")

	cfg, err := LoadWithOverrides("dpkms", "", []string{dockerConfigPath}, nil)
	require.NoError(t, err)

	// HTTP and gRPC are published by the images and compose; the cookie
	// bridge binds loopback only, so the file leaves it alone.
	assertPorts(t, cfg.Server, portsWant{
		http: 8080, grpc: 9090, cookie: DefaultCookieBridgePort,
		httpSet: true, grpcSet: true,
	})
	// Published ports are only reachable when serve binds all interfaces,
	// which it does for non-private instances.
	assert.Equal(t, AccessPublic, cfg.Server.EffectiveAccess(), "server.access")
	assert.Equal(t, AccessPublic, cfg.Server.Access, "server.access set explicitly, not via the server.public shorthand")
	assert.Equal(t, 4, cfg.Server.Workers, "server.workers")
	assert.Equal(t, 3, cfg.Jobs.MaxRetries, "jobs.max_retries")
	assert.Equal(t, "sqlite", cfg.Storage.Type, "storage.type")
	assert.Equal(t, "/data/bookmarks.db", cfg.Storage.Path, "storage.path")
	assert.False(t, cfg.Privacy.Telemetry, "privacy.telemetry")
}

// Every key in the container's config must be one Config declares. Load
// itself ignores unknown keys, so decode strictly here.
func TestDockerConfig_HasNoUnknownKeys(t *testing.T) {
	data, err := os.ReadFile(dockerConfigPath)
	require.NoError(t, err)
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var cfg Config
	require.NoError(t, dec.Decode(&cfg), "%s sets a key config.Load never reads", dockerConfigPath)
}

// The dev container builds with air, outside the Makefile. Its build must
// carry the Makefile's tags: without fts5, mattn/go-sqlite3 has no FTS5
// module and serve fails at the first FTS5 migration.
func TestDockerAirBuild_UsesMakefileBuildTags(t *testing.T) {
	makefile, err := os.ReadFile(filepath.Join("..", "..", "Makefile"))
	require.NoError(t, err)
	m := regexp.MustCompile(`(?m)^BUILD_TAGS\s*:=\s*(.+?)\s*$`).FindSubmatch(makefile)
	require.NotNil(t, m, "Makefile declares no BUILD_TAGS")
	tags := string(m[1])

	data, err := os.ReadFile(filepath.Join("..", "..", "docker", "air.toml"))
	require.NoError(t, err)
	var air struct {
		Build struct {
			Cmd string `toml:"cmd"`
		} `toml:"build"`
	}
	require.NoError(t, toml.Unmarshal(data, &air))
	assert.Contains(t, air.Build.Cmd, tags, "docker/air.toml build.cmd")
	assert.Contains(t, air.Build.Cmd, "CGO_ENABLED=1", "docker/air.toml build.cmd")
}
