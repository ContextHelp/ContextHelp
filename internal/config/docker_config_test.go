package config

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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
	for _, k := range []string{"CH_PUBLIC", EnvDPKMSWorkers, "DPKMS_MAX_RETRIES", "CH_STORAGE_TYPE", EnvDataDir, "CTXT_BLOB_BACKEND"} {
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
	// Everything persistent lives on the /data volume; the file is the
	// one place that says where.
	assert.Equal(t, "/data/dpkms.db", cfg.Storage.Path, "storage.path")
	assert.Equal(t, "local", cfg.Storage.Blob.Backend, "storage.blob.backend")
	assert.Equal(t, "/data/blobs", cfg.Storage.Blob.Local.Path, "storage.blob.local.path")
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

// The container's config file is the single source for its settings:
// compose and the image must not set an env var that overrides a key
// (env beats the file), nor one nothing reads.
func TestDockerRuntime_EnvDoesNotRestateConfig(t *testing.T) {
	denied := map[string]string{EnvDPKMSDataDir: "nothing reads it"}
	for _, b := range envBindings {
		denied[b.Env] = "overrides " + b.Key + " from " + dockerConfigPath
	}

	compose := loadCompose(t)
	for name, svc := range compose.Services {
		for k := range svc.Environment {
			if why, bad := denied[k]; bad {
				t.Errorf("docker-compose.yml service %s sets %s: %s", name, k, why)
			}
		}
	}

	dockerfile, err := os.ReadFile(filepath.Join("..", "..", "Dockerfile"))
	require.NoError(t, err)
	joined := strings.ReplaceAll(string(dockerfile), "\\\n", " ")
	for _, m := range regexp.MustCompile(`(?m)^ENV\s+(.+)$`).FindAllStringSubmatch(joined, -1) {
		for _, tok := range strings.Fields(m[1]) {
			k, _, _ := strings.Cut(tok, "=")
			if why, bad := denied[k]; bad {
				t.Errorf("Dockerfile ENV %s: %s", k, why)
			}
		}
	}
}

// The database and blobs must land on a compose volume, or they vanish
// with the container.
func TestDockerRuntime_StoragePathsOnVolumes(t *testing.T) {
	data, err := os.ReadFile(dockerConfigPath)
	require.NoError(t, err)
	var cfg Config
	require.NoError(t, yaml.Unmarshal(data, &cfg))

	compose := loadCompose(t)
	for _, name := range []string{"dpkms", "dpkms-dev"} {
		svc, ok := compose.Services[name]
		require.True(t, ok, "docker-compose.yml has no service %s", name)
		for key, path := range map[string]string{
			"storage.path":            cfg.Storage.Path,
			"storage.blob.local.path": cfg.Storage.Blob.Local.Path,
		} {
			assert.True(t, onVolume(svc.Volumes, path),
				"service %s: %s %s is on no named volume (%v)", name, key, path, svc.Volumes)
		}
	}
}

// dockerAuthExamplePath is the committed template for the operator's
// auth secret; dockerAuthSecretPath is where serve reads it in the
// container, as a second --config after /app/config.yaml.
var (
	dockerAuthExamplePath = filepath.Join("..", "..", "docker", "dpkms-auth.example.yaml")
	dockerAuthSecretPath  = "/run/secrets/dpkms-auth.yaml"
)

// The auth example must decode into Config and hold server.auth only:
// everything else comes from config.docker.yaml.
func TestDockerAuthExample_HoldsOnlyServerAuth(t *testing.T) {
	data, err := os.ReadFile(dockerAuthExamplePath)
	require.NoError(t, err)
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var cfg Config
	require.NoError(t, dec.Decode(&cfg), "%s sets a key config.Load never reads", dockerAuthExamplePath)

	var raw map[string]map[string]any
	require.NoError(t, yaml.Unmarshal(data, &raw))
	assert.Len(t, raw, 1, "top-level keys")
	assert.Contains(t, raw, "server")
	assert.Len(t, raw["server"], 1, "server.* keys")
	assert.Contains(t, raw["server"], "auth")
}

// Serve reads server.auth from the operator's secret: compose mounts it
// at dockerAuthSecretPath in both dpkms services, and the image's CMD and
// the dev air runner pass that path as a second --config.
func TestDockerRuntime_ServeLoadsAuthSecret(t *testing.T) {
	compose := loadCompose(t)
	secret, ok := compose.Secrets["dpkms-auth"]
	require.True(t, ok, "docker-compose.yml declares no dpkms-auth secret")
	assert.Equal(t, "./docker/dpkms-auth.yaml", secret.File, "dpkms-auth secret file")
	for _, name := range []string{"dpkms", "dpkms-dev"} {
		svc := compose.Services[name]
		var mounted bool
		for _, s := range svc.Secrets {
			if s.Source == "dpkms-auth" && "/run/secrets/"+s.Target == dockerAuthSecretPath {
				mounted = true
			}
		}
		assert.True(t, mounted, "service %s mounts no dpkms-auth secret at %s (%v)", name, dockerAuthSecretPath, svc.Secrets)
	}

	dockerfile, err := os.ReadFile(filepath.Join("..", "..", "Dockerfile"))
	require.NoError(t, err)
	m := regexp.MustCompile(`(?m)^CMD\s+(\[.*\])\s*$`).FindSubmatch(dockerfile)
	require.NotNil(t, m, "Dockerfile has no exec-form CMD")
	var cmd []string
	require.NoError(t, yaml.Unmarshal(m[1], &cmd))
	assert.Equal(t, []string{"serve", "--config", "/app/config.yaml", "--config", dockerAuthSecretPath}, cmd, "Dockerfile CMD")

	data, err := os.ReadFile(filepath.Join("..", "..", "docker", "air.toml"))
	require.NoError(t, err)
	var air struct {
		Build struct {
			Bin string `toml:"bin"`
		} `toml:"build"`
	}
	require.NoError(t, toml.Unmarshal(data, &air))
	assert.Contains(t, air.Build.Bin, "--config docker/config.docker.yaml --config "+dockerAuthSecretPath, "docker/air.toml build.bin")
}

// Serve refuses to start without a bus token. Compose must pass one to
// both dpkms services and fail at `up` when the operator has not set it,
// rather than start a container that exits.
func TestDockerRuntime_BusTokenRequired(t *testing.T) {
	compose := loadCompose(t)
	for _, name := range []string{"dpkms", "dpkms-dev"} {
		v, ok := compose.Services[name].Environment["DPKMS_BUS_TOKEN"]
		if assert.True(t, ok, "service %s sets no DPKMS_BUS_TOKEN", name) {
			assert.True(t, strings.HasPrefix(v, "${DPKMS_BUS_TOKEN:?"), "service %s DPKMS_BUS_TOKEN = %q, want required interpolation", name, v)
		}
	}
}

// The dev container installs air at start-up, outside mise; an unpinned
// install changes the dev build whenever air releases.
func TestDockerDev_PinsAir(t *testing.T) {
	cmd := loadCompose(t).Services["dpkms-dev"].Command
	assert.Regexp(t, `go install github\.com/air-verse/air@v\d+\.\d+\.\d+(\s|$)`, cmd,
		"dpkms-dev must go install an exact air version")
}

// dockerEnvExamples are the compose env files the docs tell operators to
// copy, and the flag each docker compose invocation passes to read them.
var dockerEnvExamples = map[string]string{
	"docker/.env.dev.example":  "--env-file docker/.env.dev",
	"docker/.env.prod.example": "--env-file docker/.env.prod",
}

// Compose reads an --env-file for interpolation only: a key it sets
// reaches no container unless docker-compose.yml interpolates it. Every
// key the examples set or show must be one docker-compose.yml uses.
func TestDockerEnvExamples_OnlyComposeInterpolatedKeys(t *testing.T) {
	compose, err := os.ReadFile(filepath.Join("..", "..", "docker-compose.yml"))
	require.NoError(t, err)
	keyLine := regexp.MustCompile(`(?m)^#?\s*([A-Z][A-Z0-9_]*)=`)
	for example := range dockerEnvExamples {
		data, err := os.ReadFile(filepath.Join("..", "..", example))
		require.NoError(t, err)
		keys := keyLine.FindAllSubmatch(data, -1)
		require.NotEmpty(t, keys, "%s sets no key", example)
		for _, k := range keys {
			assert.Contains(t, string(compose), "${"+string(k[1]), "%s sets %s, which docker-compose.yml never interpolates", example, k[1])
		}
	}
}

// Compose interpolates the whole file for every command, and
// DPKMS_BUS_TOKEN is required, so each docker compose invocation the
// docs and Makefile show must pass the env file the examples become.
func TestDockerDocs_ComposeCommandsPassEnvFile(t *testing.T) {
	invocation := regexp.MustCompile(`(?m)^.*\bdocker compose\s+[-a-z].*$`)
	envFile := regexp.MustCompile(`--env-file (docker/\.env\.(dev|prod)|\$\(DOCKER_ENV_FILE\))`)
	for _, f := range []string{"README.md", "Makefile", "docs/deployment/docker.md", "docs/deployment/docker-dev.md", "docs/operations/pre-alpha-gate.md"} {
		data, err := os.ReadFile(filepath.Join("..", "..", f))
		require.NoError(t, err)
		for _, line := range invocation.FindAllString(string(data), -1) {
			assert.Regexp(t, envFile, line, "%s: docker compose without the env file", f)
		}
	}
}

type composeFile struct {
	Services map[string]struct {
		Environment map[string]string `yaml:"environment"`
		Volumes     []string          `yaml:"volumes"`
		Secrets     []struct {
			Source string `yaml:"source"`
			Target string `yaml:"target"`
		} `yaml:"secrets"`
		Command string `yaml:"command"`
	} `yaml:"services"`
	Secrets map[string]struct {
		File string `yaml:"file"`
	} `yaml:"secrets"`
}

func loadCompose(t *testing.T) composeFile {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "docker-compose.yml"))
	require.NoError(t, err)
	var c composeFile
	require.NoError(t, yaml.Unmarshal(data, &c))
	return c
}

// onVolume reports whether path sits under the target of a named-volume
// mount ("name:/target"); bind mounts (./src:/dst) do not persist data.
func onVolume(volumes []string, path string) bool {
	for _, v := range volumes {
		src, target, ok := strings.Cut(v, ":")
		if !ok || strings.HasPrefix(src, ".") || strings.HasPrefix(src, "/") {
			continue
		}
		target, _, _ = strings.Cut(target, ":")
		if path == target || strings.HasPrefix(path, strings.TrimSuffix(target, "/")+"/") {
			return true
		}
	}
	return false
}
