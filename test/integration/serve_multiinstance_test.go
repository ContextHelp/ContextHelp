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
// Also verifies that each instance has a unique, non-empty Name in its pidfile.
func TestMultiInstanceAutoPort(t *testing.T) {
	bin := buildDpkmsBinary(t)

	// XDG_DATA_HOME isolates pidfiles without overriding storage.path
	// (CTXT_DATA_DIR would do both since it binds to storage.path in viper).
	dataDir := t.TempDir()
	env := append(os.Environ(), "XDG_DATA_HOME="+dataDir, "BUS_TOKEN=test-bus-token")

	// Each instance gets its own db to avoid SQLite single-writer contention.
	cfg1 := writeServeConfig(t, dataDir, "alpha")
	cfg2 := writeServeConfig(t, dataDir, "beta")

	// Both instances request the same default HTTP (8080) and gRPC (9090) ports.
	// The second must auto-assign free alternatives. Wait for the first
	// instance to claim its ports before starting the second so findFreePort's
	// TOCTOU window is closed (otherwise both can probe 9090 as free).
	p1 := startServeProcessWithName(t, bin, cfg1, "alpha", env)
	defer p1.Process.Kill()

	runDir := filepath.Join(dataDir, "contexthelp", "run")
	waitForPidfiles(t, runDir, 1, 10*time.Second)

	p2 := startServeProcessWithName(t, bin, cfg2, "beta", env)
	defer p2.Process.Kill()

	instances := waitForPidfiles(t, runDir, 2, 15*time.Second)

	if instances[0].Port == instances[1].Port {
		t.Errorf("both instances share HTTP port %d — auto-assign broken", instances[0].Port)
	}
	if instances[0].GRPCPort == instances[1].GRPCPort {
		t.Errorf("both instances share gRPC port %d — auto-assign broken", instances[0].GRPCPort)
	}
	for _, info := range instances {
		waitHTTPHealthy(t, info.Port, 5*time.Second)
		if info.Name == "" {
			t.Errorf("instance on port %d has empty Name in pidfile", info.Port)
		}
	}
	// Names must be distinct.
	if instances[0].Name == instances[1].Name {
		t.Errorf("both instances have the same Name %q", instances[0].Name)
	}
}

// TestMultiInstanceNameConflict verifies that starting two instances with the
// same --name fails with a non-zero exit code.
func TestMultiInstanceNameConflict(t *testing.T) {
	bin := buildDpkmsBinary(t)
	dataDir := t.TempDir()
	env := append(os.Environ(), "XDG_DATA_HOME="+dataDir, "BUS_TOKEN=test-bus-token")

	cfg1 := writeServeConfig(t, dataDir, "conflict-a")
	cfg2 := writeServeConfig(t, dataDir, "conflict-b")

	p1 := startServeProcessWithName(t, bin, cfg1, "shared", env)
	defer p1.Process.Kill()

	// Wait for first instance to write its pidfile.
	runDir := filepath.Join(dataDir, "contexthelp", "run")
	waitForPidfiles(t, runDir, 1, 10*time.Second)

	// Second instance with the same name should exit non-zero.
	c := exec.Command(bin, "--config", cfg2, "serve", "--name", "shared")
	c.Env = env
	err := c.Run()
	if err == nil {
		t.Error("expected error starting second instance with duplicate name, got nil")
	}
}

// TestMultiInstanceAutoPort_PS verifies that dpkms ps --output json lists
// both instances with distinct ports and distinct names after auto-assignment.
func TestMultiInstanceAutoPort_PS(t *testing.T) {
	bin := buildDpkmsBinary(t)
	dataDir := t.TempDir()
	env := append(os.Environ(), "XDG_DATA_HOME="+dataDir, "BUS_TOKEN=test-bus-token")

	cfg1 := writeServeConfig(t, dataDir, "ps-alpha")
	cfg2 := writeServeConfig(t, dataDir, "ps-beta")

	p1 := startServeProcessWithName(t, bin, cfg1, "ps-alpha", env)
	defer p1.Process.Kill()

	runDir := filepath.Join(dataDir, "contexthelp", "run")
	waitForPidfiles(t, runDir, 1, 10*time.Second)

	p2 := startServeProcessWithName(t, bin, cfg2, "ps-beta", env)
	defer p2.Process.Kill()

	waitForPidfiles(t, runDir, 2, 15*time.Second)

	// --format, not --output: -o is kit's output-PATH flag, so
	// `--output json` asks for a file named "json" and leaves stdout
	// carrying the human table. The two used to be aliased by a
	// compatibility shim on the root; that shim is gone now that
	// --format is the single output-form flag.
	psCmd := exec.CommandContext(t.Context(), bin, "--config", cfg1, "ps", "--format", "json")
	psCmd.Env = env
	out, err := psCmd.Output()
	if err != nil {
		t.Fatalf("dpkms ps: %v", err)
	}

	// The listing travels inside a keyed envelope rather than as a
	// bare array, so it can grow a sibling field without changing its
	// type under every existing caller.
	var listing struct {
		Instances []pidfile.Info `json:"instances"`
	}
	if err := json.Unmarshal(out, &listing); err != nil {
		t.Fatalf("parse ps json: %v\noutput: %s", err, out)
	}
	infos := listing.Instances
	if len(infos) < 2 {
		t.Fatalf("ps listed %d instance(s), want >=2", len(infos))
	}
	seenPorts := map[int]bool{}
	seenNames := map[string]bool{}
	for _, info := range infos {
		if seenPorts[info.Port] {
			t.Errorf("duplicate HTTP port %d in ps output", info.Port)
		}
		seenPorts[info.Port] = true
		if info.Name == "" {
			t.Errorf("instance on port %d has empty Name in ps output", info.Port)
		}
		if seenNames[info.Name] {
			t.Errorf("duplicate Name %q in ps output", info.Name)
		}
		seenNames[info.Name] = true
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

// startServeProcessWithName launches dpkms serve with an explicit --name flag.
func startServeProcessWithName(t *testing.T, bin, configPath, name string, env []string) *exec.Cmd {
	t.Helper()
	c := exec.Command(bin, "--config", configPath, "serve", "--name", name)
	c.Env = env
	c.Stdout = os.Stderr
	c.Stderr = os.Stderr
	if err := c.Start(); err != nil {
		t.Fatalf("start serve process (name=%s): %v", name, err)
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
