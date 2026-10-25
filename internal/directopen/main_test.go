package directopen

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/dblock"
	"github.com/ideacrafterslabs/ctxt/internal/testguard"
)

// holderEnv, when set to a database path, turns the test binary into a
// lock-holding helper process instead of running tests.
const holderEnv = "DIRECTOPEN_TEST_HOLD_LOCK"

// TestMain isolates the package from the developer's machine (temp
// HOME/XDG, guarded dials); every test works on t.TempDir() files. The
// helper branch runs in a child process the tests start themselves.
func TestMain(m *testing.M) {
	if path := os.Getenv(holderEnv); path != "" {
		os.Exit(holdLock(path))
	}
	os.Exit(testguard.Main(m, nil))
}

// holdLock takes path's lock as a daemon, reports "ready", and holds it
// until stdin closes.
func holdLock(path string) int {
	l, err := dblock.Acquire(path, dblock.Info{Role: dblock.RoleDaemon, Instance: "helper"})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Println("ready")
	_, _ = io.Copy(io.Discard, os.Stdin)
	_ = l.Release()
	return 0
}

// startHolder runs a helper process holding dbPath's lock as a daemon
// and returns its pid; the process exits at test cleanup.
func startHolder(t *testing.T, dbPath string) int {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$") // #nosec G204 -- the test binary itself
	cmd.Env = append(os.Environ(), holderEnv+"="+dbPath)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = stdin.Close()
		_ = cmd.Wait()
	})
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || line != "ready\n" {
		t.Fatalf("lock helper did not get ready: %q, %v", line, err)
	}
	return cmd.Process.Pid
}
