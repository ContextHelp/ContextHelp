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
)

// The container's config files, as the root Dockerfile and compose
// pass them to serve: the image's /app/config.yaml, then the
// operator's auth secret.
var (
	dockerBaseConfig  = filepath.Join("..", "..", "..", "docker", "config.docker.yaml")
	dockerAuthExample = filepath.Join("..", "..", "..", "docker", "dpkms-auth.example.yaml")
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

// The image's config sets server.access: public, which needs server.auth;
// on its own it cannot start serve.
func TestDockerConfig_AloneRefusesToServe(t *testing.T) {
	cfg, err := loadDockerConfig(t, dockerBaseConfig)
	if err != nil {
		t.Fatalf("load %s: %v", dockerBaseConfig, err)
	}
	if _, _, err := ResolveInboundAuth(cfg, false); err == nil {
		t.Fatal("public docker config without server.auth passed serve's auth check")
	}
}

// The example auth secret ships a placeholder token: anyone who reads
// the repo knows it. Copied unedited, it must stop serve at startup and
// tell the operator to generate a real token.
func TestDockerConfig_ExampleAuthSecretRefusesToServe(t *testing.T) {
	cfg, err := loadDockerConfig(t, dockerBaseConfig, dockerAuthExample)
	if err != nil {
		t.Fatalf("load docker config + example auth secret: %v", err)
	}
	_, _, err = ResolveInboundAuth(cfg, false)
	if err == nil {
		t.Fatal("example auth secret with its REPLACE_ME token passed serve's auth check")
	}
	for _, want := range []string{"[0]", "REPLACE_ME", "openssl rand -hex 32"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

// With every placeholder in the example replaced by a generated token,
// as its header tells the operator, and a bus token in env, the
// container config passes every check serve runs before binding a port.
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

	cfg, err := loadDockerConfig(t, dockerBaseConfig, secret)
	if err != nil {
		t.Fatalf("load docker config + auth secret: %v", err)
	}
	access, provider, err := ResolveInboundAuth(cfg, false)
	if err != nil {
		t.Fatalf("ResolveInboundAuth: %v", err)
	}
	if access != config.AccessPublic {
		t.Errorf("access = %q, want public", access)
	}
	if provider == nil {
		t.Fatal("no inbound auth provider")
	}

	// Compose passes DPKMS_BUS_TOKEN; serve refuses to start without it.
	t.Setenv("BUS_TOKEN", "")
	t.Setenv("DPKMS_BUS_TOKEN", "test-bus-token")
	if _, ok := kitbus.AuthFromEnv("DPKMS_BUS_TOKEN", "BUS_TOKEN"); !ok {
		t.Fatal("DPKMS_BUS_TOKEN set but serve's bus auth does not read it")
	}
}

// A --config path that does not exist fails the load: an image started
// without the auth secret mounted stops at config, not later.
func TestDockerConfig_MissingAuthSecretFailsLoad(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "dpkms-auth.yaml")
	if _, err := loadDockerConfig(t, dockerBaseConfig, missing); err == nil {
		t.Fatalf("load with missing %s succeeded", missing)
	}
}
