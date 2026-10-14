package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// portEnv lists the env vars that set serve's listener ports.
var portEnv = []string{"CH_SERVER_PORT", "CH_GRPC_PORT", "CH_COOKIE_BRIDGE_PORT"}

// loadPorts loads the dpkms config against an empty XDG tree, with body
// (when non-empty) as a -c config file and overrides as -c key=value
// pairs. Call clearPortEnv first, then set the env vars under test.
func loadPorts(t *testing.T, body string, overrides map[string]any) ServerConfig {
	t.Helper()
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv(EnvConfigPath, "")
	var paths []string
	if body != "" {
		path := filepath.Join(xdg, "ports.yaml")
		require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
		paths = append(paths, path)
	}
	cfg, err := LoadWithOverrides("dpkms", "", paths, overrides)
	require.NoError(t, err)
	return cfg.Server
}

func clearPortEnv(t *testing.T) {
	t.Helper()
	for _, k := range portEnv {
		t.Setenv(k, "")
	}
}

type portsWant struct {
	http, grpc, cookie          int
	httpSet, grpcSet, cookieSet bool
}

func assertPorts(t *testing.T, s ServerConfig, want portsWant) {
	t.Helper()
	got := portsWant{
		s.Port, s.GRPCPort, s.CookieBridgePort,
		s.PortSet(), s.GRPCPortSet(), s.CookieBridgePortSet(),
	}
	assert.Equal(t, want, got, "ports (http, grpc, cookie bridge) and whether each was set")
}

// With nothing configured the listener ports are the built-in defaults,
// and none of them counts as set, so serve may move off a busy one.
func TestLoad_ServerPortsDefaultToBuiltIns(t *testing.T) {
	clearPortEnv(t)
	assertPorts(t, loadPorts(t, "", nil), portsWant{
		http: DefaultServerPort, grpc: DefaultGRPCPort, cookie: DefaultCookieBridgePort,
	})
	assert.Equal(t, 8080, DefaultServerPort)
	assert.Equal(t, 9090, DefaultGRPCPort)
	assert.Equal(t, 9377, DefaultCookieBridgePort, "the browser extension connects to 9377")
}

// Ports in a config file are operator-set.
func TestLoad_ServerPortsFromFileAreSet(t *testing.T) {
	clearPortEnv(t)
	s := loadPorts(t, "server:\n  port: 18431\n  grpc_port: 18432\n  cookie_bridge_port: 18433\n", nil)
	assertPorts(t, s, portsWant{18431, 18432, 18433, true, true, true})
}

// A file that names the default value has still set it: the operator
// asked for that port, so serve must not drift off it.
func TestLoad_ServerPortsSetToDefaultValueAreSet(t *testing.T) {
	clearPortEnv(t)
	s := loadPorts(t, "server:\n  port: 8080\n", nil)
	assertPorts(t, s, portsWant{
		http: 8080, grpc: DefaultGRPCPort, cookie: DefaultCookieBridgePort,
		httpSet: true,
	})
}

// Port 0 in a file is a set value (OS-assigned port), not "unset".
func TestLoad_ServerPortsZeroIsSet(t *testing.T) {
	clearPortEnv(t)
	s := loadPorts(t, "server:\n  port: 0\n  grpc_port: 0\n  cookie_bridge_port: 0\n", nil)
	assertPorts(t, s, portsWant{0, 0, 0, true, true, true})
}

// CH_* env vars beat the file, and set a port the file leaves alone.
func TestLoad_ServerPortsEnvOverridesFile(t *testing.T) {
	clearPortEnv(t)
	t.Setenv("CH_SERVER_PORT", "18441")
	t.Setenv("CH_COOKIE_BRIDGE_PORT", "18443")
	s := loadPorts(t, "server:\n  port: 18431\n  cookie_bridge_port: 18433\n", nil)
	assertPorts(t, s, portsWant{
		http: 18441, grpc: DefaultGRPCPort, cookie: 18443,
		httpSet: true, cookieSet: true,
	})

	t.Setenv("CH_GRPC_PORT", "18442")
	s = loadPorts(t, "", nil)
	assertPorts(t, s, portsWant{
		http: 18441, grpc: 18442, cookie: 18443,
		httpSet: true, grpcSet: true, cookieSet: true,
	})
}

// -c server.cookie_bridge_port=N sets the port like the file does.
func TestLoad_ServerPortsFromOverrideAreSet(t *testing.T) {
	clearPortEnv(t)
	s := loadPorts(t, "", map[string]any{
		"server": map[string]any{"cookie_bridge_port": 18453},
	})
	assertPorts(t, s, portsWant{
		http: DefaultServerPort, grpc: DefaultGRPCPort, cookie: 18453,
		cookieSet: true,
	})
}
