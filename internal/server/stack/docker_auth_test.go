package stack

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	kitbus "hop.top/kit/go/runtime/bus"

	"github.com/ideacrafterslabs/ctxt/internal/config"
	"github.com/ideacrafterslabs/ctxt/internal/storageutil"
)

// The container's config files, as the root Dockerfile and compose
// pass them to serve: the image's /app/config.yaml, then the
// operator's auth and local secrets.
var (
	dockerBaseConfig   = filepath.Join("..", "..", "..", "docker", "config.docker.yaml")
	dockerAuthExample  = filepath.Join("..", "..", "..", "docker", "dpkms-auth.example.yaml")
	dockerLocalExample = filepath.Join("..", "..", "..", "docker", "dpkms-local.example.yaml")
)

// loadDockerConfig loads paths the way dpkms serve does (repeated
// --config through config.Bootstrap), with no ambient config or env.
func loadDockerConfig(t *testing.T, paths ...string) (*config.Config, error) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(config.EnvConfigPath, "")
	t.Setenv("CH_PUBLIC", "")
	cfg, _, err := config.Bootstrap("dpkms", paths, nil, nil)
	return cfg, err
}

// The image's config sets server.access: public, which mints sign-in
// links and so needs server.public_url from the operator; on its own it
// cannot start serve.
func TestDockerConfig_AloneRefusesToServe(t *testing.T) {
	cfg, err := loadDockerConfig(t, dockerBaseConfig)
	if err != nil {
		t.Fatalf("load %s: %v", dockerBaseConfig, err)
	}
	if _, err := ResolveInboundAuth(cfg, false); err == nil {
		t.Fatal("public docker config without the operator's auth file passed serve's auth check")
	}
}

// The example auth secret ships a placeholder hash: anyone who reads
// the repo knows it. Copied unedited, it must stop serve at startup and
// tell the operator how to get a real one.
func TestDockerConfig_ExampleAuthSecretRefusesToServe(t *testing.T) {
	cfg, err := loadDockerConfig(t, dockerBaseConfig, dockerAuthExample, dockerLocalExample)
	if err != nil {
		t.Fatalf("load docker config + example secrets: %v", err)
	}
	_, err = ResolveInboundAuth(cfg, false)
	if err == nil {
		t.Fatal("example auth secret with its REPLACE_ME hash passed serve's auth check")
	}
	for _, want := range []string{"keys[0].hash", "REPLACE_ME", "dpkms auth key create"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

// With the placeholder in the auth example replaced by a key's SHA-256,
// as its header tells the operator, the local example copied as is, and
// a bus token in env, the container config passes every check serve
// runs before binding a port, and the stack builds over an empty
// registry: the declared owner key is the owner credential.
func TestDockerConfig_WithAuthSecretPassesServeStartup(t *testing.T) {
	example, err := os.ReadFile(dockerAuthExample)
	if err != nil {
		t.Fatal(err)
	}
	placeholder := regexp.MustCompile(`REPLACE_ME\w*`)
	if !placeholder.Match(example) {
		t.Fatalf("%s has no REPLACE_ME placeholder to fill", dockerAuthExample)
	}
	filled := placeholder.ReplaceAllFunc(example, func([]byte) []byte {
		buf := make([]byte, 32) // openssl rand -hex 32
		if _, err := rand.Read(buf); err != nil {
			t.Fatal(err)
		}
		return []byte(hex.EncodeToString(buf))
	})
	secret := filepath.Join(t.TempDir(), "dpkms-auth.yaml")
	if err := os.WriteFile(secret, filled, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadDockerConfig(t, dockerBaseConfig, secret, dockerLocalExample)
	if err != nil {
		t.Fatalf("load docker config + operator secrets: %v", err)
	}
	access, err := ResolveInboundAuth(cfg, false)
	if err != nil {
		t.Fatalf("ResolveInboundAuth: %v", err)
	}
	if access != config.AccessPublic {
		t.Errorf("access = %q, want public", access)
	}
	stubProviders(cfg)
	bus := kitbus.New()
	st, err := Build(Inputs{Config: cfg, Driver: storageutil.NewTestDriver(t), Access: access, PolicyBus: bus})
	if err != nil {
		t.Fatalf("Build over a fresh registry with the declared owner key: %v", err)
	}
	t.Cleanup(st.Close)

	// Compose passes DPKMS_BUS_TOKEN; serve refuses to start without it.
	t.Setenv("BUS_TOKEN", "")
	t.Setenv("DPKMS_BUS_TOKEN", "test-bus-token")
	if _, ok := kitbus.AuthFromEnv("DPKMS_BUS_TOKEN", "BUS_TOKEN"); !ok {
		t.Fatal("DPKMS_BUS_TOKEN set but serve's bus auth does not read it")
	}
}

// A --config path that does not exist fails the load: an image started
// without either operator secret mounted stops at config, not later.
func TestDockerConfig_MissingOperatorSecretFailsLoad(t *testing.T) {
	missingAuth := filepath.Join(t.TempDir(), "dpkms-auth.yaml")
	if _, err := loadDockerConfig(t, dockerBaseConfig, missingAuth, dockerLocalExample); err == nil {
		t.Fatalf("load with missing %s succeeded", missingAuth)
	}
	missingLocal := filepath.Join(t.TempDir(), "dpkms-local.yaml")
	if _, err := loadDockerConfig(t, dockerBaseConfig, dockerAuthExample, missingLocal); err == nil {
		t.Fatalf("load with missing %s succeeded", missingLocal)
	}
}
