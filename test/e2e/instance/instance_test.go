//go:build unix

// Package instance_test drives a built ctxt against a built, protected
// `dpkms serve` through named server.urls entries: --instance,
// CTXT_INSTANCE and `ctxt instance use` select a URL plus its token, an
// unknown name exits 70, a rejected token exits 5, and nothing fails
// over to another entry. Skipped under -short.
package instance_test

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ideacrafterslabs/ctxt/internal/dpkmstest"
	"github.com/ideacrafterslabs/ctxt/internal/testguard"
	"github.com/ideacrafterslabs/ctxt/test/testutil"
)

func TestMain(m *testing.M) {
	os.Exit(testguard.Main(m, nil, "ctxt", "dpkms"))
}

var (
	ctxtOnce sync.Once
	ctxtPath string
	ctxtErr  error
)

// ctxtBinary builds ./cmd/ctxt from this checkout once per test binary,
// so a stale bin/ctxt never stands in for the code under test.
func ctxtBinary(t *testing.T) string {
	t.Helper()
	ctxtOnce.Do(func() {
		root, err := moduleRoot()
		if err != nil {
			ctxtErr = err
			return
		}
		dir, err := os.MkdirTemp(os.Getenv("XDG_CACHE_HOME"), "ctxt-e2e-")
		if err != nil {
			ctxtErr = err
			return
		}
		ctxtPath = filepath.Join(dir, "ctxt")
		build := exec.Command("go", "build", "-tags", "fts5", "-buildvcs=false", "-o", ctxtPath, "./cmd/ctxt") // #nosec G204 -- fixed args; fresh temp path
		build.Dir = root
		build.Env = append(os.Environ(), "CGO_ENABLED=1")
		if out, err := build.CombinedOutput(); err != nil {
			ctxtErr = fmt.Errorf("go build ./cmd/ctxt: %w\n%s", err, out)
		}
	})
	if ctxtErr != nil {
		t.Fatal(ctxtErr)
	}
	return ctxtPath
}

func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("go.mod not found")
		}
		dir = parent
	}
}

// ctxt runs the built binary in the instance's hermetic environment and
// returns its combined output and exit code.
func ctxt(t *testing.T, d *testutil.Dpkms, env []string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(ctxtBinary(t), args...) // #nosec G204 -- the ctxt built from this checkout
	cmd.Dir = d.Root
	cmd.Env = append(append([]string(nil), d.Env...), env...)
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return string(out), 0
	case errors.As(err, &exit):
		return string(out), exit.ExitCode()
	default:
		t.Fatalf("run ctxt %v: %v", args, err)
		return "", -1
	}
}

// writeCtxtConfig writes the ctxt user config in the instance's XDG tree.
func writeCtxtConfig(t *testing.T, d *testutil.Dpkms, body string) {
	t.Helper()
	path := filepath.Join(d.Root, "config", "contexthelp", "ctxt.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestNamedEndpointsAgainstProtectedDpkms(t *testing.T) {
	d := testutil.StartDpkms(t)
	admin := d.Token(dpkmstest.RoleAdmin)
	// decoy is first, so it is what an unselected command would use; it
	// points at a closed port.
	writeCtxtConfig(t, d, "server:\n  urls:\n"+
		"    - name: decoy\n      url: "+testguard.ClosedServerURL+"\n"+
		"    - name: home\n      url: "+d.URL+"\n      token: "+admin+"\n"+
		"    - name: stale\n      url: "+d.URL+"/\n      token: not-a-token\n")

	expect := func(t *testing.T, env []string, want int, args ...string) string {
		t.Helper()
		out, code := ctxt(t, d, env, args...)
		if code != want {
			t.Fatalf("ctxt %s: exit %d; want %d\n%s", strings.Join(args, " "), code, want, out)
		}
		if strings.Contains(out, admin) {
			t.Fatalf("ctxt %s printed the token:\n%s", strings.Join(args, " "), out)
		}
		return out
	}

	t.Run("--instance selects URL and token", func(t *testing.T) {
		expect(t, nil, 0, "status", "--instance", "home")
		// The audit log needs admin:audit: only home's token grants it.
		expect(t, nil, 0, "log", "--instance", "home")
	})
	t.Run("CTXT_INSTANCE selects URL and token", func(t *testing.T) {
		expect(t, []string{"CTXT_INSTANCE=home"}, 0, "log")
	})
	t.Run("a rejected token exits 5", func(t *testing.T) {
		expect(t, nil, 5, "log", "--instance", "stale")
	})
	t.Run("an unknown name exits 70 and lists the names", func(t *testing.T) {
		out := expect(t, nil, 70, "log", "--instance", "nope")
		if !strings.Contains(out, "decoy, home, stale") {
			t.Errorf("error does not list the configured names:\n%s", out)
		}
	})
	t.Run("no failover past an unreachable first entry", func(t *testing.T) {
		expect(t, nil, 70, "log")
	})
	t.Run("a running local instance resolves by name and port", func(t *testing.T) {
		// Its URL is home's, so it borrows home's token.
		expect(t, nil, 0, "log", "--instance", d.Instance.Name)
		expect(t, nil, 0, "log", "--instance", fmt.Sprint(d.Instance.Port))
	})
	t.Run("instance use persists the selection", func(t *testing.T) {
		expect(t, nil, 0, "instance", "use", "home")
		t.Cleanup(func() { expect(t, nil, 0, "instance", "use", "-") })
		expect(t, nil, 0, "log")
		out := expect(t, nil, 0, "instance", "current")
		for _, want := range []string{"home", d.URL, "current-instance", "token: yes"} {
			if !strings.Contains(out, want) {
				t.Errorf("instance current lacks %q:\n%s", want, out)
			}
		}
		list := expect(t, nil, 0, "instance", "list")
		for _, want := range []string{"decoy", "home", "stale", d.Instance.Name} {
			if !strings.Contains(list, want) {
				t.Errorf("instance list lacks %q:\n%s", want, list)
			}
		}
		expect(t, nil, 70, "instance", "use", "nope")
	})
}
