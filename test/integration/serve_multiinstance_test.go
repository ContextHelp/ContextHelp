//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ideacrafterslabs/ctxt/internal/pidfile"
)

// TestMultiInstanceAutoPort is a regression test for the bug where
// findFreePort was only called for gRPC, not HTTP. The second daemon
// instance would fail to bind port 8080 and silently exit instead of
// auto-assigning a free port.
//
// The test builds the real dpkms binary, starts two instances that both
// request the default ports, and verifies they both come up on distinct ports.
func TestMultiInstanceAutoPort(t *testing.T) {
	bin := buildDpkmsBinary(t)

	// XDG_DATA_HOME isolates pidfiles without overriding storage.path
	// (CTXT_DATA_DIR would do both since it binds to storage.path in viper).
	dataDir := t.TempDir()
	env := append(os.Environ(), "XDG_DATA_HOME="+dataDir)

	// Each instance gets its own db to avoid SQLite single-writer contention.
	cfg1 := writeServeConfig(t, dataDir, "1")
	cfg2 := writeServeConfig(t, dataDir, "2")

	// Both instances request the same default HTTP (8080) and gRPC (9090) ports.
	// The second must auto-assign free alternatives.
	p1 := startServeProcess(t, bin, cfg1, env)
	defer p1.Process.Kill()

	p2 := startServeProcess(t, bin, cfg2, env)
	defer p2.Process.Kill()

	runDir := filepath.Join(dataDir, "contexthelp", "run")
	instances := waitForPidfiles(t, runDir, 2, 15*time.Second)

	if instances[0].Port == instances[1].Port {
		t.Errorf("both instances share HTTP port %d — auto-assign broken", instances[0].Port)
	}
	if instances[0].GRPCPort == instances[1].GRPCPort {
		t.Errorf("both instances share gRPC port %d — auto-assign broken", instances[0].GRPCPort)
	}
	for _, info := range instances {
		waitHTTPHealthy(t, info.Port, 5*time.Second)
	}
}

// TestMultiInstanceAutoPort_PS verifies that dpkms ps --output json lists
// both instances with distinct ports after they auto-assigned.
func TestMultiInstanceAutoPort_PS(t *testing.T) {
	bin := buildDpkmsBinary(t)
	dataDir := t.TempDir()
	env := append(os.Environ(), "XDG_DATA_HOME="+dataDir)

	cfg1 := writeServeConfig(t, dataDir, "1")
	cfg2 := writeServeConfig(t, dataDir, "2")

	p1 := startServeProcess(t, bin, cfg1, env)
	defer p1.Process.Kill()
	p2 := startServeProcess(t, bin, cfg2, env)
	defer p2.Process.Kill()

	runDir := filepath.Join(dataDir, "contexthelp", "run")
	waitForPidfiles(t, runDir, 2, 15*time.Second)

	psCmd := exec.Command(bin, "--config", cfg1, "ps", "--output", "json")
	psCmd.Env = env
	out, err := psCmd.Output()
	if err != nil {
		t.Fatalf("dpkms ps: %v", err)
	}

	var infos []pidfile.Info
	if err := json.Unmarshal(out, &infos); err != nil {
		t.Fatalf("parse ps json: %v\noutput: %s", err, out)
	}
	if len(infos) < 2 {
		t.Fatalf("ps listed %d instance(s), want >=2", len(infos))
	}
	seen := map[int]bool{}
	for _, info := range infos {
		if seen[info.Port] {
			t.Errorf("duplicate HTTP port %d in ps output", info.Port)
		}
		seen[info.Port] = true
	}
}

// writeServeConfig writes a minimal dpkms config with a unique db path.
func writeServeConfig(t *testing.T, dir, id string) string {
	t.Helper()
	dbPath := filepath.Join(dir, "db"+id+".sqlite")
	content := fmt.Sprintf("storage:\n  type: sqlite\n  path: %s\n", dbPath)
	path := filepath.Join(dir, "config"+id+".yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write config %s: %v", id, err)
	}
	return path
}

// startServeProcess launches a foreground (non-daemon) dpkms serve process.
func startServeProcess(t *testing.T, bin, configPath string, env []string) *exec.Cmd {
	t.Helper()
	c := exec.Command(bin, "--config", configPath, "serve")
	c.Env = env
	c.Stdout = os.Stderr
	c.Stderr = os.Stderr
	if err := c.Start(); err != nil {
		t.Fatalf("start serve process: %v", err)
	}
	return c
}

// waitForPidfiles polls pidfile.Scan until at least n live instances appear.
func waitForPidfiles(t *testing.T, runDir string, n int, timeout time.Duration) []pidfile.Info {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		infos, _ := pidfile.Scan(runDir)
		if len(infos) >= n {
			return infos
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("wanted %d running instances after %s; check stderr above for startup errors", n, timeout)
	return nil
}

// waitHTTPHealthy polls /health until HTTP 200 or timeout.
func waitHTTPHealthy(t *testing.T, port int, timeout time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	url := fmt.Sprintf("http://127.0.0.1:%d/health", port)
	for {
		select {
		case <-ctx.Done():
			t.Errorf("port %d /health never returned 200 within %s", port, timeout)
			return
		default:
		}
		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// buildDpkmsBinary compiles cmd/dpkms with CGO+fts5 and returns the binary path.
func buildDpkmsBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "dpkms")
	cmd := exec.Command("go", "build", "-buildvcs=false", "-tags", "fts5", "-o", bin, "./cmd/dpkms")
	cmd.Dir = findModuleRoot(t)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=1")
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("build dpkms binary: %v", err)
	}
	return bin
}

// findModuleRoot walks up from the test working directory to find go.mod.
func findModuleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}
