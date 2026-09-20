package testutil

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// projectRoot walks up from the current working directory until it finds go.mod.
func projectRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getwd: %w", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("could not find go.mod in any parent directory")
		}
		dir = parent
	}
}

// BinaryPath returns the absolute path to bin/{name} relative to the project root.
func BinaryPath(name string) string {
	root, err := projectRoot()
	if err != nil {
		panic(fmt.Sprintf("testutil.BinaryPath: %v", err))
	}
	return filepath.Join(root, "bin", name)
}

func buildCommand(root string) *exec.Cmd {
	if _, err := exec.LookPath("task"); err == nil {
		cmd := exec.Command("task", "build")
		cmd.Dir = root
		return cmd
	}
	if _, err := exec.LookPath("mise"); err == nil {
		cmd := exec.Command("mise", "exec", "--", "task", "build")
		cmd.Dir = root
		return cmd
	}
	return nil
}

// EnsureBuilt checks if both bin/ctxt and bin/dpkms exist. If either is
// missing, it runs "task build" from the project root. If the build fails
// the test is skipped.
func EnsureBuilt(t *testing.T) {
	t.Helper()

	root, err := projectRoot()
	if err != nil {
		t.Skipf("cannot find project root: %v", err)
		return
	}

	ctxtBin := filepath.Join(root, "bin", "ctxt")
	dpkmsBin := filepath.Join(root, "bin", "dpkms")

	_, errCtxt := os.Stat(ctxtBin)
	_, errDpkms := os.Stat(dpkmsBin)

	if errCtxt == nil && errDpkms == nil {
		return // both exist
	}

	cmd := buildCommand(root)
	if cmd == nil {
		t.Skip("build skipped: neither task nor mise is available")
		return
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Skipf("build failed: %v\n%s", err, string(out))
	}
}

// EnsureBuiltM is the same as EnsureBuilt but works with *testing.M for use
// in TestMain.
func EnsureBuiltM() error {
	root, err := projectRoot()
	if err != nil {
		return fmt.Errorf("cannot find project root: %w", err)
	}

	ctxtBin := filepath.Join(root, "bin", "ctxt")
	dpkmsBin := filepath.Join(root, "bin", "dpkms")

	_, errCtxt := os.Stat(ctxtBin)
	_, errDpkms := os.Stat(dpkmsBin)

	if errCtxt == nil && errDpkms == nil {
		return nil
	}

	cmd := buildCommand(root)
	if cmd == nil {
		return fmt.Errorf("build failed: neither task nor mise is available")
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("build failed: %w\n%s", err, string(out))
	}
	return nil
}

// Run executes the binary at BinaryPath(name) with the given args, captures
// combined stdout+stderr, and returns the output and any error.
func Run(t *testing.T, name string, args ...string) (string, error) {
	t.Helper()
	bin := BinaryPath(name)
	cmd := exec.Command(bin, args...) // #nosec G204 -- test helper runs built test binaries
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// StartServer starts "bin/dpkms serve" on a random port. It waits for
// /health to return 200 (retry up to 5s). Returns the base URL and a
// cleanup function that sends SIGTERM and waits for exit.
func StartServer(t *testing.T, extraArgs ...string) (url string, cleanup func()) {
	t.Helper()

	// Find a free port by binding then releasing.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	bin := BinaryPath("dpkms")
	args := []string{"serve", "--port", fmt.Sprintf("%d", port)}
	args = append(args, extraArgs...)

	cmd := exec.Command(bin, args...) // #nosec G204 -- test helper runs built test binaries
	// Use a temp dir for the database so tests are isolated.
	tmpDir := t.TempDir()
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("HOME=%s", tmpDir),
		fmt.Sprintf("XDG_CONFIG_HOME=%s", filepath.Join(tmpDir, ".config")),
		fmt.Sprintf("XDG_DATA_HOME=%s", filepath.Join(tmpDir, ".local", "share")),
	)
	cmd.Dir = tmpDir

	// Capture output for debugging.
	var outBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &outBuf

	if err := cmd.Start(); err != nil {
		t.Fatalf("start dpkms serve: %v", err)
	}

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	// Wait for health endpoint.
	deadline := time.Now().Add(5 * time.Second)
	healthy := false
	for time.Now().Before(deadline) {
		resp, err := http.Get(baseURL + "/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				healthy = true
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !healthy {
		// Kill the process since it never became healthy.
		cmd.Process.Kill()
		cmd.Wait()
		t.Fatalf("dpkms serve did not become healthy within 5s\nOutput:\n%s", outBuf.String())
	}

	cleanup = func() {
		cmd.Process.Signal(syscall.SIGTERM)
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			cmd.Process.Kill()
			<-done
		}
	}

	return baseURL, cleanup
}
