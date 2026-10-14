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

// dockerS3EnvSettings are the env vars bound to the non-secret
// storage.blob.s3.* keys. Compose passes each one from the env file to
// both dpkms services; unset, it is empty, which config reads as not set.
var dockerS3EnvSettings = []string{
	"CTXT_BLOB_S3_ENDPOINT",
	"CTXT_BLOB_S3_REGION",
	"CTXT_BLOB_S3_BUCKET",
	"CTXT_BLOB_S3_PREFIX",
	"CTXT_BLOB_S3_USE_PATH_STYLE",
}

// dockerS3EnvCredentials returns the env vars bound to the S3
// credentials (storage.blob.s3.access_key / secret_key). They stay in
// docker/dpkms-local.yaml: compose must never pass them.
func dockerS3EnvCredentials(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, b := range envBindings {
		if isS3CredentialKey(b.Key) {
			out = append(out, b.Env)
		}
	}
	require.Len(t, out, 2, "env bindings for the S3 access and secret keys")
	return out
}

func isS3CredentialKey(key string) bool {
	return key == "storage.blob.s3.access_key" || key == "storage.blob.s3.secret_key"
}

// The S3 allowlist is exactly the non-secret storage.blob.s3.* bindings:
// a new S3 binding must be sorted into the settings or the credentials.
func TestDockerS3EnvSettings_AreTheNonSecretS3Bindings(t *testing.T) {
	var settings []string
	for _, b := range envBindings {
		if strings.HasPrefix(b.Key, "storage.blob.s3.") && !isS3CredentialKey(b.Key) {
			settings = append(settings, b.Env)
		}
	}
	assert.ElementsMatch(t, dockerS3EnvSettings, settings, "non-secret storage.blob.s3.* env bindings")
}

// The container's config file is the single source for its settings:
// compose and the image must not set an env var that overrides a key
// (env beats the file), nor one nothing reads. The one exception is
// compose's pass-through of the non-secret S3 settings from the env
// file; the S3 credentials never reach compose.
func TestDockerRuntime_EnvDoesNotRestateConfig(t *testing.T) {
	denied := map[string]string{EnvDPKMSDataDir: "nothing reads it"}
	for _, b := range envBindings {
		denied[b.Env] = "overrides " + b.Key + " from " + dockerConfigPath
	}
	composeAllowed := map[string]bool{}
	for _, k := range dockerS3EnvSettings {
		composeAllowed[k] = true
	}

	compose := loadCompose(t)
	for name, svc := range compose.Services {
		for k := range svc.Environment {
			if why, bad := denied[k]; bad && !composeAllowed[k] {
				t.Errorf("docker-compose.yml service %s sets %s: %s", name, k, why)
			}
		}
	}

	// The credentials stay in docker/dpkms-local.yaml: compose neither
	// sets nor interpolates them anywhere.
	raw, err := os.ReadFile(filepath.Join("..", "..", "docker-compose.yml"))
	require.NoError(t, err)
	for _, k := range dockerS3EnvCredentials(t) {
		assert.NotContains(t, string(raw), k, "docker-compose.yml must never pass the S3 credential %s", k)
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

// dockerAuthExamplePath and dockerLocalExamplePath are the committed
// templates for the operator's secrets; the *SecretPath vars are where
// serve reads them in the container, as --config layers after
// /app/config.yaml.
var (
	dockerAuthExamplePath  = filepath.Join("..", "..", "docker", "dpkms-auth.example.yaml")
	dockerAuthSecretPath   = "/run/secrets/dpkms-auth.yaml"
	dockerLocalExamplePath = filepath.Join("..", "..", "docker", "dpkms-local.example.yaml")
	dockerLocalSecretPath  = "/run/secrets/dpkms-local.yaml"
)

// dockerOperatorSecrets maps each compose secret serve loads to the
// file it is read from on the host and its path in the container, in
// --config order.
var dockerOperatorSecrets = []struct{ name, file, target string }{
	{"dpkms-auth", "./docker/dpkms-auth.yaml", dockerAuthSecretPath},
	{"dpkms-local", "./docker/dpkms-local.yaml", dockerLocalSecretPath},
}

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

// Serve reads the operator's secrets: compose mounts each one at its
// /run/secrets path in both dpkms services, and the image's CMD and the
// dev air runner pass those paths as --config layers after the image's
// config.
func TestDockerRuntime_ServeLoadsOperatorSecrets(t *testing.T) {
	compose := loadCompose(t)
	wantArgs := []string{"serve", "--config", "/app/config.yaml"}
	airArgs := "--config docker/config.docker.yaml"
	for _, sec := range dockerOperatorSecrets {
		secret, ok := compose.Secrets[sec.name]
		require.True(t, ok, "docker-compose.yml declares no %s secret", sec.name)
		assert.Equal(t, sec.file, secret.File, "%s secret file", sec.name)
		for _, name := range []string{"dpkms", "dpkms-dev"} {
			svc := compose.Services[name]
			var mounted bool
			for _, s := range svc.Secrets {
				if s.Source == sec.name && "/run/secrets/"+s.Target == sec.target {
					mounted = true
				}
			}
			assert.True(t, mounted, "service %s mounts no %s secret at %s (%v)", name, sec.name, sec.target, svc.Secrets)
		}
		wantArgs = append(wantArgs, "--config", sec.target)
		airArgs += " --config " + sec.target
	}

	dockerfile, err := os.ReadFile(filepath.Join("..", "..", "Dockerfile"))
	require.NoError(t, err)
	m := regexp.MustCompile(`(?m)^CMD\s+(\[.*\])\s*$`).FindSubmatch(dockerfile)
	require.NotNil(t, m, "Dockerfile has no exec-form CMD")
	var cmd []string
	require.NoError(t, yaml.Unmarshal(m[1], &cmd))
	assert.Equal(t, wantArgs, cmd, "Dockerfile CMD")

	data, err := os.ReadFile(filepath.Join("..", "..", "docker", "air.toml"))
	require.NoError(t, err)
	var air struct {
		Build struct {
			Bin string `toml:"bin"`
		} `toml:"build"`
	}
	require.NoError(t, toml.Unmarshal(data, &air))
	assert.Contains(t, air.Build.Bin, airArgs, "docker/air.toml build.bin")
}

// uncommentLocalExample returns the local example with its commented
// settings enabled: a line of '#', an even indent and a key is YAML;
// every other comment is prose.
func uncommentLocalExample(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(dockerLocalExamplePath)
	require.NoError(t, err)
	return regexp.MustCompile(`(?m)^#((?:  )*[a-z_][a-z0-9_]*:.*)$`).ReplaceAll(data, []byte("$1"))
}

// As copied, the local example sets nothing: compose requires the file,
// so the copy must leave the image's config as it is. Its commented
// settings must be keys Config declares, under storage.blob and
// providers only.
func TestDockerLocalExample_HoldsOnlyBlobAndProviders(t *testing.T) {
	data, err := os.ReadFile(dockerLocalExamplePath)
	require.NoError(t, err)
	var asCopied map[string]any
	require.NoError(t, yaml.Unmarshal(data, &asCopied))
	assert.Empty(t, asCopied, "%s sets keys as copied", dockerLocalExamplePath)

	enabled := uncommentLocalExample(t)
	dec := yaml.NewDecoder(bytes.NewReader(enabled))
	dec.KnownFields(true)
	var cfg Config
	require.NoError(t, dec.Decode(&cfg), "%s comments out a key config.Load never reads", dockerLocalExamplePath)

	var raw map[string]map[string]any
	require.NoError(t, yaml.Unmarshal(enabled, &raw))
	assert.ElementsMatch(t, []string{"storage", "providers"}, mapKeys(raw), "top-level keys")
	assert.ElementsMatch(t, []string{"blob"}, mapKeys(raw["storage"]), "storage.* keys")
}

// Layered on the image's config the way serve loads it, the local
// example's settings land in the typed Config and leave the image's
// other storage keys alone; as copied, it changes nothing.
func TestDockerLocalExample_LandsInTypedConfig(t *testing.T) {
	clearPortEnv(t)
	for _, b := range envBindings {
		t.Setenv(b.Env, "")
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(EnvConfigPath, "")

	base, err := LoadWithOverrides("dpkms", "", []string{dockerConfigPath}, nil)
	require.NoError(t, err)

	asCopied, err := LoadWithOverrides("dpkms", "", []string{dockerConfigPath, dockerLocalExamplePath}, nil)
	require.NoError(t, err)
	assert.Equal(t, base.Storage, asCopied.Storage, "storage with the example as copied")
	assert.Equal(t, base.Providers, asCopied.Providers, "providers with the example as copied")

	enabledPath := filepath.Join(t.TempDir(), "dpkms-local.yaml")
	require.NoError(t, os.WriteFile(enabledPath, uncommentLocalExample(t), 0o600))
	cfg, err := LoadWithOverrides("dpkms", "", []string{dockerConfigPath, enabledPath}, nil)
	require.NoError(t, err)
	assert.Equal(t, "s3", cfg.Storage.Blob.Backend, "storage.blob.backend")
	assert.NotEmpty(t, cfg.Storage.Blob.S3.Bucket, "storage.blob.s3.bucket")
	assert.NotEmpty(t, cfg.Storage.Blob.S3.SecretKey, "storage.blob.s3.secret_key")
	assert.NotEmpty(t, cfg.Providers.LLM.Backend, "providers.llm.backend")
	assert.NotEmpty(t, cfg.Providers.Embedding.Endpoint, "providers.embedding.endpoint")
	assert.Equal(t, base.Storage.Path, cfg.Storage.Path, "storage.path stays the image's")
	assert.Equal(t, base.Storage.Type, cfg.Storage.Type, "storage.type stays the image's")
}

// dockerProviderKeys are the env vars the provider factory reads API
// keys from. They have no config key, so compose passes each one from
// the env file to both dpkms services; unset, it is empty.
var dockerProviderKeys = []string{"OPENAI_API_KEY", "ANTHROPIC_API_KEY", "GEMINI_API_KEY", "OPENROUTER_API_KEY"}

func TestDockerRuntime_ProviderKeysFromEnvFile(t *testing.T) {
	compose := loadCompose(t)
	for _, name := range []string{"dpkms", "dpkms-dev"} {
		env := compose.Services[name].Environment
		for _, k := range dockerProviderKeys {
			assert.Equal(t, "${"+k+":-}", env[k], "service %s %s", name, k)
		}
	}
	for example := range dockerEnvExamples {
		data, err := os.ReadFile(filepath.Join("..", "..", example))
		require.NoError(t, err)
		for _, k := range dockerProviderKeys {
			assert.Regexp(t, `(?m)^#?\s*`+k+`=`, string(data), "%s does not show %s", example, k)
		}
	}
}

// Compose passes the non-secret S3 settings from the env file to both
// dpkms services, empty when unset; the env examples show each one and
// never the credentials, which belong in docker/dpkms-local.yaml.
func TestDockerRuntime_S3SettingsFromEnvFile(t *testing.T) {
	compose := loadCompose(t)
	for _, name := range []string{"dpkms", "dpkms-dev"} {
		env := compose.Services[name].Environment
		for _, k := range dockerS3EnvSettings {
			assert.Equal(t, "${"+k+":-}", env[k], "service %s %s", name, k)
		}
	}
	for example := range dockerEnvExamples {
		data, err := os.ReadFile(filepath.Join("..", "..", example))
		require.NoError(t, err)
		for _, k := range dockerS3EnvSettings {
			assert.Regexp(t, `(?m)^#?\s*`+k+`=`, string(data), "%s does not show %s", example, k)
		}
		for _, k := range dockerS3EnvCredentials(t) {
			assert.NotContains(t, string(data), k, "%s shows the S3 credential %s", example, k)
		}
	}
}

// Compose passes an unset S3 setting as an empty value. Empty must read
// as not set, so the operator's file keeps its value; a set one wins
// over the file.
func TestDockerRuntime_S3SettingsEnvOverFile(t *testing.T) {
	clearPortEnv(t)
	for _, b := range envBindings {
		t.Setenv(b.Env, "")
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(EnvConfigPath, "")

	local := filepath.Join(t.TempDir(), "dpkms-local.yaml")
	require.NoError(t, os.WriteFile(local, []byte(`storage:
  blob:
    backend: s3
    s3:
      endpoint: https://file.example.com
      region: file-region
      bucket: file-bucket
      prefix: file/
      use_path_style: true
      access_key: file-access
      secret_key: file-secret
`), 0o600))
	layers := []string{dockerConfigPath, local}

	// Every pass-through set, but empty, as compose does when the env
	// file leaves them out.
	for _, k := range dockerS3EnvSettings {
		t.Setenv(k, "")
	}
	cfg, err := LoadWithOverrides("dpkms", "", layers, nil)
	require.NoError(t, err)
	assert.Equal(t, BlobS3Config{
		Endpoint: "https://file.example.com", Region: "file-region", Bucket: "file-bucket",
		Prefix: "file/", UsePathStyle: true, AccessKey: "file-access", SecretKey: "file-secret",
		PresignExpiry: cfg.Storage.Blob.S3.PresignExpiry, MaxRetries: cfg.Storage.Blob.S3.MaxRetries,
	}, cfg.Storage.Blob.S3, "empty pass-through keeps the file's values")

	t.Setenv("CTXT_BLOB_S3_ENDPOINT", "https://env.example.com")
	t.Setenv("CTXT_BLOB_S3_REGION", "env-region")
	t.Setenv("CTXT_BLOB_S3_BUCKET", "env-bucket")
	t.Setenv("CTXT_BLOB_S3_PREFIX", "env/")
	t.Setenv("CTXT_BLOB_S3_USE_PATH_STYLE", "false")
	cfg, err = LoadWithOverrides("dpkms", "", layers, nil)
	require.NoError(t, err)
	s3 := cfg.Storage.Blob.S3
	assert.Equal(t, "https://env.example.com", s3.Endpoint, "endpoint")
	assert.Equal(t, "env-region", s3.Region, "region")
	assert.Equal(t, "env-bucket", s3.Bucket, "bucket")
	assert.Equal(t, "env/", s3.Prefix, "prefix")
	assert.False(t, s3.UsePathStyle, "use_path_style")
	assert.Equal(t, "file-access", s3.AccessKey, "access_key stays the file's")
	assert.Equal(t, "file-secret", s3.SecretKey, "secret_key stays the file's")
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

func mapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
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
